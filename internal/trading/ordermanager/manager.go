package ordermanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/tracectx"

	"github.com/cenkalti/backoff/v4"
	"github.com/patrickmn/go-cache"
)

const (
	defaultCacheTTL        = 1 * time.Hour
	defaultCleanupInterval = 10 * time.Minute
)

// OrderCompletedCallback is invoked when an OrderCompletedEvent occurs.
type OrderCompletedCallback func(ctx context.Context, evt OrderCompletedEvent)

// OrderManager is a business-agnostic reactive order execution engine using Micro-Events.
type OrderManager struct {
	engine               *infraapp.Engine
	repo                 TradeRepository
	notifier             notifier.Notifier
	bus                  *eventbus.Bus
	log                  *slog.Logger
	timers               sync.Map
	aggregates           *cache.Cache
	orderIDMapCache      *cache.Cache
	onCompletedCallbacks []OrderCompletedCallback
	mu                   sync.RWMutex
}

// RegisterOnCompletedCallback registers an observer callback executed when an order completes.
func (m *OrderManager) RegisterOnCompletedCallback(fn OrderCompletedCallback) {
	if fn == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onCompletedCallbacks = append(m.onCompletedCallbacks, fn)
}

func (m *OrderManager) invokeOnCompletedCallbacks(ctx context.Context, evt OrderCompletedEvent) {
	m.mu.RLock()
	if len(m.onCompletedCallbacks) == 0 {
		m.mu.RUnlock()
		return
	}
	callbacks := make([]OrderCompletedCallback, len(m.onCompletedCallbacks))
	copy(callbacks, m.onCompletedCallbacks)
	m.mu.RUnlock()

	for _, fn := range callbacks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					m.log.ErrorContext(ctx, "OrderCompletedCallback panicked", slog.Any("panic", r), slog.String("req_id", evt.GetReqID()))
				}
			}()
			fn(ctx, evt)
		}()
	}
}

// NewOrderManager initializes a new OrderManager with engine runtime exchange resolution, eventbus, trade repository, and notifier.
func NewOrderManager(
	ctx context.Context,
	engine *infraapp.Engine,
	bus *eventbus.Bus,
	repo TradeRepository,
	n notifier.Notifier,
	log *slog.Logger,
) (*OrderManager, error) {
	if engine == nil {
		return nil, fmt.Errorf("engine is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("eventbus is required")
	}
	if repo == nil {
		return nil, fmt.Errorf("repository is required")
	}
	if n == nil {
		return nil, fmt.Errorf("notifier is required")
	}
	if log == nil {
		log = slog.Default()
	}

	m := &OrderManager{
		engine:          engine,
		bus:             bus,
		repo:            repo,
		notifier:        n,
		log:             log.With("component", "GenericOrderManager"),
		aggregates:      cache.New(defaultCacheTTL, defaultCleanupInterval),
		orderIDMapCache: cache.New(defaultCacheTTL, defaultCleanupInterval),
	}

	if err := m.Init(ctx); err != nil {
		return nil, fmt.Errorf("failed to init order manager: %w", err)
	}

	return m, nil
}

// Init registers all OrderManager micro-step topic handlers on the event bus and wires personal position stream callbacks.
func (m *OrderManager) Init(ctx context.Context) error {
	InitGlobalSubscriptions(ctx, m)

	for _, prov := range m.engine.Providers {
		if prov != nil {
			prov.WirePersonalWS(ctx, m.log)
		}
	}
	return nil
}

// Shutdown gracefully shuts down OrderManager resources upon application shutdown signal.
func (m *OrderManager) Shutdown(ctx context.Context) error {
	return nil
}

func (m *OrderManager) resolveClient(exchangeName string) (ExchangeClient, error) {
	prov, err := m.engine.GetProvider(exchangeName)
	if err != nil {
		resErr := fmt.Errorf("failed to get provider for exchange %q: %w", exchangeName, err)
		m.log.Error("Failed to resolve exchange client", "exchange", exchangeName, "error", resErr)
		return nil, resErr
	}
	if prov == nil || prov.Client == nil {
		resErr := fmt.Errorf("no exchange client available for exchange %q", exchangeName)
		m.log.Error("Failed to resolve exchange client", "exchange", exchangeName, "error", resErr)
		return nil, resErr
	}
	return prov.Client, nil
}

func (m *OrderManager) resolvePositionWatcher(exchangeName string) (PositionWatcher, error) {
	prov, err := m.engine.GetProvider(exchangeName)
	if err != nil {
		resErr := fmt.Errorf("failed to get provider for exchange %q: %w", exchangeName, err)
		m.log.Error("Failed to resolve position watcher", "exchange", exchangeName, "error", resErr)
		return nil, resErr
	}
	if prov == nil || prov.Watcher == nil {
		resErr := fmt.Errorf("no position watcher available for exchange %q", exchangeName)
		m.log.Debug("No position watcher available", "exchange", exchangeName)
		return nil, resErr
	}
	if pw, ok := any(prov.Watcher).(PositionWatcher); ok {
		return pw, nil
	}
	resErr := fmt.Errorf("watcher for exchange %q does not implement PositionWatcher interface", exchangeName)
	m.log.Debug("Position watcher interface not implemented", "exchange", exchangeName)
	return nil, resErr
}

func (m *OrderManager) resolveAdapter(exchangeName string) (infraws.ExchangeManagerAdapter, error) {
	prov, err := m.engine.GetProvider(exchangeName)
	if err != nil {
		resErr := fmt.Errorf("failed to get provider for exchange %q: %w", exchangeName, err)
		m.log.Debug("Failed to resolve exchange adapter", "exchange", exchangeName, "error", resErr)
		return nil, resErr
	}
	if prov == nil || prov.Adapter == nil {
		resErr := fmt.Errorf("no exchange adapter available for exchange %q", exchangeName)
		m.log.Debug("Failed to resolve exchange adapter", "exchange", exchangeName, "error", resErr)
		return nil, resErr
	}
	return prov.Adapter, nil
}

// SubscribePositionWatch subscribes to personal private WS channel via Adapter using reference counting.
func (m *OrderManager) SubscribePositionWatch(ctx context.Context, exchangeName, strategyType, reqID string) {
	prov, err := m.engine.GetProvider(exchangeName)
	if err != nil || prov == nil {
		return
	}
	prov.EnsurePersonalWS(ctx, m.log)

	flowID := fmt.Sprintf("%s_%s", strategyType, reqID)
	if prov.Adapter != nil {
		if err := prov.Adapter.SubscribePersonal(ctx, flowID); err != nil {
			m.log.WarnContext(ctx, "Failed to subscribe personal private WS channel via Adapter", slog.String("exchange", exchangeName), slog.Any("error", err))
		}
	}
}

// UnsubscribePositionWatch unsubscribes personal private WS channel via Adapter when order completes or aborts.
func (m *OrderManager) UnsubscribePositionWatch(ctx context.Context, exchangeName, strategyType, reqID string) {
	adapter, err := m.resolveAdapter(exchangeName)
	if err != nil {
		return
	}
	flowID := fmt.Sprintf("%s_%s", strategyType, reqID)
	if err := adapter.UnsubscribePersonal(ctx, flowID); err != nil {
		m.log.WarnContext(ctx, "Failed to unsubscribe personal private WS channel via Adapter", slog.String("exchange", exchangeName), slog.Any("error", err))
	}
}

func (m *OrderManager) resolveClock(exchangeName string) (Clock, error) {
	prov, err := m.engine.GetProvider(exchangeName)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider for exchange %q: %w", exchangeName, err)
	}
	if prov == nil {
		return nil, fmt.Errorf("no provider available for exchange %q", exchangeName)
	}
	if prov.TimeSync == nil {
		return nil, fmt.Errorf("no time sync clock available for exchange %q", exchangeName)
	}
	return prov.TimeSync, nil
}

// Dispatch publishes an OrderEvent to Watermill event bus for EDD reactive execution.
func (m *OrderManager) Dispatch(ctx context.Context, evt OrderEvent) error {
	if evt == nil {
		return fmt.Errorf("cannot dispatch nil event")
	}
	reqID := evt.GetReqID()
	agg := m.GetAggregate(reqID)
	if err := agg.Record(evt); err != nil {
		return fmt.Errorf("aggregate record failed: %w", err)
	}
	return m.publishEvent(ctx, evt.GetTopic(), evt)
}

func (m *OrderManager) publishEvent(ctx context.Context, topic string, payload any) error {
	if m.bus == nil {
		return nil
	}
	m.log.InfoContext(ctx, "OrderManager: Publishing Watermill micro-event", slog.String("topic", topic), slog.Any("payload", payload))
	return m.bus.Publish(topic, payload)
}

// GetAggregate retrieves or creates an OrderExecutionAggregate for a given reqID with 1h TTL.
func (m *OrderManager) GetAggregate(reqID string) *OrderExecutionAggregate {
	if val, found := m.aggregates.Get(reqID); found {
		if agg, ok := val.(*OrderExecutionAggregate); ok {
			return agg
		}
	}

	agg := NewOrderExecutionAggregate(reqID)
	m.aggregates.Set(reqID, agg, defaultCacheTTL)
	return agg
}

// SetExchangeOrderIDByReqID stores mapping from reqID to exchangeOrderID in cache (1h TTL).
func (m *OrderManager) SetExchangeOrderIDByReqID(reqID, exchangeOrderID string) {
	if reqID == "" || exchangeOrderID == "" || m.orderIDMapCache == nil {
		return
	}
	m.orderIDMapCache.Set(reqID, exchangeOrderID, defaultCacheTTL)
}

// GetExchangeOrderIDByReqID retrieves exchangeOrderID associated with reqID from cache.
func (m *OrderManager) GetExchangeOrderIDByReqID(reqID string) (string, bool) {
	if reqID == "" || m.orderIDMapCache == nil {
		return "", false
	}
	val, found := m.orderIDMapCache.Get(reqID)
	if !found {
		return "", false
	}
	exOID, ok := val.(string)
	return exOID, ok
}

// HasActiveOrder checks if an order with reqID is currently in active (non-terminal) state.
func (m *OrderManager) HasActiveOrder(reqID string) bool {
	if val, found := m.aggregates.Get(reqID); found {
		if agg, ok := val.(*OrderExecutionAggregate); ok {
			state := agg.State()
			return state != StateCompleted && state != StateAborted && state != StateCanceled
		}
	}
	return false
}

// GetActiveOrders returns all active (non-terminal) order aggregates for a given exchange and symbol.
func (m *OrderManager) GetActiveOrders(exchangeName, symbol string) []*OrderExecutionAggregate {
	var active []*OrderExecutionAggregate
	items := m.aggregates.Items()
	for _, item := range items {
		if agg, ok := item.Object.(*OrderExecutionAggregate); ok {
			if (exchangeName == "" || agg.Exchange() == exchangeName) &&
				(symbol == "" || agg.Symbol() == symbol) {
				state := agg.State()
				if state != StateCompleted && state != StateAborted && state != StateCanceled {
					active = append(active, agg)
				}
			}
		}
	}
	return active
}

// CancelOrder cancels an active order on the exchange and updates OrderManager state.
func (m *OrderManager) CancelOrder(ctx context.Context, reqID string) error {
	m.CancelTimeoutGuard(reqID)
	agg := m.GetAggregate(reqID)
	if agg == nil {
		return fmt.Errorf("order aggregate not found for reqID: %s", reqID)
	}

	exchangeName := agg.Exchange()
	symbol := agg.Symbol()
	orderID := agg.OrderID()
	clientOrderID := agg.ClientOrderID()

	prov, err := m.engine.GetProvider(exchangeName)
	if err != nil || prov == nil || prov.Client == nil {
		return fmt.Errorf("failed to resolve exchange provider for %s: %w", exchangeName, err)
	}

	if executor, ok := prov.Client.(exchange.OrderExecutor); ok {
		if orderID != "" {
			if err := executor.CancelOrder(ctx, symbol, orderID); err != nil {
				m.log.WarnContext(ctx, "Failed to cancel order by exchange order ID",
					slog.String("req_id", reqID), slog.String("order_id", orderID), slog.Any("error", err))
			}
		}
	}

	clock, err := m.resolveClock(exchangeName)
	if err != nil {
		return fmt.Errorf("resolve clock failed: %w", err)
	}
	now := clock.Now()

	cancelEvt := OrderCanceledEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         reqID,
			ClientOrderID: clientOrderID,
			Symbol:        symbol,
			Exchange:      exchangeName,
			MarketType:    agg.MarketType(),
			StrategyType:  agg.StrategyType(),
			PreTopic:      TopicOrderSubmitted,
			NextTopic:     TopicOrderCanceled,
			Timestamp:     now,
		},
		OrderID:    orderID,
		Reason:     "manual_cancel",
		CanceledAt: now,
	}

	if err := agg.Record(cancelEvt); err != nil {
		m.log.ErrorContext(ctx, "Failed to record OrderCanceledEvent to aggregate", slog.String("req_id", reqID), slog.Any("error", err))
	}

	return m.publishEvent(ctx, TopicOrderCanceled, cancelEvt)
}

// CancelOpenOrders cancels all open resting orders for a symbol on an exchange and cleans up active timers.
func (m *OrderManager) CancelOpenOrders(ctx context.Context, exchangeName, symbol string) error {
	prov, err := m.engine.GetProvider(exchangeName)
	if err != nil || prov == nil || prov.Client == nil {
		return fmt.Errorf("failed to resolve exchange provider for %s: %w", exchangeName, err)
	}

	if executor, ok := prov.Client.(exchange.OrderExecutor); ok {
		if err := executor.CancelAllOpenOrders(ctx, symbol); err != nil {
			m.log.WarnContext(ctx, "Failed to cancel all open orders on exchange",
				slog.String("exchange", exchangeName), slog.String("symbol", symbol), slog.Any("error", err))
		}
	}

	activeOrders := m.GetActiveOrders(exchangeName, symbol)
	for _, agg := range activeOrders {
		m.CancelTimeoutGuard(agg.ReqID())
	}

	return nil
}

func (m *OrderManager) configureExchangeLeverage(ctx context.Context, client ExchangeClient, evt OrderIntentEvent) (int, error) {
	adjustedLeverage := evt.Leverage
	if evt.Leverage > 0 && !client.SupportLeverageOnOrder() {
		posType := exchange.PositionTypeLong
		if !evt.Side.IsLong() {
			posType = exchange.PositionTypeShort
		}

		err := client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{
			Symbol:       evt.Symbol,
			Leverage:     adjustedLeverage,
			PositionType: posType,
		})
		if err != nil {
			m.log.ErrorContext(ctx, "Change leverage failed", slog.Any("error", err))
			return 0, fmt.Errorf("change leverage failed: %w", err)
		}
	}
	return adjustedLeverage, nil
}

// HandlePreFlight calculates Margin Mode, Position Mode & Risk Limit Leverage.
func (m *OrderManager) HandlePreFlight(ctx context.Context, evt OrderIntentEvent) (OrderPreFlightCompletedEvent, error) {
	m.log.InfoContext(ctx, "[Micro-Step 1] HandlePreFlight", slog.String("req_id", evt.GetReqID()), slog.String("symbol", evt.Symbol))

	client, err := m.resolveClient(evt.Exchange)
	if err != nil {
		return OrderPreFlightCompletedEvent{}, err
	}
	clock, err := m.resolveClock(evt.Exchange)
	if err != nil {
		return OrderPreFlightCompletedEvent{}, fmt.Errorf("pre-flight failed to resolve clock: %w", err)
	}

	if syncer, ok := clock.(SyncerClock); ok {
		m.log.InfoContext(ctx, "Forcing clock sync on pre-flight")
		syncer.SyncNow(ctx)
	}

	adjustedLeverage := evt.Leverage
	if !evt.Side.IsClose() && !evt.SkipPreFlight {
		// Switch Margin Mode
		if err := client.SwitchMarginMode(ctx, evt.Symbol, evt.MarginMode, evt.Leverage, evt.Side); err != nil {
			m.log.ErrorContext(ctx, "Switch margin mode failed", slog.Any("error", err))
			return OrderPreFlightCompletedEvent{}, fmt.Errorf("switch margin mode failed: %w", err)
		}

		// Switch Position Mode if provider exists
		if switcher, ok := client.(PositionModeSwitcher); ok && evt.PositionMode > 0 {
			if err := switcher.SwitchPositionMode(ctx, evt.Symbol, evt.PositionMode); err != nil {
				m.log.ErrorContext(ctx, "Switch position mode failed", slog.Any("error", err))
				return OrderPreFlightCompletedEvent{}, fmt.Errorf("switch position mode failed: %w", err)
			}
		}

		var err error
		adjustedLeverage, err = m.configureExchangeLeverage(ctx, client, evt)
		if err != nil {
			return OrderPreFlightCompletedEvent{}, err
		}
	}

	evt.PreTopic = TopicOrderIntent
	evt.NextTopic = TopicOrderPreFlightDone

	return OrderPreFlightCompletedEvent{
		OrderIntentEvent: evt,
		AdjustedLeverage: adjustedLeverage,
		PreFlightDoneAt:  clock.Now(),
	}, nil
}

// HandleFireTiming calculates latency offset & precision sleep window.
func (m *OrderManager) HandleFireTiming(ctx context.Context, evt OrderPreFlightCompletedEvent) (OrderFireWindowReachedEvent, error) {
	m.log.InfoContext(ctx, "[Micro-Step 2] HandleFireTiming", slog.String("req_id", evt.GetReqID()), slog.String("symbol", evt.Symbol))

	clock, err := m.resolveClock(evt.Exchange)
	if err != nil {
		return OrderFireWindowReachedEvent{}, fmt.Errorf("fire timing failed to resolve clock: %w", err)
	}

	fireTime := evt.FireTime
	if !fireTime.IsZero() {
		if clock.Until(fireTime) > 0 {
			m.log.InfoContext(ctx, "Sleeping for fire window target", slog.Time("target", fireTime))
			if err := clock.Sleep(ctx, clock.Until(fireTime)); err != nil {
				return OrderFireWindowReachedEvent{}, fmt.Errorf("fire timing sleep failed: %w", err)
			}
		}
	}

	evt.PreTopic = TopicOrderPreFlightDone
	evt.NextTopic = TopicOrderFireWindowReached

	return OrderFireWindowReachedEvent{
		OrderPreFlightCompletedEvent: evt,
		FireWindowReachedAt:          clock.Now(),
	}, nil
}

// HandlePositionWatchReady subscribes to real-time personal position stream updates BEFORE order execution.
func (m *OrderManager) HandlePositionWatchReady(ctx context.Context, evt OrderFireWindowReachedEvent) (OrderPositionWatchReadyEvent, error) {
	timeout := 30 * time.Minute
	if evt.PositionCloseTimeout > 0 && evt.PositionCloseTimeout*2 > timeout {
		timeout = evt.PositionCloseTimeout * 2
	}
	m.log.InfoContext(ctx, "[Micro-Step 3] HandlePositionWatchReady",
		slog.String("req_id", evt.GetReqID()),
		slog.String("symbol", evt.Symbol),
		slog.String("strategy", string(evt.StrategyType)),
		slog.String("exchange", evt.Exchange),
		slog.Duration("timeout", timeout))

	clock, err := m.resolveClock(evt.Exchange)
	if err != nil {
		return OrderPositionWatchReadyEvent{}, fmt.Errorf("position watch failed to resolve clock: %w", err)
	}

	if !evt.Side.IsClose() {
		m.SubscribePositionWatch(ctx, evt.Exchange, string(evt.GetStrategyType()), evt.GetReqID())

		posWatcher, _ := m.resolvePositionWatcher(evt.Exchange)
		if posWatcher != nil {
			posWatcher.OnPositionUpdate(ctx, evt.Symbol, timeout, func(pos exchange.PersonalPositionUpdate) {
				m.log.Debug("[Micro-Step 3] HandlePositionUpdate OnPositionUpdate", slog.Any("pos", pos))
				m.HandlePositionUpdate(ctx, evt.GetReqID(), pos)
			})
		}
	}

	evt.PreTopic = TopicOrderFireWindowReached
	evt.NextTopic = TopicOrderPositionWatchReady

	return OrderPositionWatchReadyEvent{
		OrderFireWindowReachedEvent: evt,
		Timeout:                     timeout,
		WatchReadyAt:                clock.Now(),
	}, nil
}

// HandlePositionUpdate processes real-time position updates and dispatches filled/closed position events.
func (m *OrderManager) HandlePositionUpdate(ctx context.Context, reqID string, pos exchange.PersonalPositionUpdate) {
	agg := m.GetAggregate(reqID)
	if agg == nil || agg.State() == StateCompleted || agg.State() == StateAborted || agg.State() == StateCanceled {
		return
	}
	clock, err := m.resolveClock(agg.Exchange())
	if err != nil {
		m.log.ErrorContext(ctx, "Failed to resolve clock for position update", slog.String("req_id", reqID), slog.Any("error", err))
		return
	}

	contractSize := agg.ContractSize()
	holdVolContract, holdVolCoin := normalizeVolume(pos.HoldVolContract, pos.HoldVolCoin, contractSize)

	ctx = tracectx.WithRequestIDValue(ctx, reqID)
	if holdVolContract > 0 || holdVolCoin > 0 {
		m.handlePositionFilled(ctx, reqID, agg, clock, pos, contractSize, holdVolContract, holdVolCoin)
	} else {
		m.handlePositionClosed(ctx, reqID, agg, clock, pos, contractSize)
	}
}

func (m *OrderManager) handlePositionFilled(
	ctx context.Context,
	reqID string,
	agg *OrderExecutionAggregate,
	clk Clock,
	pos exchange.PersonalPositionUpdate,
	contractSize, holdVolContract, holdVolCoin float64,
) {
	if agg.State() == StateCompleted || agg.State() == StateAborted || agg.State() == StateCanceled {
		return
	}
	fillPrice := pos.OpenAvgPrice
	if fillPrice == 0 {
		fillPrice = pos.HoldAvgPrice
	}
	volumeUSDT := calculateVolumeUSDT(holdVolCoin, holdVolContract, fillPrice, contractSize)
	evt := OrderFilledEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         reqID,
			ClientOrderID: agg.ClientOrderID(),
			Symbol:        pos.Symbol,
			Exchange:      agg.Exchange(),
			MarketType:    agg.MarketType(),
			StrategyType:  agg.StrategyType(),
			PreTopic:      TopicOrderPositionWatchReady,
			NextTopic:     TopicOrderFilled,
			Timestamp:     clk.Now(),
		},
		Side:            agg.Side(),
		FillPrice:       fillPrice,
		FillVolContract: holdVolContract,
		FillVolCoin:     holdVolCoin,
		VolumeUSDT:      volumeUSDT,
		FilledAt:        clk.Now(),
	}
	_ = agg.Record(evt)
	_ = m.publishEvent(ctx, TopicOrderFilled, evt)
}

func (m *OrderManager) handlePositionClosed(
	ctx context.Context,
	reqID string,
	agg *OrderExecutionAggregate,
	clk Clock,
	pos exchange.PersonalPositionUpdate,
	contractSize float64,
) {
	if agg.State() == StateCompleted || agg.State() == StateAborted || agg.State() == StateCanceled {
		return
	}
	closePrice := pos.CloseAvgPrice
	if closePrice == 0 {
		closePrice = pos.HoldAvgPrice
	}
	closeVolContract, closeVolCoin := normalizeVolume(pos.CloseVolContract, pos.CloseVolCoin, contractSize)
	if closeVolContract == 0 && closeVolCoin == 0 {
		closeVolContract, closeVolCoin = normalizeVolume(agg.FillVolContract(), agg.FillVolCoin(), contractSize)
	}

	if shouldIgnoreZeroVolumeUpdate(agg, pos, closePrice, closeVolContract, closeVolCoin) {
		m.log.DebugContext(ctx, "Ignoring 0 volume position update for unopened position aggregate",
			slog.String("req_id", reqID),
			slog.String("symbol", pos.Symbol),
		)
		return
	}

	volUSDT := calculateVolumeUSDT(closeVolCoin, closeVolContract, closePrice, contractSize)
	evt := OrderPositionClosedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         reqID,
			ClientOrderID: agg.ClientOrderID(),
			Symbol:        pos.Symbol,
			Exchange:      agg.Exchange(),
			MarketType:    agg.MarketType(),
			StrategyType:  agg.StrategyType(),
			PreTopic:      TopicOrderFilled,
			NextTopic:     TopicOrderPositionClosed,
			Timestamp:     clk.Now(),
		},
		EntryPrice:       pos.OpenAvgPrice,
		ClosePrice:       closePrice,
		CloseVolContract: closeVolContract,
		CloseVolCoin:     closeVolCoin,
		VolumeUSDT:       volUSDT,
		Reason:           "exchange_push",
		GrossProfit:      pos.CloseProfitLoss,
		NetProfit:        decmath.Add(decmath.Sub(pos.CloseProfitLoss, pos.Fee), pos.HoldFee),
		Fee:              pos.Fee,
		FundingFee:       pos.HoldFee,
	}
	if err := agg.Record(evt); err != nil {
		m.log.ErrorContext(ctx, "Failed to record OrderPositionClosedEvent to aggregate", slog.String("req_id", reqID), slog.Any("error", err))
	}
	_ = m.publishEvent(ctx, TopicOrderPositionClosed, evt)
}

func shouldIgnoreZeroVolumeUpdate(
	agg *OrderExecutionAggregate,
	pos exchange.PersonalPositionUpdate,
	closePrice, closeVolContract, closeVolCoin float64,
) bool {
	if agg.Side().IsClose() {
		return false
	}
	return !agg.HasFilled() &&
		pos.CloseProfitLoss == 0 &&
		pos.OpenAvgPrice == 0 &&
		closePrice == 0 &&
		closeVolContract == 0 &&
		closeVolCoin == 0
}

func calculateVolumeUSDT(volCoin, volContract, price, contractSize float64) float64 {
	volUSDT := decmath.Mul(volCoin, price)
	if volUSDT == 0 && price > 0 && volContract > 0 {
		cs := contractSize
		if cs <= 0 {
			cs = 1.0
		}
		volUSDT = decmath.Mul(decmath.Mul(volContract, price), cs)
	}
	return volUSDT
}

// HandleExecuteOrder executes order submission REST API.
func (m *OrderManager) HandleExecuteOrder(ctx context.Context, evt OrderPositionWatchReadyEvent) (OrderSubmittedEvent, error) {
	m.log.InfoContext(ctx, "[Micro-Step 4] HandleExecuteOrder", slog.String("req_id", evt.GetReqID()), slog.String("symbol", evt.Symbol))

	client, err := m.resolveClient(evt.Exchange)
	if err != nil {
		return OrderSubmittedEvent{}, err
	}
	clock, err := m.resolveClock(evt.Exchange)
	if err != nil {
		return OrderSubmittedEvent{}, fmt.Errorf("execute order failed to resolve clock: %w", err)
	}

	isReduceOnly := evt.Side == shared.SideCloseLong || evt.Side == shared.SideCloseShort

	clientOID := evt.GetClientOrderID()
	req := exchange.SubmitOrderRequest{
		Symbol:          evt.Symbol,
		Price:           evt.Price,
		Vol:             evt.Volume,
		Leverage:        evt.AdjustedLeverage,
		Side:            evt.Side,
		Type:            mapOrderType(evt.OrderType),
		OpenType:        mapOpenType(evt.MarginMode),
		PositionMode:    evt.PositionMode,
		ExternalOID:     clientOID,
		ReduceOnly:      isReduceOnly,
		TakeProfitPrice: evt.TakeProfitPrice,
		StopLossPrice:   evt.StopLossPrice,
	}

	resp, err := client.CreateOrder(ctx, req)
	if err != nil {
		m.log.ErrorContext(ctx, "Create order failed", slog.Any("error", err))
		return OrderSubmittedEvent{}, fmt.Errorf("create order failed: %w", err)
	}

	if resp.OrderID != "" && evt.GetReqID() != "" {
		m.SetExchangeOrderIDByReqID(evt.GetReqID(), resp.OrderID)
	}

	evt.PreTopic = TopicOrderPositionWatchReady
	evt.NextTopic = TopicOrderSubmitted

	return OrderSubmittedEvent{
		OrderPositionWatchReadyEvent: evt,
		OrderID:                      resp.OrderID,
		Price:                        evt.Price,
		Volume:                       evt.Volume,
		TPSLSubmitted:                resp.TPSLSubmitted,
		SubmittedAt:                  clock.Now(),
	}, nil
}

// HandleTPSLContingency places background TP/SL trigger if not supported inline.
func (m *OrderManager) HandleTPSLContingency(ctx context.Context, evt OrderSubmittedEvent, intent OrderIntentEvent) (*OrderTPSLDispatchedEvent, error) {
	if evt.TPSLSubmitted || (intent.TakeProfitPrice == 0 && intent.StopLossPrice == 0) {
		return nil, nil
	}

	client, err := m.resolveClient(evt.Exchange)
	if err != nil {
		return nil, err
	}
	clock, err := m.resolveClock(evt.Exchange)
	if err != nil {
		return nil, fmt.Errorf("place TPSL failed to resolve clock: %w", err)
	}

	provider, ok := client.(TPSLProvider)
	if !ok {
		m.log.WarnContext(ctx, "Exchange does not support standalone PlaceTPSL")
		return nil, nil
	}

	m.log.InfoContext(ctx, "[Micro-Step 5A] HandleTPSLContingency", slog.String("symbol", evt.Symbol), slog.Float64("tp", intent.TakeProfitPrice), slog.Float64("sl", intent.StopLossPrice))

	req := exchange.TPSLRequest{
		Symbol:          evt.Symbol,
		PositionMode:    intent.PositionMode,
		Side:            intent.Side,
		TakeProfitPrice: intent.TakeProfitPrice,
		StopLossPrice:   intent.StopLossPrice,
		Volume:          intent.Volume,
	}

	if err := provider.PlaceTPSL(ctx, req); err != nil {
		m.log.ErrorContext(ctx, "PlaceTPSL contingency failed", slog.Any("error", err))
		return nil, fmt.Errorf("place TPSL failed: %w", err)
	}

	return &OrderTPSLDispatchedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         evt.GetReqID(),
			ClientOrderID: evt.GetClientOrderID(),
			Symbol:        evt.Symbol,
			Exchange:      evt.Exchange,
			MarketType:    evt.GetMarketType(),
			StrategyType:  evt.StrategyType,
			PreTopic:      TopicOrderSubmitted,
			NextTopic:     TopicOrderTPSLDispatched,
			Timestamp:     clock.Now(),
		},
		TakeProfitPrice: intent.TakeProfitPrice,
		StopLossPrice:   intent.StopLossPrice,
		DispatchedAt:    clock.Now(),
	}, nil
}

// HandleTPSLSubmission places background TP/SL trigger if configured and not supported inline.
func (m *OrderManager) HandleTPSLSubmission(ctx context.Context, evt OrderSubmittedEvent) (*OrderTPSLDispatchedEvent, error) {
	return m.HandleTPSLContingency(ctx, evt, evt.OrderIntentEvent)
}

// HandleScheduleUnfilledCancelTimeout schedules pre-fill resting cancel timeout (for maker orders) or immediate position close timeout (for taker orders).
func (m *OrderManager) HandleScheduleUnfilledCancelTimeout(ctx context.Context, evt OrderSubmittedEvent) error {
	// 1. For Maker / PostOnly / Limit orders: schedule pre-fill unfilled cancel timeout if configured.
	if evt.OrderType.IsMaker() {
		unfilledTimeout := evt.UnfilledCancelTimeout
		if unfilledTimeout <= 0 {
			m.log.InfoContext(ctx, "Skipping pre-fill timeout schedule for resting maker order (no UnfilledCancelTimeout set)",
				slog.String("req_id", evt.GetReqID()))
			return nil
		}

		m.log.InfoContext(ctx, "Starting pre-fill resting order timeout watchdog",
			slog.String("req_id", evt.GetReqID()), slog.Duration("unfilled_cancel_timeout", unfilledTimeout))
		return m.scheduleUnfilledCancelTimeoutInternal(ctx, evt.GetReqID(), evt.OrderID, evt.Symbol, evt.Exchange, evt.StrategyType, evt.GetMarketType(), unfilledTimeout)
	}

	// 2. For Taker / IOC / Market orders: schedule immediate post-submit position close timeout.
	dur := evt.PositionCloseTimeout
	if dur <= 0 {
		return nil
	}

	m.log.InfoContext(ctx, "[Micro-Step 5C] HandleScheduleUnfilledCancelTimeout", slog.String("req_id", evt.GetReqID()), slog.Duration("duration", dur))
	return m.schedulePositionCloseTimeoutInternal(ctx, evt.GetReqID(), evt.GetClientOrderID(), evt.Symbol, evt.Exchange, evt.StrategyType, evt.GetMarketType(), dur, TopicOrderSubmitted)
}

func (m *OrderManager) scheduleUnfilledCancelTimeoutInternal(ctx context.Context, reqID, orderID, symbol, exchangeName string, st StrategyType, mt MarketType, dur time.Duration) error {
	clock, err := m.resolveClock(exchangeName)
	if err != nil {
		return fmt.Errorf("schedule unfilled cancel timeout failed to resolve clock: %w", err)
	}

	timer := time.AfterFunc(dur, func() {
		m.timers.Delete(reqID)
		agg := m.GetAggregate(reqID)
		if agg == nil {
			return
		}

		state := agg.State()
		// Only auto-cancel if order is still resting/submitted (not filled, completed, or aborted)
		if state == StateSubmitted || state == StateResting || state == StateInit || state == StatePreFlightDone || state == StateFireWindow || state == StatePositionWatchReady {
			m.log.WarnContext(ctx, "⏱️ Resting order timeout expired without fill; auto-canceling on exchange",
				slog.String("req_id", reqID),
				slog.String("order_id", orderID),
				slog.String("symbol", symbol),
				slog.Duration("duration", dur),
			)

			prov, err := m.engine.GetProvider(exchangeName)
			if err == nil && prov != nil && prov.Client != nil && orderID != "" {
				if executor, ok := prov.Client.(exchange.OrderExecutor); ok {
					_ = executor.CancelOrder(ctx, symbol, orderID)
				}
			}

			resolvedEvt := OrderOutcomeResolvedEvent{
				BaseExecutionEvent: BaseExecutionEvent{
					ReqID:        reqID,
					Symbol:       symbol,
					Exchange:     exchangeName,
					MarketType:   mt,
					StrategyType: st,
					PreTopic:     TopicOrderSubmitted,
					NextTopic:    TopicOrderOutcomeResolved,
					Timestamp:    clock.Now(),
				},
				Outcome:   OutcomeCanceledNoFill,
				Reason:    "resting_timeout_expired",
				FilledVol: 0,
			}
			if err := agg.Record(resolvedEvt); err != nil {
				m.log.ErrorContext(ctx, "Failed to record resting timeout outcome to aggregate", slog.String("req_id", reqID), slog.Any("error", err))
			}
			_ = m.publishEvent(ctx, TopicOrderOutcomeResolved, resolvedEvt)
		}
	})

	m.timers.Store(reqID, timer)
	return nil
}

// HandleSchedulePositionCloseTimeout schedules hold timeout watchdog timer upon position fill.
func (m *OrderManager) HandleSchedulePositionCloseTimeout(ctx context.Context, evt OrderFilledEvent) error {
	agg := m.GetAggregate(evt.GetReqID())
	dur := agg.PositionCloseTimeout()
	if dur <= 0 {
		return nil
	}

	// If timer already active for this reqID, don't restart
	if _, loaded := m.timers.Load(evt.GetReqID()); loaded {
		return nil
	}

	m.log.InfoContext(ctx, "Starting post-fill hold timeout watchdog", slog.String("req_id", evt.GetReqID()), slog.Duration("duration", dur))
	return m.schedulePositionCloseTimeoutInternal(ctx, evt.GetReqID(), evt.GetClientOrderID(), evt.Symbol, evt.Exchange, evt.StrategyType, evt.GetMarketType(), dur, TopicOrderFilled)
}

func (m *OrderManager) schedulePositionCloseTimeoutInternal(ctx context.Context, reqID, clientOrderID, symbol, exchangeName string, st StrategyType, mt MarketType, dur time.Duration, preTopic string) error {
	clock, err := m.resolveClock(exchangeName)
	if err != nil {
		return fmt.Errorf("schedule position close timeout failed to resolve clock: %w", err)
	}
	agg := m.GetAggregate(reqID)

	timer := time.AfterFunc(dur, func() {
		m.timers.Delete(reqID)
		timeoutEvt := OrderTimeoutScheduledEvent{
			BaseExecutionEvent: BaseExecutionEvent{
				ReqID:         reqID,
				ClientOrderID: clientOrderID,
				Symbol:        symbol,
				Exchange:      exchangeName,
				MarketType:    mt,
				StrategyType:  st,
				PreTopic:      preTopic,
				NextTopic:     TopicOrderTimeoutScheduled,
				Timestamp:     clock.Now(),
			},
			Duration:    dur,
			ScheduledAt: clock.Now(),
		}
		_ = agg.Record(timeoutEvt)
		_ = m.publishEvent(context.WithoutCancel(ctx), TopicOrderTimeoutScheduled, timeoutEvt)
	})
	m.timers.Store(reqID, timer)
	return nil
}

// ScheduleTimeoutTimer starts a direct background timeout timer with a custom callback.
func (m *OrderManager) ScheduleTimeoutTimer(reqID, symbol string, dur time.Duration, onTimeout func()) (OrderTimeoutScheduledEvent, error) {
	m.log.Info("[Micro-Step] ScheduleTimeoutTimer", slog.String("req_id", reqID), slog.Duration("duration", dur))

	agg := m.GetAggregate(reqID)
	clock, err := m.resolveClock(agg.Exchange())
	if err != nil {
		return OrderTimeoutScheduledEvent{}, fmt.Errorf("schedule timeout timer failed to resolve clock: %w", err)
	}

	timer := time.AfterFunc(dur, func() {
		m.timers.Delete(reqID)
		if onTimeout != nil {
			onTimeout()
		}
	})
	m.timers.Store(reqID, timer)

	return OrderTimeoutScheduledEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         reqID,
			ClientOrderID: agg.ClientOrderID(),
			Symbol:        symbol,
			Exchange:      agg.Exchange(),
			MarketType:    agg.MarketType(),
			StrategyType:  agg.StrategyType(),
			PreTopic:      TopicOrderSubmitted,
			NextTopic:     TopicOrderTimeoutScheduled,
			Timestamp:     clock.Now(),
		},
		Duration:    dur,
		ScheduledAt: clock.Now(),
	}, nil
}

// CancelTimeoutGuard cancels an active timeout guard timer.
func (m *OrderManager) CancelTimeoutGuard(reqID string) bool {
	if val, ok := m.timers.LoadAndDelete(reqID); ok {
		if timer, isTimer := val.(*time.Timer); isTimer {
			return timer.Stop()
		}
	}
	return false
}

// HandleWaitTimeoutDeadline waits for the timeout deadline and checks open positions.
func (m *OrderManager) HandleWaitTimeoutDeadline(ctx context.Context, evt OrderTimeoutScheduledEvent) (OrderTimeoutPositionCheckedEvent, error) {
	m.log.InfoContext(ctx, "Timeout guard started", slog.String("req_id", evt.GetReqID()), slog.Duration("timeout", evt.Duration))

	clock, err := m.resolveClock(evt.Exchange)
	if err != nil {
		return OrderTimeoutPositionCheckedEvent{}, fmt.Errorf("wait timeout deadline failed to resolve clock: %w", err)
	}

	var positions []exchange.Position
	errText := ""
	client, err := m.resolveClient(evt.Exchange)
	if err != nil {
		errText = err.Error()
	} else {
		pos, errQuery := client.GetOpenPositions(ctx, evt.Symbol)
		if errQuery != nil {
			errText = errQuery.Error()
			m.log.ErrorContext(ctx, "Timeout guard failed to query position", slog.String("symbol", evt.Symbol), slog.Any("error", errQuery))
		} else {
			positions = pos
		}
	}

	holdVol := 0.0
	for _, p := range positions {
		if p.HoldVolContract > 0 {
			holdVol += p.HoldVolContract
		} else if p.HoldVolCoin > 0 {
			holdVol += p.HoldVolCoin
		}
	}

	return OrderTimeoutPositionCheckedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         evt.GetReqID(),
			ClientOrderID: evt.GetClientOrderID(),
			Symbol:        evt.Symbol,
			Exchange:      evt.Exchange,
			MarketType:    evt.GetMarketType(),
			StrategyType:  evt.StrategyType,
			PreTopic:      TopicOrderTimeoutScheduled,
			NextTopic:     TopicOrderTimeoutPositionChecked,
			Timestamp:     clock.Now(),
		},
		Timeout:   evt.Duration,
		HoldVol:   holdVol,
		Error:     errText,
		CheckedAt: clock.Now(),
	}, nil
}

// HandleOutcomeWatcher performs exponential backoff polling & WS stream watching for fill outcome classification.
func (m *OrderManager) HandleOutcomeWatcher(ctx context.Context, evt OrderSubmittedEvent) (OrderOutcomeResolvedEvent, error) {
	clock, err := m.resolveClock(evt.Exchange)
	if err != nil {
		return OrderOutcomeResolvedEvent{}, fmt.Errorf("outcome watcher failed to resolve clock: %w", err)
	}
	// wait for order sync status fill or cancel from exchange
	if err := clock.Sleep(ctx, time.Second*2); err != nil {
		return OrderOutcomeResolvedEvent{}, err
	}

	exchangeOrderID, _ := m.GetExchangeOrderIDByReqID(evt.GetReqID())
	m.log.InfoContext(ctx, "[Micro-Step 6] HandleOutcomeWatcher", slog.String("req_id", evt.GetReqID()), slog.String("client_order_id", evt.GetClientOrderID()), slog.String("exchange_order_id", exchangeOrderID))

	order, err := m.pollOrderUntilTerminal(ctx, evt.Exchange, evt.Symbol, exchangeOrderID)

	outcome, filledVol, avgPrice := classifyOrderOutcome(order)
	reason := ""
	if err != nil {
		reason = err.Error()
	}

	if (evt.OrderType == OrderTypePostOnly || evt.OrderType == OrderTypeLimit) && (outcome == OutcomeUnknown || outcome == OutcomeResting) {
		if order != nil && order.State == exchange.OrderStateNew {
			outcome = OutcomeResting
			reason = "order_resting_on_book"
		}
	}

	return OrderOutcomeResolvedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         evt.GetReqID(),
			ClientOrderID: evt.GetClientOrderID(),
			Symbol:        evt.Symbol,
			Exchange:      evt.Exchange,
			MarketType:    evt.GetMarketType(),
			StrategyType:  evt.StrategyType,
			PreTopic:      TopicOrderSubmitted,
			NextTopic:     TopicOrderOutcomeResolved,
			Timestamp:     clock.Now(),
		},
		Outcome:    outcome,
		FilledVol:  filledVol,
		AvgPrice:   avgPrice,
		Reason:     reason,
		ResolvedAt: clock.Now(),
	}, nil
}

func (m *OrderManager) pollOrderUntilTerminal(ctx context.Context, exchangeName, symbol, exchangeOrderID string) (*exchange.OrderInfo, error) {
	client, err := m.resolveClient(exchangeName)
	if err != nil {
		return nil, err
	}
	var order *exchange.OrderInfo
	bo := backoff.WithContext(
		backoff.WithMaxRetries(
			backoff.NewExponentialBackOff(
				backoff.WithInitialInterval(time.Second),
				backoff.WithMaxInterval(2*time.Second),
			),
			5,
		),
		ctx,
	)

	err = backoff.Retry(func() error {
		got, err := client.GetOrder(ctx, symbol, exchangeOrderID)
		if err != nil {
			return err
		}
		if got == nil {
			return errors.New("order not found")
		}
		order = got
		if !exchange.IsTerminalOrderState(got.State) {
			return errors.New("order state not terminal yet")
		}
		return nil
	}, bo)

	return order, err
}

func classifyOrderOutcome(order *exchange.OrderInfo) (OrderOutcome, float64, float64) {
	if order == nil {
		return OutcomeUnknown, 0.0, 0.0
	}
	switch order.State {
	case exchange.OrderStateFilled:
		return OutcomeFilled, order.DealVol, order.DealAvgPrice
	case exchange.OrderStatePartial, exchange.OrderStatePartiallyFilled:
		return OutcomePartialFilled, order.DealVol, order.DealAvgPrice
	case exchange.OrderStateCanceled:
		return OutcomeCanceledNoFill, order.DealVol, order.DealAvgPrice
	case exchange.OrderStateNew:
		return OutcomeResting, order.DealVol, order.DealAvgPrice
	default:
		return OutcomeUnknown, order.DealVol, order.DealAvgPrice
	}
}

// HandleTimeoutCheck checks open position HoldVol when timeout expires.
func (m *OrderManager) HandleTimeoutCheck(ctx context.Context, evt OrderTimeoutScheduledEvent) (*OrderTimeoutExpiredEvent, error) {
	m.log.InfoContext(ctx, "[Micro-Step 7] HandleTimeoutCheck", slog.String("req_id", evt.GetReqID()), slog.String("symbol", evt.Symbol))

	client, err := m.resolveClient(evt.Exchange)
	if err != nil {
		return nil, fmt.Errorf("query position on timeout failed: %w", err)
	}
	clock, err := m.resolveClock(evt.Exchange)
	if err != nil {
		return nil, fmt.Errorf("query position on timeout failed to resolve clock: %w", err)
	}

	positions, err := client.GetOpenPositions(ctx, evt.Symbol)
	if err != nil {
		m.log.ErrorContext(ctx, "GetOpenPositions on timeout failed", slog.Any("error", err))
		return nil, fmt.Errorf("query position on timeout failed: %w", err)
	}

	holdVol := 0.0
	for _, p := range positions {
		if p.HoldVolContract > 0 {
			holdVol += p.HoldVolContract
		} else if p.HoldVolCoin > 0 {
			holdVol += p.HoldVolCoin
		}
	}

	if holdVol <= 0 {
		return nil, nil
	}

	return &OrderTimeoutExpiredEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         evt.GetReqID(),
			ClientOrderID: evt.GetClientOrderID(),
			Symbol:        evt.Symbol,
			Exchange:      evt.Exchange,
			MarketType:    evt.GetMarketType(),
			StrategyType:  evt.StrategyType,
			PreTopic:      TopicOrderTimeoutScheduled,
			NextTopic:     TopicOrderTimeoutExpired,
			Timestamp:     clock.Now(),
		},
		HoldVol:   holdVol,
		ExpiredAt: clock.Now(),
	}, nil
}

// HandleExecuteBailout performs high-priority emergency force close position with retries.
func (m *OrderManager) HandleExecuteBailout(ctx context.Context, reqID, exchangeName, symbol string, side shared.Side, volume float64, reason string) (OrderBailoutExecutedEvent, error) {
	m.log.WarnContext(ctx, "[Micro-Step 8] HandleExecuteBailout", slog.String("req_id", reqID), slog.String("symbol", symbol), slog.String("reason", reason))

	client, err := m.resolveClient(exchangeName)
	if err != nil {
		return OrderBailoutExecutedEvent{}, err
	}
	clock, err := m.resolveClock(exchangeName)
	if err != nil {
		return OrderBailoutExecutedEvent{}, fmt.Errorf("bailout failed to resolve clock: %w", err)
	}

	agg := m.GetAggregate(reqID)
	posMode := agg.PositionMode()
	leverage := agg.Leverage()

	retries := 0

	closeSide := shared.CloseSideFor(side)
	if closeSide == shared.SideUnknown {
		closeSide = side
	}

	if err := client.CloseAllPositions(ctx, symbol); err != nil {
		m.log.ErrorContext(ctx, "CloseAllPositions bailout failed, entering ClosePosition retry loop", slog.Any("error", err))
		maxRetries := 3
		var errClose error
		for i := 1; i <= maxRetries; i++ {
			retries = i
			errClose = client.ClosePosition(ctx, symbol, closeSide, volume, posMode, leverage)
			if errClose == nil {
				break
			}
			m.log.WarnContext(ctx, "ClosePosition bailout retry failed", slog.Int("attempt", i), slog.Any("error", errClose))
		}
		if errClose != nil {
			return OrderBailoutExecutedEvent{}, fmt.Errorf("bailout failed after %d retries: %w", retries, errClose)
		}
	}

	return OrderBailoutExecutedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         reqID,
			ClientOrderID: agg.ClientOrderID(),
			Symbol:        symbol,
			Exchange:      exchangeName,
			MarketType:    agg.MarketType(),
			StrategyType:  agg.StrategyType(),
			PreTopic:      TopicOrderTimeoutPositionChecked,
			NextTopic:     TopicOrderBailoutExecuted,
			Timestamp:     clock.Now(),
		},
		Side:            side,
		Volume:          volume,
		ExitPrice:       0.0,
		CloseRetryCount: retries,
		Reason:          reason,
		ExecutedAt:      clock.Now(),
	}, nil
}

type pnlMetrics struct {
	status           shared.OrderState
	entryPrice       float64
	exitPrice        float64
	volume           float64
	closeVolContract float64
	closeVolCoin     float64
	VolumeUSDT       float64
	grossPnL         float64
	netPnL           float64
	pnlPct           float64
	fee              float64
	fundingFee       float64
	holdDurationMs   int64
}

func normalizeVolume(volContract, volCoin, contractSize float64) (float64, float64) {
	if volContract == 0 && volCoin > 0 && contractSize > 0 {
		volContract = volCoin / contractSize
	}
	if volCoin == 0 && volContract > 0 && contractSize > 0 {
		volCoin = volContract * contractSize
	}
	return volContract, volCoin
}

func (m *OrderManager) fetchClosedPnL(ctx context.Context, exchangeName, symbol, exchangeOrderID string, contractSize float64) pnlMetrics {
	var metrics pnlMetrics
	if exchangeOrderID == "" {
		return metrics
	}
	client, err := m.resolveClient(exchangeName)
	if err != nil || client == nil {
		return metrics
	}
	provider, ok := client.(ClosedPnLProvider)
	if !ok {
		return metrics
	}

	var closedInfo *exchange.ClosedPnLInfo
	bo := backoff.WithContext(
		backoff.WithMaxRetries(
			backoff.NewExponentialBackOff(
				backoff.WithInitialInterval(time.Second),
				backoff.WithMaxInterval(2*time.Second),
				backoff.WithRandomizationFactor(0.5),
			),
			10,
		),
		ctx,
	)

	errRetry := backoff.Retry(func() error {
		info, errPnl := provider.GetOrderPNL(ctx, symbol, exchangeOrderID)
		if errPnl != nil {
			return errPnl
		}
		if info == nil {
			return errors.New("closed pnl info not ready yet")
		}
		closedInfo = info
		return nil
	}, bo)

	if errRetry != nil {
		m.log.WarnContext(ctx, "Failed to fetch closed PnL metrics after backoff retries", slog.String("symbol", symbol), slog.String("order_id", exchangeOrderID), slog.Any("error", errRetry))
	} else if closedInfo != nil {
		metrics.status = closedInfo.Status
		metrics.entryPrice = closedInfo.EntryPrice
		metrics.exitPrice = closedInfo.ExitPrice
		if closedInfo.ClosedSizeContract != nil {
			metrics.closeVolContract = *closedInfo.ClosedSizeContract
		}
		if closedInfo.ClosedSizeCoin != nil {
			metrics.closeVolCoin = *closedInfo.ClosedSizeCoin
		}
		metrics.closeVolContract, metrics.closeVolCoin = normalizeVolume(metrics.closeVolContract, metrics.closeVolCoin, contractSize)
		if metrics.closeVolContract > 0 {
			metrics.volume = metrics.closeVolContract
		} else if metrics.closeVolCoin > 0 {
			metrics.volume = metrics.closeVolCoin
		}
		metrics.grossPnL = closedInfo.GrossPnL
		metrics.netPnL = closedInfo.NetPnl
		metrics.pnlPct = closedInfo.PnLRate
		metrics.fee = closedInfo.Fee
		metrics.fundingFee = closedInfo.FundingFee
		metrics.holdDurationMs = closedInfo.DurationMs
		metrics.VolumeUSDT = metrics.closeVolCoin * closedInfo.ExitPrice
	}

	return metrics
}

type completedDetails struct {
	refID            string
	side             shared.Side
	fundingRate      float64
	vol24h           float64
	contractSize     float64
	extra            map[string]any
	submittedAt      time.Time
	settleTime       *time.Time
	entryPrice       float64
	exitPrice        float64
	closeVolContract float64
	closeVolCoin     float64
	VolumeUSDT       float64
	grossPnL         float64
	netPnL           float64
	pnlPct           float64
	fee              float64
	fundingFee       float64
	holdDurationMs   int64
}

func (d *completedDetails) applyPositionClosedEvent(ev OrderPositionClosedEvent) {
	if ev.EntryPrice > 0 {
		d.entryPrice = ev.EntryPrice
	}
	if ev.ClosePrice > 0 {
		d.exitPrice = ev.ClosePrice
	}
	if ev.CloseVolContract > 0 {
		d.closeVolContract = ev.CloseVolContract
	}
	if ev.CloseVolCoin > 0 {
		d.closeVolCoin = ev.CloseVolCoin
	}
	if ev.GrossProfit != 0 {
		d.grossPnL = ev.GrossProfit
	}
	if ev.NetProfit != 0 {
		d.netPnL = ev.NetProfit
	}
	if ev.PnLPct != 0 {
		d.pnlPct = ev.PnLPct
	}
	if ev.Fee != 0 {
		d.fee = ev.Fee
	}
	if ev.FundingFee != 0 {
		d.fundingFee = ev.FundingFee
	}
	if ev.HoldDurationMs > 0 {
		d.holdDurationMs = ev.HoldDurationMs
	}
	if ev.VolumeUSDT > 0 {
		d.VolumeUSDT = ev.VolumeUSDT
	} else if ev.CloseVolCoin > 0 && ev.ClosePrice > 0 {
		d.VolumeUSDT = ev.CloseVolCoin * ev.ClosePrice
	}
}

func (d *completedDetails) applyFilledEvent(ev OrderFilledEvent) {
	if ev.FillPrice > 0 && d.entryPrice == 0 {
		d.entryPrice = ev.FillPrice
	}
	if ev.FillVolContract > 0 && d.closeVolContract == 0 {
		d.closeVolContract = ev.FillVolContract
	}
	if ev.FillVolCoin > 0 && d.closeVolCoin == 0 {
		d.closeVolCoin = ev.FillVolCoin
	}
	if ev.VolumeUSDT > 0 && d.VolumeUSDT == 0 {
		d.VolumeUSDT = ev.VolumeUSDT
	}
}

func (d *completedDetails) applySubmittedEvent(ev OrderSubmittedEvent) {
	if ev.FundingRate != 0 {
		d.fundingRate = ev.FundingRate
	}
	if ev.Vol24hUSDT != 0 {
		d.vol24h = ev.Vol24hUSDT
	}
	if ev.ContractSize != 0 {
		d.contractSize = ev.ContractSize
	}
	if ev.Extra != nil {
		d.extra = ev.Extra
	}
	if d.settleTime == nil && ev.SettleTime != nil {
		d.settleTime = ev.SettleTime
	}
	switch {
	case !ev.FireTime.IsZero():
		d.submittedAt = ev.FireTime
	case !ev.SubmittedAt.IsZero():
		d.submittedAt = ev.SubmittedAt
	case !ev.Timestamp.IsZero():
		d.submittedAt = ev.Timestamp
	}
}

func (d *completedDetails) applyIntentEvent(ev OrderIntentEvent) {
	if d.refID == "" && ev.RefID != "" {
		d.refID = ev.RefID
	}
	if d.fundingRate == 0 {
		d.fundingRate = ev.FundingRate
	}
	if d.vol24h == 0 {
		d.vol24h = ev.Vol24hUSDT
	}
	if d.contractSize == 0 {
		d.contractSize = ev.ContractSize
	}
	if d.extra == nil {
		d.extra = ev.Extra
	}
	if d.settleTime == nil && ev.SettleTime != nil {
		d.settleTime = ev.SettleTime
	}
	switch {
	case !ev.FireTime.IsZero():
		d.submittedAt = ev.FireTime
	case d.submittedAt.IsZero() && !ev.Timestamp.IsZero():
		d.submittedAt = ev.Timestamp
	}
}

func (m *OrderManager) extractAggregateCompletedDetails(agg *OrderExecutionAggregate) completedDetails {
	var details completedDetails
	if agg == nil {
		return details
	}
	details.side = agg.Side()
	for _, e := range agg.UncommittedEvents() {
		switch ev := e.(type) {
		case OrderPositionClosedEvent:
			details.applyPositionClosedEvent(ev)
		case OrderFilledEvent:
			details.applyFilledEvent(ev)
		case OrderSubmittedEvent:
			details.applySubmittedEvent(ev)
		case OrderIntentEvent:
			details.applyIntentEvent(ev)
		}
	}
	if details.refID == "" {
		details.refID = agg.RefID()
	}
	return details
}

func (d *completedDetails) mergePnL(pnl pnlMetrics) {
	if pnl.entryPrice > 0 {
		d.entryPrice = pnl.entryPrice
	}
	if pnl.exitPrice > 0 {
		d.exitPrice = pnl.exitPrice
	}
	if pnl.closeVolContract > 0 {
		d.closeVolContract = pnl.closeVolContract
	}
	if pnl.closeVolCoin > 0 {
		d.closeVolCoin = pnl.closeVolCoin
	}
	if pnl.VolumeUSDT > 0 {
		d.VolumeUSDT = pnl.VolumeUSDT
	}
	if pnl.grossPnL != 0 {
		d.grossPnL = pnl.grossPnL
	}
	if pnl.netPnL != 0 {
		d.netPnL = pnl.netPnL
	}
	if pnl.pnlPct != 0 {
		d.pnlPct = pnl.pnlPct
	}
	if pnl.fee != 0 {
		d.fee = pnl.fee
	}
	if pnl.fundingFee != 0 {
		d.fundingFee = pnl.fundingFee
	}
	if pnl.holdDurationMs > 0 {
		d.holdDurationMs = pnl.holdDurationMs
	}
}

// HandleEnrichAndComplete enriches PnL metrics via ClosedPnLProvider and returns terminal OrderCompletedEvent.
func (m *OrderManager) HandleEnrichAndComplete(ctx context.Context, exchangeName, reqID, clientOrderID, symbol string, strategyType StrategyType, outcome OrderOutcome, reason string) (OrderCompletedEvent, error) {
	clock, err := m.resolveClock(exchangeName)
	if err != nil {
		return OrderCompletedEvent{}, fmt.Errorf("failed to resolve clock: %w", err)
	}

	if err := clock.Sleep(ctx, time.Second*30); err != nil {
		m.log.Error("[Micro-Step 9] HandleEnrichAndComplete sleep error", slog.String("req_id", reqID),
			slog.Any("error", err))
	}

	exchangeOrderID, _ := m.GetExchangeOrderIDByReqID(reqID)
	m.log.InfoContext(ctx, "[Micro-Step 9] HandleEnrichAndComplete", slog.String("req_id", reqID), slog.String("client_order_id", clientOrderID), slog.String("exchange_order_id", exchangeOrderID), slog.String("strategy", string(strategyType)), slog.String("outcome", string(outcome)))

	agg := m.GetAggregate(reqID)
	details := m.extractAggregateCompletedDetails(agg)

	contractSize := details.contractSize
	if contractSize <= 0 {
		contractSize = 1.0
	}

	pnl := m.fetchClosedPnL(ctx, exchangeName, symbol, exchangeOrderID, contractSize)
	if pnl.status == exchange.OrderStateCanceled {
		outcome = OutcomeCanceledNoFill
	}
	details.mergePnL(pnl)

	return OrderCompletedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         reqID,
			RefID:         details.refID,
			ClientOrderID: clientOrderID,
			Symbol:        symbol,
			Exchange:      exchangeName,
			MarketType:    MarketTypeFuture,
			StrategyType:  strategyType,
			PreTopic:      TopicOrderPositionClosed,
			NextTopic:     TopicOrderCompleted,
			Timestamp:     clock.Now(),
		},
		Side:             details.side,
		OrderID:          exchangeOrderID,
		Outcome:          outcome,
		EntryPrice:       details.entryPrice,
		ExitPrice:        details.exitPrice,
		VolumeUSDT:       details.VolumeUSDT,
		ContractSize:     details.contractSize,
		CloseVolContract: details.closeVolContract,
		CloseVolCoin:     details.closeVolCoin,
		GrossProfit:      details.grossPnL,
		NetProfit:        details.netPnL,
		PnLPct:           details.pnlPct,
		Fee:              details.fee,
		FundingFee:       details.fundingFee,
		FundingRate:      details.fundingRate,
		Vol24hUSDT:       details.vol24h,
		HoldDurationMs:   details.holdDurationMs,
		Reason:           reason,
		SettleTime:       details.settleTime,
		Extra:            details.extra,
		CompletedAt:      clock.Now(),
	}, nil
}

func mapOrderType(ot OrderType) shared.OrderType {
	switch ot {
	case OrderTypePostOnly:
		return shared.OrderTypePostOnly
	case OrderTypeIOC:
		return shared.OrderTypeIOC
	case OrderTypeMarket:
		return shared.OrderTypeMarket
	default:
		return shared.OrderTypeLimit
	}
}

func mapOpenType(mm shared.MarginMode) shared.OpenType {
	if mm == shared.MarginModeCross {
		return shared.OpenTypeCross
	}
	return shared.OpenTypeIsolated
}

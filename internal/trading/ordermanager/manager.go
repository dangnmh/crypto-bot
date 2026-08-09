package ordermanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/eventbus"

	"github.com/cenkalti/backoff/v4"
	"github.com/patrickmn/go-cache"
)

const (
	defaultCacheTTL        = 1 * time.Hour
	defaultCleanupInterval = 10 * time.Minute
)

// OrderManager is a business-agnostic reactive order execution engine using Micro-Events.
type OrderManager struct {
	client          ExchangeClient
	watcher         OrderWatcher
	clock           Clock
	repo            TradeRepository
	notifier        notifier.Notifier
	bus             *eventbus.Bus
	log             *slog.Logger
	timers          sync.Map
	aggregates      *cache.Cache
	orderIDMapCache *cache.Cache
}

// NewOrderManager initializes a new OrderManager with non-nil dependencies.
func NewOrderManager(client ExchangeClient, watcher OrderWatcher, bus *eventbus.Bus, clock Clock, log *slog.Logger) (*OrderManager, error) {
	if client == nil || bus == nil || clock == nil {
		return nil, fmt.Errorf("missing params")
	}

	if log == nil {
		log = slog.Default()
	}

	m := &OrderManager{
		client:          client,
		watcher:         watcher,
		clock:           clock,
		bus:             bus,
		log:             log.With("component", "GenericOrderManager"),
		aggregates:      cache.New(defaultCacheTTL, defaultCleanupInterval),
		orderIDMapCache: cache.New(defaultCacheTTL, defaultCleanupInterval),
	}

	InitGlobalSubscriptions(context.Background(), m)

	return m, nil
}

// SetRepository sets the trade PnL persistence repository.
func (m *OrderManager) SetRepository(repo TradeRepository) {
	m.repo = repo
}

// SetNotifier sets the Telegram notification provider.
func (m *OrderManager) SetNotifier(n notifier.Notifier) {
	m.notifier = n
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

// SetExchangeOrderIDByClientOrderID stores mapping from clientOrderID to exchangeOrderID in cache (1h TTL).
func (m *OrderManager) SetExchangeOrderIDByClientOrderID(clientOrderID, exchangeOrderID string) {
	if clientOrderID == "" || exchangeOrderID == "" || m.orderIDMapCache == nil {
		return
	}
	m.orderIDMapCache.Set(clientOrderID, exchangeOrderID, defaultCacheTTL)
}

// GetExchangeOrderIDByClientOrderID retrieves exchangeOrderID associated with clientOrderID from cache.
func (m *OrderManager) GetExchangeOrderIDByClientOrderID(clientOrderID string) (string, bool) {
	if clientOrderID == "" || m.orderIDMapCache == nil {
		return "", false
	}
	val, found := m.orderIDMapCache.Get(clientOrderID)
	if !found {
		return "", false
	}
	exOID, ok := val.(string)
	return exOID, ok
}

// HandlePreFlight calculates Margin Mode, Position Mode & Risk Limit Leverage.
func (m *OrderManager) HandlePreFlight(ctx context.Context, evt OrderIntentEvent) (OrderPreFlightCompletedEvent, error) {
	m.log.InfoContext(ctx, "[Micro-Step 1] HandlePreFlight", slog.String("req_id", evt.GetReqID()), slog.String("symbol", evt.Symbol))

	if syncer, ok := m.clock.(SyncerClock); ok {
		m.log.InfoContext(ctx, "Forcing clock sync on pre-flight")
		syncer.SyncNow(ctx)
	}

	// Switch Margin Mode
	if err := m.client.SwitchMarginMode(ctx, evt.Symbol, evt.MarginMode, evt.Leverage, evt.Side); err != nil {
		m.log.ErrorContext(ctx, "Switch margin mode failed", slog.Any("error", err))
		return OrderPreFlightCompletedEvent{}, fmt.Errorf("switch margin mode failed: %w", err)
	}

	// Switch Position Mode if provider exists
	if switcher, ok := m.client.(PositionModeSwitcher); ok && evt.PositionMode > 0 {
		if err := switcher.SwitchPositionMode(ctx, evt.Symbol, evt.PositionMode); err != nil {
			m.log.ErrorContext(ctx, "Switch position mode failed", slog.Any("error", err))
			return OrderPreFlightCompletedEvent{}, fmt.Errorf("switch position mode failed: %w", err)
		}
	}

	// Determine Safe Leverage against Exchange Risk Limits
	adjustedLeverage := evt.Leverage
	if evt.Leverage > 0 && !m.client.SupportLeverageOnOrder() {
		if provider, ok := m.client.(RiskLimitLeverageProvider); ok {
			notionalVal := evt.Volume * evt.ContractSize * evt.Price
			if notionalVal > 0 {
				if maxLev, err := provider.GetMaxLeverageForValue(ctx, evt.Symbol, notionalVal); err == nil && maxLev > 0 && adjustedLeverage > maxLev {
					m.log.InfoContext(ctx, "Leverage exceeds risk limits, auto-adjusting to max safe leverage",
						slog.Int("configured", adjustedLeverage),
						slog.Int("max_safe", maxLev),
					)
					adjustedLeverage = maxLev
				}
			}
		}

		posType := exchange.PositionTypeLong
		if !evt.Side.IsLong() {
			posType = exchange.PositionTypeShort
		}

		err := m.client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{
			Symbol:       evt.Symbol,
			Leverage:     adjustedLeverage,
			PositionType: posType,
		})
		if err != nil {
			m.log.ErrorContext(ctx, "Change leverage failed", slog.Any("error", err))
			return OrderPreFlightCompletedEvent{}, fmt.Errorf("change leverage failed: %w", err)
		}
	}

	evt.PreTopic = TopicOrderIntent
	evt.NextTopic = TopicOrderPreFlightDone

	return OrderPreFlightCompletedEvent{
		OrderIntentEvent: evt,
		AdjustedLeverage: adjustedLeverage,
		PreFlightDoneAt:  m.clock.Now(),
	}, nil
}

// HandleFireTiming calculates latency offset & precision sleep window.
func (m *OrderManager) HandleFireTiming(ctx context.Context, evt OrderPreFlightCompletedEvent) (OrderFireWindowReachedEvent, error) {
	m.log.InfoContext(ctx, "[Micro-Step 2] HandleFireTiming", slog.String("req_id", evt.GetReqID()), slog.String("symbol", evt.Symbol))

	if !evt.FireTime.IsZero() {
		latencyMs := m.clock.LatencyMs()
		oneWayMs := latencyMs / 2
		fireOffset := time.Duration(oneWayMs) * time.Millisecond
		targetTime := evt.FireTime.Add(-fireOffset)

		if m.clock.Until(targetTime) > 0 {
			m.log.InfoContext(ctx, "Sleeping for fire window target", slog.Time("target", targetTime))
			if err := m.clock.Sleep(ctx, m.clock.Until(targetTime)); err != nil {
				return OrderFireWindowReachedEvent{}, fmt.Errorf("fire timing sleep failed: %w", err)
			}
		}
	}

	evt.PreTopic = TopicOrderPreFlightDone
	evt.NextTopic = TopicOrderFireWindowReached

	return OrderFireWindowReachedEvent{
		OrderPreFlightCompletedEvent: evt,
		FireWindowReachedAt:          m.clock.Now(),
	}, nil
}

// HandleExecuteOrder executes order submission REST API.
func (m *OrderManager) HandleExecuteOrder(ctx context.Context, evt OrderFireWindowReachedEvent) (OrderSubmittedEvent, error) {
	m.log.InfoContext(ctx, "[Micro-Step 3] HandleExecuteOrder", slog.String("req_id", evt.GetReqID()), slog.String("symbol", evt.Symbol))

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

	resp, err := m.client.CreateOrder(ctx, req)
	if err != nil {
		m.log.ErrorContext(ctx, "Create order failed", slog.Any("error", err))
		return OrderSubmittedEvent{}, fmt.Errorf("create order failed: %w", err)
	}

	if clientOID != "" && resp.OrderID != "" {
		m.SetExchangeOrderIDByClientOrderID(clientOID, resp.OrderID)
	}

	return OrderSubmittedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         evt.GetReqID(),
			ClientOrderID: clientOID,
			Symbol:        evt.Symbol,
			Exchange:      evt.Exchange,
			MarketType:    evt.GetMarketType(),
			StrategyType:  evt.StrategyType,
			PreTopic:      TopicOrderFireWindowReached,
			NextTopic:     TopicOrderSubmitted,
			SendNotify:    evt.SendNotify,
			Timestamp:     m.clock.Now(),
		},
		Price:         evt.Price,
		Volume:        evt.Volume,
		TPSLSubmitted: resp.TPSLSubmitted,
		SubmittedAt:   m.clock.Now(),
	}, nil
}

// HandleTPSLContingency places background TP/SL trigger if not supported inline.
func (m *OrderManager) HandleTPSLContingency(ctx context.Context, evt OrderSubmittedEvent, intent OrderIntentEvent) (*OrderTPSLDispatchedEvent, error) {
	if evt.TPSLSubmitted || (intent.TakeProfitPrice == 0 && intent.StopLossPrice == 0) {
		return nil, nil
	}

	provider, ok := m.client.(TPSLProvider)
	if !ok {
		m.log.WarnContext(ctx, "Exchange does not support standalone PlaceTPSL")
		return nil, nil
	}

	m.log.InfoContext(ctx, "[Micro-Step 4] HandleTPSLContingency", slog.String("symbol", evt.Symbol), slog.Float64("tp", intent.TakeProfitPrice), slog.Float64("sl", intent.StopLossPrice))

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
			SendNotify:    evt.SendNotify,
			Timestamp:     m.clock.Now(),
		},
		TakeProfitPrice: intent.TakeProfitPrice,
		StopLossPrice:   intent.StopLossPrice,
		DispatchedAt:    m.clock.Now(),
	}, nil
}

// HandleScheduleTimeout starts background timeout timer.
func (m *OrderManager) HandleScheduleTimeout(reqID, symbol string, dur time.Duration, onTimeout func()) OrderTimeoutScheduledEvent {
	m.log.Info("[Micro-Step 5] HandleScheduleTimeout", slog.String("req_id", reqID), slog.Duration("duration", dur))

	timer := time.AfterFunc(dur, func() {
		m.timers.Delete(reqID)
		if onTimeout != nil {
			onTimeout()
		}
	})
	m.timers.Store(reqID, timer)

	agg := m.GetAggregate(reqID)

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
			SendNotify:    true,
			Timestamp:     m.clock.Now(),
		},
		Duration:    dur,
		ScheduledAt: m.clock.Now(),
	}
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

// HandleOutcomeWatcher performs exponential backoff polling & WS stream watching for fill outcome classification.
func (m *OrderManager) HandleOutcomeWatcher(ctx context.Context, evt OrderSubmittedEvent) (OrderOutcomeResolvedEvent, error) {
	exchangeOrderID, _ := m.GetExchangeOrderIDByClientOrderID(evt.GetClientOrderID())
	m.log.InfoContext(ctx, "[Micro-Step 6] HandleOutcomeWatcher", slog.String("req_id", evt.GetReqID()), slog.String("client_order_id", evt.GetClientOrderID()), slog.String("exchange_order_id", exchangeOrderID))

	wsChan := m.startWSStreamWatcher(ctx, evt.Symbol, exchangeOrderID, evt.GetClientOrderID())
	order, err := m.pollOrderUntilTerminal(ctx, evt.Symbol, exchangeOrderID, wsChan)

	outcome, filledVol, avgPrice := classifyOrderOutcome(order)
	reason := ""
	if err != nil {
		reason = err.Error()
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
			SendNotify:    evt.SendNotify,
			Timestamp:     m.clock.Now(),
		},
		Outcome:    outcome,
		FilledVol:  filledVol,
		AvgPrice:   avgPrice,
		Reason:     reason,
		ResolvedAt: m.clock.Now(),
	}, nil
}

func (m *OrderManager) startWSStreamWatcher(ctx context.Context, symbol, exchangeOrderID, clientOrderID string) <-chan *exchange.OrderInfo {
	wsChan := make(chan *exchange.OrderInfo, 1)
	if m.watcher == nil {
		return wsChan
	}
	stream, err := m.watcher.SubscribeOrderUpdates(ctx, symbol)
	if err != nil || stream == nil {
		return wsChan
	}
	go func() {
		for update := range stream {
			if update.OrderID == exchangeOrderID || (clientOrderID != "" && update.OrderID == clientOrderID) {
				st := parseOrderState(update.Status)
				if exchange.IsTerminalOrderState(st) {
					select {
					case wsChan <- &exchange.OrderInfo{
						OrderID:      update.OrderID,
						State:        st,
						DealVol:      update.FilledVol,
						DealAvgPrice: update.AvgPrice,
					}:
					default:
					}
					return
				}
			}
		}
	}()
	return wsChan
}

func (m *OrderManager) pollOrderUntilTerminal(ctx context.Context, symbol, exchangeOrderID string, wsChan <-chan *exchange.OrderInfo) (*exchange.OrderInfo, error) {
	var order *exchange.OrderInfo
	bo := backoff.WithContext(
		backoff.WithMaxRetries(
			backoff.NewExponentialBackOff(
				backoff.WithInitialInterval(500*time.Millisecond),
				backoff.WithMaxInterval(2*time.Second),
			),
			5,
		),
		ctx,
	)

	err := backoff.Retry(func() error {
		select {
		case wsOrder := <-wsChan:
			if wsOrder != nil {
				order = wsOrder
				return nil
			}
		default:
		}

		got, err := m.client.GetOrder(ctx, symbol, exchangeOrderID)
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
	case exchange.OrderStatePartial:
		return OutcomePartialFilled, order.DealVol, order.DealAvgPrice
	case exchange.OrderStateCanceled:
		return OutcomeCanceledNoFill, order.DealVol, order.DealAvgPrice
	default:
		return OutcomeUnknown, order.DealVol, order.DealAvgPrice
	}
}

// HandleTimeoutCheck checks open position HoldVol when timeout expires.
func (m *OrderManager) HandleTimeoutCheck(ctx context.Context, evt OrderTimeoutScheduledEvent) (*OrderTimeoutExpiredEvent, error) {
	m.log.InfoContext(ctx, "[Micro-Step 7] HandleTimeoutCheck", slog.String("req_id", evt.GetReqID()), slog.String("symbol", evt.Symbol))

	positions, err := m.client.GetOpenPositions(ctx, evt.Symbol)
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
			SendNotify:    evt.SendNotify,
			Timestamp:     m.clock.Now(),
		},
		HoldVol:   holdVol,
		ExpiredAt: m.clock.Now(),
	}, nil
}

// HandleExecuteBailout performs high-priority emergency force close position with retries.
func (m *OrderManager) HandleExecuteBailout(ctx context.Context, symbol string, side shared.Side, volume float64, reason string) (OrderBailoutExecutedEvent, error) {
	m.log.WarnContext(ctx, "[Micro-Step 8] HandleExecuteBailout", slog.String("symbol", symbol), slog.String("reason", reason))

	reqID := fmt.Sprintf("bailout-%d", m.clock.Now().UnixNano())
	retries := 0

	closeSide := shared.CloseSideFor(side)
	if closeSide == shared.SideUnknown {
		closeSide = side
	}

	if err := m.client.CloseAllPositions(ctx, symbol); err != nil {
		m.log.ErrorContext(ctx, "CloseAllPositions bailout failed, entering ClosePosition retry loop", slog.Any("error", err))
		maxRetries := 3
		var errClose error
		for i := 1; i <= maxRetries; i++ {
			retries = i
			errClose = m.client.ClosePosition(ctx, symbol, closeSide, volume, shared.PositionModeOneWay, 1)
			if errClose == nil {
				break
			}
			m.log.WarnContext(ctx, "ClosePosition bailout retry failed", slog.Int("attempt", i), slog.Any("error", errClose))
		}
		if errClose != nil {
			return OrderBailoutExecutedEvent{}, fmt.Errorf("bailout failed after %d retries: %w", retries, errClose)
		}
	}

	agg := m.GetAggregate(reqID)

	return OrderBailoutExecutedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         reqID,
			ClientOrderID: agg.ClientOrderID(),
			Symbol:        symbol,
			Exchange:      agg.Exchange(),
			MarketType:    agg.MarketType(),
			StrategyType:  agg.StrategyType(),
			PreTopic:      TopicOrderTimeoutExpired,
			NextTopic:     TopicOrderBailoutExecuted,
			SendNotify:    true,
			Timestamp:     m.clock.Now(),
		},
		Side:            side,
		Volume:          volume,
		ExitPrice:       0.0,
		CloseRetryCount: retries,
		Reason:          reason,
		ExecutedAt:      m.clock.Now(),
	}, nil
}

// HandleEnrichAndComplete enriches PnL metrics via ClosedPnLProvider and returns terminal OrderCompletedEvent.
func (m *OrderManager) HandleEnrichAndComplete(ctx context.Context, reqID, clientOrderID, symbol string, strategyType StrategyType, outcome, reason string) OrderCompletedEvent {
	exchangeOrderID, _ := m.GetExchangeOrderIDByClientOrderID(clientOrderID)
	m.log.InfoContext(ctx, "[Micro-Step 9] HandleEnrichAndComplete", slog.String("req_id", reqID), slog.String("client_order_id", clientOrderID), slog.String("exchange_order_id", exchangeOrderID), slog.String("strategy", string(strategyType)), slog.String("outcome", outcome))

	entryPrice := 0.0
	exitPrice := 0.0
	volume := 0.0
	grossPnL := 0.0
	netPnL := 0.0
	pnlPct := 0.0
	fee := 0.0
	fundingFee := 0.0

	if provider, ok := m.client.(ClosedPnLProvider); ok && exchangeOrderID != "" {
		var closedInfo *exchange.ClosedPnLInfo
		bo := backoff.WithContext(
			backoff.WithMaxRetries(
				backoff.NewExponentialBackOff(
					backoff.WithInitialInterval(500*time.Millisecond),
					backoff.WithMaxInterval(2*time.Second),
				),
				5,
			),
			ctx,
		)

		err := backoff.Retry(func() error {
			info, err := provider.GetOrderPNL(ctx, symbol, exchangeOrderID)
			if err != nil {
				return err
			}
			if info == nil {
				return errors.New("closed pnl info not ready yet")
			}
			closedInfo = info
			return nil
		}, bo)

		if err != nil {
			m.log.WarnContext(ctx, "Failed to fetch closed PnL metrics after backoff retries", slog.String("symbol", symbol), slog.String("order_id", exchangeOrderID), slog.Any("error", err))
		} else if closedInfo != nil {
			entryPrice = closedInfo.EntryPrice
			exitPrice = closedInfo.ExitPrice
			if closedInfo.ClosedSizeContract != nil {
				volume = *closedInfo.ClosedSizeContract
			} else if closedInfo.ClosedSizeCoin != nil {
				volume = *closedInfo.ClosedSizeCoin
			}
			grossPnL = closedInfo.GrossPnL
			netPnL = closedInfo.NetPnl
			pnlPct = closedInfo.PnLRate
			fee = closedInfo.Fee
			fundingFee = closedInfo.FundingFee
		}
	}

	agg := m.GetAggregate(reqID)

	return OrderCompletedEvent{
		BaseExecutionEvent: BaseExecutionEvent{
			ReqID:         reqID,
			ClientOrderID: clientOrderID,
			Symbol:        symbol,
			Exchange:      agg.Exchange(),
			MarketType:    agg.MarketType(),
			StrategyType:  strategyType,
			PreTopic:      TopicOrderOutcomeResolved,
			NextTopic:     TopicOrderCompleted,
			SendNotify:    true,
			Timestamp:     m.clock.Now(),
		},

		Outcome:     outcome,
		EntryPrice:  entryPrice,
		ExitPrice:   exitPrice,
		Volume:      volume,
		GrossProfit: grossPnL,
		NetProfit:   netPnL,
		PnLPct:      pnlPct,
		Fee:         fee,

		FundingFee:  fundingFee,
		Reason:      reason,
		CompletedAt: m.clock.Now(),
	}
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

func parseOrderState(s string) shared.OrderState {
	switch s {
	case "FILLED", "filled":
		return exchange.OrderStateFilled
	case "CANCELED", "CANCELLED", "canceled":
		return exchange.OrderStateCanceled
	case "PARTIAL", "PARTIALLY_FILLED", "partial_filled":
		return exchange.OrderStatePartial
	default:
		return exchange.OrderStateNew
	}
}

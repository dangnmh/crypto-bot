package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	infrawatcher "crypto-bot/internal/infrastructure/watcher"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/cenkalti/backoff/v4"
	"github.com/patrickmn/go-cache"
)

const (
	reversionReasonNoFill        ReversionReason = "no_fill"
	reversionMethodFallbackClose ReversionReason = "fallback_close"
)

// Strategy implements strategy.BackgroundStrategy interface in a lightweight, stateless manner.
type Strategy struct {
	engine   *app.Engine
	global   *config.Config
	notifier notifier.Notifier
	log      *slog.Logger
	stores   map[string]strategy.FundingStoreSet
	repo     domain.TradeReportRepository
	cache    *cache.Cache

	// Test fallbacks
	clock         shared.Clock
	orderNotifier infrawatcher.OrderNotifier
	wsSub         infraws.Subscriber
}

func NewStrategy(
	engine *app.Engine,
	global *config.Config,
	n notifier.Notifier,
	repo domain.TradeReportRepository,
	c *cache.Cache,
	log *slog.Logger,
) *Strategy {
	logger := log.With("flow", FlowReversion)
	return &Strategy{
		engine:   engine,
		global:   global,
		notifier: n,
		repo:     repo,
		cache:    c,
		log:      logger,
	}
}

var _ strategy.BackgroundStrategy = (*Strategy)(nil)

func (s *Strategy) Flow() string {
	return FlowReversion
}

func (s *Strategy) Enabled(cfg config.SymbolConfig) bool {
	return cfg.FundingReversion.Enabled
}

func (s *Strategy) Start(ctx context.Context, stores map[string]strategy.FundingStoreSet) error {
	s.stores = stores

	runner := &StatelessRunner{
		globalCfg:  s.global,
		bus:        s.engine.Bus,
		log:        s.log,
		engine:     s.engine,
		stores:     s.stores,
		notifier:   s.notifier,
		cache:      s.cache,
		reportRepo: s.repo,
		// Pass test fallbacks
		clock:         s.clock,
		orderNotifier: s.orderNotifier,
		wsSub:         s.wsSub,
	}

	InitGlobalSubscriptions(ctx, runner)
	return nil
}

func (s *Strategy) SetTestFallbacks(clock shared.Clock, orderNotifier infrawatcher.OrderNotifier, wsSub infraws.Subscriber) {
	s.clock = clock
	s.orderNotifier = orderNotifier
	s.wsSub = wsSub
}

func (s *Strategy) Stop(ctx context.Context) error {
	return nil
}

// StatelessRunner handles global, single-instance reversion event subscriptions.
type StatelessRunner struct {
	deps      strategy.Deps
	globalCfg *config.Config
	bus       *eventbus.Bus
	log       *slog.Logger

	engine     *app.Engine
	stores     map[string]strategy.FundingStoreSet
	notifier   notifier.Notifier
	cache      *cache.Cache
	reportRepo domain.TradeReportRepository

	// Target context to resolve configuration conflicts across multiple exchanges
	exchange string
	symbol   string

	// Test fallbacks
	clock         shared.Clock
	orderNotifier infrawatcher.OrderNotifier
	wsSub         infraws.Subscriber
}

func (r *StatelessRunner) clone(exch, reqID, symbol string) *StatelessRunner {
	local := *r
	local.exchange = exch
	local.symbol = symbol
	var clonedLog *slog.Logger
	if r.log != nil {
		clonedLog = r.log.With("exchange", exch, "req", reqID, "symbol", symbol)
	} else {
		clonedLog = slog.Default()
	}
	local.log = clonedLog

	if r.engine != nil {
		prov, err := r.engine.GetProvider(exch)
		if err != nil {
			r.log.Error("Failed to locate exchange provider for clone", slog.String("exchange", exch), slog.Any("error", err))
			return r
		}
		stores := r.stores[exch]
		if stores == nil {
			r.log.Error("Failed to locate stores for clone", slog.String("exchange", exch))
			return r
		}

		var clock shared.Clock = prov.TimeSync
		if r.clock != nil {
			clock = r.clock
		}

		var orderNotifier infrawatcher.OrderNotifier = prov.Watcher
		if r.orderNotifier != nil {
			orderNotifier = r.orderNotifier
		}

		var wsSub infraws.Subscriber = prov.Adapter
		if r.wsSub != nil {
			wsSub = r.wsSub
		}

		local.deps = strategy.Deps{
			Client:        prov.Client,
			WsSub:         wsSub,
			OrderNotifier: orderNotifier,
			TickerStore:   stores.Ticker(),
			ContractStore: stores.Contract(),
			PriceStore:    stores.Price(),
			FundingStore:  stores.Funding(),
			DepthStore:    stores.Depth(),
			Clock:         clock,
			Log:           clonedLog,
			Notifier:      r.notifier,
			EventBus:      r.engine.Bus,
		}
	} else {
		local.deps.Log = clonedLog
	}
	return &local
}

func (r *StatelessRunner) publishEvent(ctx context.Context, topic string, payload any) error {
	if r.bus == nil {
		return nil
	}

	payload = stampEventTrace(topic, payload)
	r.log.InfoContext(ctx, "Reversion: Publishing event", slog.String("topic", topic), slog.Any("payload", payload))

	if err := r.bus.Publish(topic, payload); err != nil {
		r.log.ErrorContext(ctx, "Failed to publish event", slog.String("topic", topic), slog.Any("error", err))
		return err
	}

	// Check if the event wants to trigger a notification
	if revEvt, ok := payload.(ReversionEvent); ok && revEvt.ShouldNotify() {
		if r.deps.Notifier != nil {
			level := notifier.LevelTrading
			if topic == TopicReversionAbort || topic == TopicReversionError {
				level = notifier.LevelCritical
			}

			evt := notifier.Event{
				Level:     level,
				Exchange:  revEvt.GetExchange(),
				Symbol:    revEvt.GetSymbol(),
				Message:   revEvt.GetMessage(),
				Color:     string(revEvt.GetColor()),
				Data:      revEvt.GetDataMap(),
				Timestamp: r.deps.Clock.Now(),
			}

			go func() {
				_ = r.deps.Notifier.Send(ctx, evt)
			}()
		}
	}

	return nil
}

func stampEventTrace(topic string, payload any) any {
	copyVal, base, ok := mutableBaseReversionEvent(payload)
	if !ok {
		return payload
	}

	setStringFieldIfEmpty(base, "EventID", watermill.NewUUID())
	setStringField(base, "Topic", topic)

	return copyVal.Interface()
}

func mutableBaseReversionEvent(payload any) (reflect.Value, reflect.Value, bool) {
	v := reflect.ValueOf(payload)
	if !v.IsValid() {
		return reflect.Value{}, reflect.Value{}, false
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() || v.Elem().Kind() != reflect.Struct {
			return reflect.Value{}, reflect.Value{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, reflect.Value{}, false
	}

	copyVal := reflect.New(v.Type()).Elem()
	copyVal.Set(v)
	base := copyVal.FieldByName("BaseReversionEvent")
	if !base.IsValid() || !base.CanSet() {
		return reflect.Value{}, reflect.Value{}, false
	}
	return copyVal, base, true
}

func setStringField(base reflect.Value, name, value string) {
	field := base.FieldByName(name)
	if field.IsValid() && field.CanSet() {
		field.SetString(value)
	}
}

func setStringFieldIfEmpty(base reflect.Value, name, value string) {
	field := base.FieldByName(name)
	if field.IsValid() && field.CanSet() && field.String() == "" {
		field.SetString(value)
	}
}

func nextReversionBase(prev BaseReversionEvent, symbol string, timestamp time.Time) BaseReversionEvent {
	seq := int64(0)
	if prev.Seq > 0 {
		seq = prev.Seq + 1
	}
	return BaseReversionEvent{
		Flow:          FlowReversion,
		ReqID:         prev.ReqID,
		Symbol:        symbol,
		Exchange:      prev.Exchange,
		Color:         prev.Color,
		OrderID:       prev.OrderID,
		ExternalID:    prev.ExternalID,
		Timestamp:     timestamp,
		Seq:           seq,
		PreviousTopic: prev.Topic,
		SettleTime:    prev.SettleTime,
		Side:          prev.Side,
		FundingRate:   prev.FundingRate,
	}
}

func nextNotifyReversionBase(prev BaseReversionEvent, symbol string, timestamp time.Time) BaseReversionEvent {
	base := nextReversionBase(prev, symbol, timestamp)
	base.SendNotify = true
	return base
}

func (r *StatelessRunner) WaitUntil(ctx context.Context, symbol string, target time.Time) bool {
	if d := r.deps.Clock.Until(target); d > 0 {
		r.log.DebugContext(ctx, "⏱️ wait", slog.String("symbol", symbol), slog.Time("target", target), slog.Duration("wait", d))
		return r.deps.Clock.Sleep(ctx, d) == nil
	}
	return ctx.Err() == nil
}

func (r *StatelessRunner) waitUntilFuture(ctx context.Context, symbol string, target time.Time) error {
	d := r.deps.Clock.Until(target)
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("timer target already passed for %s at %s", symbol, target.Format(time.RFC3339Nano))
	}

	r.log.DebugContext(ctx, "⏱️ wait", slog.String("symbol", symbol), slog.Time("target", target), slog.Duration("wait", d))
	return r.deps.Clock.Sleep(ctx, d)
}

func (r *StatelessRunner) subscribeWS(ctx context.Context, symbol string) error {
	return r.deps.WsSub.SubscribeTicker(ctx, symbol)
}

func (r *StatelessRunner) unsubscribeWS(ctx context.Context, symbol string) {
	if err := r.deps.WsSub.UnsubscribeTicker(ctx, symbol); err != nil {
		r.log.WarnContext(ctx, "⚠️ Failed to unsubscribe ticker", slog.String("symbol", symbol), slog.Any("error", err))
	}
}

func (r *StatelessRunner) refreshPrice(ctx context.Context, c *domain.Candidate) error {
	pd, err := r.deps.PriceStore.GetPrice(ctx, c.Symbol, 5*time.Second)
	if err == nil && pd.BestBid > 0 && pd.BestAsk > 0 {
		c.BestBid, c.BestAsk, c.LastPrice = pd.BestBid, pd.BestAsk, pd.LastPrice
		return nil
	}

	// Fallback: If price data is stale or missing in PriceStore, query the exchange REST API
	r.log.WarnContext(ctx, "Price store data stale or missing; querying REST API fallback", slog.String("symbol", c.Symbol), slog.Any("error", err))
	tickers, apiErr := r.deps.Client.GetTickers(ctx, c.Symbol)
	if apiErr != nil {
		r.log.ErrorContext(ctx, "REST API fallback failed", slog.String("symbol", c.Symbol), slog.Any("error", apiErr))
		return fmt.Errorf("price store stale (%w) and REST API fallback failed (%w)", err, apiErr)
	}
	if len(tickers) == 0 {
		return fmt.Errorf("price store stale (%w) and REST API fallback returned no tickers", err)
	}

	ticker := tickers[0]
	c.BestBid = ticker.Bid1
	c.BestAsk = ticker.Ask1
	c.LastPrice = ticker.LastPrice

	r.log.InfoContext(ctx, "🟢 REST API fallback succeeded",
		slog.String("symbol", c.Symbol),
		slog.Float64("lastPrice", c.LastPrice),
		slog.Float64("bid", c.BestBid),
		slog.Float64("ask", c.BestAsk),
	)
	return nil
}

func (r *StatelessRunner) abort(ctx context.Context, symbol, reqID, exchangeName string, reason ReversionReason) {
	evt := AbortEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:      FlowReversion,
			ReqID:     reqID,
			Symbol:    symbol,
			Exchange:  exchangeName,
			Timestamp: r.deps.Clock.Now(),
		},
		Reason: reason,
	}
	_ = r.publishEvent(ctx, TopicReversionAbort, evt)
}

func (r *StatelessRunner) abortAfter(ctx context.Context, prev BaseReversionEvent, symbol string, reason ReversionReason) {
	evt := AbortEvent{
		BaseReversionEvent: nextReversionBase(prev, symbol, r.deps.Clock.Now()),
		Reason:             reason,
	}
	_ = r.publishEvent(ctx, TopicReversionAbort, evt)
}

func (r *StatelessRunner) handlePositionUpdate(ctx context.Context, pos exchange.PersonalPositionUpdate, prev BaseReversionEvent) {
	if r.cache != nil {
		if _, found := r.cache.Get(prev.ReqID); !found {
			r.log.DebugContext(ctx, "Ignoring position update; cycle already cleaned up or inactive", slog.String("req_id", prev.ReqID))
			return
		}
	}

	r.log.Debug("Position update received", slog.Any("pos", pos))

	contractSize := 1.0
	if r.deps.ContractStore != nil {
		if cd, err := r.deps.ContractStore.GetContract(ctx, pos.Symbol); err == nil && cd.ContractSize > 0 {
			contractSize = cd.ContractSize
		}
	}

	fillPrice := pos.OpenAvgPrice
	if fillPrice == 0 {
		fillPrice = pos.HoldAvgPrice
	}
	if fillPrice == 0 {
		if pd, err := r.deps.PriceStore.GetPrice(ctx, pos.Symbol, 5*time.Second); err == nil {
			fillPrice = pd.LastPrice
		}
	}

	side := shared.SideOpenLong
	closeSide := shared.SideCloseLong
	if pos.PositionType == exchange.PositionTypeShort { // Short position
		side = shared.SideOpenShort
		closeSide = shared.SideCloseShort
	}

	if pos.HoldVol > 0 {
		base := nextReversionBase(prev, pos.Symbol, r.deps.Clock.Now())
		if base.OrderID == "" {
			if resolved, err := r.resolveOrderID(prev.ReqID, prev.OrderID); err == nil {
				base.OrderID = resolved
			}
		}
		evt := OrderFilledEvent{
			BaseReversionEvent: base,
			Side:               side,
			CloseSide:          closeSide,
			FillPrice:          fillPrice,
			FillVol:            pos.HoldVol,
			VolumeUSDT:         fillPrice * pos.HoldVol * contractSize,
		}
		go func() {
			_ = r.publishEvent(ctx, TopicReversionOrderFilled, evt)
		}()
	} else if pos.HoldVol == 0 {
		evt, err := r.buildAndEnrichClosedEvent(ctx, pos, fillPrice, side, prev, contractSize)
		if err != nil {
			r.log.ErrorContext(ctx, "Failed to build closed event", slog.Any("pos", pos), slog.Any("error", err))
			return
		}
		go func() {
			_ = r.publishEvent(ctx, TopicReversionPositionClosed, evt)
		}()
	}
}

func calculatePnLPct(entry, exit float64, side shared.Side) float64 {
	if entry <= 0 {
		return 0
	}
	if side == shared.SideOpenLong {
		return ((exit - entry) / entry) * 100.0
	}
	return ((entry - exit) / entry) * 100.0
}

func (r *StatelessRunner) buildAndEnrichClosedEvent(
	ctx context.Context,
	pos exchange.PersonalPositionUpdate,
	fillPrice float64,
	side shared.Side,
	prev BaseReversionEvent,
	contractSize float64,
) (*PositionClosedEvent, error) {
	closePrice := fillPrice
	if pos.CloseAvgPrice > 0 {
		closePrice = pos.CloseAvgPrice
	}

	var vol24h float64
	if r.cache != nil {
		if cachedVal, found := r.cache.Get(prev.ReqID); found {
			if cs, ok := cachedVal.(*CycleState); ok {
				vol24h = cs.Vol24hUSDT
			}
		}
	}

	evt := PositionClosedEvent{
		BaseReversionEvent: nextNotifyReversionBase(prev, pos.Symbol, r.deps.Clock.Now()),
		EntryPrice:         fillPrice,
		ClosePrice:         closePrice,
		CloseVol:           pos.CloseVol,
		Reason:             "exchange_push",
		GrossProfit:        pos.CloseProfitLoss,
		NetProfit:          pos.CloseProfitLoss - pos.Fee + pos.HoldFee,
		PnLPct:             calculatePnLPct(fillPrice, closePrice, side),
		VolumeUSDT:         pos.CloseVol * closePrice * contractSize,
		Fee:                pos.Fee,
		HoldFee:            pos.HoldFee,
		Method:             "watcher",
		Vol24hUSDT:         vol24h,
	}

	if provider, ok := r.deps.Client.(exchange.ClosedPnLProvider); ok {
		// Wait 5 seconds before calling GetRecentClosedPnL to let exchange update trade database
		_ = r.deps.Clock.Sleep(ctx, 30*time.Second)

		var orderID string
		var closedInfo *exchange.ClosedPnLInfo

		bo := backoff.WithContext(
			backoff.WithMaxRetries(
				backoff.NewExponentialBackOff(
					backoff.WithInitialInterval(2*time.Second),
					backoff.WithMaxInterval(time.Second*10),
					backoff.WithRandomizationFactor(0.5)),
				10),
			ctx,
		)

		err := backoff.Retry(func() error {
			var err error
			orderID, err = r.resolveOrderID(prev.ReqID, prev.OrderID)
			if err != nil {
				return err
			}
			closedInfo, err = provider.GetOrderPNL(ctx, pos.Symbol, orderID)
			return err
		}, bo)
		if err != nil {
			return nil, err
		}

		evt.OrderID = orderID
		evt.EntryPrice = closedInfo.EntryPrice
		evt.ClosePrice = closedInfo.ExitPrice
		evt.CloseVol = closedInfo.ClosedSize
		evt.GrossProfit = closedInfo.GrossPnL
		evt.Fee = closedInfo.Fee
		evt.HoldFee = closedInfo.FundingFee
		evt.PnLPct = closedInfo.PnLRate
		evt.NetProfit = closedInfo.NetPnl
		evt.VolumeUSDT = closedInfo.ClosedSize * closedInfo.ExitPrice * contractSize
		evt.HoldDurationMs = closedInfo.DurationMs
	}

	if evt.OrderID == "" {
		if resolved, err := r.resolveOrderID(prev.ReqID, prev.OrderID); err == nil {
			evt.OrderID = resolved
		}
	}

	return &evt, nil
}

func (r *StatelessRunner) resolveOrderID(reqID, orderID string) (string, error) {
	if orderID != "" {
		return orderID, nil
	}
	if r.cache != nil {
		if cachedVal, found := r.cache.Get(reqID); found {
			if state, ok := cachedVal.(*CycleState); ok {
				state.mu.Lock()
				resolved := state.IOCOrderID
				state.mu.Unlock()
				if resolved != "" {
					return resolved, nil
				}
			}
		}
	}
	return "", fmt.Errorf("order ID is empty and could not be resolved from cache for request %s", reqID)
}

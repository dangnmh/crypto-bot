package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/notifier"
	infrawatcher "crypto-bot/internal/infrastructure/watcher"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/formatutil"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/patrickmn/go-cache"
)

// Strategy implements strategy.BackgroundStrategy interface in a lightweight, stateless manner.
type Strategy struct {
	engine   *app.Engine
	global   *config.Config
	notifier notifier.Notifier
	log      *slog.Logger
	stores   map[string]strategy.FundingStoreSet
	cache    *cache.Cache

	// Test fallbacks
	clock         shared.Clock
	orderNotifier infrawatcher.OrderNotifier
	wsSub         infraws.ExchangeManagerAdapterSubscriber
}

func NewStrategy(
	engine *app.Engine,
	global *config.Config,
	n notifier.Notifier,
	c *cache.Cache,
	log *slog.Logger,
) *Strategy {
	logger := log.With("flow", FlowIDFundingReversion)
	return &Strategy{
		engine:   engine,
		global:   global,
		notifier: n,
		cache:    c,
		log:      logger,
	}
}

var _ strategy.BackgroundStrategy = (*Strategy)(nil)

func (s *Strategy) Flow() string {
	return FlowIDFundingReversion
}

func (s *Strategy) Enabled(cfg config.SymbolConfig) bool {
	return cfg.FundingReversion.Enabled
}

func (s *Strategy) Start(ctx context.Context, stores map[string]strategy.FundingStoreSet) error {
	s.stores = stores

	runner := &StatelessRunner{
		globalCfg: s.global,
		bus:       s.engine.Bus,
		log:       s.log,
		engine:    s.engine,
		stores:    s.stores,
		notifier:  s.notifier,
		cache:     s.cache,
		// Pass test fallbacks
		clock:         s.clock,
		orderNotifier: s.orderNotifier,
		wsSub:         s.wsSub,
	}

	InitGlobalSubscriptions(ctx, runner)
	return nil
}

func (s *Strategy) SetTestFallbacks(clock shared.Clock, orderNotifier infrawatcher.OrderNotifier, wsSub infraws.ExchangeManagerAdapterSubscriber) {
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

	engine   *app.Engine
	stores   map[string]strategy.FundingStoreSet
	notifier notifier.Notifier
	cache    *cache.Cache

	// Target context to resolve configuration conflicts across multiple exchanges
	exchange string
	symbol   string

	// Test fallbacks
	clock         shared.Clock
	orderNotifier infrawatcher.OrderNotifier
	wsSub         infraws.ExchangeManagerAdapterSubscriber
}

func (r *StatelessRunner) clone(exch, reqID, symbol string) *StatelessRunner {
	local := *r
	local.exchange = exch
	local.symbol = symbol
	clonedLog := r.log.With("exchange", exch, "req", reqID, "symbol", symbol)
	local.log = clonedLog

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

	var orderNotifier = prov.Watcher
	if r.orderNotifier != nil {
		orderNotifier = r.orderNotifier
	}

	var wsSub infraws.ExchangeManagerAdapterSubscriber = prov.Adapter
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
	return &local
}

func (r *StatelessRunner) publishEvent(ctx context.Context, topic string, payload any) error {
	payload = stampEventTrace(topic, payload)
	r.log.InfoContext(ctx, "Reversion: Publishing event", slog.String("topic", topic), slog.Any("payload", payload))

	if err := r.bus.Publish(topic, payload); err != nil {
		r.log.ErrorContext(ctx, "Failed to publish event", slog.String("topic", topic), slog.Any("error", err))
		return err
	}

	if topic == TopicReversionArmed || topic == TopicReversionAbort {
		if revEvt, ok := payload.(ReversionEvent); ok {
			msg := formatReversionNotification(topic, revEvt)
			if err := r.deps.Notifier.Send(ctx, notifier.Event{
				Level:     notifier.LevelNormal,
				Message:   msg,
				Timestamp: r.deps.Clock.Now(),
			}); err != nil {
				r.log.ErrorContext(ctx, "Failed to send notification", slog.Any("error", err))
			}
		}
	}

	return nil
}

const (
	sideDisplayLong  = "Long"
	sideDisplayShort = "Short"

	statusCandidate = "CANDIDATE"
	statusAborted   = "ABORTED"
)

var topicStatusTags = map[string]string{
	TopicReversionArmed: statusCandidate,
	TopicReversionAbort: statusAborted,
}

func topicToStatusTag(topic string) string {
	if tag, ok := topicStatusTags[topic]; ok {
		return tag
	}
	parts := strings.Split(topic, ".")
	if len(parts) > 0 {
		return strings.ToUpper(parts[len(parts)-1])
	}
	return "REVERSION"
}

func formatSide(side shared.Side) string {
	switch side {
	case shared.SideOpenLong, shared.SideCloseLong:
		return sideDisplayLong
	case shared.SideOpenShort, shared.SideCloseShort:
		return sideDisplayShort
	default:
		return ""
	}
}

func formatPrice(price float64) string {
	if price == 0 {
		return "0"
	}
	return formatutil.FormatPriceWithCommas(price)
}

func formatFR(rate float64) string {
	sign := ""
	if rate > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.1f%%", sign, rate*100)
}

func formatCandidateNotification(
	header, symbol string, side shared.Side, cfg domain.TradeConfig,
	price, volume, fundingRate, vol24h float64,
	orderID, extID, reqID, note string,
) string {
	var lines []string
	lines = append(lines, header)

	sideStr := formatSide(side)
	if sideStr != "" {
		lines = append(lines, fmt.Sprintf("• Symbol: %s | Side: %s", symbol, sideStr))
	} else {
		lines = append(lines, fmt.Sprintf("• Symbol: %s", symbol))
	}

	if cfg.MarginUSDT > 0 || cfg.Leverage > 0 {
		lines = append(lines, fmt.Sprintf("• Margin: %.2f USDT | Leverage: %dx", cfg.MarginUSDT, cfg.Leverage))
	}

	sizeUSDT := price * volume
	if sizeUSDT > 0 {
		lines = append(lines, fmt.Sprintf("• Price: %s | Size: %.2f USDT", formatPrice(price), sizeUSDT))
	} else if price > 0 {
		lines = append(lines, fmt.Sprintf("• Price: %s", formatPrice(price)))
	}

	frStr := formatFR(fundingRate)
	volStr := formatutil.FormatCompactUSD(vol24h)
	lines = append(lines, fmt.Sprintf("• FR: %s | Vol24h: $%s", frStr, volStr))

	if note != "" {
		lines = append(lines, fmt.Sprintf("• Note: %s", note))
	}
	if orderID != "" {
		lines = append(lines, fmt.Sprintf("• Order ID: %s", orderID))
	}
	if extID != "" {
		lines = append(lines, fmt.Sprintf("• Client ID: %s", extID))
	}
	if reqID != "" {
		lines = append(lines, fmt.Sprintf("• Req ID: %s", reqID))
	}
	return strings.Join(lines, "\n")
}

func formatAbortNotification(header string, e AbortEvent) string {
	lines := []string{header, fmt.Sprintf("• Symbol: %s", e.Symbol)}
	if e.Reason != "" {
		lines = append(lines, fmt.Sprintf("• Reason: %s", e.Reason))
	}
	if e.OrderID != "" {
		lines = append(lines, fmt.Sprintf("• Order ID: %s", e.OrderID))
	}
	if e.ExternalID != "" {
		lines = append(lines, fmt.Sprintf("• Client ID: %s", e.ExternalID))
	}
	if e.ReqID != "" {
		lines = append(lines, fmt.Sprintf("• Req ID: %s", e.ReqID))
	}
	return strings.Join(lines, "\n")
}

func formatDefaultNotification(header string, revEvt ReversionEvent) string {
	lines := []string{header, fmt.Sprintf("• Symbol: %s", revEvt.GetSymbol())}
	if revEvt.GetOrderID() != "" {
		lines = append(lines, fmt.Sprintf("• Order ID: %s", revEvt.GetOrderID()))
	}
	if revEvt.GetExternalID() != "" {
		lines = append(lines, fmt.Sprintf("• Client ID: %s", revEvt.GetExternalID()))
	}
	if revEvt.GetReqID() != "" {
		lines = append(lines, fmt.Sprintf("• Req ID: %s", revEvt.GetReqID()))
	}
	return strings.Join(lines, "\n")
}

func formatReversionNotification(topic string, revEvt ReversionEvent) string {
	emoji := "🟡"
	if topic == TopicReversionAbort {
		emoji = "🔴"
	}
	exch := strings.ToLower(revEvt.GetExchange())
	if exch == "" {
		exch = "unknown"
	}
	status := topicToStatusTag(topic)
	header := fmt.Sprintf("%s [FUNDING_REVERSION] [%s] [%s]", emoji, exch, status)

	switch e := revEvt.(type) {
	case CandidateFoundEvent:
		side := e.Candidate.Side
		if side == shared.SideUnknown {
			side = e.Side
		}
		return formatCandidateNotification(
			header, e.Symbol, side, e.Candidate.Config,
			e.Candidate.LastPrice, e.Candidate.Volume, e.Candidate.FundingRate, e.Candidate.Vol24USDT,
			e.OrderID, e.ExternalID, e.ReqID, "",
		)

	case ArmMarketReadyEvent:
		side := e.Candidate.Side
		if side == shared.SideUnknown {
			side = e.Side
		}
		return formatCandidateNotification(
			header, e.Symbol, side, e.Candidate.Config,
			e.Candidate.LastPrice, e.Candidate.Volume, e.Candidate.FundingRate, e.Candidate.Vol24USDT,
			e.OrderID, e.ExternalID, e.ReqID, "",
		)

	case ArmedEvent:
		side := e.Candidate.Side
		if side == shared.SideUnknown {
			side = e.Side
		}
		return formatCandidateNotification(
			header, e.Symbol, side, e.Candidate.Config,
			e.Candidate.LastPrice, e.Candidate.Volume, e.Candidate.FundingRate, e.Candidate.Vol24USDT,
			e.OrderID, e.ExternalID, e.ReqID, "",
		)

	case AbortEvent:
		return formatAbortNotification(header, e)

	default:
		return formatDefaultNotification(header, revEvt)
	}
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
		Flow:          FlowIDFundingReversion,
		ReqID:         prev.ReqID,
		Symbol:        symbol,
		Exchange:      prev.Exchange,
		OrderID:       prev.OrderID,
		ExternalID:    prev.ExternalID,
		Timestamp:     timestamp,
		Seq:           seq,
		PreviousTopic: prev.Topic,
		SettleTime:    prev.SettleTime,
		Side:          prev.Side,
		FundingRate:   prev.FundingRate,
		Vol24hUSDT:    prev.Vol24hUSDT,
		ContractSize:  prev.ContractSize,
	}
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
	if r.deps.WsSub == nil {
		return nil
	}
	return r.deps.WsSub.SubscribeTicker(ctx, FlowIDFundingReversion, symbol)
}

func (r *StatelessRunner) unsubscribeWS(ctx context.Context, symbol string) {
	if r.deps.WsSub == nil {
		return
	}
	if err := r.deps.WsSub.UnsubscribeTicker(ctx, FlowIDFundingReversion, symbol); err != nil {
		r.log.WarnContext(ctx, "⚠️ Failed to unsubscribe ticker", slog.String("symbol", symbol), slog.Any("error", err))
	}
}

func (r *StatelessRunner) refreshPrice(ctx context.Context, c *domain.Candidate) error {
	pd, err := r.deps.PriceStore.GetPrice(ctx, c.Symbol, 5*time.Second)
	if err == nil && pd != nil && pd.BestBid > 0 && pd.BestAsk > 0 {
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

func (r *StatelessRunner) abortAfter(ctx context.Context, prev BaseReversionEvent, symbol, reason string) {
	evt := AbortEvent{
		BaseReversionEvent: nextReversionBase(prev, symbol, r.deps.Clock.Now()),
		Reason:             reason,
	}
	_ = r.publishEvent(ctx, TopicReversionAbort, evt)
}

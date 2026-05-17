package cycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/eventbus"

	"github.com/ThreeDotsLabs/watermill/message"
)

// Deps holds all external dependencies for a funding cycle.
type Deps struct {
	Client        exchange.Client
	WsSub         ws.Subscriber
	OrderNotifier watcher.OrderNotifier
	TickerStore   store.TickerReader
	ContractStore store.ContractReader
	PriceStore    store.PriceReader
	FundingStore  store.FundingReader
	KlineStore    store.KlineReadWriter
	DepthStore    store.DepthReader
	Clock         shared.Clock
	Log           *slog.Logger
	CycleRecorder domain.CycleRecorder
}

// Runtime owns shared per-cycle state and dependencies used by flow packages.
type Runtime struct {
	cfg    config.SymbolConfig
	global *config.Config
	deps   Deps
	subs   *SubscriptionManager

	bus       *eventbus.Bus
	mu        sync.Mutex
	candidate domain.Candidate
	results   []any
	recorder  *domain.CycleRecordBuilder

	excursionCancel context.CancelFunc
}

func NewRuntime(cfg config.SymbolConfig, global *config.Config, deps Deps) *Runtime {
	return &Runtime{
		cfg:    cfg,
		global: global,
		deps:   deps,
		subs: NewSubscriptionManager(
			deps.WsSub,
			cfg.Symbol,
			ToTradeConfig(cfg).FundingReversion.DynamicPricing,
			ToTradeConfig(cfg).FundingReversion.ImbalanceFilter,
			deps.Log,
		),
	}
}

func (r *Runtime) Begin(reqID string, settle time.Time, log *slog.Logger) {
	r.deps.Log = log
	r.recorder = domain.NewCycleRecordBuilder(reqID, settle)
	r.bus = eventbus.New(log)
}

func (r *Runtime) CloseBus() error {
	if r.bus == nil {
		return nil
	}
	return r.bus.Close()
}

func (r *Runtime) DumpTimeline(log *slog.Logger) {
	if r.bus != nil {
		r.bus.DumpTimeline(log)
	}
}

func (r *Runtime) Config() config.SymbolConfig {
	return r.cfg
}

func (r *Runtime) Global() *config.Config {
	return r.global
}

func (r *Runtime) Deps() *Deps {
	return &r.deps
}

func (r *Runtime) Log() *slog.Logger {
	return r.deps.Log
}

func (r *Runtime) Recorder() *domain.CycleRecordBuilder {
	return r.recorder
}

func (r *Runtime) MutateRecorder(fn func(*domain.CycleRecordBuilder)) {
	if r.recorder == nil {
		return
	}
	r.recorder.Mutate(fn)
}

func (r *Runtime) Subscribe(ctx context.Context, topic string, handler func(*message.Message)) {
	msgs, err := r.bus.Subscribe(ctx, topic)
	if err != nil {
		r.deps.Log.Error("Failed to subscribe to topic", slog.String("topic", topic), slog.Any("error", err))
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				msg.Ack()
				handler(msg)
			}
		}
	}()
}

func (r *Runtime) Publish(topic string, payload any) {
	if err := r.bus.Publish(topic, payload); err != nil {
		r.deps.Log.Error("🔴 Publish failed", slog.String("topic", topic), slog.Any("error", err))
	}
}

func (r *Runtime) PublishStart(settle time.Time) error {
	return r.bus.Publish(events.TopicScanStart, events.CycleStartEvent{
		Symbol:     r.cfg.Symbol,
		SettleTime: settle,
	})
}

func (r *Runtime) Abort(source, reason string) {
	if source == "scan" {
		r.Publish(events.TopicScanAbort, events.CycleAbortEvent{
			Symbol: r.cfg.Symbol,
			Reason: reason,
		})
	}
	r.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.AbortReason = reason
		b.AbortFlow = events.FlowReversion
		b.AbortTopic = events.TopicReversionAbort
	})
	r.Publish(events.TopicReversionAbort, events.CycleAbortEvent{
		Flow:   events.FlowReversion,
		Symbol: r.cfg.Symbol,
		Reason: reason,
	})
}

func (r *Runtime) CandidateCopy() domain.Candidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.candidate
}

func (r *Runtime) SetCandidate(c domain.Candidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.candidate = c
}

func (r *Runtime) UpdateCandidate(fn func(*domain.Candidate)) domain.Candidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.candidate)
	return r.candidate
}

func (r *Runtime) AppendResult(result any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, result)
}

func (r *Runtime) SubscribeAll(ctx context.Context) error {
	return r.subs.SubscribeAll(ctx)
}

func (r *Runtime) UnsubscribeAll(ctx context.Context) {
	r.subs.UnsubscribeAll(ctx)
}

func (r *Runtime) BuildCandidate(td *store.TickerData) domain.Candidate {
	intent := domain.TradeIntent{
		Symbol:      td.Symbol,
		FundingRate: td.FundingRate,
	}
	if td.FundingRate > 0 {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenLong, shared.SideCloseLong, "bestAsk"
	} else {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenShort, shared.SideCloseShort, "bestBid"
	}

	return domain.Candidate{
		Config:      ToTradeConfig(r.cfg),
		TradeIntent: intent,
		MarketData: domain.MarketData{
			LastPrice: td.LastPrice,
			BestBid:   td.BestBid,
			BestAsk:   td.BestAsk,
			Volume24:  td.Volume24,
			Amount24:  td.Amount24,
		},
	}
}

func (r *Runtime) Enrich(ctx context.Context, c *domain.Candidate) bool {
	cd, err := r.deps.ContractStore.GetContract(ctx, c.Symbol)
	if err != nil {
		r.deps.Log.Warn("🟡 No contract data — skip")
		return false
	}
	c.ContractSpec = domain.ContractSpec{
		PriceUnit:    cd.PriceUnit,
		VolUnit:      cd.VolUnit,
		MinVol:       cd.MinVol,
		PriceScale:   cd.PriceScale,
		VolScale:     cd.VolScale,
		ContractSize: cd.ContractSize,
		TakerFeeRate: cd.TakerFeeRate,
		MakerFeeRate: cd.MakerFeeRate,
	}
	return true
}

func (r *Runtime) RefreshPrice(ctx context.Context, c *domain.Candidate) error {
	pd, err := r.deps.PriceStore.GetPrice(ctx, c.Symbol, 5*time.Second)
	if err == nil {
		c.BestBid, c.BestAsk, c.LastPrice = pd.BestBid, pd.BestAsk, pd.LastPrice
	}
	return err
}

func (r *Runtime) InitKlines(ctx context.Context) {
	klines := r.deps.KlineStore.GetKlines(ctx, r.cfg.Symbol)
	if len(klines) >= 14 {
		return
	}
	r.deps.Log.Info("📊 Fetching initial 1-minute klines via REST")
	apiKlines, err := r.deps.Client.GetKlines(ctx, r.cfg.Symbol, exchange.IntervalMin1, 0, 0)
	if err != nil {
		r.deps.Log.Warn("🟡 Failed to fetch initial klines", slog.Any("error", err))
		return
	}
	if len(apiKlines) > 20 {
		apiKlines = apiKlines[len(apiKlines)-20:]
	}
	r.deps.KlineStore.InitKlines(r.cfg.Symbol, 20, apiKlines)
}

func (r *Runtime) WaitUntil(ctx context.Context, target time.Time) {
	if d := r.deps.Clock.Until(target); d > 0 {
		r.deps.Log.Debug("⏱️ wait", slog.Time("target", target), slog.Duration("wait", d))
		_ = r.deps.Clock.Sleep(ctx, d)
	}
}

func (r *Runtime) Sleep(ctx context.Context, d time.Duration) bool {
	err := r.deps.Clock.Sleep(ctx, d)
	return err == nil
}

func (r *Runtime) StartExcursionPriceStream(ctx context.Context) {
	if r.deps.PriceStore == nil || r.excursionCancel != nil {
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	r.excursionCancel = cancel
	updates := r.deps.PriceStore.SubscribePrice(streamCtx, r.cfg.Symbol, 32)

	go func() {
		for pd := range updates {
			if pd == nil || pd.LastPrice <= 0 {
				continue
			}
			r.MutateRecorder(func(b *domain.CycleRecordBuilder) {
				if b.IOCExcursion != nil {
					b.IOCExcursion.Update(pd.LastPrice, pd.UpdatedAt)
					b.Excursion = b.IOCExcursion
				}
				if b.TrapExcursion != nil {
					b.TrapExcursion.Update(pd.LastPrice, pd.UpdatedAt)
				}
			})
		}
	}()
}

func (r *Runtime) StopExcursionPriceStream() {
	if r.excursionCancel != nil {
		r.excursionCancel()
		r.excursionCancel = nil
	}
}

func (r *Runtime) FinalizeExcursion(ctx context.Context) bool {
	if r.recorder == nil || r.recorder.Excursion == nil {
		return false
	}
	if pd, err := r.deps.PriceStore.GetPrice(ctx, r.cfg.Symbol, 2*time.Second); err == nil {
		r.recorder.Excursion.Update(pd.LastPrice, time.Now())
		return true
	}
	return false
}

func (r *Runtime) CycleRiskAllowsReversion(c domain.Candidate) error {
	return r.checkCycleRisk(c.ReversionNotionalUSDT(), 0, false)
}

func (r *Runtime) CycleRiskAllowsTrap(c domain.Candidate, trapNotional float64) error {
	return r.checkCycleRisk(c.ReversionNotionalUSDT(), trapNotional, true)
}

func (r *Runtime) checkCycleRisk(reversionNotional, trapNotional float64, includeTrap bool) error {
	safety := r.global.System.Safety

	cycleNotional := reversionNotional
	if includeTrap {
		cycleNotional = decmath.Add(cycleNotional, trapNotional)
	}
	if safety.MaxCycleNotionalUSDT > 0 && cycleNotional > safety.MaxCycleNotionalUSDT {
		return fmt.Errorf("cycle notional %.4f exceeds max %.4f", cycleNotional, safety.MaxCycleNotionalUSDT)
	}

	reversionLoss := decmath.Mul(reversionNotional, r.cfg.FundingReversion.StopLossPct)
	cycleLoss := reversionLoss
	if includeTrap {
		cycleLoss = decmath.Add(cycleLoss, decmath.Mul(trapNotional, r.cfg.FundingTrap.StopLossPct))
	}
	if safety.MaxCycleLossUSDT > 0 && cycleLoss > safety.MaxCycleLossUSDT {
		return fmt.Errorf("cycle loss %.4f exceeds max %.4f", cycleLoss, safety.MaxCycleLossUSDT)
	}

	return nil
}

func (r *Runtime) PersistCycleRecord(ctx context.Context) {
	if r.recorder == nil || r.deps.CycleRecorder == nil || r.bus == nil {
		return
	}

	tl := r.bus.Timeline()
	entries := make([]domain.TimelineEntry, 0, len(tl))
	for _, e := range tl {
		entries = append(entries, domain.TimelineEntry{
			Time:    e.Time,
			Topic:   e.Topic,
			MsgID:   e.MsgID,
			Payload: e.Payload,
		})
	}

	cfgJSON, _ := json.Marshal(ToTradeConfig(r.cfg))
	record := r.recorder.Build(
		r.cfg.Symbol,
		r.cfg.FundingReversion.TakeProfitPct,
		r.cfg.FundingReversion.StopLossPct,
		cfgJSON,
		entries,
	)
	if err := r.deps.CycleRecorder.Record(ctx, record); err != nil {
		r.deps.Log.Error("🔴 Failed to persist cycle record", slog.Any("error", err))
	} else {
		r.deps.Log.Info("📝 Cycle record persisted", slog.String("outcome", string(record.Outcome)))
	}
}

func CalcSpreadPct(bestBid, bestAsk float64) float64 {
	if bestBid <= 0 {
		return 0
	}
	return decmath.Mul(decmath.Div(decmath.Sub(bestAsk, bestBid), bestBid), 100.0)
}

func ToTradeConfig(sc config.SymbolConfig) domain.TradeConfig {
	return domain.TradeConfig{
		Symbol:              sc.Symbol,
		SimulateSettle:      sc.SimulateSettle,
		MaxPriceDiffPercent: sc.MaxPriceDiffPercent,
		MarginUSDT:          sc.MarginUSDT,
		Leverage:            sc.Leverage,
		FundingReversion:    sc.FundingReversion,
		FundingTrap:         sc.FundingTrap,
		ParsedOpenType:      sc.ParsedOpenType,
		ParsedPositionMode:  sc.ParsedPositionMode,
	}
}

func Unmarshal[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("unmarshal %T: %w", v, err)
	}
	return v, nil
}

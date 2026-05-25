package application

import (
	"context"
	"log/slog"
	"math"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
	applogger "crypto-bot/pkg/logger"
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
	DepthStore    store.DepthReader
	Clock         shared.Clock
	Log           *slog.Logger
	Notifier      notifier.Notifier
	EventBus      *eventbus.Bus
}

// OrderRef is the flow-agnostic order metadata needed to match fills and closes.
type OrderRef struct {
	Flow      string
	Symbol    string
	OrderID   string
	Side      shared.Side
	CloseSide shared.Side
	OrderType int
	Price     float64
	Volume    float64
	TPPrice   float64
	SLPrice   float64
}

// FillInfo stores generic fill details recorded in a cycle.
type FillInfo struct {
	Flow      string
	Symbol    string
	OrderID   string
	Side      shared.Side
	CloseSide shared.Side
	FillPrice float64
	FillVol   float64
	Fee       float64
	Profit    float64
	HoldFee   float64
	TPPrice   float64
	SLPrice   float64
}

// Orchestrator manages one funding cycle sequentially.
type Orchestrator struct {
	log    *slog.Logger
	cfg    config.SymbolConfig
	global *config.Config
	deps   Deps

	strategies *strategy.Registry
}

// NewOrchestrator creates a new Orchestrator using injected strategies.
func NewOrchestrator(
	cfg config.SymbolConfig,
	global *config.Config,
	deps Deps,
	strategies ...strategy.Strategy,
) *Orchestrator {
	return &Orchestrator{
		log:        deps.Log,
		cfg:        cfg,
		global:     global,
		deps:       deps,
		strategies: strategy.NewRegistry(strategies...),
	}
}

func (o *Orchestrator) Run(ctx context.Context, settle time.Time) {
	if observability.CorrelationID(ctx) == "" {
		ctx = observability.WithCorrelationID(ctx)
	}
	log := applogger.WithCtx(ctx, o.log)

	log.Info("━━━ Run start ━━━", slog.Time("settle", settle))

	candidate, ok := o.doScan(ctx)
	if !ok {
		log.Info("━━━ Run end (No candidate) ━━━")
		return
	}

	err := o.strategies.ExecuteAll(ctx, settle, o.cfg, candidate)

	if err != nil {
		log.Error("🔴 Run execution error", slog.Any("error", err))
		o.strategies.CleanupOpenExposure(ctx, o.cfg)
	}

	log.Info("━━━ Run end ━━━")
}

func (o *Orchestrator) doScan(ctx context.Context) (domain.Candidate, bool) {
	if o.deps.TickerStore == nil {
		applogger.WithCtx(ctx, o.log).Warn("No ticker store")
		return domain.Candidate{}, false
	}
	td, err := o.deps.TickerStore.GetTicker(ctx, o.cfg.Symbol)
	if err != nil {
		applogger.WithCtx(ctx, o.log).Warn("No ticker", slog.Any("error", err))
		return domain.Candidate{}, false
	}

	if math.Abs(td.FundingRate) < o.cfg.MinFundingRate {
		applogger.WithCtx(ctx, o.log).Info("FR below threshold", slog.Float64("fr", td.FundingRate*100))
		return domain.Candidate{}, false
	}

	if minVol24USD := o.global.System.Safety.MinVol24USD; minVol24USD > 0 && td.Amount24 < minVol24USD {
		applogger.WithCtx(ctx, o.log).Info("24h volume below threshold",
			slog.Float64("amount24_usd", td.Amount24),
			slog.Float64("minVol24USD", minVol24USD),
		)
		return domain.Candidate{}, false
	}

	candidate := o.BuildCandidate(td)
	if !o.Enrich(ctx, &candidate) {
		return domain.Candidate{}, false
	}

	return candidate, true
}

func (o *Orchestrator) BuildCandidate(td *store.TickerData) domain.Candidate {
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
		Config:      ToTradeConfig(o.cfg),
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

func (o *Orchestrator) Enrich(ctx context.Context, c *domain.Candidate) bool {
	cd, err := o.deps.ContractStore.GetContract(ctx, c.Symbol)
	if err != nil {
		applogger.WithCtx(ctx, o.log).Warn("🟡 No contract data — skip")
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

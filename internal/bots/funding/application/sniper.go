package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	infraws "crypto-bot/internal/infrastructure/ws"
	pkgws "crypto-bot/pkg/ws"

	"github.com/samber/lo"
)

// CloseResult holds the outcome of closing a single position.
type CloseResult struct {
	Symbol      string
	ExitOrderID string
	ExitPrice   float64
	ExitVol     float64
	Closed      bool
	TakerFee    float64
	MakerFee    float64
	Profit      float64
}

// ──────────────────────────────────────────────────────────────────────
// Sniper — top-level orchestrator.
// ──────────────────────────────────────────────────────────────────────.

// ReversionStrategyFactory creates a new reversion strategy instance.
type ReversionStrategyFactory func(config.SymbolConfig, *config.Config, Deps) strategy.Strategy

// TrapStrategyFactory creates a new trap strategy instance.
type TrapStrategyFactory func(config.SymbolConfig, *config.Config, Deps) strategy.Strategy

// TrailingStrategyFactory creates a new trailing strategy instance.
type TrailingStrategyFactory func(config.SymbolConfig, *config.Config, Deps) strategy.Strategy

// SubscriptionInitializer represents a function that initializes global subscriptions for a strategy.
type SubscriptionInitializer func(ctx context.Context, deps Deps, cfg *config.Config)

type fundingStoreSet interface {
	Start(ctx context.Context)
	WaitReady(ctx context.Context) error
	WireWS(pool *pkgws.Pool, adapter infraws.ExchangeAdapter)
	Ticker() store.TickerReader
	Contract() store.ContractReader
	Price() store.PriceReader
	Funding() store.FundingReader
	Depth() store.DepthReader
	Kline() store.KlineReadWriter
}

// Sniper spawns one independent worker goroutine per configured symbol.
type Sniper struct {
	cfg              *config.Config
	sysCfg           *config.SystemConfig
	engine           *app.Engine
	orderNotifiers   map[string]*watcher.OrderWatcher
	stores           map[string]fundingStoreSet
	notifier         notifier.Notifier
	disabled         map[string]string
	disabledMu       sync.RWMutex
	reversionFactory ReversionStrategyFactory
	trapFactory      TrapStrategyFactory
	trailingFactory  TrailingStrategyFactory
	initializers     []SubscriptionInitializer
	log              *slog.Logger
	bgWg             sync.WaitGroup // tracks background goroutines (§5.2)
}

type fundingScannerJob struct {
	sniper *Sniper
}

func newFundingScannerJob(sniper *Sniper) *fundingScannerJob {
	return &fundingScannerJob{sniper: sniper}
}

func (j *fundingScannerJob) Run(ctx context.Context) error {
	j.sniper.log.InfoContext(ctx, "🚀 Funding scanner job started", slog.Int("symbols", len(j.sniper.cfg.Symbols)))
	defer j.sniper.log.InfoContext(context.WithoutCancel(ctx), "🛑 Funding scanner job stopped")

	j.publishConfiguredSymbols(ctx)

	<-ctx.Done()
	return nil
}

func (j *fundingScannerJob) publishConfiguredSymbols(ctx context.Context) {
	for i := range j.sniper.cfg.Symbols {
		if ctx.Err() != nil {
			return
		}

		symCfg := j.sniper.cfg.Symbols[i]
		baseLog := j.sniper.log.With("sym", symCfg.Symbol, "exchange", symCfg.Exchange)
		j.sniper.publishCandidate(ctx, baseLog, symCfg)
	}
}

// NewSniper creates a new Sniper instance.
func NewSniper(
	cfg *config.Config,
	sysCfg *config.SystemConfig,
	engine *app.Engine,
	n notifier.Notifier,
	reversionFactory ReversionStrategyFactory,
	trapFactory TrapStrategyFactory,
	trailingFactory TrailingStrategyFactory,
	log *slog.Logger,
	initializers ...SubscriptionInitializer,
) *Sniper {
	orderWatchers := make(map[string]*watcher.OrderWatcher)
	for name, prov := range engine.Providers {
		orderWatchers[name] = prov.Watcher
	}

	// Build map of stores per active exchange
	storesMap := make(map[string]fundingStoreSet)

	for name, prov := range engine.Providers {
		var symbols []string
		for i := range cfg.Symbols {
			exch := cfg.Symbols[i].Exchange
			if exch == name {
				symbols = append(symbols, cfg.Symbols[i].Symbol)
			}
		}

		if len(symbols) > 0 {
			storesMap[name] = app.NewCentralStore(
				app.WithTicker(prov.Client, time.Duration(sysCfg.Sync.Ticker)),
				app.WithContract(prov.Client, time.Duration(sysCfg.Sync.Contract)),
				app.WithFunding(prov.Client, time.Duration(sysCfg.Sync.FundingSync), symbols),
				app.WithPrice(),
				app.WithDepth(),
				app.WithKline(),
			)
		}
	}

	return &Sniper{
		cfg:              cfg,
		sysCfg:           sysCfg,
		engine:           engine,
		orderNotifiers:   orderWatchers,
		stores:           storesMap,
		notifier:         n,
		disabled:         make(map[string]string),
		reversionFactory: reversionFactory,
		trapFactory:      trapFactory,
		trailingFactory:  trailingFactory,
		initializers:     initializers,
		log:              log,
	}
}

// RunAsBackground launches all required sync and connection routines for all active exchanges.
func (s *Sniper) RunAsBackground(ctx context.Context) error {
	for name, prov := range s.engine.Providers {
		stores, hasStore := s.stores[name]
		if !hasStore {
			continue
		}

		provLogger := s.log.With("exchange", name)
		provLogger.InfoContext(ctx, "🔗 Starting background services...")

		// 1. WarmUp + TimeSync.
		s.bgWg.Add(1)
		go func(p *app.ExchangeProvider) {
			defer s.bgWg.Done()
			p.Client.WarmUp(ctx, 4*time.Second)
		}(prov)

		s.bgWg.Add(1)
		go func(p *app.ExchangeProvider) {
			defer s.bgWg.Done()
			p.TimeSync.Start(ctx)
		}(prov)

		if err := prov.TimeSync.WaitReady(ctx); err != nil {
			return err
		}

		// 2. Start stores + wait for initial data.
		stores.Start(ctx)
		if err := stores.WaitReady(ctx); err != nil {
			return err
		}

		// 3. Connect WS + subscribe personal channels.
		s.bgWg.Add(1)
		go func(p *app.ExchangeProvider) {
			defer s.bgWg.Done()
			p.WS.Connect(ctx)
		}(prov)

		if err := prov.WS.WaitReady(ctx); err != nil {
			return err
		}

		// 4. Wire WS streams to stores (auto-routes ticker/depth/kline).
		stores.WireWS(prov.WS, prov.Adapter)
		s.wirePersonalWSForProvider(ctx, prov)

		if prov.Adapter != nil {
			if err := prov.Adapter.SubscribePersonal(ctx); err != nil {
				provLogger.WarnContext(ctx, "⚠️ Failed to subscribe personal channels", slog.Any("error", err))
			}
		}

		// 5. Initialize global event subscriptions for strategies.
		deps := Deps{
			Client:        prov.Client,
			WsSub:         prov.Adapter,
			OrderNotifier: prov.Watcher,
			TickerStore:   stores.Ticker(),
			ContractStore: stores.Contract(),
			PriceStore:    stores.Price(),
			FundingStore:  stores.Funding(),
			DepthStore:    stores.Depth(),
			Clock:         prov.TimeSync,
			Log:           provLogger,
			Notifier:      s.notifier,
			EventBus:      s.engine.Bus,
		}
		for _, init := range s.initializers {
			init(ctx, deps, s.cfg)
		}

		provLogger.InfoContext(ctx, "🟢 Exchange Background Services Ready")
	}

	s.log.InfoContext(ctx, "🟢 Funding Reversion Background Services Ready")
	return nil
}

func (s *Sniper) wirePersonalWSForProvider(ctx context.Context, prov *app.ExchangeProvider) {
	log := s.log.With("exchange", prov.Name)
	if prov.WS == nil || prov.Adapter == nil || prov.Watcher == nil {
		return
	}

	prov.WS.On("personal.position", func(data []byte) {
		update, err := prov.Adapter.ParsePosition(data)
		if err != nil {
			log.WarnContext(ctx, "🟡 Failed to parse personal position WS", slog.Any("error", err))
			return
		}
		if update != nil {
			prov.Watcher.PublishPosition(lo.FromPtr(update))
		}
	})
}

// Run starts the funding scanner job and keeps it alive until context is cancelled.
func (s *Sniper) Run(ctx context.Context) error {
	return newFundingScannerJob(s).Run(ctx)
}

// Stop implements the app.Bot interface. It executes any explicit teardown.
// The primary graceful shutdown is handled by the context passed to Run().
func (s *Sniper) Stop(ctx context.Context) error {
	s.bgWg.Wait()
	return nil
}

// publishCandidate builds one candidate event source message for a symbol.
func (s *Sniper) publishCandidate(
	ctx context.Context,
	baseLog *slog.Logger,
	symCfg config.SymbolConfig,
) bool {
	exchName := symCfg.Exchange
	prov, err := s.engine.GetProvider(exchName)
	if err != nil {
		s.log.ErrorContext(ctx, "🔴 Failed to locate exchange provider", slog.String("exchange", exchName), slog.Any("error", err))
		return false
	}

	stores := s.stores[exchName]
	if stores == nil {
		s.log.ErrorContext(ctx, "🔴 Failed to locate central store for exchange", slog.String("exchange", exchName))
		return false
	}

	if reason, disabled := s.disabledReason(symCfg.Symbol); disabled {
		baseLog.WarnContext(ctx, "🔴 Symbol disabled in-memory", slog.String("reason", reason))
		return false
	}

	settle, err := GetNextSettleTime(ctx, symCfg.SimulateSettle, symCfg.Symbol, stores.Funding())
	if err != nil {
		baseLog.ErrorContext(ctx, "🔴 No settle time", slog.Any("error", err))
		return false
	}

	if ctx.Err() != nil {
		return false
	}

	deps := Deps{
		Client:        prov.Client,
		WsSub:         prov.Adapter,
		OrderNotifier: prov.Watcher,
		TickerStore:   stores.Ticker(),
		ContractStore: stores.Contract(),
		PriceStore:    stores.Price(),
		FundingStore:  stores.Funding(),
		DepthStore:    stores.Depth(),
		Clock:         prov.TimeSync,
		Log:           baseLog,
		Notifier:      s.notifier,
		EventBus:      s.engine.Bus,
	}
	orchestrator := NewOrchestrator(
		symCfg,
		s.cfg,
		deps,
		s.reversionFactory(symCfg, s.cfg, deps),
		s.trapFactory(symCfg, s.cfg, deps),
		s.trailingFactory(symCfg, s.cfg, deps),
	)
	orchestrator.Run(ctx, settle)
	return true
}

func (s *Sniper) disabledReason(symbol string) (string, bool) {
	s.disabledMu.RLock()
	defer s.disabledMu.RUnlock()
	reason, ok := s.disabled[symbol]
	return reason, ok
}

package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	applogger "crypto-bot/pkg/logger"

	"golang.org/x/sync/errgroup"
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

// Sniper spawns one independent worker goroutine per configured symbol.
type Sniper struct {
	cfg              *config.Config
	sysCfg           *config.SystemConfig
	engine           *app.Engine
	client           exchange.Client
	ws               ws.Subscriber
	orderNotifiers   map[string]*watcher.OrderWatcher
	stores           map[string]*app.CentralStore
	timeSync         shared.Clock
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
	wsSub := engine.Adapter
	orderWatchers := make(map[string]*watcher.OrderWatcher)
	for name, prov := range engine.Providers {
		orderWatchers[name] = prov.Watcher
	}

	// Build map of stores per active exchange
	storesMap := make(map[string]*app.CentralStore)

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
		client:           engine.Client,
		ws:               wsSub,
		orderNotifiers:   orderWatchers,
		stores:           storesMap,
		timeSync:         engine.TimeSync,
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
	log := applogger.WithCtx(ctx, s.log)

	for name, prov := range s.engine.Providers {
		stores, hasStore := s.stores[name]
		if !hasStore {
			continue
		}

		provLogger := s.log.With("exchange", name)
		provCtxLog := applogger.WithCtx(ctx, provLogger)

		provCtxLog.Info("🔗 Starting background services...")

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

		prov.TimeSync.WaitReady(ctx)

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

		prov.WS.WaitReady(ctx)

		// 4. Wire WS streams to stores (auto-routes ticker/depth/kline).
		stores.WireWS(prov.WS, prov.Adapter)
		s.wirePersonalWSForProvider(ctx, prov)

		if prov.Adapter != nil {
			if err := prov.Adapter.SubscribePersonal(ctx); err != nil {
				provCtxLog.Warn("⚠️ Failed to subscribe personal channels", slog.Any("error", err))
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

		provCtxLog.Info("🟢 Exchange Background Services Ready")
	}

	log.Info("🟢 Funding Reversion Background Services Ready")
	return nil
}

func (s *Sniper) wirePersonalWSForProvider(ctx context.Context, prov *app.ExchangeProvider) {
	log := applogger.WithCtx(ctx, s.log.With("exchange", prov.Name))
	if prov.WS == nil || prov.Adapter == nil || prov.Watcher == nil {
		return
	}

	prov.WS.On("personal.position", func(data []byte) {
		update, err := prov.Adapter.ParsePosition(data)
		if err != nil {
			log.Warn("🟡 Failed to parse personal position WS", slog.Any("error", err))
			return
		}
		if update != nil {
			prov.Watcher.PublishPosition(*update)
		}
	})
}

// Run starts all symbol workers. Blocks until all stop or context is cancelled.
func (s *Sniper) Run(ctx context.Context) error {
	log := applogger.WithCtx(ctx, s.log)
	log.Info("🚀 Sniper — launching per-symbol workers", slog.Int("symbols", len(s.cfg.Symbols)))

	g, workerCtx := errgroup.WithContext(ctx)
	for i := range s.cfg.Symbols {
		g.Go(s.spawnWorker(workerCtx, s.cfg.Symbols[i]))
	}

	err := g.Wait()
	log.Info("🛑 All workers stopped")
	return err
}

// Stop implements the app.Bot interface. It executes any explicit teardown.
// The primary graceful shutdown is handled by the context passed to Run().
func (s *Sniper) Stop(ctx context.Context) error {
	applogger.WithCtx(ctx, s.log).Info("🛑 Sniper explicit stop invoked")
	s.bgWg.Wait()
	return nil
}

// spawnWorker creates a closure that runs one complete cycle for a symbol.
func (s *Sniper) spawnWorker(ctx context.Context, symCfg config.SymbolConfig) func() error {
	return func() error {
		exchName := symCfg.Exchange
		prov, err := s.engine.GetProvider(exchName)
		if err != nil {
			s.log.Error("🔴 Failed to locate exchange provider", slog.String("exchange", exchName), slog.Any("error", err))
			return nil
		}

		stores := s.stores[exchName]
		if stores == nil {
			s.log.Error("🔴 Failed to locate central store for exchange", slog.String("exchange", exchName))
			return nil
		}

		baseLog := s.log.With("sym", symCfg.Symbol, "exchange", exchName)
		log := applogger.WithCtx(ctx, baseLog)
		log.Info("🚀 Worker started")
		defer log.Info("🛑 Worker stopped")

		if reason, disabled := s.disabledReason(symCfg.Symbol); disabled {
			log.Warn("🔴 Symbol disabled in-memory", slog.String("reason", reason))
			return nil
		}

		settle, err := GetNextSettleTime(ctx, symCfg.SimulateSettle, symCfg.Symbol, stores.Funding())
		if err != nil {
			log.Error("🔴 No settle time", slog.Any("error", err))
			return nil
		}

		// Wait until T-5m before actively entering the cycle.
		if d := prov.TimeSync.Until(settle.Add(-5 * time.Minute)); d > 0 {
			log.Debug("😴 Waiting for funding window", slog.Time("settle", settle), slog.Duration("wait", d))
			if err := prov.TimeSync.Sleep(ctx, d); err != nil {
				return nil
			}
		}

		// If we are already past the firing deadline (T-5s), skip.
		if prov.TimeSync.Until(settle.Add(-5*time.Second)) <= 0 {
			log.Warn("🔴 Settle time passed or missed", slog.Time("settle", settle))
			return nil
		}

		// Execute one funding cycle via event-driven orchestrator.
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
		return nil
	}
}

func (s *Sniper) disabledReason(symbol string) (string, bool) {
	s.disabledMu.RLock()
	defer s.disabledMu.RUnlock()
	reason, ok := s.disabled[symbol]
	return reason, ok
}

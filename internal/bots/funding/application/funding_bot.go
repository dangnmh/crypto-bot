package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/pkg/version"
)

// FundingBot spawns one independent worker goroutine per configured symbol.
type FundingBot struct {
	cfg            *config.Config
	sysCfg         *config.SystemConfig
	engine         *app.Engine
	orderNotifiers map[string]watcher.OrderNotifier
	stores         map[string]strategy.FundingStoreSet
	notifier       notifier.Notifier
	disabled       map[string]string
	disabledMu     sync.RWMutex
	strategies     []strategy.BackgroundStrategy
	log            *slog.Logger
	bgWg           sync.WaitGroup
}

func buildOrderWatchers(engine *app.Engine) map[string]watcher.OrderNotifier {
	orderWatchers := make(map[string]watcher.OrderNotifier)
	if engine == nil {
		return orderWatchers
	}
	for name, prov := range engine.Providers {
		orderWatchers[name] = prov.Watcher
	}
	for id, accProv := range engine.AccountProviders {
		orderWatchers[id] = accProv.Watcher
	}
	return orderWatchers
}

type exchangeStoreSettings struct {
	scheduleEnabled     bool
	tickerDuration      time.Duration
	contractDuration    time.Duration
	fundingSyncDuration time.Duration
}

func minDuration(curr, next time.Duration) time.Duration {
	if next <= 0 {
		return curr
	}
	if curr <= 0 || next < curr {
		return next
	}
	return curr
}

func resolveExchangeStoreSettings(cfg *config.Config, exchangeName string) exchangeStoreSettings {
	var s exchangeStoreSettings
	if cfg == nil {
		return s
	}
	for _, acc := range cfg.Accounts {
		if acc == nil || !acc.Account.Enabled || !strings.EqualFold(acc.Account.Exchange, exchangeName) {
			continue
		}
		if acc.Account.Scanners.Schedule {
			s.scheduleEnabled = true
		}
		if acc.Reversion != nil {
			s.tickerDuration = minDuration(s.tickerDuration, time.Duration(acc.Reversion.Sync.Ticker))
			s.contractDuration = minDuration(s.contractDuration, time.Duration(acc.Reversion.Sync.Contract))
			s.fundingSyncDuration = minDuration(s.fundingSyncDuration, time.Duration(acc.Reversion.Sync.FundingSync))
		}
	}
	return s
}

func buildExchangeStores(cfg *config.Config, engine *app.Engine, log *slog.Logger) map[string]strategy.FundingStoreSet {
	storesMap := make(map[string]strategy.FundingStoreSet)
	if engine == nil {
		return storesMap
	}
	for name, prov := range engine.Providers {
		symbols := getActiveSymbols(cfg, name)
		settings := resolveExchangeStoreSettings(cfg, name)

		if len(symbols) > 0 || settings.scheduleEnabled {
			opts := []app.StoreOption{
				app.WithLogger(log.With("exchange", name)),
				app.WithTicker(prov.Client, settings.tickerDuration),
				app.WithContract(prov.Client, settings.contractDuration),
				app.WithPrice(),
				app.WithDepth(),
				app.WithKline(),
			}
			if len(symbols) > 0 {
				opts = append(opts, app.WithFunding(prov.Client, settings.fundingSyncDuration, symbols))
			}
			storesMap[name] = app.NewCentralStore(opts...)
		}
	}
	return storesMap
}

// NewFundingBot creates a new FundingBot instance.
func NewFundingBot(
	cfg *config.Config,
	sysCfg *config.SystemConfig,
	engine *app.Engine,
	n notifier.Notifier,
	strategies []strategy.BackgroundStrategy,
	log *slog.Logger,
) *FundingBot {
	return &FundingBot{
		cfg:            cfg,
		sysCfg:         sysCfg,
		engine:         engine,
		orderNotifiers: buildOrderWatchers(engine),
		stores:         buildExchangeStores(cfg, engine, log),
		notifier:       n,
		disabled:       make(map[string]string),
		strategies:     strategies,
		log:            log,
	}
}

func (s *FundingBot) startExchangeProvider(ctx context.Context, name string, prov *app.ExchangeProvider) error {
	provLogger := s.log.With("exchange", name)
	provLogger.InfoContext(ctx, "🔗 Starting background services...")

	// 1. WarmUp + TimeSync.
	s.bgWg.Add(1)
	go func(p *app.ExchangeProvider) {
		defer s.bgWg.Done()
		p.Client.WarmUp(ctx, 4*time.Second)
	}(prov)

	if runner, ok := prov.Client.(exchange.BackgroundTaskRunner); ok {
		runner.StartBackgroundTasks(ctx)
	}

	s.bgWg.Add(1)
	go func(p *app.ExchangeProvider) {
		defer s.bgWg.Done()
		p.TimeSync.Start(ctx)
	}(prov)

	if err := prov.TimeSync.WaitReady(ctx); err != nil {
		return err
	}

	stores, hasStore := s.stores[name]
	if hasStore {
		// 2. Start stores + wait for initial data.
		stores.Start(ctx)
		if err := stores.WaitReady(ctx); err != nil {
			return err
		}
	}

	// 3. Connect WS + subscribe personal channels.
	prov.WSPool.Connect(ctx)

	if err := prov.WSPool.WaitReady(ctx); err != nil {
		return err
	}

	// 4. Wire WS streams to stores (auto-routes ticker/depth/kline).
	if hasStore {
		stores.WireWS(prov.WSPool, prov.Adapter)
	}
	prov.WirePersonalWS(ctx, s.log)

	if prov.Adapter != nil {
		if err := prov.Adapter.SubscribePersonal(ctx, reversion.FlowIDFundingReversion); err != nil {
			provLogger.WarnContext(ctx, "⚠️ Failed to subscribe personal channels", slog.Any("error", err))
		}
	}

	provLogger.InfoContext(ctx, "🟢 Exchange Background Services Ready")
	return nil
}

func (s *FundingBot) startAccountProvider(ctx context.Context, accID string, accProv *app.AccountProvider) error {
	accLogger := s.log.With("account_id", accID, "exchange", accProv.ExchangeName)
	accLogger.InfoContext(ctx, "🔗 Starting account background services...")

	s.bgWg.Add(1)
	go func(p *app.AccountProvider) {
		defer s.bgWg.Done()
		p.Client.WarmUp(ctx, 4*time.Second)
	}(accProv)

	if runner, ok := accProv.Client.(exchange.BackgroundTaskRunner); ok {
		runner.StartBackgroundTasks(ctx)
	}

	if accProv.WSPool != nil {
		accProv.WSPool.Connect(ctx)
		if err := accProv.WSPool.WaitReady(ctx); err != nil {
			return err
		}
		accProv.WirePersonalWS(ctx, s.log)
		if accProv.Adapter != nil {
			if err := accProv.Adapter.SubscribePersonal(ctx, reversion.FlowIDFundingReversion); err != nil {
				accLogger.WarnContext(ctx, "⚠️ Failed to subscribe personal channels", slog.Any("error", err))
			}
		}
	}
	accLogger.InfoContext(ctx, "🟢 Account Background Services Ready")
	return nil
}

// RunAsBackground launches all required sync and connection routines for all active exchanges.
func (s *FundingBot) RunAsBackground(ctx context.Context) error {
	for name, prov := range s.engine.Providers {
		if err := s.startExchangeProvider(ctx, name, prov); err != nil {
			return err
		}
	}

	for accID, accProv := range s.engine.AccountProviders {
		if err := s.startAccountProvider(ctx, accID, accProv); err != nil {
			return err
		}
	}

	// Initialize all background strategies globally exactly once
	for _, st := range s.strategies {
		if st == nil {
			continue
		}
		if err := st.Start(ctx, s.stores); err != nil {
			s.log.ErrorContext(ctx, "Failed to start global strategy", slog.Any("error", err))
			return err
		}
	}

	s.log.InfoContext(ctx, "🟢 Funding Bot Background Services Ready")
	return nil
}

// Run starts the funding scanner loops for all symbols and keeps them alive.
func (s *FundingBot) Run(ctx context.Context) error {
	s.log.InfoContext(ctx, "🚀 Funding bot manager started",
		slog.String("version", version.Version),
		slog.String("commit", version.Commit),
		slog.String("built", version.BuildTime),
	)
	defer s.log.InfoContext(context.WithoutCancel(ctx), "🛑 Funding bot manager stopped")

	scanners, err := s.initScanners(ctx)
	if err != nil {
		return err
	}

	scannerJob, err := NewScannerJob(
		scanners,
		s.engine,
		s.cfg,
		s.log,
	)
	if err != nil {
		return fmt.Errorf("failed to create scanner job: %w", err)
	}

	s.bgWg.Go(func() {
		if err := scannerJob.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.log.ErrorContext(ctx, "Scanner job execution error", slog.Any("error", err))
		}
	})

	<-ctx.Done()
	return nil
}

func (s *FundingBot) initScheduleScanners(ctx context.Context) ([]Scanner, error) {
	var scanners []Scanner
	for accID, acc := range s.cfg.Accounts {
		if acc == nil || !acc.Account.Enabled || !acc.Account.Scanners.Schedule {
			continue
		}
		exch := acc.Account.Exchange

		exchangeProvider, err := s.engine.GetProvider(exch)
		if err != nil || exchangeProvider == nil {
			s.log.WarnContext(ctx, "provider not found. ScheduleScanner is disabled.",
				slog.String("account_id", accID),
				slog.String("exchange", exch),
			)
			continue
		}

		client := exchangeProvider.Client
		if accProv, accErr := s.engine.GetAccountProvider(accID); accErr == nil && accProv != nil && accProv.Client != nil {
			client = accProv.Client
		}

		scheduleScanner, err := NewScheduleScanner(
			accID,
			exch,
			s.cfg,
			client,
			s.log,
			s.disabledReason,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create schedule scanner for account %s, exchange %s: %w", accID, exch, err)
		}
		if exchangeProvider.TimeSync != nil {
			scheduleScanner.SetTimeSync(exchangeProvider.TimeSync)
		}
		scanners = append(scanners, scheduleScanner)
		s.log.InfoContext(ctx, "Registered ScheduleScanner for account",
			slog.String("account_id", accID),
			slog.String("exchange", exch),
		)
	}
	return scanners, nil
}

func (s *FundingBot) initScanners(ctx context.Context) ([]Scanner, error) {
	var scanners []Scanner

	hasConfigured := false
	for _, acc := range s.cfg.Accounts {
		if acc != nil && acc.Account.Enabled && acc.Account.Scanners.Configured {
			hasConfigured = true
			break
		}
	}
	if hasConfigured {
		configuredScanner, err := NewConfiguredScanner(
			s.cfg,
			s.engine,
			s.stores,
			s.log,
			s.disabledReason,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create configured scanner: %w", err)
		}
		scanners = append(scanners, configuredScanner)
		s.log.InfoContext(ctx, "Registered ConfiguredScanner")
	}

	scheduleScanners, err := s.initScheduleScanners(ctx)
	if err != nil {
		return nil, err
	}
	scanners = append(scanners, scheduleScanners...)

	if len(scanners) == 0 {
		s.log.WarnContext(ctx, "⚠️ No scanners are enabled. Background scanner job will run idle.")
	}

	return scanners, nil
}

// Stop implements the app.Bot interface. It executes any explicit teardown.
func (s *FundingBot) Stop(ctx context.Context) error {
	for _, st := range s.strategies {
		if st == nil {
			continue
		}
		if err := st.Stop(ctx); err != nil {
			s.log.ErrorContext(ctx, "Failed to stop global strategy", slog.Any("error", err))
		}
	}
	s.bgWg.Wait()
	return nil
}

func (s *FundingBot) disabledReason(symbol string) (string, bool) {
	s.disabledMu.RLock()
	defer s.disabledMu.RUnlock()
	reason, ok := s.disabled[symbol]
	return reason, ok
}

func getActiveSymbols(cfg *config.Config, exchangeName string) []string {
	if cfg == nil {
		return nil
	}
	var symbols []string
	for _, acc := range cfg.Accounts {
		if acc == nil || !acc.Account.Enabled || !acc.Account.Scanners.Configured {
			continue
		}
		for i := range acc.Symbols {
			sym := acc.Symbols[i]
			if !strings.EqualFold(sym.Exchange, exchangeName) {
				continue
			}
			if cfg.Blacklist != nil && cfg.Blacklist.IsBlacklisted(exchangeName, sym.Symbol) {
				continue
			}
			symbols = append(symbols, sym.Symbol)
		}
	}
	return symbols
}

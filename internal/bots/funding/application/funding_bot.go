package application

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/watcher"

	"github.com/samber/lo"
)

// FundingBot spawns one independent worker goroutine per configured symbol.
type FundingBot struct {
	cfg            *config.Config
	sysCfg         *config.SystemConfig
	engine         *app.Engine
	orderNotifiers map[string]*watcher.OrderWatcher
	stores         map[string]strategy.FundingStoreSet
	notifier       notifier.Notifier
	disabled       map[string]string
	disabledMu     sync.RWMutex
	strategies     []strategy.BackgroundStrategy
	log            *slog.Logger
	bgWg           sync.WaitGroup
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
	orderWatchers := make(map[string]*watcher.OrderWatcher)
	for name, prov := range engine.Providers {
		orderWatchers[name] = prov.Watcher
	}

	// Build map of stores per active exchange
	storesMap := make(map[string]strategy.FundingStoreSet)

	for name, prov := range engine.Providers {
		var symbols []string
		for i := range cfg.Symbols {
			exch := cfg.Symbols[i].Exchange
			if exch == name {
				if cfg.Blacklist != nil && cfg.Blacklist.IsBlacklisted(exch, cfg.Symbols[i].Symbol) {
					continue
				}
				symbols = append(symbols, cfg.Symbols[i].Symbol)
			}
		}

		if len(symbols) > 0 || name == exchange.ExchangeMexc {
			storesMap[name] = app.NewCentralStore(
				app.WithLogger(log.With("exchange", name)),
				app.WithTicker(prov.Client, time.Duration(sysCfg.Sync.Ticker)),
				app.WithContract(prov.Client, time.Duration(sysCfg.Sync.Contract)),
				app.WithFunding(prov.Client, time.Duration(sysCfg.Sync.FundingSync), symbols),
				app.WithPrice(),
				app.WithDepth(),
				app.WithKline(),
			)
		}
	}

	return &FundingBot{
		cfg:            cfg,
		sysCfg:         sysCfg,
		engine:         engine,
		orderNotifiers: orderWatchers,
		stores:         storesMap,
		notifier:       n,
		disabled:       make(map[string]string),
		strategies:     strategies,
		log:            log,
	}
}

// RunAsBackground launches all required sync and connection routines for all active exchanges.
func (s *FundingBot) RunAsBackground(ctx context.Context) error {
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
		prov.WS.Connect(ctx)

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

		provLogger.InfoContext(ctx, "🟢 Exchange Background Services Ready")
	}

	// Initialize all background strategies globally exactly once
	for _, st := range s.strategies {
		if err := st.Start(ctx, s.stores); err != nil {
			s.log.ErrorContext(ctx, "Failed to start global strategy", slog.Any("error", err))
			return err
		}
	}

	s.log.InfoContext(ctx, "🟢 Funding Bot Background Services Ready")
	return nil
}

func (s *FundingBot) wirePersonalWSForProvider(ctx context.Context, prov *app.ExchangeProvider) {
	log := s.log.With("exchange", prov.Name)
	if prov.WS == nil || prov.Adapter == nil || prov.Watcher == nil {
		return
	}

	prov.WS.On("personal.position", func(data []byte) {
		log.DebugContext(ctx, "wirePersonalWSForProvider position", slog.String("data", string(data)))
		update, err := prov.Adapter.ParsePosition(data)
		if err != nil {
			log.ErrorContext(ctx, "🟡 Failed to parse personal position WS", slog.Any("error", err))
			return
		}
		if update != nil {
			prov.Watcher.PublishPosition(lo.FromPtr(update))
		}
	})
}

// Run starts the funding scanner loops for all symbols and keeps them alive.
func (s *FundingBot) Run(ctx context.Context) error {
	s.log.InfoContext(ctx, "🚀 Funding bot manager started", slog.Int("symbols", len(s.cfg.Symbols)))
	defer s.log.InfoContext(context.WithoutCancel(ctx), "🛑 Funding bot manager stopped")

	var scanners []Scanner

	if s.sysCfg.Scanners.Configured {
		configuredScanner := NewConfiguredScanner(
			s.cfg,
			s.engine,
			s.stores,
			s.log,
			s.disabledReason,
		)
		scanners = append(scanners, configuredScanner)
		s.log.InfoContext(ctx, "Registered ConfiguredScanner")
	}

	for exch, enabled := range s.sysCfg.Scanners.Schedule {
		if !enabled {
			continue
		}

		if exch != exchange.ExchangeMexc {
			s.log.WarnContext(ctx, "ScheduleScanner only supports mexc at the moment. Skipping.", slog.String("exchange", exch))
			continue
		}

		if mexcProvider, ok := s.engine.Providers[exch]; ok {
			scheduleScanner := NewScheduleScanner(
				s.cfg,
				mexcProvider.Client,
				s.log,
				s.disabledReason,
			)
			scanners = append(scanners, scheduleScanner)
			s.log.InfoContext(ctx, "Registered ScheduleScanner for MEXC exchange")
		} else {
			s.log.WarnContext(ctx, "MEXC provider not found. ScheduleScanner is disabled.")
		}
	}

	if len(scanners) == 0 {
		s.log.WarnContext(ctx, "⚠️ No scanners are enabled. Background scanner job will run idle.")
	}

	scannerJob := NewScannerJob(
		scanners,
		s.engine,
		s.log,
	)

	s.bgWg.Go(func() {
		if err := scannerJob.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			s.log.ErrorContext(ctx, "Scanner job execution error", slog.Any("error", err))
		}
	})

	<-ctx.Done()
	return nil
}

// Stop implements the app.Bot interface. It executes any explicit teardown.
func (s *FundingBot) Stop(ctx context.Context) error {
	s.bgWg.Wait()
	return nil
}

func (s *FundingBot) disabledReason(symbol string) (string, bool) {
	s.disabledMu.RLock()
	defer s.disabledMu.RUnlock()
	reason, ok := s.disabled[symbol]
	return reason, ok
}

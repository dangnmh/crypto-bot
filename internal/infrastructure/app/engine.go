package app

import (
	"context"
	"log/slog"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/logger"
)

// Bot defines the interface that any sub-bot must implement to be run by the Engine.
type Bot interface {
	Run(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Engine manages the lifecycle of the shared infrastructure components.
type Engine struct {
	Cfg           *sysconfig.SystemConfig
	Client        *exchange.Client
	Store         *store.GlobalStore
	TimeSync      *timesync.TimeSync
	WS            *ws.Client
	loggerCleanup func()
}

// NewEngine initializes the core components and logger.
func NewEngine(cfg *sysconfig.SystemConfig) *Engine {
	cleanup := logger.InitLogger(cfg.Logging.Level)

	slog.Info("⚙️ Initializing Engine...", "base_url", cfg.API.BaseURL)

	client := exchange.NewClient(cfg.API.BaseURL, cfg.APIKey, cfg.APISecret, cfg.Logging)
	gs := store.New()
	ts := timesync.New(client, time.Duration(cfg.API.TimeSyncInterval))
	wsClient := ws.NewClient(cfg.API.WSURL, cfg.APIKey, cfg.APISecret, gs, cfg.Logging)

	return &Engine{
		Cfg:           cfg,
		Client:        client,
		Store:         gs,
		TimeSync:      ts,
		WS:            wsClient,
		loggerCleanup: cleanup,
	}
}

// StartBackgroundServices launches all required sync and connection routines.
// It blocks until initial data has been populated.
func (e *Engine) StartBackgroundServices(ctx context.Context, symbols []string) {
	go e.Client.WarmUp(ctx, 4*time.Second)

	go e.TimeSync.Start(ctx)
	e.TimeSync.WaitReady(ctx)

	go e.Store.StartTickerSync(ctx, e.Client, time.Duration(e.Cfg.API.TickerSyncInterval))
	go e.Store.StartContractSync(ctx, e.Client, time.Duration(e.Cfg.API.ContractSyncInterval))

	if len(symbols) > 0 {
		go e.Store.StartFundingSync(ctx, e.Client, symbols, time.Duration(e.Cfg.API.FundingSyncInterval))
	}

	e.Store.WaitReady(ctx)

	go e.WS.Connect(ctx)
	e.WS.WaitReady(ctx)

	_ = e.WS.SubscribeOrderDeals()

	slog.Info("🟢 Engine Background Services Ready")
}

// Shutdown cleans up the engine resources.
func (e *Engine) Shutdown() {
	if e.WS != nil {
		e.WS.Close()
	}
	if e.loggerCleanup != nil {
		e.loggerCleanup()
	}
}

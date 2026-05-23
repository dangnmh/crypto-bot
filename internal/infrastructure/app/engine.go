package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
	pkgws "crypto-bot/pkg/ws"
)

// Bot defines the interface that any sub-bot must implement to be run by the Engine.
type Bot interface {
	RunAsBackground(ctx context.Context) error
	Run(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Engine manages the lifecycle of the shared infrastructure components.
type Engine struct {
	Cfg      *sysconfig.SystemConfig
	Client   exchange.Client
	Adapter  ws.ExchangeAdapter
	TimeSync *timesync.TimeSync
	WS       *pkgws.Pool
	Bus      *eventbus.Bus
}

// EngineConfig holds the dependencies needed to create an Engine.
// The Client and Adapter are injected, allowing different exchange providers (MEXC, Binance, etc.)
type EngineConfig struct {
	SystemConfig *sysconfig.SystemConfig
	Client       exchange.Client
	Adapter      ws.ExchangeAdapter
}

// NewEngine initializes the core components with an injected exchange client.
func NewEngine(cfg EngineConfig) *Engine {
	sysCfg := cfg.SystemConfig

	engineLogger := slog.Default().With("component", "engine")
	engineLogger.Info("⚙️ Initializing Engine...", "base_url", sysCfg.API.Future.BaseURL)

	ts := timesync.New(cfg.Client, time.Duration(sysCfg.Sync.Time))

	// Create generic WS pool with exchange-specific auth and extractors
	wsLogger := engineLogger.With("subsystem", "websocket")
	wsClientOpts := []pkgws.ClientOption{}

	if cfg.Adapter != nil {
		if payload, interval := cfg.Adapter.GetPingConfig(); payload != nil && interval > 0 {
			wsClientOpts = append(wsClientOpts, pkgws.WithPing(payload, interval))
		}

		if extractor := cfg.Adapter.GetChannelExtractor(); extractor != nil {
			wsClientOpts = append(wsClientOpts, pkgws.WithChannelExtractor(extractor))
		}

		if hook := cfg.Adapter.GetAuthHook(sysCfg.APIKey, sysCfg.APISecret); hook != nil {
			wsClientOpts = append(wsClientOpts, pkgws.WithOnConnected(hook))
		}
	}

	wsPool := pkgws.NewPool(sysCfg.API.WebSocket.WSURL, sysCfg.API.WebSocket.MaxPairsPerWSConn, wsLogger, wsClientOpts...)

	if cfg.Adapter != nil {
		cfg.Adapter.SetPool(wsPool)
	}

	bus := eventbus.New(engineLogger.With("subsystem", "eventbus"))

	return &Engine{
		Cfg:      sysCfg,
		Client:   cfg.Client,
		Adapter:  cfg.Adapter,
		TimeSync: ts,
		WS:       wsPool,
		Bus:      bus,
	}
}

// Shutdown cleans up the engine resources with a timeout.
func (e *Engine) Shutdown(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		var errs []error
		if e.WS != nil {
			e.WS.Close()
		}
		if e.Bus != nil {
			if err := e.Bus.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		errCh <- errors.Join(errs...)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

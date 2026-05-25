package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
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

// ExchangeProvider isolates all networking and timing resources for an exchange.
type ExchangeProvider struct {
	Name     string
	Client   exchange.Client
	Adapter  ws.ExchangeAdapter
	WS       *pkgws.Pool
	TimeSync *timesync.TimeSync
	Watcher  *watcher.OrderWatcher
}

// Engine manages the lifecycle of all dynamic ExchangeProvider instances.
type Engine struct {
	Cfg       *sysconfig.SystemConfig
	Client    exchange.Client    // Default client (backwards compatibility)
	Adapter   ws.ExchangeAdapter // Default adapter (backwards compatibility)
	TimeSync  *timesync.TimeSync // Default timesync (backwards compatibility)
	WS        *pkgws.Pool        // Default WS pool (backwards compatibility)
	Bus       *eventbus.Bus
	Providers map[string]*ExchangeProvider
}

// EngineConfig holds the dependencies needed to create an Engine.
type EngineConfig struct {
	SystemConfig *sysconfig.SystemConfig
	HTTPClient   *http.Client
}

// NewEngine dynamically instantiates exchange providers based on configured credentials and endpoints.
func NewEngine(cfg EngineConfig) *Engine {
	sysCfg := cfg.SystemConfig
	engineLogger := slog.Default().With("component", "engine")
	bus := eventbus.New(engineLogger.With("subsystem", "eventbus"))

	engine := &Engine{
		Cfg:       sysCfg,
		Bus:       bus,
		Providers: make(map[string]*ExchangeProvider),
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	}

	// 1. Initialize MEXC if configured
	if sysCfg.ExchangeConfig.Mexc.Future.BaseURL != "" {
		engineLogger.Info("⚙️ Initializing MEXC Exchange Provider...", "base_url", sysCfg.ExchangeConfig.Mexc.Future.BaseURL)
		mexcClient := mexc.NewClient(
			httpClient,
			sysCfg.ExchangeConfig.Mexc.Future.BaseURL,
			sysCfg.ExchangeConfig.Mexc.APIKey,
			sysCfg.ExchangeConfig.Mexc.APISecret,
			sysCfg.Logging,
		)

		var client exchange.Client = mexcClient
		if sysCfg.DryRun {
			client = exchange.NewDryRunClient(client)
		}

		mexcAdapter := mexc.NewWsAdapter()
		mexcTimeSync := timesync.New(client, time.Duration(sysCfg.Sync.Time))

		wsLogger := engineLogger.With("subsystem", "websocket", "exchange", exchange.ExchangeMexc)
		wsClientOpts := []pkgws.ClientOption{}

		if payload, interval := mexcAdapter.GetPingConfig(); payload != nil && interval > 0 {
			wsClientOpts = append(wsClientOpts, pkgws.WithPing(payload, interval))
		}
		if extractor := mexcAdapter.GetChannelExtractor(); extractor != nil {
			wsClientOpts = append(wsClientOpts, pkgws.WithChannelExtractor(extractor))
		}
		if hook := mexcAdapter.GetAuthHook(sysCfg.ExchangeConfig.Mexc.APIKey, sysCfg.ExchangeConfig.Mexc.APISecret); hook != nil {
			wsClientOpts = append(wsClientOpts, pkgws.WithOnConnected(hook))
		}

		mexcWSPool := pkgws.NewPool(
			sysCfg.ExchangeConfig.Mexc.WebSocket.WSURL,
			sysCfg.ExchangeConfig.Mexc.WebSocket.MaxPairsPerWSConn,
			wsLogger,
			wsClientOpts...,
		)
		mexcAdapter.SetPool(mexcWSPool)

		mexcWatcher := watcher.NewOrderWatcher(bus, exchange.ExchangeMexc, engineLogger.With("component", "order_watcher", "exchange", exchange.ExchangeMexc))

		prov := &ExchangeProvider{
			Name:     exchange.ExchangeMexc,
			Client:   client,
			Adapter:  mexcAdapter,
			WS:       mexcWSPool,
			TimeSync: mexcTimeSync,
			Watcher:  mexcWatcher,
		}
		engine.Providers[exchange.ExchangeMexc] = prov

		// Set default fallback properties for backwards compatibility
		engine.Client = client
		engine.Adapter = mexcAdapter
		engine.TimeSync = mexcTimeSync
		engine.WS = mexcWSPool
	}

	// 2. Initialize Gate.io if configured
	if sysCfg.ExchangeConfig.Gate.Future.BaseURL != "" {
		engineLogger.Info("⚙️ Initializing Gate.io Exchange Provider...", "base_url", sysCfg.ExchangeConfig.Gate.Future.BaseURL)
		gateClient := gate.NewClient(
			httpClient,
			sysCfg.ExchangeConfig.Gate.Future.BaseURL,
			sysCfg.ExchangeConfig.Gate.APIKey,
			sysCfg.ExchangeConfig.Gate.APISecret,
			sysCfg.Logging,
		)

		var client exchange.Client = gateClient
		if sysCfg.DryRun {
			client = exchange.NewDryRunClient(client)
		}

		gateAdapter := gate.NewWsAdapter()
		gateTimeSync := timesync.New(client, time.Duration(sysCfg.Sync.Time))

		wsLogger := engineLogger.With("subsystem", "websocket", "exchange", "gate")
		wsClientOpts := []pkgws.ClientOption{}

		if payload, interval := gateAdapter.GetPingConfig(); payload != nil && interval > 0 {
			wsClientOpts = append(wsClientOpts, pkgws.WithPing(payload, interval))
		}
		if extractor := gateAdapter.GetChannelExtractor(); extractor != nil {
			wsClientOpts = append(wsClientOpts, pkgws.WithChannelExtractor(extractor))
		}
		// Auth hook is stored and computed inside subscriptions for Gate.io, but we call GetAuthHook to pass keys
		gateAdapter.GetAuthHook(sysCfg.ExchangeConfig.Gate.APIKey, sysCfg.ExchangeConfig.Gate.APISecret)

		gateWSPool := pkgws.NewPool(
			sysCfg.ExchangeConfig.Gate.WebSocket.WSURL,
			sysCfg.ExchangeConfig.Gate.WebSocket.MaxPairsPerWSConn,
			wsLogger,
			wsClientOpts...,
		)
		gateAdapter.SetPool(gateWSPool)

		gateWatcher := watcher.NewOrderWatcher(bus, "gate", engineLogger.With("component", "order_watcher", "exchange", "gate"))

		prov := &ExchangeProvider{
			Name:     "gate",
			Client:   client,
			Adapter:  gateAdapter,
			WS:       gateWSPool,
			TimeSync: gateTimeSync,
			Watcher:  gateWatcher,
		}
		engine.Providers["gate"] = prov

		// If no default provider was set yet (e.g. MEXC is disabled), default to Gate
		if engine.Client == nil {
			engine.Client = client
			engine.Adapter = gateAdapter
			engine.TimeSync = gateTimeSync
			engine.WS = gateWSPool
		}
	}

	return engine
}

// GetProvider retrieves an ExchangeProvider by name.
func (e *Engine) GetProvider(name string) (*ExchangeProvider, error) {
	name = strings.ToLower(name)
	prov, ok := e.Providers[name]
	if !ok {
		return nil, fmt.Errorf("exchange provider %q not initialized or configured", name)
	}
	return prov, nil
}

// Shutdown cleans up all initialized exchange provider connections and resources.
func (e *Engine) Shutdown(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		var errs []error
		for _, prov := range e.Providers {
			if prov.WS != nil {
				prov.WS.Close()
			}
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

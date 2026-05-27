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
	Bus       *eventbus.Bus
	Providers map[string]*ExchangeProvider
	log       *slog.Logger
}

// EngineConfig holds the dependencies needed to create an Engine.
type EngineConfig struct {
	SystemConfig      *sysconfig.SystemConfig
	HTTPClient        *http.Client
	Logger            *slog.Logger
	ProviderFactories []ProviderFactory
	ActiveExchanges   []string
}

// NewEngine dynamically instantiates exchange providers based on configured credentials and endpoints.
func NewEngine(ctx context.Context, cfg EngineConfig) (*Engine, error) {
	sysCfg := cfg.SystemConfig
	if sysCfg == nil {
		return nil, fmt.Errorf("system config is required")
	}
	if cfg.Logger == nil {
		return nil, fmt.Errorf("logger is required")
	}

	engineLogger := cfg.Logger.With("component", "engine")
	bus := eventbus.New(engineLogger.With("subsystem", "eventbus"))

	engine := &Engine{
		Cfg:       sysCfg,
		Bus:       bus,
		Providers: make(map[string]*ExchangeProvider),
		log:       engineLogger,
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	}

	activeMap := make(map[string]bool)
	for _, exch := range cfg.ActiveExchanges {
		activeMap[strings.ToLower(strings.TrimSpace(exch))] = true
	}

	factories := cfg.ProviderFactories
	if len(factories) == 0 {
		factories = DefaultProviderFactories()
	}
	factoryCfg := ProviderFactoryConfig{
		SystemConfig: sysCfg,
		HTTPClient:   httpClient,
		Logger:       engineLogger,
		Bus:          bus,
	}
	if err := validateProviderFactoryConfig(factoryCfg); err != nil {
		return nil, err
	}
	for _, factory := range factories {
		if !factory.Enabled(sysCfg) {
			continue
		}
		if len(cfg.ActiveExchanges) > 0 {
			name := strings.ToLower(factory.Name())
			if name == "bybit-unified" {
				name = "bybit"
			}
			if !activeMap[name] {
				continue
			}
		}
		engineLogger.Info("initializing exchange provider", slog.String("exchange", factory.Name()))
		prov, err := factory.Build(ctx, factoryCfg)
		if err != nil {
			return nil, fmt.Errorf("build %s provider: %w", factory.Name(), err)
		}
		engine.Providers[prov.Name] = prov
	}
	if len(engine.Providers) == 0 {
		return nil, fmt.Errorf("no exchange providers configured")
	}

	return engine, nil
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

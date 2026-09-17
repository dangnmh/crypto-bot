package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/domain"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/httpclient"
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
	Name           string
	Client         exchange.Client
	Adapter        ws.ExchangeManagerAdapter
	WSPool         *pkgws.Pool
	TimeSync       *timesync.TimeSync
	Watcher        watcher.OrderNotifier
	personalWSOnce sync.Once
}

// WirePersonalWS auto-connects personal WebSocket position handlers to the Watcher publisher.
// It is safe for concurrent use and is executed at most once per provider instance.
func (p *ExchangeProvider) WirePersonalWS(ctx context.Context, logger *slog.Logger) {
	if p == nil || p.WSPool == nil || p.Adapter == nil || p.Watcher == nil {
		return
	}
	p.personalWSOnce.Do(func() {
		if logger == nil {
			logger = slog.Default()
		}
		log := logger.With("exchange", p.Name)
		p.WSPool.On("personal.position", func(data []byte) {
			log.DebugContext(ctx, "Received personal position WS update", slog.String("data", string(data)))
			update, err := p.Adapter.ParsePosition(data)
			if err != nil {
				log.ErrorContext(ctx, "🟡 Failed to parse personal position WS", slog.Any("error", err))
				return
			}
			if update != nil {
				if publisher, ok := p.Watcher.(interface {
					PublishPosition(exchange.PersonalPositionUpdate)
				}); ok {
					publisher.PublishPosition(*update)
				}
			}
		})
		p.WSPool.On("trade", func(data []byte) {
			sym, trades, err := p.Adapter.ParseTrade(data)
			if err != nil {
				log.ErrorContext(ctx, "🟡 Failed to parse trade WS", slog.Any("error", err))
				return
			}
			if len(trades) > 0 {
				if publisher, ok := p.Watcher.(interface {
					PublishTrades(string, []domain.PublicTrade)
				}); ok {
					publisher.PublishTrades(sym, trades)
				}
			}
		})
	})
}

// EnsurePersonalWS ensures that the WebSocket pool is connected and the personal position stream is wired.
// It is thread-safe and idempotent.
func (p *ExchangeProvider) EnsurePersonalWS(ctx context.Context, logger *slog.Logger) {
	if p == nil {
		return
	}
	p.WirePersonalWS(ctx, logger)
	if p.WSPool != nil {
		p.WSPool.Connect(ctx)
	}
}

// AccountProvider isolates account-scoped credentials, dedicated HTTP client (with EIP), private WebSocket pool, and order event watcher.
type AccountProvider struct {
	AccountID      string
	ExchangeName   string
	Client         exchange.Client
	Adapter        ws.ExchangeManagerAdapter
	WSPool         *pkgws.Pool
	Watcher        watcher.OrderNotifier
	personalWSOnce sync.Once
}

// WirePersonalWS auto-connects personal WebSocket position handlers to the Watcher publisher for this account.
func (p *AccountProvider) WirePersonalWS(ctx context.Context, logger *slog.Logger) {
	if p == nil || p.WSPool == nil || p.Adapter == nil || p.Watcher == nil {
		return
	}
	p.personalWSOnce.Do(func() {
		if logger == nil {
			logger = slog.Default()
		}
		log := logger.With("account", p.AccountID, "exchange", p.ExchangeName)
		p.WSPool.On("personal.position", func(data []byte) {
			log.DebugContext(ctx, "Received personal position WS update", slog.String("data", string(data)))
			update, err := p.Adapter.ParsePosition(data)
			if err != nil {
				log.ErrorContext(ctx, "🟡 Failed to parse personal position WS", slog.Any("error", err))
				return
			}
			if update != nil {
				if publisher, ok := p.Watcher.(interface {
					PublishPosition(exchange.PersonalPositionUpdate)
				}); ok {
					publisher.PublishPosition(*update)
				}
			}
		})
		p.WSPool.On("trade", func(data []byte) {
			sym, trades, err := p.Adapter.ParseTrade(data)
			if err != nil {
				log.ErrorContext(ctx, "🟡 Failed to parse trade WS", slog.Any("error", err))
				return
			}
			if len(trades) > 0 {
				if publisher, ok := p.Watcher.(interface {
					PublishTrades(string, []domain.PublicTrade)
				}); ok {
					publisher.PublishTrades(sym, trades)
				}
			}
		})
	})
}

// EnsurePersonalWS ensures that the WebSocket pool is connected and the personal position stream is wired.
func (p *AccountProvider) EnsurePersonalWS(ctx context.Context, logger *slog.Logger) {
	if p == nil {
		return
	}
	p.WirePersonalWS(ctx, logger)
	if p.WSPool != nil {
		p.WSPool.Connect(ctx)
	}
}

// Engine manages the lifecycle of all dynamic ExchangeProvider and AccountProvider instances.
type Engine struct {
	Cfg              *sysconfig.SystemConfig
	Bus              *eventbus.Bus
	Providers        map[string]*ExchangeProvider
	AccountProviders map[string]*AccountProvider
	log              *slog.Logger
}

// EngineConfig holds the dependencies needed to create an Engine.
type EngineConfig struct {
	SystemConfig      *sysconfig.SystemConfig
	HTTPClient        *http.Client
	OrderHTTPClient   *http.Client
	Logger            *slog.Logger
	ProviderFactories []ProviderFactory
	ActiveExchanges   []string
	Accounts          []sysconfig.AccountConfig
	TimeSyncInterval  time.Duration
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
		Cfg:              sysCfg,
		Bus:              bus,
		Providers:        make(map[string]*ExchangeProvider),
		AccountProviders: make(map[string]*AccountProvider),
		log:              engineLogger,
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 15 * time.Second,
		}
	}

	orderHTTPClient := cfg.OrderHTTPClient
	if orderHTTPClient == nil {
		orderHTTPClient = httpclient.NewPool(httpclient.OrderPoolConfig())
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
		SystemConfig:     sysCfg,
		HTTPClient:       httpClient,
		OrderHTTPClient:  orderHTTPClient,
		Logger:           engineLogger,
		Bus:              bus,
		TimeSyncInterval: cfg.TimeSyncInterval,
	}
	if err := validateProviderFactoryConfig(factoryCfg); err != nil {
		return nil, err
	}
	if err := engine.buildExchangeProviders(ctx, sysCfg, factories, factoryCfg, activeMap); err != nil {
		return nil, err
	}
	if len(engine.Providers) == 0 {
		return nil, fmt.Errorf("no exchange providers configured")
	}

	if err := engine.buildAccountProviders(ctx, cfg.Accounts, factoryCfg); err != nil {
		return nil, err
	}

	return engine, nil
}

func (e *Engine) buildExchangeProviders(ctx context.Context, sysCfg *sysconfig.SystemConfig, factories []ProviderFactory, factoryCfg ProviderFactoryConfig, activeMap map[string]bool) error {
	for _, factory := range factories {
		if !factory.Enabled(sysCfg) {
			continue
		}
		if len(activeMap) > 0 {
			name := strings.ToLower(factory.Name())
			if !activeMap[name] {
				continue
			}
		}
		e.log.Info("initializing exchange provider", slog.String("exchange", factory.Name()))
		prov, err := factory.Build(ctx, factoryCfg)
		if err != nil {
			return fmt.Errorf("build %s provider: %w", factory.Name(), err)
		}
		e.Providers[prov.Name] = prov
	}
	return nil
}

func (e *Engine) buildAccountProviders(ctx context.Context, accounts []sysconfig.AccountConfig, factoryCfg ProviderFactoryConfig) error {
	if len(accounts) > 0 {
		for i := range accounts {
			acc := &accounts[i]
			if !acc.Enabled {
				continue
			}
			e.log.Info("initializing account provider", slog.String("account", acc.ID), slog.String("exchange", acc.Exchange))
			accProv, err := BuildAccountProvider(ctx, *acc, factoryCfg)
			if err != nil {
				return fmt.Errorf("build account provider %q: %w", acc.ID, err)
			}
			e.AccountProviders[acc.ID] = accProv
		}
		return nil
	}

	// Single-account fallback: mirror each ExchangeProvider as an AccountProvider
	for name, prov := range e.Providers {
		e.AccountProviders[name] = &AccountProvider{
			AccountID:    name,
			ExchangeName: prov.Name,
			Client:       prov.Client,
			Adapter:      prov.Adapter,
			WSPool:       prov.WSPool,
			Watcher:      prov.Watcher,
		}
	}
	return nil
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

// GetAccountProvider retrieves an AccountProvider by account ID or exchange name fallback.
func (e *Engine) GetAccountProvider(id string) (*AccountProvider, error) {
	id = strings.TrimSpace(id)
	if prov, ok := e.AccountProviders[id]; ok {
		return prov, nil
	}
	lowerID := strings.ToLower(id)
	if prov, ok := e.AccountProviders[lowerID]; ok {
		return prov, nil
	}
	if prov, ok := e.Providers[lowerID]; ok {
		return &AccountProvider{
			AccountID:    id,
			ExchangeName: prov.Name,
			Client:       prov.Client,
			Adapter:      prov.Adapter,
			WSPool:       prov.WSPool,
			Watcher:      prov.Watcher,
		}, nil
	}
	return nil, fmt.Errorf("account provider %q not initialized or configured", id)
}

// Shutdown cleans up all initialized exchange provider connections and resources.
func (e *Engine) Shutdown(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		var errs []error
		for _, prov := range e.Providers {
			if prov.WSPool != nil {
				prov.WSPool.Close()
			}
			if closer, ok := prov.Client.(interface{ Close() }); ok {
				closer.Close()
			}
		}
		for _, prov := range e.AccountProviders {
			// Skip if this account provider is mirroring an exchange provider already closed above
			if _, isExchangeProv := e.Providers[prov.AccountID]; isExchangeProv {
				continue
			}
			if prov.WSPool != nil {
				prov.WSPool.Close()
			}
			if closer, ok := prov.Client.(interface{ Close() }); ok {
				closer.Close()
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

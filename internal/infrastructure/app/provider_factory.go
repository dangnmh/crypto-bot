package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
	pkgws "crypto-bot/pkg/ws"
)

const bybitUnifiedName = "bybit-unified"

// ProviderFactory builds one exchange provider from system configuration.
type ProviderFactory interface {
	Name() string
	Enabled(cfg *sysconfig.SystemConfig) bool
	Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error)
}

// ProviderFactoryConfig holds shared dependencies for provider construction.
type ProviderFactoryConfig struct {
	SystemConfig *sysconfig.SystemConfig
	HTTPClient   *http.Client
	Logger       *slog.Logger
	Bus          *eventbus.Bus
}

// DefaultProviderFactories returns the exchange factories supported by the app layer.
func DefaultProviderFactories() []ProviderFactory {
	return []ProviderFactory{
		MexcProviderFactory{},
		GateProviderFactory{},
		BybitStandardProviderFactory{},
		BybitUnifiedProviderFactory{},
		BinanceProviderFactory{},
	}
}

// MexcProviderFactory builds MEXC infrastructure.
type MexcProviderFactory struct{}

func (MexcProviderFactory) Name() string { return exchange.ExchangeMexc }

func (MexcProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Mexc.Future.BaseURL != ""
}

func (MexcProviderFactory) Build(_ context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Mexc
	client := exchange.Client(mexc.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysCfg.Logging,
	))
	if sysCfg.DryRun {
		client = exchange.NewDryRunClient(client)
	}

	adapter := mexc.NewWsAdapter()
	wsPool := newWSPool(exchange.ExchangeMexc, apiCfg, adapter, cfg.Logger, apiCfg.APIKey, apiCfg.APISecret)
	adapter.SetPool(wsPool)

	return &ExchangeProvider{
		Name:     exchange.ExchangeMexc,
		Client:   client,
		Adapter:  adapter,
		WS:       wsPool,
		TimeSync: timesync.New(client, time.Duration(sysCfg.Sync.Time)),
		Watcher:  watcher.NewOrderWatcher(cfg.Bus, exchange.ExchangeMexc, cfg.Logger.With("component", "order_watcher", "exchange", exchange.ExchangeMexc)),
	}, nil
}

// GateProviderFactory builds Gate.io infrastructure.
type GateProviderFactory struct{}

func (GateProviderFactory) Name() string { return exchange.ExchangeGate }

func (GateProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Gate.Future.BaseURL != ""
}

func (GateProviderFactory) Build(_ context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Gate
	client := exchange.Client(gate.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysCfg.Logging,
	))
	if sysCfg.DryRun {
		client = exchange.NewDryRunClient(client)
	}

	adapter := gate.NewWsAdapter()
	adapter.GetAuthHook(apiCfg.APIKey, apiCfg.APISecret)
	wsPool := newWSPool(exchange.ExchangeGate, apiCfg, adapter, cfg.Logger, apiCfg.APIKey, apiCfg.APISecret)
	adapter.SetPool(wsPool)

	return &ExchangeProvider{
		Name:     exchange.ExchangeGate,
		Client:   client,
		Adapter:  adapter,
		WS:       wsPool,
		TimeSync: timesync.New(client, time.Duration(sysCfg.Sync.Time)),
		Watcher:  watcher.NewOrderWatcher(cfg.Bus, exchange.ExchangeGate, cfg.Logger.With("component", "order_watcher", "exchange", exchange.ExchangeGate)),
	}, nil
}

// BinanceProviderFactory builds Binance infrastructure.
type BinanceProviderFactory struct{}

func (BinanceProviderFactory) Name() string { return exchange.ExchangeBinance }

func (BinanceProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Binance.Future.BaseURL != ""
}

func (BinanceProviderFactory) Build(_ context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Binance
	client := exchange.Client(binance.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysCfg.Logging,
	))
	if sysCfg.DryRun {
		client = exchange.NewDryRunClient(client)
	}

	adapter := binance.NewWsAdapter()
	wsPool := newWSPool(exchange.ExchangeBinance, apiCfg, adapter, cfg.Logger, apiCfg.APIKey, apiCfg.APISecret)
	adapter.SetPool(wsPool)

	return &ExchangeProvider{
		Name:     exchange.ExchangeBinance,
		Client:   client,
		Adapter:  adapter,
		WS:       wsPool,
		TimeSync: timesync.New(client, time.Duration(sysCfg.Sync.Time)),
		Watcher:  watcher.NewOrderWatcher(cfg.Bus, exchange.ExchangeBinance, cfg.Logger.With("component", "order_watcher", "exchange", exchange.ExchangeBinance)),
	}, nil
}

func newWSPool(
	exchangeName string,
	apiCfg sysconfig.APIConfig,
	adapter ws.ExchangeAdapter,
	logger *slog.Logger,
	apiKey string,
	apiSecret string,
) *pkgws.Pool {
	wsLogger := logger.With("subsystem", "websocket", "exchange", exchangeName)
	wsClientOpts := []pkgws.ClientOption{}
	if payload, interval := adapter.GetPingConfig(); payload != nil && interval > 0 {
		wsClientOpts = append(wsClientOpts, pkgws.WithPing(payload, interval))
	}
	if extractor := adapter.GetChannelExtractor(); extractor != nil {
		wsClientOpts = append(wsClientOpts, pkgws.WithChannelExtractor(extractor))
	}
	if hook := adapter.GetAuthHook(apiKey, apiSecret); hook != nil {
		wsClientOpts = append(wsClientOpts, pkgws.WithOnConnected(hook))
	}

	return pkgws.NewPool(apiCfg.WebSocket.WSURL, apiCfg.WebSocket.MaxPairsPerWSConn, wsLogger, wsClientOpts...)
}

func validateProviderFactoryConfig(cfg ProviderFactoryConfig) error {
	if cfg.SystemConfig == nil {
		return fmt.Errorf("system config is required")
	}
	if cfg.HTTPClient == nil {
		return fmt.Errorf("http client is required")
	}
	if cfg.Logger == nil {
		return fmt.Errorf("logger is required")
	}
	if cfg.Bus == nil {
		return fmt.Errorf("event bus is required")
	}
	return nil
}

// BybitStandardProviderFactory builds Standard Classic Bybit infrastructure.
type BybitStandardProviderFactory struct{}

func (BybitStandardProviderFactory) Name() string { return exchange.ExchangeBybit }

func (BybitStandardProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Bybit.Future.BaseURL != "" && (cfg.ExchangeConfig.Bybit.AccountType == "" || cfg.ExchangeConfig.Bybit.AccountType == "standard")
}

func (BybitStandardProviderFactory) Build(_ context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Bybit
	client := exchange.Client(bybit.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		"standard",
		sysCfg.Logging,
	))
	if sysCfg.DryRun {
		client = exchange.NewDryRunClient(client)
	}

	adapter := bybit.NewWsAdapter()
	wsPool := newWSPool(exchange.ExchangeBybit, apiCfg, adapter, cfg.Logger, apiCfg.APIKey, apiCfg.APISecret)
	adapter.SetPool(wsPool)

	return &ExchangeProvider{
		Name:     exchange.ExchangeBybit,
		Client:   client,
		Adapter:  adapter,
		WS:       wsPool,
		TimeSync: timesync.New(client, time.Duration(sysCfg.Sync.Time)),
		Watcher:  watcher.NewOrderWatcher(cfg.Bus, exchange.ExchangeBybit, cfg.Logger.With("component", "order_watcher", "exchange", exchange.ExchangeBybit)),
	}, nil
}

// BybitUnifiedProviderFactory builds Unified Bybit infrastructure (UTA).
type BybitUnifiedProviderFactory struct{}

func (BybitUnifiedProviderFactory) Name() string { return bybitUnifiedName }

func (BybitUnifiedProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Bybit.Future.BaseURL != "" && cfg.ExchangeConfig.Bybit.AccountType == "unified"
}

func (BybitUnifiedProviderFactory) Build(_ context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Bybit
	client := exchange.Client(bybit.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		"unified",
		sysCfg.Logging,
	))
	if sysCfg.DryRun {
		client = exchange.NewDryRunClient(client)
	}

	adapter := bybit.NewWsAdapter()
	wsPool := newWSPool(exchange.ExchangeBybit, apiCfg, adapter, cfg.Logger, apiCfg.APIKey, apiCfg.APISecret)
	adapter.SetPool(wsPool)

	return &ExchangeProvider{
		Name:     bybitUnifiedName,
		Client:   client,
		Adapter:  adapter,
		WS:       wsPool,
		TimeSync: timesync.New(client, time.Duration(sysCfg.Sync.Time)),
		Watcher:  watcher.NewOrderWatcher(cfg.Bus, exchange.ExchangeBybit, cfg.Logger.With("component", "order_watcher", "exchange", "bybit-unified")),
	}, nil
}

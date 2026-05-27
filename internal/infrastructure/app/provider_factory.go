package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bingx"
	"crypto-bot/internal/infrastructure/exchange/bitget"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
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
		OkxProviderFactory{},
		HyperliquidProviderFactory{},
		BitgetProviderFactory{},
		BingxProviderFactory{},
		KucoinProviderFactory{},
	}
}

// MexcProviderFactory builds MEXC infrastructure.
type MexcProviderFactory struct{}

func (MexcProviderFactory) Name() string { return exchange.ExchangeMexc }

func (MexcProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Mexc.Enable
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

	adapter := mexc.NewWsAdapter()
	return buildProvider(exchange.ExchangeMexc, exchange.ExchangeMexc, cfg, apiCfg, client, adapter), nil
}

// GateProviderFactory builds Gate.io infrastructure.
type GateProviderFactory struct{}

func (GateProviderFactory) Name() string { return exchange.ExchangeGate }

func (GateProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Gate.Enable
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

	adapter := gate.NewWsAdapter()
	return buildProvider(exchange.ExchangeGate, exchange.ExchangeGate, cfg, apiCfg, client, adapter), nil
}

// BinanceProviderFactory builds Binance infrastructure.
type BinanceProviderFactory struct{}

func (BinanceProviderFactory) Name() string { return exchange.ExchangeBinance }

func (BinanceProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Binance.Enable
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

	adapter := binance.NewWsAdapter()
	return buildProvider(exchange.ExchangeBinance, exchange.ExchangeBinance, cfg, apiCfg, client, adapter), nil
}

// OkxProviderFactory builds OKX infrastructure.
type OkxProviderFactory struct{}

func (OkxProviderFactory) Name() string { return exchange.ExchangeOkx }

func (OkxProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Okx.Enable
}

func (OkxProviderFactory) Build(_ context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Okx
	client := exchange.Client(okx.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		"", // Passphrase from environment variables in okx.NewClient
		sysCfg.Logging,
	))

	adapter := okx.NewWsAdapter()
	return buildProvider(exchange.ExchangeOkx, exchange.ExchangeOkx, cfg, apiCfg, client, adapter), nil
}

// BitgetProviderFactory builds Bitget infrastructure.
type BitgetProviderFactory struct{}

func (BitgetProviderFactory) Name() string { return exchange.ExchangeBitget }

func (BitgetProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Bitget.Enable
}

func (BitgetProviderFactory) Build(_ context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Bitget
	client := exchange.Client(bitget.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		"", // Passphrase from environment variables or config
		sysCfg.Logging,
	))

	adapter := bitget.NewWsAdapter()
	return buildProvider(exchange.ExchangeBitget, exchange.ExchangeBitget, cfg, apiCfg, client, adapter), nil
}

// HyperliquidProviderFactory builds Hyperliquid infrastructure.
type HyperliquidProviderFactory struct{}

func (HyperliquidProviderFactory) Name() string { return exchange.ExchangeHyperliquid }

func (HyperliquidProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Hyperliquid.Enable
}

func (HyperliquidProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Hyperliquid
	client := exchange.Client(hyperliquid.NewClient(
		ctx,
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysCfg.Logging,
	))

	adapter := hyperliquid.NewWsAdapter()
	return buildProvider(exchange.ExchangeHyperliquid, exchange.ExchangeHyperliquid, cfg, apiCfg, client, adapter), nil
}

func buildProvider(
	providerName string,
	watcherExchangeName string,
	cfg ProviderFactoryConfig,
	apiCfg sysconfig.APIConfig,
	client exchange.Client,
	adapter ws.ExchangeAdapter,
) *ExchangeProvider {
	sysCfg := cfg.SystemConfig
	if sysCfg.DryRun {
		client = exchange.NewDryRunClient(client)
	}

	wsPool := newWSPool(watcherExchangeName, apiCfg, adapter, cfg.Logger, apiCfg.APIKey, apiCfg.APISecret)
	adapter.SetPool(wsPool)

	return &ExchangeProvider{
		Name:     providerName,
		Client:   client,
		Adapter:  adapter,
		WS:       wsPool,
		TimeSync: timesync.New(client, time.Duration(sysCfg.Sync.Time)),
		Watcher:  watcher.NewOrderWatcher(cfg.Bus, watcherExchangeName, cfg.Logger.With("component", "order_watcher", "exchange", providerName)),
	}
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

	type PreprocessorProvider interface {
		GetPreprocessor() func([]byte) ([]byte, error)
	}
	if pp, ok := adapter.(PreprocessorProvider); ok {
		if preprocessor := pp.GetPreprocessor(); preprocessor != nil {
			wsClientOpts = append(wsClientOpts, pkgws.WithPreprocessor(preprocessor))
		}
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
	return cfg.ExchangeConfig.Bybit.Enable && (cfg.ExchangeConfig.Bybit.AccountType == "" || cfg.ExchangeConfig.Bybit.AccountType == "standard")
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

	adapter := bybit.NewWsAdapter()
	return buildProvider(exchange.ExchangeBybit, exchange.ExchangeBybit, cfg, apiCfg, client, adapter), nil
}

// BybitUnifiedProviderFactory builds Unified Bybit infrastructure (UTA).
type BybitUnifiedProviderFactory struct{}

func (BybitUnifiedProviderFactory) Name() string { return bybitUnifiedName }

func (BybitUnifiedProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Bybit.Enable && cfg.ExchangeConfig.Bybit.AccountType == "unified"
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

	adapter := bybit.NewWsAdapter()
	return buildProvider(bybitUnifiedName, exchange.ExchangeBybit, cfg, apiCfg, client, adapter), nil
}

// BingxProviderFactory builds BingX infrastructure.
type BingxProviderFactory struct{}

func (BingxProviderFactory) Name() string { return exchange.ExchangeBingx }

func (BingxProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Bingx.Enable
}

func (BingxProviderFactory) Build(_ context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Bingx
	client := exchange.Client(bingx.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysCfg.Logging,
	))

	adapter := bingx.NewWsAdapter()
	return buildProvider(exchange.ExchangeBingx, exchange.ExchangeBingx, cfg, apiCfg, client, adapter), nil
}

// KucoinProviderFactory builds KuCoin infrastructure.
type KucoinProviderFactory struct{}

func (KucoinProviderFactory) Name() string { return exchange.ExchangeKucoin }

func (KucoinProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Kucoin.Enable
}

func (KucoinProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Kucoin
	kucoinClient := kucoin.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		"", // Passphrase from environment variables or config
		sysCfg.Logging,
	)
	client := exchange.Client(kucoinClient)
	if sysCfg.DryRun {
		client = exchange.NewDryRunClient(client)
	}

	adapter := kucoin.NewWsAdapter()
	urlFunc := kucoin.GetURLFunc(ctx, kucoinClient)

	wsLogger := cfg.Logger.With("subsystem", "websocket", "exchange", exchange.ExchangeKucoin)
	wsClientOpts := []pkgws.ClientOption{
		pkgws.WithURLFunc(urlFunc),
	}
	if payload, interval := adapter.GetPingConfig(); payload != nil && interval > 0 {
		wsClientOpts = append(wsClientOpts, pkgws.WithPing(payload, interval))
	}
	if extractor := adapter.GetChannelExtractor(); extractor != nil {
		wsClientOpts = append(wsClientOpts, pkgws.WithChannelExtractor(extractor))
	}
	wsPool := pkgws.NewPool("", apiCfg.WebSocket.MaxPairsPerWSConn, wsLogger, wsClientOpts...)
	adapter.SetPool(wsPool)

	return &ExchangeProvider{
		Name:     exchange.ExchangeKucoin,
		Client:   client,
		Adapter:  adapter,
		WS:       wsPool,
		TimeSync: timesync.New(client, time.Duration(sysCfg.Sync.Time)),
		Watcher:  watcher.NewOrderWatcher(cfg.Bus, exchange.ExchangeKucoin, cfg.Logger.With("component", "order_watcher", "exchange", exchange.ExchangeKucoin)),
	}, nil
}

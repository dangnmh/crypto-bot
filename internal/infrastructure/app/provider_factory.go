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
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
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

const (
	bybitUnifiedName = "bybit"
)

// ProviderFactory builds one exchange provider from system configuration.
type ProviderFactory interface {
	Name() string
	Enabled(cfg *sysconfig.SystemConfig) bool
	Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error)
}

// ProviderFactoryConfig holds shared dependencies for provider construction.
type ProviderFactoryConfig struct {
	SystemConfig     *sysconfig.SystemConfig
	HTTPClient       *http.Client
	Logger           *slog.Logger
	Bus              *eventbus.Bus
	TimeSyncInterval time.Duration
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
		DeepcoinProviderFactory{},
	}
}

// MexcProviderFactory builds MEXC infrastructure.
type MexcProviderFactory struct{}

func (MexcProviderFactory) Name() string { return exchange.ExchangeMexc }

func (MexcProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Mexc.Enable
}

func (MexcProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
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
	return buildProvider(ctx, exchange.ExchangeMexc, exchange.ExchangeMexc, cfg, apiCfg, client, adapter), nil
}

// GateProviderFactory builds Gate.io infrastructure.
type GateProviderFactory struct{}

func (GateProviderFactory) Name() string { return exchange.ExchangeGate }

func (GateProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Gate.Enable
}

func (GateProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
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
	return buildProvider(ctx, exchange.ExchangeGate, exchange.ExchangeGate, cfg, apiCfg, client, adapter), nil
}

// BinanceProviderFactory builds Binance infrastructure.
type BinanceProviderFactory struct{}

func (BinanceProviderFactory) Name() string { return exchange.ExchangeBinance }

func (BinanceProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Binance.Enable
}

func (BinanceProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Binance
	client := exchange.Client(binance.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysCfg.Logging,
	))

	adapter := binance.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint())
	adapter.SetURLs(apiCfg.WebSocket.PublicEndpoint(), apiCfg.WebSocket.MarketEndpoint())
	if concreteClient, ok := client.(*binance.Client); ok {
		adapter.SetClient(concreteClient)
	}
	return buildProvider(ctx, exchange.ExchangeBinance, exchange.ExchangeBinance, cfg, apiCfg, client, adapter), nil
}

// OkxProviderFactory builds OKX infrastructure.
type OkxProviderFactory struct{}

func (OkxProviderFactory) Name() string { return exchange.ExchangeOkx }

func (OkxProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Okx.Enable
}

func (OkxProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Okx
	client := exchange.Client(okx.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		apiCfg.APIPassphrase,
		sysCfg.Logging,
	))

	adapter := okx.NewWsAdapter(apiCfg.APIPassphrase)
	return buildProvider(ctx, exchange.ExchangeOkx, exchange.ExchangeOkx, cfg, apiCfg, client, adapter), nil
}

// BitgetProviderFactory builds Bitget infrastructure.
type BitgetProviderFactory struct{}

func (BitgetProviderFactory) Name() string { return exchange.ExchangeBitget }

func (BitgetProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Bitget.Enable
}

func (BitgetProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
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
	return buildProvider(ctx, exchange.ExchangeBitget, exchange.ExchangeBitget, cfg, apiCfg, client, adapter), nil
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
	return buildProvider(ctx, exchange.ExchangeHyperliquid, exchange.ExchangeHyperliquid, cfg, apiCfg, client, adapter), nil
}

func buildProvider(
	ctx context.Context,
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

	log := cfg.Logger.With("exchange", providerName)

	wsPool := newWSPool(ctx, watcherExchangeName, apiCfg, adapter, log, apiCfg.APIKey, apiCfg.APISecret)
	adapter.SetPool(wsPool)

	syncTime := cfg.TimeSyncInterval
	if syncTime <= 0 {
		syncTime = 30 * time.Second
	}
	ts := timesync.New(client, log, syncTime)
	type clockSetter interface {
		SetClock(exchange.Clock)
	}
	if setter, ok := client.(clockSetter); ok {
		setter.SetClock(ts)
	}
	if setter, ok := adapter.(clockSetter); ok {
		setter.SetClock(ts)
	}

	return &ExchangeProvider{
		Name:     providerName,
		Client:   client,
		Adapter:  adapter,
		WS:       wsPool,
		TimeSync: ts,
		Watcher:  watcher.NewOrderWatcher(cfg.Bus, watcherExchangeName, log),
	}
}

func newWSPool(
	ctx context.Context,
	exchangeName string,
	apiCfg sysconfig.APIConfig,
	adapter ws.ExchangeAdapter,
	logger *slog.Logger,
	apiKey string,
	apiSecret string,
) *pkgws.Pool {
	wsLogger := logger.With("subsystem", "websocket", "exchange", exchangeName)
	publicOpts := []pkgws.ClientOption{}
	privateOpts := []pkgws.ClientOption{}
	appendCommonOpt := func(opt pkgws.ClientOption) {
		publicOpts = append(publicOpts, opt)
		privateOpts = append(privateOpts, opt)
	}
	if payload, interval := adapter.GetPingConfig(); payload != nil && interval > 0 {
		appendCommonOpt(pkgws.WithPing(payload, interval))
	}
	if extractor := adapter.GetChannelExtractor(); extractor != nil {
		appendCommonOpt(pkgws.WithChannelExtractor(extractor))
	}
	if hook := adapter.GetAuthHook(apiKey, apiSecret); hook != nil {
		privateOpts = append(privateOpts, pkgws.WithOnConnected(hook))
	}
	privateOpts = append(privateOpts, pkgws.WithOnReady(func(c *pkgws.Client) {
		go func() {
			if err := adapter.SubscribePersonal(ctx); err != nil {
				wsLogger.Error("🔴 Failed to automatically subscribe/re-subscribe to personal channels", slog.Any("error", err))
			} else {
				wsLogger.Info("🟢 Automatically subscribed/re-subscribed to personal channels")
			}
		}()
	}))

	type PreprocessorProvider interface {
		GetPreprocessor() func([]byte) ([]byte, error)
	}
	if pp, ok := adapter.(PreprocessorProvider); ok {
		if preprocessor := pp.GetPreprocessor(); preprocessor != nil {
			appendCommonOpt(pkgws.WithPreprocessor(preprocessor))
		}
	}

	type PublicURLProvider interface {
		GetPublicURLFunc(ctx context.Context) func() (string, error)
	}
	if up, ok := adapter.(PublicURLProvider); ok {
		publicOpts = append(publicOpts, pkgws.WithURLFunc(up.GetPublicURLFunc(ctx)))
	}

	type PrivateURLProvider interface {
		GetPrivateURLFunc(ctx context.Context) func() (string, error)
	}
	if up, ok := adapter.(PrivateURLProvider); ok {
		privateOpts = append(privateOpts, pkgws.WithURLFunc(up.GetPrivateURLFunc(ctx)))
	}

	return pkgws.NewPoolWithURLs(
		apiCfg.WebSocket.PublicEndpoint(),
		apiCfg.WebSocket.PrivateEndpoint(),
		apiCfg.WebSocket.MaxPairsPerWSConn,
		wsLogger,
		publicOpts,
		privateOpts,
	)
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
	return cfg.ExchangeConfig.Bybit.Enable && sysconfig.NormalizeBybitAccountType(cfg.ExchangeConfig.Bybit.AccountType) != sysconfig.BybitAccountTypeUnified
}

func (BybitStandardProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Bybit
	accountType := sysconfig.NormalizeBybitAccountType(apiCfg.AccountType)
	client := exchange.Client(bybit.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		accountType,
		sysCfg.Logging,
	))

	adapter := bybit.NewWsAdapter()
	return buildProvider(ctx, exchange.ExchangeBybit, exchange.ExchangeBybit, cfg, apiCfg, client, adapter), nil
}

// BybitUnifiedProviderFactory builds Unified Bybit infrastructure (UTA).
type BybitUnifiedProviderFactory struct{}

func (BybitUnifiedProviderFactory) Name() string { return bybitUnifiedName }

func (BybitUnifiedProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Bybit.Enable && sysconfig.NormalizeBybitAccountType(cfg.ExchangeConfig.Bybit.AccountType) == sysconfig.BybitAccountTypeUnified
}

func (BybitUnifiedProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Bybit
	client := exchange.Client(bybit.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysconfig.BybitAccountTypeUnified,
		sysCfg.Logging,
	))

	adapter := bybit.NewWsAdapter()
	return buildProvider(ctx, bybitUnifiedName, exchange.ExchangeBybit, cfg, apiCfg, client, adapter), nil
}

// BingxProviderFactory builds BingX infrastructure.
type BingxProviderFactory struct{}

func (BingxProviderFactory) Name() string { return exchange.ExchangeBingx }

func (BingxProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Bingx.Enable
}

func (BingxProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Bingx
	client := exchange.Client(bingx.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysCfg.Logging,
	))

	adapter := bingx.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint())
	if concreteClient, ok := client.(*bingx.Client); ok {
		adapter.SetClient(concreteClient)
	}
	return buildProvider(ctx, exchange.ExchangeBingx, exchange.ExchangeBingx, cfg, apiCfg, client, adapter), nil
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
		apiCfg.APIPassphrase,
		sysCfg.Logging,
	)
	client := exchange.Client(kucoinClient)

	adapter := kucoin.NewWsAdapter()
	adapter.SetClient(kucoinClient)

	return buildProvider(ctx, exchange.ExchangeKucoin, exchange.ExchangeKucoin, cfg, apiCfg, client, adapter), nil
}

// DeepcoinProviderFactory builds Deepcoin infrastructure.
type DeepcoinProviderFactory struct{}

func (DeepcoinProviderFactory) Name() string { return exchange.ExchangeDeepcoin }

func (DeepcoinProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	return cfg.ExchangeConfig.Deepcoin.Enable
}

func (DeepcoinProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	sysCfg := cfg.SystemConfig
	apiCfg := sysCfg.ExchangeConfig.Deepcoin
	client := exchange.Client(deepcoin.NewClient(
		cfg.HTTPClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		apiCfg.APIPassphrase,
		sysCfg.Logging,
	))

	adapter := deepcoin.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint())
	if concreteClient, ok := client.(*deepcoin.Client); ok {
		adapter.SetClient(concreteClient)
	}
	return buildProvider(ctx, exchange.ExchangeDeepcoin, exchange.ExchangeDeepcoin, cfg, apiCfg, client, adapter), nil
}

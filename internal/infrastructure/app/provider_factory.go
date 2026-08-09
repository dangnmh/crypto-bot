package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/aster"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/internal/infrastructure/exchange/bingx"
	"crypto-bot/internal/infrastructure/exchange/bitget"
	"crypto-bot/internal/infrastructure/exchange/bitmart"
	"crypto-bot/internal/infrastructure/exchange/bitunix"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/hotcoin"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
	"crypto-bot/internal/infrastructure/exchange/orangex"
	"crypto-bot/internal/infrastructure/exchange/pionex"
	"crypto-bot/internal/infrastructure/exchange/toobit"
	"crypto-bot/internal/infrastructure/exchange/weex"
	"crypto-bot/internal/infrastructure/exchange/xt"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
	pkgws "crypto-bot/pkg/ws"

	"github.com/gorilla/websocket"
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

// SimpleProviderFactory is a closure-based implementation of ProviderFactory.
type SimpleProviderFactory struct {
	name        string
	enabledFunc func(cfg *sysconfig.SystemConfig) bool
	buildFunc   func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error)
}

func (s SimpleProviderFactory) Name() string {
	return s.name
}

func (s SimpleProviderFactory) Enabled(cfg *sysconfig.SystemConfig) bool {
	if s.enabledFunc != nil {
		return s.enabledFunc(cfg)
	}
	return cfg.ExchangeConfig[s.name].IsEnabled()
}

func (s SimpleProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	return s.buildFunc(ctx, cfg)
}

// DefaultProviderFactories returns the exchange factories supported by the app layer.
func DefaultProviderFactories() []ProviderFactory {
	mexcFactories := newExchangeFactories(exchange.ExchangeMexc, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(mexc.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		return client, mexc.NewWsAdapter()
	})

	toobitFactories := newExchangeFactories(exchange.ExchangeToobit, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(toobit.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		adapter := toobit.NewWsAdapter(ep.WebSocket.PrivateEndpoint())
		if concreteClient, ok := client.(*toobit.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	orangexFactories := newExchangeFactories(exchange.ExchangeOrangex, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := orangex.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging)
		return client, orangex.NewWsAdapter(client)
	})

	pionexFactories := newExchangeFactories(exchange.ExchangePionex, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := pionex.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging)
		return client, pionex.NewWsAdapter(client, ep.WebSocket.PrivateURL)
	})

	bitunixFactories := newExchangeFactories(exchange.ExchangeBitunix, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(bitunix.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		return client, bitunix.NewWsAdapter()
	})

	gateFactories := newExchangeFactories(exchange.ExchangeGate, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(gate.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		return client, gate.NewWsAdapter()
	})

	bybitFactories := newExchangeFactories(exchange.ExchangeBybit, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		accountType := sysconfig.NormalizeBybitAccountType(apiCfg.AccountType)
		client := exchange.Client(bybit.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, accountType, cfg.SystemConfig.Logging))
		return client, bybit.NewWsAdapter()
	})

	binanceFactories := newExchangeFactories(exchange.ExchangeBinance, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(binance.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		adapter := binance.NewWsAdapter(ep.WebSocket.PrivateEndpoint())
		adapter.SetURLs(ep.WebSocket.PublicEndpoint(), ep.WebSocket.MarketEndpoint())
		if concreteClient, ok := client.(*binance.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	okxFactories := newExchangeFactories(exchange.ExchangeOkx, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(okx.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
		adapter := okx.NewWsAdapter(apiCfg.APIPassphrase)
		return client, adapter
	})

	hyperliquidFactories := newExchangeFactories(exchange.ExchangeHyperliquid, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(hyperliquid.NewClient(ctx, cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		adapter := hyperliquid.NewWsAdapter()
		return client, adapter
	})

	bitgetFactories := newExchangeFactories(exchange.ExchangeBitget, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(bitget.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
		adapter := bitget.NewWsAdapter(apiCfg.APIPassphrase)
		return client, adapter
	})

	bingxFactories := newExchangeFactories(exchange.ExchangeBingx, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(bingx.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		adapter := bingx.NewWsAdapter(ep.WebSocket.PrivateEndpoint())
		if concreteClient, ok := client.(*bingx.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	kucoinFactories := newExchangeFactories(exchange.ExchangeKucoin, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		kucoinClient := kucoin.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging)
		client := exchange.Client(kucoinClient)
		adapter := kucoin.NewWsAdapter()
		adapter.SetClient(kucoinClient)
		return client, adapter
	})

	deepcoinFactories := newExchangeFactories(exchange.ExchangeDeepcoin, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(deepcoin.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
		adapter := deepcoin.NewWsAdapter(ep.WebSocket.PrivateEndpoint())
		if concreteClient, ok := client.(*deepcoin.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	weexFactories := newExchangeFactories(exchange.ExchangeWeex, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(weex.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
		adapter := weex.NewWsAdapter(apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase)
		if concreteClient, ok := client.(*weex.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	hotcoinFactories := newExchangeFactories(exchange.ExchangeHotcoin, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(hotcoin.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		adapter := hotcoin.NewWsAdapter(apiCfg.APIKey, apiCfg.APISecret)
		if concreteClient, ok := client.(*hotcoin.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	bitmartFactories := newExchangeFactories(exchange.ExchangeBitmart, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(bitmart.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
		adapter := bitmart.NewWsAdapter(ep.WebSocket.PrivateEndpoint(), apiCfg.APIPassphrase)
		if concreteClient, ok := client.(*bitmart.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	xtFactories := newExchangeFactories(exchange.ExchangeXt, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(xt.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
		adapter := xt.NewWsAdapter()
		if concreteClient, ok := client.(*xt.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	asterFactories := newExchangeFactories(exchange.ExchangeAster, func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter) {
		client := exchange.Client(aster.NewClient(cfg.HTTPClient, ep.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
		adapter := aster.NewWsAdapter(apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, ep.WebSocket.PrivateEndpoint())
		if concreteClient, ok := client.(*aster.Client); ok {
			adapter.SetClient(concreteClient)
		}
		return client, adapter
	})

	factories := make([]ProviderFactory, 0, 38)
	factories = append(factories, mexcFactories...)
	factories = append(factories, toobitFactories...)
	factories = append(factories, orangexFactories...)
	factories = append(factories, pionexFactories...)
	factories = append(factories, bitunixFactories...)
	factories = append(factories, gateFactories...)
	factories = append(factories, bybitFactories...)
	factories = append(factories, binanceFactories...)
	factories = append(factories, okxFactories...)
	factories = append(factories, hyperliquidFactories...)
	factories = append(factories, bitgetFactories...)
	factories = append(factories, bingxFactories...)
	factories = append(factories, kucoinFactories...)
	factories = append(factories, deepcoinFactories...)
	factories = append(factories, weexFactories...)
	factories = append(factories, hotcoinFactories...)
	factories = append(factories, bitmartFactories...)
	factories = append(factories, xtFactories...)
	factories = append(factories, asterFactories...)
	return factories
}

func buildProvider(
	ctx context.Context,
	providerName string,
	watcherExchangeName string,
	cfg ProviderFactoryConfig,
	ep sysconfig.EndpointConfig,
	apiCfg sysconfig.APIConfig,
	client exchange.Client,
	adapter ws.ExchangeAdapter,
) *ExchangeProvider {
	sysCfg := cfg.SystemConfig
	if sysCfg.DryRun {
		client = exchange.NewDryRunClient(client)
	}

	log := cfg.Logger.With("exchange", providerName)

	wsPool := newWSPool(ctx, watcherExchangeName, ep, adapter, log, apiCfg.APIKey, apiCfg.APISecret)
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

func buildWSCommonOpts(adapter ws.ExchangeAdapter) []pkgws.ClientOption {
	var opts []pkgws.ClientOption
	if payload, interval := adapter.GetPingConfig(); payload != nil && interval > 0 {
		opts = append(opts, pkgws.WithPing(payload, interval))
	}
	if extractor := adapter.GetChannelExtractor(); extractor != nil {
		opts = append(opts, pkgws.WithChannelExtractor(extractor))
	}
	type CustomPingHandlerProvider interface {
		GetCustomPingHandler() func(*websocket.Conn, []byte) bool
	}
	if cph, ok := adapter.(CustomPingHandlerProvider); ok {
		if handler := cph.GetCustomPingHandler(); handler != nil {
			opts = append(opts, pkgws.WithCustomPingHandler(handler))
		}
	}
	type PreprocessorProvider interface {
		GetPreprocessor() func([]byte) ([]byte, error)
	}
	if pp, ok := adapter.(PreprocessorProvider); ok {
		if preprocessor := pp.GetPreprocessor(); preprocessor != nil {
			opts = append(opts, pkgws.WithPreprocessor(preprocessor))
		}
	}
	return opts
}

func buildWSPrivateOpts(
	ctx context.Context,
	adapter ws.ExchangeAdapter,
	wsLogger *slog.Logger,
	apiKey string,
	apiSecret string,
) []pkgws.ClientOption {
	var opts []pkgws.ClientOption
	if hook := adapter.GetAuthHook(apiKey, apiSecret); hook != nil {
		opts = append(opts, pkgws.WithOnConnected(hook))
	}
	opts = append(opts, pkgws.WithOnReady(func(c *pkgws.Client) {
		go func() {
			if err := adapter.SubscribePersonal(ctx); err != nil {
				wsLogger.Error("🔴 Failed to automatically subscribe/re-subscribe to personal channels", slog.Any("error", err))
			} else {
				wsLogger.Info("🟢 Automatically subscribed/re-subscribed to personal channels")
			}
		}()
	}))
	type PrivateURLProvider interface {
		GetPrivateURLFunc(ctx context.Context) func() (string, error)
	}
	if up, ok := adapter.(PrivateURLProvider); ok {
		opts = append(opts, pkgws.WithURLFunc(up.GetPrivateURLFunc(ctx)))
	}
	type HeadersProvider interface {
		HandshakeHeaders() (http.Header, error)
	}
	if hp, ok := adapter.(HeadersProvider); ok {
		opts = append(opts, pkgws.WithHeadersFunc(hp.HandshakeHeaders))
	}
	return opts
}

func newWSPool(
	ctx context.Context,
	exchangeName string,
	ep sysconfig.EndpointConfig,
	adapter ws.ExchangeAdapter,
	logger *slog.Logger,
	apiKey string,
	apiSecret string,
) *pkgws.Pool {
	wsLogger := logger.With("subsystem", "websocket", "exchange", exchangeName)
	commonOpts := buildWSCommonOpts(adapter)
	publicOpts := append([]pkgws.ClientOption{}, commonOpts...)
	privateOpts := append([]pkgws.ClientOption{}, commonOpts...)

	privateOpts = append(privateOpts, buildWSPrivateOpts(ctx, adapter, wsLogger, apiKey, apiSecret)...)

	type PublicURLProvider interface {
		GetPublicURLFunc(ctx context.Context) func() (string, error)
	}
	if up, ok := adapter.(PublicURLProvider); ok {
		publicOpts = append(publicOpts, pkgws.WithURLFunc(up.GetPublicURLFunc(ctx)))
	}

	return pkgws.NewPoolWithURLs(
		ep.WebSocket.PublicEndpoint(),
		ep.WebSocket.PrivateEndpoint(),
		ep.WebSocket.MaxPairsPerWSConn,
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

func newMarketVariantFactory(
	variantName string,
	baseExchange string,
	isFutures bool,
	clientFunc func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter),
) ProviderFactory {
	return SimpleProviderFactory{
		name: variantName,
		enabledFunc: func(cfg *sysconfig.SystemConfig) bool {
			if c, ok := cfg.ExchangeConfig[variantName]; ok {
				return c.IsEnabled()
			}
			m := cfg.ExchangeConfig[baseExchange]
			if isFutures {
				return m.Future != nil && m.Future.Enable
			}
			return m.Spot != nil && m.Spot.Enable
		},
		buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
			apiCfg, ok := cfg.SystemConfig.ExchangeConfig[variantName]
			if !ok {
				apiCfg = cfg.SystemConfig.ExchangeConfig[baseExchange]
			}
			var ep sysconfig.EndpointConfig
			if isFutures {
				ep = apiCfg.GetFutureEndpoint()
			} else {
				ep = apiCfg.GetSpotEndpoint()
			}
			client, adapter := clientFunc(ctx, cfg, ep, apiCfg)
			return buildProvider(ctx, variantName, variantName, cfg, ep, apiCfg, client, adapter), nil
		},
	}
}

func newExchangeFactories(
	baseExchange string,
	clientFunc func(ctx context.Context, cfg ProviderFactoryConfig, ep sysconfig.EndpointConfig, apiCfg sysconfig.APIConfig) (exchange.Client, ws.ExchangeAdapter),
) []ProviderFactory {
	return []ProviderFactory{
		newMarketVariantFactory(baseExchange+"_spot", baseExchange, false, clientFunc),
		newMarketVariantFactory(baseExchange+"_futures", baseExchange, true, clientFunc),
	}
}

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
	"crypto-bot/internal/infrastructure/exchange/bitmart"
	"crypto-bot/internal/infrastructure/exchange/bitunix"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/internal/infrastructure/exchange/deepcoin"
	"crypto-bot/internal/infrastructure/exchange/gate"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/internal/infrastructure/exchange/okx"
	"crypto-bot/internal/infrastructure/exchange/toobit"
	"crypto-bot/internal/infrastructure/exchange/weex"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
	pkgws "crypto-bot/pkg/ws"

	"github.com/gorilla/websocket"
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
	return cfg.ExchangeConfig[s.name].Enable
}

func (s SimpleProviderFactory) Build(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
	return s.buildFunc(ctx, cfg)
}

// DefaultProviderFactories returns the exchange factories supported by the app layer.
func DefaultProviderFactories() []ProviderFactory {
	return []ProviderFactory{
		SimpleProviderFactory{
			name: exchange.ExchangeMexc,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeMexc]
				client := exchange.Client(mexc.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
				return buildProvider(ctx, exchange.ExchangeMexc, exchange.ExchangeMexc, cfg, apiCfg, client, mexc.NewWsAdapter()), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeBitunix,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeBitunix]
				client := exchange.Client(bitunix.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
				return buildProvider(ctx, exchange.ExchangeBitunix, exchange.ExchangeBitunix, cfg, apiCfg, client, bitunix.NewWsAdapter()), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeGate,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeGate]
				client := exchange.Client(gate.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
				return buildProvider(ctx, exchange.ExchangeGate, exchange.ExchangeGate, cfg, apiCfg, client, gate.NewWsAdapter()), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeBybit,
			enabledFunc: func(cfg *sysconfig.SystemConfig) bool {
				bybitCfg := cfg.ExchangeConfig["bybit"]
				return bybitCfg.Enable && sysconfig.NormalizeBybitAccountType(bybitCfg.AccountType) != sysconfig.BybitAccountTypeUnified
			},
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig["bybit"]
				accountType := sysconfig.NormalizeBybitAccountType(apiCfg.AccountType)
				client := exchange.Client(bybit.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, accountType, cfg.SystemConfig.Logging))
				return buildProvider(ctx, exchange.ExchangeBybit, exchange.ExchangeBybit, cfg, apiCfg, client, bybit.NewWsAdapter()), nil
			},
		},
		SimpleProviderFactory{
			name: bybitUnifiedName,
			enabledFunc: func(cfg *sysconfig.SystemConfig) bool {
				bybitCfg := cfg.ExchangeConfig["bybit"]
				return bybitCfg.Enable && sysconfig.NormalizeBybitAccountType(bybitCfg.AccountType) == sysconfig.BybitAccountTypeUnified
			},
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig["bybit"]
				client := exchange.Client(bybit.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, sysconfig.BybitAccountTypeUnified, cfg.SystemConfig.Logging))
				return buildProvider(ctx, bybitUnifiedName, exchange.ExchangeBybit, cfg, apiCfg, client, bybit.NewWsAdapter()), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeBinance,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeBinance]
				client := exchange.Client(binance.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
				adapter := binance.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint())
				adapter.SetURLs(apiCfg.WebSocket.PublicEndpoint(), apiCfg.WebSocket.MarketEndpoint())
				if concreteClient, ok := client.(*binance.Client); ok {
					adapter.SetClient(concreteClient)
				}
				return buildProvider(ctx, exchange.ExchangeBinance, exchange.ExchangeBinance, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeOkx,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeOkx]
				client := exchange.Client(okx.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
				adapter := okx.NewWsAdapter(apiCfg.APIPassphrase)
				return buildProvider(ctx, exchange.ExchangeOkx, exchange.ExchangeOkx, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeHyperliquid,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeHyperliquid]
				client := exchange.Client(hyperliquid.NewClient(ctx, cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
				adapter := hyperliquid.NewWsAdapter()
				return buildProvider(ctx, exchange.ExchangeHyperliquid, exchange.ExchangeHyperliquid, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeBitget,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeBitget]
				client := exchange.Client(bitget.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, "", cfg.SystemConfig.Logging))
				adapter := bitget.NewWsAdapter()
				return buildProvider(ctx, exchange.ExchangeBitget, exchange.ExchangeBitget, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeBingx,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeBingx]
				client := exchange.Client(bingx.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
				adapter := bingx.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint())
				if concreteClient, ok := client.(*bingx.Client); ok {
					adapter.SetClient(concreteClient)
				}
				return buildProvider(ctx, exchange.ExchangeBingx, exchange.ExchangeBingx, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeKucoin,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeKucoin]
				kucoinClient := kucoin.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging)
				client := exchange.Client(kucoinClient)
				adapter := kucoin.NewWsAdapter()
				adapter.SetClient(kucoinClient)
				return buildProvider(ctx, exchange.ExchangeKucoin, exchange.ExchangeKucoin, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeDeepcoin,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeDeepcoin]
				client := exchange.Client(deepcoin.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
				adapter := deepcoin.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint())
				if concreteClient, ok := client.(*deepcoin.Client); ok {
					adapter.SetClient(concreteClient)
				}
				return buildProvider(ctx, exchange.ExchangeDeepcoin, exchange.ExchangeDeepcoin, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeToobit,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeToobit]
				client := exchange.Client(toobit.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, cfg.SystemConfig.Logging))
				adapter := toobit.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint())
				if concreteClient, ok := client.(*toobit.Client); ok {
					adapter.SetClient(concreteClient)
				}
				return buildProvider(ctx, exchange.ExchangeToobit, exchange.ExchangeToobit, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeWeex,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeWeex]
				client := exchange.Client(weex.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
				adapter := weex.NewWsAdapter(apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase)
				if concreteClient, ok := client.(*weex.Client); ok {
					adapter.SetClient(concreteClient)
				}
				return buildProvider(ctx, exchange.ExchangeWeex, exchange.ExchangeWeex, cfg, apiCfg, client, adapter), nil
			},
		},
		SimpleProviderFactory{
			name: exchange.ExchangeBitmart,
			buildFunc: func(ctx context.Context, cfg ProviderFactoryConfig) (*ExchangeProvider, error) {
				apiCfg := cfg.SystemConfig.ExchangeConfig[exchange.ExchangeBitmart]
				client := exchange.Client(bitmart.NewClient(cfg.HTTPClient, apiCfg.Future.BaseURL, apiCfg.APIKey, apiCfg.APISecret, apiCfg.APIPassphrase, cfg.SystemConfig.Logging))
				adapter := bitmart.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint(), apiCfg.APIPassphrase)
				if concreteClient, ok := client.(*bitmart.Client); ok {
					adapter.SetClient(concreteClient)
				}
				return buildProvider(ctx, exchange.ExchangeBitmart, exchange.ExchangeBitmart, cfg, apiCfg, client, adapter), nil
			},
		},
	}
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
	type CustomPingHandlerProvider interface {
		GetCustomPingHandler() func(*websocket.Conn, []byte) bool
	}
	if cph, ok := adapter.(CustomPingHandlerProvider); ok {
		if handler := cph.GetCustomPingHandler(); handler != nil {
			appendCommonOpt(pkgws.WithCustomPingHandler(handler))
		}
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

	type HeadersProvider interface {
		HandshakeHeaders() (http.Header, error)
	}
	if hp, ok := adapter.(HeadersProvider); ok {
		privateOpts = append(privateOpts, pkgws.WithHeadersFunc(hp.HandshakeHeaders))
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

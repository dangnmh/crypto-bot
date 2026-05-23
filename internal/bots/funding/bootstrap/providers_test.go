package bootstrap

import (
	"context"
	"log/slog"
	"net/http"
	"reflect"
	"testing"

	fundingconfig "crypto-bot/internal/bots/funding/config"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

type recordingLifecycle struct {
	hooks []fx.Hook
}

func (r *recordingLifecycle) Append(hook fx.Hook) {
	r.hooks = append(r.hooks, hook)
}

func TestProvideNotifierUsesNoopAndRegistersLifecycle(t *testing.T) {
	t.Parallel()

	lc := &recordingLifecycle{}
	n, err := provideNotifier(lc, &fundingconfig.SystemConfig{}, slog.Default())
	require.NoError(t, err)
	require.Implements(t, (*notifier.Notifier)(nil), n)
	require.Len(t, lc.hooks, 1)
	require.NoError(t, lc.hooks[0].OnStart(context.Background()))
	require.NoError(t, lc.hooks[0].OnStop(context.Background()))
}

func TestProvideLoggerRegistersCleanup(t *testing.T) {
	lc := &recordingLifecycle{}

	log := provideLogger(lc, &fundingconfig.SystemConfig{
		SystemConfig: sysconfig.SystemConfig{
			Logging: sysconfig.LoggingConfig{Level: "error"},
		},
	})

	require.NotNil(t, log)
	require.Len(t, lc.hooks, 1)
	require.NoError(t, lc.hooks[0].OnStop(context.Background()))
}

func TestProvideHTTPClient(t *testing.T) {
	t.Parallel()

	client := provideHTTPClient()
	require.NotNil(t, client)
	require.IsType(t, &http.Client{}, client)
	require.NotNil(t, client.Transport)
}

func TestProvideExchangeClientWrapsDryRun(t *testing.T) {
	t.Parallel()

	cfg := &fundingconfig.SystemConfig{
		SystemConfig: sysconfig.SystemConfig{
			DryRun: true,
			ExchangeConfig: sysconfig.ExchangeConfig{
				Mexc: sysconfig.APIConfig{
					Future: sysconfig.RESTConfig{BaseURL: "https://example.test"},
				},
			},
		},
	}

	client := provideExchangeClient(&http.Client{}, cfg, slog.Default())
	require.IsType(t, &exchange.DryRunClient{}, client)
}

func TestProvideExchangeClientLive(t *testing.T) {
	t.Parallel()

	cfg := &fundingconfig.SystemConfig{
		SystemConfig: sysconfig.SystemConfig{
			ExchangeConfig: sysconfig.ExchangeConfig{
				Mexc: sysconfig.APIConfig{
					Future: sysconfig.RESTConfig{BaseURL: "https://example.test"},
				},
			},
		},
	}

	client := provideExchangeClient(&http.Client{}, cfg, slog.Default())
	require.NotNil(t, client)
	require.NotEqual(t, reflect.TypeOf(&exchange.DryRunClient{}), reflect.TypeOf(client))
}

func TestProvideWSAdapter(t *testing.T) {
	t.Parallel()

	require.NotNil(t, provideWSAdapter())
}

func TestProvideEngine(t *testing.T) {
	t.Parallel()

	cfg := &fundingconfig.SystemConfig{
		SystemConfig: sysconfig.SystemConfig{
			ExchangeConfig: sysconfig.ExchangeConfig{
				Mexc: sysconfig.APIConfig{
					Future: sysconfig.RESTConfig{BaseURL: "https://example.test"},
					WebSocket: sysconfig.WebSocketConfig{
						WSURL:             "wss://example.test/ws",
						MaxPairsPerWSConn: 2,
					},
				},
			},
		},
	}
	adapter := provideWSAdapter()
	client := provideExchangeClient(&http.Client{}, cfg, slog.Default())

	engine := provideEngine(cfg, client, adapter)
	require.NotNil(t, engine)
	require.Same(t, &cfg.SystemConfig, engine.Cfg)
	require.Same(t, client, engine.Client)
	require.Same(t, adapter, engine.Adapter)
	require.NotNil(t, engine.TimeSync)
	require.NotNil(t, engine.WS)
	require.NotNil(t, engine.Bus)
}

func TestProvideBot(t *testing.T) {
	t.Parallel()

	cfg := &fundingconfig.Config{System: &fundingconfig.SystemConfig{}}
	sysCfg := &fundingconfig.SystemConfig{
		SystemConfig: sysconfig.SystemConfig{
			ExchangeConfig: sysconfig.ExchangeConfig{
				Mexc: sysconfig.APIConfig{
					Future: sysconfig.RESTConfig{BaseURL: "https://example.test"},
					WebSocket: sysconfig.WebSocketConfig{
						WSURL:             "wss://example.test/ws",
						MaxPairsPerWSConn: 2,
					},
				},
			},
		},
	}
	cfg.System = sysCfg
	engine := provideEngine(sysCfg, provideExchangeClient(&http.Client{}, sysCfg, slog.Default()), provideWSAdapter())

	bot := provideBot(cfg, sysCfg, engine, &notifierStub{}, slog.Default())
	require.NotNil(t, bot)
}

type notifierStub struct{}

func (n *notifierStub) Send(context.Context, notifier.Event) error { return nil }
func (n *notifierStub) Start(context.Context) error                { return nil }
func (n *notifierStub) Stop(context.Context) error                 { return nil }

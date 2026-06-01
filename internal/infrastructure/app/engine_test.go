package app_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/app"

	"crypto-bot/internal/domain"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/types"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockAdapter for testing engine setup.
type mockAdapter struct {
	pingPayload []byte
	pingInt     time.Duration
	pool        *pkgws.Pool
}

func (m *mockAdapter) GetPingConfig() (any, time.Duration) {
	return m.pingPayload, m.pingInt
}
func (m *mockAdapter) GetChannelExtractor() func([]byte) string { return nil }
func (m *mockAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}
func (m *mockAdapter) SetPool(p *pkgws.Pool) { m.pool = p }

// Subscriber methods.
func (m *mockAdapter) SubscribeTicker(context.Context, string) error          { return nil }
func (m *mockAdapter) UnsubscribeTicker(context.Context, string) error        { return nil }
func (m *mockAdapter) SubscribeKline(context.Context, string) error           { return nil }
func (m *mockAdapter) UnsubscribeKline(context.Context, string) error         { return nil }
func (m *mockAdapter) SubscribeDepth(context.Context, string, string) error   { return nil }
func (m *mockAdapter) UnsubscribeDepth(context.Context, string, string) error { return nil }
func (m *mockAdapter) SubscribePersonal(context.Context) error                { return nil }

// Parser methods.
func (m *mockAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) { return "", nil, nil }
func (m *mockAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) { return "", nil, nil }
func (m *mockAdapter) ParseKline(data []byte) (string, *domain.Kline, error)     { return "", nil, nil }
func (m *mockAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error)     { return nil, nil }
func (m *mockAdapter) ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error) {
	return nil, nil
}
func (m *mockAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	return nil, nil
}
func (m *mockAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}

type dummyClient struct{ exchange.Client }

func (d *dummyClient) GetTickers(_ context.Context, _ string) ([]exchange.Ticker, error) {
	return nil, nil
}
func (d *dummyClient) GetContractDetails(_ context.Context) ([]exchange.ContractDetail, error) {
	return nil, nil
}
func (d *dummyClient) GetFundingRates(_ context.Context, _ []string) ([]exchange.FundingRateResult, error) {
	return []exchange.FundingRateResult{
		{
			Symbol:     "BTC_USDT",
			Rate:       0.01,
			SettleTime: time.Now().Add(time.Hour).UnixMilli(),
		},
	}, nil
}

type fakeProviderFactory struct {
	name    string
	enabled bool
	err     error
}

func (f fakeProviderFactory) Name() string { return f.name }
func (f fakeProviderFactory) Enabled(*sysconfig.SystemConfig) bool {
	return f.enabled
}
func (f fakeProviderFactory) Build(context.Context, app.ProviderFactoryConfig) (*app.ExchangeProvider, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &app.ExchangeProvider{Name: f.name, Client: &dummyClient{}}, nil
}

func TestNewEngine_Success(t *testing.T) {
	t.Parallel()

	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			Mexc: sysconfig.APIConfig{
				Enable:    true,
				Future:    sysconfig.RESTConfig{BaseURL: "https://api.mexc.com"},
				WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.mexc.com", MaxPairsPerWSConn: 10},
				APIKey:    "mexc-key",
				APISecret: "mexc-secret",
			},
			Gate: sysconfig.APIConfig{
				Enable:    true,
				Future:    sysconfig.RESTConfig{BaseURL: "https://api.gate.com"},
				WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.gate.com", MaxPairsPerWSConn: 5},
				APIKey:    "gate-key",
				APISecret: "gate-secret",
			},
		},
		Logging: sysconfig.LoggingConfig{Level: "debug"},
	}

	engineCfg := app.EngineConfig{
		SystemConfig: cfg,
		Logger:       testLogger(),
	}

	e, err := app.NewEngine(context.Background(), engineCfg)
	require.NoError(t, err)
	require.NotNil(t, e)
	assert.NotNil(t, e.Cfg)
	assert.NotNil(t, e.Bus)

	// Ensure both MEXC and Gate providers are registered
	assert.Len(t, e.Providers, 2)
	assert.Contains(t, e.Providers, "mexc")
	assert.Contains(t, e.Providers, "gate")

	mexcProv := e.Providers["mexc"]
	assert.Equal(t, "mexc", mexcProv.Name)
	assert.NotNil(t, mexcProv.Client)
	assert.NotNil(t, mexcProv.Adapter)
	assert.NotNil(t, mexcProv.WS)
	assert.NotNil(t, mexcProv.TimeSync)
	assert.NotNil(t, mexcProv.Watcher)

	gateProv := e.Providers["gate"]
	assert.Equal(t, "gate", gateProv.Name)
	assert.NotNil(t, gateProv.Client)
	assert.NotNil(t, gateProv.Adapter)
	assert.NotNil(t, gateProv.WS)
	assert.NotNil(t, gateProv.TimeSync)
	assert.NotNil(t, gateProv.Watcher)

	// Test Shutdown
	assert.NotPanics(t, func() {
		_ = e.Shutdown(context.Background())
	})
}

func TestNewEngine_ValidatesCoreDependencies(t *testing.T) {
	t.Parallel()

	_, err := app.NewEngine(context.Background(), app.EngineConfig{Logger: testLogger()})
	require.ErrorContains(t, err, "system config is required")

	_, err = app.NewEngine(context.Background(), app.EngineConfig{SystemConfig: &sysconfig.SystemConfig{}})
	require.ErrorContains(t, err, "logger is required")
}

func TestNewEngine_CustomProviderFactoryPaths(t *testing.T) {
	t.Parallel()

	cfg := &sysconfig.SystemConfig{}

	_, err := app.NewEngine(context.Background(), app.EngineConfig{
		SystemConfig:      cfg,
		Logger:            testLogger(),
		ProviderFactories: []app.ProviderFactory{fakeProviderFactory{name: "fake", enabled: false}},
	})
	require.ErrorContains(t, err, "no exchange providers configured")

	_, err = app.NewEngine(context.Background(), app.EngineConfig{
		SystemConfig:      cfg,
		Logger:            testLogger(),
		ProviderFactories: []app.ProviderFactory{fakeProviderFactory{name: "fake", enabled: true, err: errors.New("boom")}},
	})
	require.ErrorContains(t, err, "build fake provider")

	e, err := app.NewEngine(context.Background(), app.EngineConfig{
		SystemConfig:      cfg,
		Logger:            testLogger(),
		ProviderFactories: []app.ProviderFactory{fakeProviderFactory{name: "fake", enabled: true}},
	})
	require.NoError(t, err)
	defer func() { _ = e.Shutdown(context.Background()) }()

	prov, err := e.GetProvider("FAKE")
	require.NoError(t, err)
	assert.Equal(t, "fake", prov.Name)

	_, err = e.GetProvider("missing")
	require.ErrorContains(t, err, `exchange provider "missing"`)
}

func TestStoreRegistry_StartStores(t *testing.T) {
	t.Parallel()

	reg := app.NewStoreRegistry(testLogger())
	require.NotNil(t, reg.Ticker)
	require.NotNil(t, reg.Contract)
	require.NotNil(t, reg.Price)
	require.NotNil(t, reg.Depth)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // instantly cancel to prevent loops

	assert.NotPanics(t, func() {
		reg.StartStores(ctx, &dummyClient{}, app.StoreSyncConfig{
			TickerInterval:   types.Duration(time.Second),
			ContractInterval: types.Duration(time.Second),
		})
	})
}

func TestStoreRegistry_StartStoresWithFunding(t *testing.T) {
	t.Parallel()

	reg := app.NewStoreRegistry(testLogger()).WithFunding()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.NotPanics(t, func() {
		reg.StartStores(ctx, &dummyClient{}, app.StoreSyncConfig{
			TickerInterval:   types.Duration(time.Second),
			ContractInterval: types.Duration(time.Second),
			FundingInterval:  types.Duration(time.Second),
			FundingSymbols:   []string{"BTC_USDT"},
		})
	})
}

func TestStoreRegistry_WaitReadyContextCancelled(t *testing.T) {
	t.Parallel()

	reg := app.NewStoreRegistry(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := reg.WaitReady(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNewEngine_OnlyMexc(t *testing.T) {
	t.Parallel()

	assertSingleProviderEngine(t, exchange.ExchangeMexc, sysconfig.ExchangeConfig{
		Mexc: sysconfig.APIConfig{
			Enable:    true,
			Future:    sysconfig.RESTConfig{BaseURL: "https://api.example.com"},
			WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
		},
	})
}

func TestNewEngine_OnlyGate(t *testing.T) {
	t.Parallel()

	assertSingleProviderEngine(t, exchange.ExchangeGate, sysconfig.ExchangeConfig{
		Gate: sysconfig.APIConfig{
			Enable:    true,
			Future:    sysconfig.RESTConfig{BaseURL: "https://api.example.com"},
			WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
		},
	})
}

func TestNewEngine_BybitAccountTypeNormalization(t *testing.T) {
	t.Parallel()

	assertSingleProviderEngine(t, exchange.ExchangeBybit, sysconfig.ExchangeConfig{
		Bybit: sysconfig.APIConfig{
			Enable:      true,
			AccountType: " STANDARD ",
			Future:      sysconfig.RESTConfig{BaseURL: "https://api.bybit.com"},
			WebSocket: sysconfig.WebSocketConfig{
				PublicURL:         "wss://stream.bybit.com/v5/public/linear",
				PrivateURL:        "wss://stream.bybit.com/v5/private",
				MaxPairsPerWSConn: 10,
			},
		},
	})
}

func assertSingleProviderEngine(t *testing.T, exchangeName string, exchangeCfg sysconfig.ExchangeConfig) {
	t.Helper()

	e, err := app.NewEngine(context.Background(), app.EngineConfig{
		SystemConfig: &sysconfig.SystemConfig{ExchangeConfig: exchangeCfg},
		Logger:       testLogger(),
	})
	require.NoError(t, err)
	require.NotNil(t, e)
	assert.Len(t, e.Providers, 1)
	assert.Contains(t, e.Providers, exchangeName)

	_ = e.Shutdown(context.Background())
}

func TestEngine_Shutdown_NilWS(t *testing.T) {
	t.Parallel()

	e := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{},
	}

	assert.NotPanics(t, func() {
		_ = e.Shutdown(context.Background())
	})
}

func TestNewEngine_FilterByActiveExchanges(t *testing.T) {
	t.Parallel()

	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			Mexc: sysconfig.APIConfig{
				Enable:    true,
				Future:    sysconfig.RESTConfig{BaseURL: "https://api.mexc.com"},
				WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.mexc.com", MaxPairsPerWSConn: 10},
			},
			Gate: sysconfig.APIConfig{
				Enable:    true,
				Future:    sysconfig.RESTConfig{BaseURL: "https://api.gate.com"},
				WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.gate.com", MaxPairsPerWSConn: 5},
			},
		},
	}

	engineCfg := app.EngineConfig{
		SystemConfig:    cfg,
		Logger:          testLogger(),
		ActiveExchanges: []string{"mexc"}, // only Mexc is active in funding.jsonc
	}

	e, err := app.NewEngine(context.Background(), engineCfg)
	require.NoError(t, err)
	require.NotNil(t, e)

	// Mexc provider should be loaded, but Gate should be skipped despite being enabled in system config!
	assert.Len(t, e.Providers, 1)
	assert.Contains(t, e.Providers, "mexc")
	assert.NotContains(t, e.Providers, "gate")

	_ = e.Shutdown(context.Background())
}

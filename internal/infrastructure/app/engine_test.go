package app_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/app"
	"time"

	"crypto-bot/internal/domain"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/types"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockAdapter for testing engine setup.
type mockAdapter struct {
	pingPayload []byte
	pingInt     time.Duration
	pool        *pkgws.Pool
}

func (m *mockAdapter) GetPingConfig() (interface{}, time.Duration) {
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
func (d *dummyClient) GetFundingRate(_ context.Context, _ string) (*exchange.FundingRateDetail, error) {
	return &exchange.FundingRateDetail{
		Symbol:         "BTC_USDT",
		FundingRate:    0.01,
		NextSettleTime: time.Now().Add(time.Hour).UnixMilli(),
	}, nil
}

func TestNewEngine_Success(t *testing.T) {
	t.Parallel()

	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			Mexc: sysconfig.APIConfig{
				Future:    sysconfig.RESTConfig{BaseURL: "https://api.mexc.com"},
				WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.mexc.com", MaxPairsPerWSConn: 10},
				APIKey:    "mexc-key",
				APISecret: "mexc-secret",
			},
			Gate: sysconfig.APIConfig{
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
	}

	e := app.NewEngine(engineCfg)
	require.NotNil(t, e)
	assert.NotNil(t, e.Cfg)
	assert.NotNil(t, e.Client)
	assert.NotNil(t, e.Adapter)
	assert.NotNil(t, e.TimeSync)
	assert.NotNil(t, e.WS)
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

func TestStoreRegistry_StartStores(t *testing.T) {
	t.Parallel()

	reg := app.NewStoreRegistry()
	require.NotNil(t, reg.Ticker)
	require.NotNil(t, reg.Contract)
	require.NotNil(t, reg.Price)
	require.NotNil(t, reg.Depth)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // instantly cancel to prevent loops

	assert.NotPanics(t, func() {
		reg.StartStores(ctx, &app.Engine{Client: &dummyClient{}}, app.StoreSyncConfig{
			TickerInterval:   types.Duration(time.Second),
			ContractInterval: types.Duration(time.Second),
		})
	})
}

func TestStoreRegistry_StartStoresWithFunding(t *testing.T) {
	t.Parallel()

	reg := app.NewStoreRegistry().WithFunding()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.NotPanics(t, func() {
		reg.StartStores(ctx, &app.Engine{Client: &dummyClient{}}, app.StoreSyncConfig{
			TickerInterval:   types.Duration(time.Second),
			ContractInterval: types.Duration(time.Second),
			FundingInterval:  types.Duration(time.Second),
			FundingSymbols:   []string{"BTC_USDT"},
		})
	})
}

func TestStoreRegistry_WaitReadyContextCancelled(t *testing.T) {
	t.Parallel()

	reg := app.NewStoreRegistry()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := reg.WaitReady(ctx)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestNewEngine_OnlyMexc(t *testing.T) {
	t.Parallel()

	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			Mexc: sysconfig.APIConfig{
				Future:    sysconfig.RESTConfig{BaseURL: "https://api.example.com"},
				WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
			},
		},
	}

	e := app.NewEngine(app.EngineConfig{
		SystemConfig: cfg,
	})
	require.NotNil(t, e)
	assert.Len(t, e.Providers, 1)
	assert.Contains(t, e.Providers, "mexc")
	assert.NotNil(t, e.Client)

	_ = e.Shutdown(context.Background())
}

func TestNewEngine_OnlyGate(t *testing.T) {
	t.Parallel()

	cfg := &sysconfig.SystemConfig{
		ExchangeConfig: sysconfig.ExchangeConfig{
			Gate: sysconfig.APIConfig{
				Future:    sysconfig.RESTConfig{BaseURL: "https://api.example.com"},
				WebSocket: sysconfig.WebSocketConfig{WSURL: "wss://ws.example.com", MaxPairsPerWSConn: 10},
			},
		},
	}

	e := app.NewEngine(app.EngineConfig{
		SystemConfig: cfg,
	})
	require.NotNil(t, e)
	assert.Len(t, e.Providers, 1)
	assert.Contains(t, e.Providers, "gate")
	assert.NotNil(t, e.Client)

	_ = e.Shutdown(context.Background())
}

func TestEngine_Shutdown_NilWS(t *testing.T) {
	t.Parallel()

	e := &app.Engine{
		WS: nil,
	}

	assert.NotPanics(t, func() {
		_ = e.Shutdown(context.Background())
	})
}

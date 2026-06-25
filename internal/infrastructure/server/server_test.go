package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/server"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

type stubClient struct {
	lastParams  map[string]string
	lastOrderID string
	lastMethod  string
	returnBytes []byte
	returnErr   error
	tickers     []exchange.Ticker
	rates       []exchange.FundingRateResult
}

func (s *stubClient) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	if s.tickers != nil {
		return s.tickers, nil
	}
	return nil, nil
}
func (s *stubClient) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	return nil, nil
}
func (s *stubClient) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if s.rates != nil {
		return s.rates, nil
	}
	return nil, nil
}
func (s *stubClient) GetServerTime(ctx context.Context) (int64, error) { return 0, nil }
func (s *stubClient) GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist, blacklist []string) ([]exchange.PotentialFundingResult, error) {
	return nil, nil
}

func (s *stubClient) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	return exchange.CreateOrderResult{}, nil
}

func (s *stubClient) CancelOrder(ctx context.Context, symbol, orderID string) error { return nil }
func (s *stubClient) CancelOrders(ctx context.Context, orderIDs []string) error     { return nil }
func (s *stubClient) CancelAllOpenOrders(ctx context.Context, symbol string) error  { return nil }
func (s *stubClient) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	return nil, nil
}
func (s *stubClient) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	return nil, nil
}
func (s *stubClient) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	return nil, nil
}
func (s *stubClient) CloseAllPositions(ctx context.Context, symbol string) error { return nil }
func (s *stubClient) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
	return nil
}
func (s *stubClient) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	return nil
}
func (s *stubClient) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	return nil
}
func (s *stubClient) WarmUp(ctx context.Context, interval time.Duration) {}
func (s *stubClient) SupportLeverageOnOrder() bool                       { return false }

func (s *stubClient) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	return nil, nil
}

func (s *stubClient) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	return &exchange.ClosedPnLInfo{
		Exchange:  "mexc",
		Symbol:    symbol,
		ExitPrice: 50000,
	}, nil
}

func (s *stubClient) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetFundingRateRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClient) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetTickersRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClient) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetOpenPositionsRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClient) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetHistoryPositionsRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClient) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetOrderDetailRaw"
	s.lastOrderID = orderID
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClient) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetHistoryOrdersRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClient) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetOrderPNLRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

type stubClientNoClosedPnL struct {
	lastParams  map[string]string
	lastOrderID string
	lastMethod  string
	returnBytes []byte
	returnErr   error
}

func (s *stubClientNoClosedPnL) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	return nil, nil
}
func (s *stubClientNoClosedPnL) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	return nil, nil
}
func (s *stubClientNoClosedPnL) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	return nil, nil
}
func (s *stubClientNoClosedPnL) GetServerTime(ctx context.Context) (int64, error) { return 0, nil }
func (s *stubClientNoClosedPnL) GetPotentialFundingSymbols(ctx context.Context, minVol24h, maxVol24h float64, whitelist, blacklist []string) ([]exchange.PotentialFundingResult, error) {
	return nil, nil
}

func (s *stubClientNoClosedPnL) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	return exchange.CreateOrderResult{}, nil
}

func (s *stubClientNoClosedPnL) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return nil
}
func (s *stubClientNoClosedPnL) CancelOrders(ctx context.Context, orderIDs []string) error {
	return nil
}
func (s *stubClientNoClosedPnL) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	return nil
}
func (s *stubClientNoClosedPnL) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	return nil, nil
}
func (s *stubClientNoClosedPnL) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	return nil, nil
}
func (s *stubClientNoClosedPnL) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	return nil, nil
}
func (s *stubClientNoClosedPnL) CloseAllPositions(ctx context.Context, symbol string) error {
	return nil
}
func (s *stubClientNoClosedPnL) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
	return nil
}
func (s *stubClientNoClosedPnL) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	return nil
}
func (s *stubClientNoClosedPnL) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	return nil
}
func (s *stubClientNoClosedPnL) WarmUp(ctx context.Context, interval time.Duration) {}
func (s *stubClientNoClosedPnL) SupportLeverageOnOrder() bool                       { return false }

func (s *stubClientNoClosedPnL) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	return nil, nil
}

func (s *stubClientNoClosedPnL) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetFundingRateRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClientNoClosedPnL) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetTickersRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClientNoClosedPnL) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetOpenPositionsRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClientNoClosedPnL) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetHistoryPositionsRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClientNoClosedPnL) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetOrderDetailRaw"
	s.lastOrderID = orderID
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClientNoClosedPnL) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetHistoryOrdersRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func (s *stubClientNoClosedPnL) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	s.lastMethod = "GetOrderPNLRaw"
	s.lastParams = params
	return s.returnBytes, s.returnErr
}

func setupTestServer(t *testing.T, engine *app.Engine, port int) *server.APIServer {
	cfg := &sysconfig.SystemConfig{}
	cfg.APIServer.Port = port
	cfg.APIServer.Host = "127.0.0.1"
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	return server.NewAPIServer(engine, cfg, nil, logger)
}

func TestAPIServer_ExchangeValidationMiddleware(t *testing.T) {
	t.Parallel()
	engine := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:   "mexc",
				Client: &stubClient{},
			},
		},
	}
	srv := setupTestServer(t, engine, 9871)
	require.NoError(t, srv.Start(context.Background()))
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	// CallMEXC (Active)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9871/debug/mexc/tickers", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// Call OKX (Not Configured)
	reqOKX, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9871/debug/okx/tickers", http.NoBody)
	require.NoError(t, err)
	respOKX, err := http.DefaultClient.Do(reqOKX)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, respOKX.StatusCode)
	_ = respOKX.Body.Close()
}

func TestAPIServer_ProxyRoutes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		method string
		path   string
		body   string
		expect string
		port   int
	}{
		{http.MethodGet, "/debug/mexc/funding_rate?symbol=BTC", "", "GetFundingRateRaw", 9880},
		{http.MethodGet, "/debug/mexc/tickers", "", "GetTickersRaw", 9881},
		{http.MethodGet, "/debug/mexc/open_positions", "", "GetOpenPositionsRaw", 9882},
		{http.MethodGet, "/debug/mexc/history_positions", "", "GetHistoryPositionsRaw", 9883},
		{http.MethodGet, "/debug/mexc/history_orders", "", "GetHistoryOrdersRaw", 9884},
		{http.MethodGet, "/debug/mexc/order/order-123", "", "GetOrderDetailRaw", 9886},
	}

	for _, tt := range tests {
		t.Run(tt.expect, func(t *testing.T) {
			t.Parallel()
			client := &stubClient{
				returnBytes: []byte(`{"status":"success"}`),
			}
			engine := &app.Engine{
				Providers: map[string]*app.ExchangeProvider{
					"mexc": {
						Name:   "mexc",
						Client: client,
					},
				},
			}
			srv := setupTestServer(t, engine, tt.port)
			require.NoError(t, srv.Start(context.Background()))
			time.Sleep(50 * time.Millisecond)
			t.Cleanup(func() { _ = srv.Stop(context.Background()) })

			var req *http.Request
			var err error
			if tt.body != "" {
				req, err = http.NewRequestWithContext(context.Background(), tt.method, fmt.Sprintf("http://127.0.0.1:%d", tt.port)+tt.path, bytes.NewBufferString(tt.body))
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/json")
			} else {
				req, err = http.NewRequestWithContext(context.Background(), tt.method, fmt.Sprintf("http://127.0.0.1:%d", tt.port)+tt.path, http.NoBody)
				require.NoError(t, err)
			}

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, tt.expect, client.lastMethod)
			_ = resp.Body.Close()

			if tt.expect == "GetOrderDetailRaw" {
				assert.Equal(t, "order-123", client.lastOrderID)
			}
		})
	}
}

func TestAPIServer_OrderPNLProviderGate(t *testing.T) {
	t.Parallel()
	clientSupported := &stubClient{}
	clientNotSupported := &stubClientNoClosedPnL{}
	engine := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:   "mexc",
				Client: clientSupported,
			},
			"gate": {
				Name:   "gate",
				Client: clientNotSupported,
			},
		},
	}
	srv := setupTestServer(t, engine, 9873)
	require.NoError(t, srv.Start(context.Background()))
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	// MEXC (Supported)
	reqMexc, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9873/debug/mexc/order_pnl?symbol=BTCUSDT&order_id=123", http.NoBody)
	require.NoError(t, err)
	respMexc, err := http.DefaultClient.Do(reqMexc)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, respMexc.StatusCode)
	_ = respMexc.Body.Close()

	// Gate (Not Supported)
	reqGate, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9873/debug/gate/order_pnl?symbol=BTCUSDT&order_id=123", http.NoBody)
	require.NoError(t, err)
	respGate, err := http.DefaultClient.Do(reqGate)
	require.NoError(t, err)
	assert.Equal(t, http.StatusNotImplemented, respGate.StatusCode)
	_ = respGate.Body.Close()
}

func TestAPIServer_ParameterInjections(t *testing.T) {
	t.Parallel()
	clientBybit := &stubClient{}
	clientOKX := &stubClient{}
	engine := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{
			"bybit": {
				Name:   "bybit",
				Client: clientBybit,
			},
			"okx": {
				Name:   "okx",
				Client: clientOKX,
			},
		},
	}
	srv := setupTestServer(t, engine, 9874)
	require.NoError(t, srv.Start(context.Background()))
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	// Bybit parameter injection
	reqBybit, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9874/debug/bybit/tickers", http.NoBody)
	require.NoError(t, err)
	respBybit, err := http.DefaultClient.Do(reqBybit)
	require.NoError(t, err)
	_ = respBybit.Body.Close()
	assert.Equal(t, "linear", clientBybit.lastParams["category"])
	assert.Equal(t, "UNIFIED", clientBybit.lastParams["accountType"])

	// OKX parameter injection
	reqOKX, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9874/debug/okx/tickers", http.NoBody)
	require.NoError(t, err)
	respOKX, err := http.DefaultClient.Do(reqOKX)
	require.NoError(t, err)
	_ = respOKX.Body.Close()
	assert.Equal(t, "SWAP", clientOKX.lastParams["instType"])
}

func TestAPIServer_Lifecycle(t *testing.T) {
	t.Parallel()
	var srv *server.APIServer
	appFx := fxtest.New(t,
		fx.Supply(slog.Default()),
		fx.Provide(
			func() *app.Engine {
				return &app.Engine{
					Providers: map[string]*app.ExchangeProvider{
						"mexc": {
							Name:   "mexc",
							Client: &stubClient{},
						},
					},
				}
			},
			func() *sysconfig.SystemConfig {
				cfg := &sysconfig.SystemConfig{}
				cfg.APIServer.Port = 9875
				cfg.APIServer.Host = "127.0.0.1"
				return cfg
			},
			func() http.Handler {
				return nil
			},
			server.NewAPIServer,
		),
		fx.Populate(&srv),
		fx.Invoke(server.Register),
	)

	appFx.RequireStart()
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() { appFx.RequireStop() })

	assert.NotNil(t, srv)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9875/debug/mexc/tickers", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestAPIServer_OrderPNL(t *testing.T) {
	t.Parallel()
	client := &stubClient{}
	engine := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:   "mexc",
				Client: client,
			},
		},
	}
	srv := setupTestServer(t, engine, 9890)
	require.NoError(t, srv.Start(context.Background()))
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	// Valid Request
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9890/debug/mexc/order_pnl?symbol=BTCUSDT&order_id=123", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var info exchange.ClosedPnLInfo
	err = json.NewDecoder(resp.Body).Decode(&info)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", info.Symbol)
	assert.Equal(t, 50000.0, info.ExitPrice)
	_ = resp.Body.Close()

	// Missing parameters
	reqBad, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9890/debug/mexc/order_pnl?symbol=BTCUSDT", http.NoBody)
	require.NoError(t, err)
	respBad, err := http.DefaultClient.Do(reqBad)
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, respBad.StatusCode)
	_ = respBad.Body.Close()
}

func TestAPIServer_FundingScanner(t *testing.T) {
	t.Parallel()
	clientMexc := &stubClient{
		tickers: []exchange.Ticker{
			{Symbol: "HIGH_USDT", AmountUSDT24: 2000000.0, LastPrice: 1.25},
		},
		rates: []exchange.FundingRateResult{
			{Symbol: "HIGH_USDT", Rate: -0.003852},
		},
	}
	clientGate := &stubClient{}
	engine := &app.Engine{
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:   "mexc",
				Client: clientMexc,
			},
			"gate": {
				Name:   "gate",
				Client: clientGate,
			},
		},
	}
	srv := setupTestServer(t, engine, 9895)
	require.NoError(t, srv.Start(context.Background()))
	time.Sleep(50 * time.Millisecond)
	t.Cleanup(func() { _ = srv.Stop(context.Background()) })

	// Request with exchange parameters
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:9895/debug/funding_scanner?exchange=mexc,gate&min_rate=0.1&min_vol=1000", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var payload struct {
		Success bool                 `json:"success"`
		Groups  []server.SymbolGroup `json:"groups"`
	}
	err = json.NewDecoder(resp.Body).Decode(&payload)
	require.NoError(t, err)
	assert.True(t, payload.Success)
	require.Len(t, payload.Groups, 1)
	assert.Equal(t, "HIGH_USDT", payload.Groups[0].StandardSymbol)
	assert.Equal(t, -0.003852, payload.Groups[0].ScoreRate)
	require.NotEmpty(t, payload.Groups[0].Opportunities)
	assert.Equal(t, 1.25, payload.Groups[0].Opportunities[0].Price)
	_ = resp.Body.Close()
}

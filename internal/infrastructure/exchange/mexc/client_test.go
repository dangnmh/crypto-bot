package mexc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestClient creates a MEXC client pointed at the given test server.
func newTestClient(server *httptest.Server) *mexc.Client {
	return mexc.NewClient(server.Client(), server.URL, "testKey", "testSecret", config.LoggingConfig{})
}

// mustJSON marshals v to JSON or panics.
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ── Ping ─────────────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_Ping(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[int64]{Success: true, Code: 0, Data: time.Now().UnixMilli()}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

// ── GetServerTime ────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()
	expected := int64(1609459200000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[int64]{Success: true, Code: 0, Data: expected}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetServerTime(context.Background())
	if err != nil {
		t.Fatalf("GetServerTime failed: %v", err)
	}
	if got != expected {
		t.Errorf("want %d, got %d", expected, got)
	}
}

// ── GetContractDetails ───────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()
	contracts := []exchange.ContractDetail{
		{Symbol: "BTC_USDT", PriceScale: 2, VolScale: 0},
		{Symbol: "ETH_USDT", PriceScale: 2, VolScale: 0},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]exchange.ContractDetail]{Success: true, Code: 0, Data: contracts}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetContractDetails(context.Background())
	if err != nil {
		t.Fatalf("GetContractDetails failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 contracts, got %d", len(got))
	}
	if got[0].Symbol != "BTC_USDT" {
		t.Errorf("want BTC_USDT, got %s", got[0].Symbol)
	}
}

// ── GetTickers (array) ───────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetTickers_Array(t *testing.T) {
	t.Parallel()
	tickers := []exchange.Ticker{
		{Symbol: "BTC_USDT", LastPrice: 50000},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := json.Marshal(tickers)
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0, Data: raw}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetTickers(context.Background(), "")
	if err != nil {
		t.Fatalf("GetTickers failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 ticker, got %d", len(got))
	}
	if got[0].LastPrice != 50000 {
		t.Errorf("LastPrice: want 50000, got %f", got[0].LastPrice)
	}
}

// ── GetTickers (single object) ───────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetTickers_Single(t *testing.T) {
	t.Parallel()
	single := exchange.Ticker{Symbol: "ETH_USDT", LastPrice: 3000}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := json.Marshal(single)
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0, Data: raw}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetTickers(context.Background(), "ETH_USDT")
	if err != nil {
		t.Fatalf("GetTickers failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 ticker, got %d", len(got))
	}
	if got[0].Symbol != "ETH_USDT" {
		t.Errorf("Symbol: want ETH_USDT, got %s", got[0].Symbol)
	}
}

func TestClient_GetTickers_Amount24Fallback(t *testing.T) {
	t.Parallel()
	type rawMexcTicker struct {
		Symbol    string  `json:"symbol"`
		LastPrice float64 `json:"lastPrice"`
		Bid1      float64 `json:"bid1"`
		Ask1      float64 `json:"ask1"`
		Volume24  float64 `json:"volume24"`
		Amount24  float64 `json:"amount24"` // 0 in response
		Timestamp int64   `json:"timestamp"`
	}

	tickers := []rawMexcTicker{
		{Symbol: "KAITO_USDT", LastPrice: 0.70, Bid1: 0.699, Ask1: 0.701, Volume24: 1000000, Amount24: 0},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := json.Marshal(tickers)
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0, Data: raw}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetTickers(context.Background(), "")
	if err != nil {
		t.Fatalf("GetTickers failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 ticker, got %d", len(got))
	}
	if got[0].AmountUSDT24 != 700000.0 {
		t.Errorf("AmountUSDT24 fallback: want 700000, got %f", got[0].AmountUSDT24)
	}
}

// ── GetTopGainer ─────────────────────────────────────────────────────.

func TestClient_GetTopGainer(t *testing.T) {
	t.Parallel()
	type rawMexcTicker struct {
		Symbol       string  `json:"symbol"`
		LastPrice    float64 `json:"lastPrice"`
		Bid1         float64 `json:"bid1"`
		Ask1         float64 `json:"ask1"`
		Volume24     float64 `json:"volume24"`
		Amount24     float64 `json:"amount24"`
		Timestamp    int64   `json:"timestamp"`
		RiseFallRate float64 `json:"riseFallRate"`
	}

	tickers := []rawMexcTicker{
		{Symbol: "BTC_USDT", LastPrice: 50000, Bid1: 49990, Ask1: 50000, Amount24: 1000000, RiseFallRate: 0.02},
		{Symbol: "SOL_USDT", LastPrice: 150, Bid1: 149.5, Ask1: 150, Amount24: 500000, RiseFallRate: 0.15},
		{Symbol: "ETH_USDT", LastPrice: 3000, Bid1: 2999, Ask1: 3000, Amount24: 800000, RiseFallRate: 0.05},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := json.Marshal(tickers)
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0, Data: raw}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetTopGainer(context.Background(), exchange.TopGainerRequest{Limit: 2})
	if err != nil {
		t.Fatalf("GetTopGainer failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 gainers, got %d", len(got))
	}

	if got[0].Symbol != "SOL_USDT" || got[0].Gain24hPct != 15.0 {
		t.Errorf("expected SOL_USDT as #1 gainer, got %s with %f%%", got[0].Symbol, got[0].Gain24hPct)
	}
	if got[1].Symbol != "ETH_USDT" || got[1].Gain24hPct != 5.0 {
		t.Errorf("expected ETH_USDT as #2 gainer, got %s with %f%%", got[1].Symbol, got[1].Gain24hPct)
	}
}

// ── GetFundingRates ───────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()
	type rawFundingRate struct {
		Symbol         string  `json:"symbol"`
		FundingRate    float64 `json:"fundingRate"`
		NextSettleTime int64   `json:"nextSettleTime"`
	}
	rates := []rawFundingRate{
		{Symbol: "BTC_USDT", FundingRate: 0.001, NextSettleTime: 1609459200000},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/contract/funding_rate" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]rawFundingRate]{Success: true, Code: 0, Data: rates}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetFundingRates(context.Background(), []string{"BTC_USDT"})
	if err != nil {
		t.Fatalf("GetFundingRates failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Rate != 0.001 {
		t.Errorf("FundingRate: want 0.001, got %f", got[0].Rate)
	}
	if got[0].SettleTime != 1609459200000 {
		t.Errorf("SettleTime: want 1609459200000, got %d", got[0].SettleTime)
	}
}

// ── GetFundingRateHistory ────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetFundingRateHistory(t *testing.T) {
	t.Parallel()
	type wrapper struct {
		ResultList []exchange.FundingRateHistory `json:"resultList"`
	}
	data := wrapper{ResultList: []exchange.FundingRateHistory{
		{Symbol: "BTC_USDT", FundingRate: 0.002, SettleTime: 1000},
	}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[wrapper]{Success: true, Code: 0, Data: data}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetFundingRateHistory(context.Background(), "BTC_USDT", 1, 10)
	if err != nil {
		t.Fatalf("GetFundingRateHistory failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
}

// ── CreateOrder ──────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()
	resp := exchange.CreateOrderResponse{OrderID: "order123", Ts: 1609459200000}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[exchange.CreateOrderResponse]{Success: true, Code: 0, Data: resp}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT", Price: 50000, Vol: 1, Side: 1, Type: 3,
	})
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	if res.OrderID != "order123" {
		t.Errorf("OrderID: want 'order123', got %q", res.OrderID)
	}
}

// ── CancelOrders ─────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_CancelOrders(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.CancelOrders(context.Background(), []string{"id1", "id2"})
	if err != nil {
		t.Fatalf("CancelOrders failed: %v", err)
	}
}

// ── CancelOrder ──────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.CancelOrder(context.Background(), "BTC_USDT", "id1")
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
}

func TestClient_CancelOrderReturnsNestedCancelError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := json.RawMessage(`[{"orderId":812413197728264320,"errorCode":2041,"errorMsg":"order state cannot be cancelled"}]`)
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{
			Success: true,
			Code:    0,
			Data:    data,
		}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.CancelOrder(context.Background(), "BTC_USDT", "812413197728264320")
	if err == nil {
		t.Fatal("expected nested cancel error")
	}
	var apiErr *exchange.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.Code != 2041 {
		t.Fatalf("expected code 2041, got %d", apiErr.Code)
	}
}

// ── CancelAllOpenOrders ──────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_CancelAllOpenOrders(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.CancelAllOpenOrders(context.Background(), "BTC_USDT")
	if err != nil {
		t.Fatalf("CancelAllOpenOrders failed: %v", err)
	}
}

// ── GetOrder ─────────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetOrder(t *testing.T) {
	t.Parallel()
	order := map[string]any{
		"orderId": "ord1",
		"symbol":  "BTC_USDT",
		"price":   50000.0,
		"side":    1,
		"state":   2,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[map[string]any]{Success: true, Code: 0, Data: order}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetOrder(context.Background(), "BTC_USDT", "ord1")
	if err != nil {
		t.Fatalf("GetOrder failed: %v", err)
	}
	if got.OrderID != "ord1" {
		t.Errorf("OrderID: want 'ord1', got %q", got.OrderID)
	}
}

// ── GetOpenOrders ────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetOpenOrders(t *testing.T) {
	t.Parallel()
	orders := []map[string]any{
		{"orderId": "o1", "symbol": "BTC_USDT", "side": 1, "state": 2},
		{"orderId": "o2", "symbol": "BTC_USDT", "side": 1, "state": 2},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]map[string]any]{Success: true, Code: 0, Data: orders}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetOpenOrders(context.Background(), "BTC_USDT")
	if err != nil {
		t.Fatalf("GetOpenOrders failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("want 2 orders, got %d", len(got))
	}
}

// ── CloseAllPositions ────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_CloseAllPositions(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.CloseAllPositions(context.Background(), "BTC_USDT")
	if err != nil {
		t.Fatalf("CloseAllPositions failed: %v", err)
	}
}

//nolint:dupl // test
func TestClient_ClosePosition(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/private/order/create" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req exchange.SubmitOrderRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Symbol != "BTC_USDT" || req.Side != exchange.SideCloseLong || req.Vol != 1.5 {
			t.Fatalf("unexpected close request: %+v", req)
		}
		if req.Type != exchange.OrderTypeMarket || !req.ReduceOnly || req.PositionMode != 1 {
			t.Fatalf("unexpected close flags: %+v", req)
		}
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[exchange.CreateOrderResponse]{
			Success: true,
			Code:    0,
			Data:    exchange.CreateOrderResponse{OrderID: "close_1"},
		}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.ClosePosition(context.Background(), "BTC_USDT", domain.SideCloseLong, 1.5, 1, 1)
	if err != nil {
		t.Fatalf("ClosePosition failed: %v", err)
	}
}

// ── ChangeLeverage ───────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_ChangeLeverage(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol: "BTC_USDT", Leverage: 20, OpenType: 1,
	})
	if err != nil {
		t.Fatalf("ChangeLeverage failed: %v", err)
	}
}

// ── GetOpenPositions ─────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()
	positions := []exchange.Position{
		{Symbol: "BTC_USDT", HoldVolContract: 10},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]exchange.Position]{Success: true, Code: 0, Data: positions}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetOpenPositions(context.Background(), "BTC_USDT")
	if err != nil {
		t.Fatalf("GetOpenPositions failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 position, got %d", len(got))
	}
}

// ── Latency ──────────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_Latency(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[int64]{Success: true, Code: 0, Data: time.Now().UnixMilli()}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	latency, err := client.Latency(context.Background())
	if err != nil {
		t.Fatalf("Latency failed: %v", err)
	}
	if latency < 0 {
		t.Errorf("latency should be >= 0, got %d", latency)
	}
}

// ── Error Handling ───────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.GetServerTime(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

//nolint:dupl // test
func TestClient_RateLimitError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`rate limited`))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.GetServerTime(context.Background())
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
	if !exchange.IsRateLimitError(err) {
		t.Errorf("expected RateLimitError, got %T: %v", err, err)
	}
}

//nolint:dupl // test
func TestClient_HTTPLogging(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[int64]{Success: true, Code: 0, Data: 123}))
	}))
	defer srv.Close()

	// Create client with HTTP logging enabled.
	logCfg := config.LoggingConfig{HTTP: true}
	client := mexc.NewClient(srv.Client(), srv.URL, "key", "secret", logCfg)

	_, err := client.GetServerTime(context.Background())
	if err != nil {
		t.Fatalf("request with logging failed: %v", err)
	}
}

func TestClient_MexcRemainingMethods(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/contract/ticker":
			_, _ = w.Write([]byte(`{"success":true,"code":0,"data":[{"symbol":"BTC_USDT","lastPrice":64000.0,"volume24":100.5,"amount24":15000000.0}]}`))
		case "/api/v1/contract/funding_rate":
			_, _ = w.Write([]byte(`{"success":true,"code":0,"data":[{"symbol":"BTC_USDT","fundingRate":0.001,"nextSettleTime":1700000000000}]}`))
		case "/api/v1/private/position/change_leverage":
			_, _ = w.Write([]byte(`{"success":true,"code":0}`))
		default:
			if strings.HasPrefix(r.URL.Path, "/api/v1/private/order/external/") {
				_, _ = w.Write([]byte(`{
					"success": true,
					"code": 0,
					"data": {
						"orderId": "order123",
						"externalOid": "client123",
						"symbol": "BTC_USDT",
						"price": 50000.0,
						"vol": 1.0,
						"dealVol": 1.0,
						"dealAvgPrice": 50000.0,
						"state": 2
					}
				}`))
			}
		}
	}))
	defer server.Close()

	client := mexc.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	// 1. GetPotentialFundingSymbols
	res, err := client.GetPotentialFundingSymbols(context.Background(), 10000000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTC_USDT", res[0].Symbol)

	// 2. GetOrderByExternalID
	order, err := client.GetOrderByExternalID(context.Background(), "BTC_USDT", "client123")
	require.NoError(t, err)
	assert.Equal(t, "order123", order.OrderID)

	// 3. SupportLeverageOnOrder
	assert.True(t, client.SupportLeverageOnOrder())

	// 4. SwitchMarginMode
	err = client.SwitchMarginMode(context.Background(), "BTC_USDT", domain.MarginModeIsolated, 10, domain.SideOpenLong)
	require.NoError(t, err)

	// 5. Raw methods
	_, _ = client.GetHistoryOrdersRaw(context.Background(), nil)
	_, _ = client.GetOrderDealsRaw(context.Background(), nil)
	_, _ = client.GetClosedPnLRaw(context.Background(), nil)
	_, _ = client.GetOrderPNLRaw(context.Background(), nil)
}

func TestClient_GetMaxLeverageForValue(t *testing.T) {
	t.Parallel()

	contractDetailJSON := `{
		"success": true,
		"code": 0,
		"data": [
			{
				"symbol": "BTC_USDT",
				"contractSize": 0.0001,
				"maxLeverage": 500,
				"riskLimitType": "BY_VOLUME",
				"riskLimitCustom": [
					{"level": 1, "maxVol": 50000, "maxLeverage": 500},
					{"level": 2, "maxVol": 310000, "maxLeverage": 200},
					{"level": 3, "maxVol": 480000, "maxLeverage": 100}
				]
			},
			{
				"symbol": "SOL_USDT",
				"contractSize": 1.0,
				"maxLeverage": 125,
				"riskLimitType": "BY_VALUE",
				"riskLimitCustom": [
					{"level": 1, "maxVol": 100000, "maxLeverage": 125},
					{"level": 2, "maxVol": 500000, "maxLeverage": 50}
				]
			}
		]
	}`

	tickersJSON := `{
		"success": true,
		"code": 0,
		"data": [
			{
				"symbol": "BTC_USDT",
				"lastPrice": 100000.0,
				"bid1": 99990.0,
				"ask1": 100010.0,
				"volume24": 1000000.0,
				"amount24": 100000000.0
			}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/contract/detail":
			_, _ = w.Write([]byte(contractDetailJSON))
		case "/api/v1/contract/ticker":
			_, _ = w.Write([]byte(tickersJSON))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := newTestClient(srv)

	// Test 1: BTC_USDT low value ($1,000 USDT) -> 100 contracts <= 50,000 level 1 -> leverage 500
	lev1, err := client.GetMaxLeverageForValue(context.Background(), "BTC_USDT", 1000.0)
	require.NoError(t, err)
	assert.Equal(t, 500, lev1)

	// Test 2: BTC_USDT high value ($1,000,000 USDT at $100,000 price) -> 100,000 contracts > 50,000 (level 1) & <= 310,000 (level 2) -> leverage 200
	lev2, err := client.GetMaxLeverageForValue(context.Background(), "BTC_USDT", 1000000.0)
	require.NoError(t, err)
	assert.Equal(t, 200, lev2)

	// Test 3: SOL_USDT BY_VALUE ($50,000 USDT <= 100,000) -> leverage 125
	lev3, err := client.GetMaxLeverageForValue(context.Background(), "SOL_USDT", 50000.0)
	require.NoError(t, err)
	assert.Equal(t, 125, lev3)

	// Test 4: SOL_USDT BY_VALUE ($200,000 USDT <= 500,000) -> leverage 50
	lev4, err := client.GetMaxLeverageForValue(context.Background(), "SOL_USDT", 200000.0)
	require.NoError(t, err)
	assert.Equal(t, 50, lev4)
}

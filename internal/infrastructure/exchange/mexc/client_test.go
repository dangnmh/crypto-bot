package mexc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/mexc"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
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

func TestClient_GetDepthSnapshot(t *testing.T) {
	t.Parallel()

	body := map[string]any{
		"asks":    [][]string{{"101.5", "2"}, {"0", "9"}, {"101.7"}},
		"bids":    [][]string{{"100.5", "3"}},
		"version": int64(42),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/contract/depth/BTC_USDT" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "20" {
			t.Fatalf("unexpected limit: %s", got)
		}
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[map[string]any]{Success: true, Code: 0, Data: body}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetDepthSnapshot(context.Background(), "BTC_USDT", 20)
	if err != nil {
		t.Fatalf("GetDepthSnapshot failed: %v", err)
	}
	if got.Symbol != "BTC_USDT" || got.Version != 42 || len(got.Asks) != 1 || len(got.Bids) != 1 {
		t.Fatalf("unexpected orderbook: %+v", got)
	}
}

func TestClient_GetDepthCommits(t *testing.T) {
	t.Parallel()

	commits := []map[string]any{
		{
			"version": int64(7),
			"asks":    [][]string{{"101.5", "2"}},
			"bids":    [][]string{{"100.5", "3"}},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/contract/depth_commits/BTC_USDT/1000" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]map[string]any]{Success: true, Code: 0, Data: commits}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetDepthCommits(context.Background(), "BTC_USDT", 0)
	if err != nil {
		t.Fatalf("GetDepthCommits failed: %v", err)
	}
	if len(got) != 1 || got[0].Version != 7 || len(got[0].Asks) != 1 || len(got[0].Bids) != 1 {
		t.Fatalf("unexpected commits: %+v", got)
	}
}

func TestClient_DepthInputValidationAndMalformedLevels(t *testing.T) {
	t.Parallel()

	emptySrv := httptest.NewServer(http.NotFoundHandler())
	defer emptySrv.Close()
	client := newTestClient(emptySrv)
	_, err := client.GetDepthSnapshot(context.Background(), "", 20)
	if err == nil {
		t.Fatal("expected empty symbol error for depth snapshot")
	}
	_, err = client.GetDepthCommits(context.Background(), "", 20)
	if err == nil {
		t.Fatal("expected empty symbol error for depth commits")
	}

	body := map[string]any{
		"asks":    [][]string{{"0", "2"}, {"101.5", "2"}, {"101.7"}},
		"bids":    [][]string{{"0", "3"}, {"100.5", "3"}, {"100.1"}},
		"version": int64(9),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[map[string]any]{Success: true, Code: 0, Data: body}))
	}))
	defer srv.Close()

	client = newTestClient(srv)
	ob, err := client.GetDepthSnapshot(context.Background(), "BTC_USDT", 0)
	if err != nil {
		t.Fatalf("GetDepthSnapshot failed: %v", err)
	}
	if len(ob.Asks) != 1 || len(ob.Bids) != 1 {
		t.Fatalf("unexpected malformed-level filtering: %+v", ob)
	}

	commits := []map[string]any{{
		"version": int64(10),
		"asks":    [][]string{{"101.5"}, {"101.6", "1"}},
		"bids":    [][]string{{"100.5"}, {"100.4", "1"}},
	}}
	commitSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]map[string]any]{Success: true, Code: 0, Data: commits}))
	}))
	defer commitSrv.Close()

	client = newTestClient(commitSrv)
	got, err := client.GetDepthCommits(context.Background(), "BTC_USDT", 5)
	if err != nil {
		t.Fatalf("GetDepthCommits failed: %v", err)
	}
	if len(got) != 1 || len(got[0].Asks) != 1 || len(got[0].Bids) != 1 {
		t.Fatalf("unexpected malformed commit filtering: %+v", got)
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

// ── GetFundingRates ───────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()
	tickers := []exchange.Ticker{
		{Symbol: "BTC_USDT", LastPrice: 50000, FundingRate: 0.001, Amount24: 1000000},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := json.Marshal(tickers)
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0, Data: raw}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetFundingRates(context.Background())
	if err != nil {
		t.Fatalf("GetFundingRates failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Rate != 0.001 {
		t.Errorf("FundingRate: want 0.001, got %f", got[0].Rate)
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

// ── GetKlines ────────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetKlines(t *testing.T) {
	t.Parallel()
	type klineData struct {
		Time   []int64   `json:"time"`
		Open   []float64 `json:"open"`
		Close  []float64 `json:"close"`
		High   []float64 `json:"high"`
		Low    []float64 `json:"low"`
		Vol    []float64 `json:"vol"`
		Amount []float64 `json:"amount"`
	}
	data := klineData{
		Time:   []int64{1609459200, 1609459260},
		Open:   []float64{100, 101},
		Close:  []float64{101, 102},
		High:   []float64{102, 103},
		Low:    []float64{99, 100},
		Vol:    []float64{1000, 2000},
		Amount: []float64{100000, 200000},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[klineData]{Success: true, Code: 0, Data: data}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetKlines(context.Background(), "BTC_USDT", "Min1", 0, 0)
	if err != nil {
		t.Fatalf("GetKlines failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 klines, got %d", len(got))
	}
	// Verify timestamp conversion (seconds × 1000).
	if got[0].Timestamp != 1609459200000 {
		t.Errorf("Timestamp: want 1609459200000, got %d", got[0].Timestamp)
	}
}

//nolint:dupl // test
func TestClient_GetKlines_EmptySymbol(t *testing.T) {
	t.Parallel()
	client := mexc.NewClient(&http.Client{}, "http://localhost", "k", "s", config.LoggingConfig{})
	_, err := client.GetKlines(context.Background(), "", "Min1", 0, 0)
	if err == nil {
		t.Fatal("expected error for empty symbol")
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

// ── CreateTrackOrder ─────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_CreateTrackOrder(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[string]{Success: true, Code: 0, Data: "track123"}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	trackID, err := client.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{Symbol: "BTC_USDT"})
	if err != nil {
		t.Fatalf("CreateTrackOrder failed: %v", err)
	}
	if trackID != "track123" {
		t.Errorf("TrackID: want 'track123', got %q", trackID)
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
	order := exchange.OrderInfo{OrderID: "ord1", Symbol: "BTC_USDT", Price: 50000}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[exchange.OrderInfo]{Success: true, Code: 0, Data: order}))
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
	orders := []exchange.OrderInfo{
		{OrderID: "o1", Symbol: "BTC_USDT"},
		{OrderID: "o2", Symbol: "BTC_USDT"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]exchange.OrderInfo]{Success: true, Code: 0, Data: orders}))
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
	err := client.ClosePosition(context.Background(), "BTC_USDT", domain.SideCloseLong, 1.5, 1)
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

// ── GetAssets ─────────────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetAssets(t *testing.T) {
	t.Parallel()
	assets := []exchange.AssetInfo{
		{Currency: "USDT", AvailableBalance: 1000},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]exchange.AssetInfo]{Success: true, Code: 0, Data: assets}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetAssets(context.Background())
	if err != nil {
		t.Fatalf("GetAssets failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 asset, got %d", len(got))
	}
	if got[0].Currency != "USDT" {
		t.Errorf("Currency: want USDT, got %s", got[0].Currency)
	}
}

// ── GetAssetByCurrency ───────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetAssetByCurrency(t *testing.T) {
	t.Parallel()
	asset := exchange.AssetInfo{Currency: "USDT", AvailableBalance: 500}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[exchange.AssetInfo]{Success: true, Code: 0, Data: asset}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetAssetByCurrency(context.Background(), "USDT")
	if err != nil {
		t.Fatalf("GetAssetByCurrency failed: %v", err)
	}
	if got.AvailableBalance != 500 {
		t.Errorf("AvailableBalance: want 500, got %f", got.AvailableBalance)
	}
}

// ── GetOpenPositions ─────────────────────────────────────────────────.

//nolint:dupl // test
func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()
	positions := []exchange.Position{
		{Symbol: "BTC_USDT", HoldVol: 10},
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

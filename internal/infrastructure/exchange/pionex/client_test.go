package pionex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/pionex"
)

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time {
	return s.now
}

func requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("PIONEX-KEY") == "" || r.Header.Get("PIONEX-SIGNATURE") == "" {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"result":false,"error":{"code":"UNAUTHORIZED","message":"missing headers"}}`))
		return false
	}
	return true
}

func setupMockServer() *httptest.Server {
	handlers := map[string]func(w http.ResponseWriter, r *http.Request){
		"/api/v1/market/trades": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000}`))
		},
		"/api/v1/market/tickers": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"data":{"tickers":[{"symbol":"BTC_USDT_PERP","close":"60000","volume":"10","amount":"600000"}]}}`))
		},
		"/api/v1/market/bookTickers": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"data":{"tickers":[{"symbol":"BTC_USDT_PERP","bidPrice":"59999","bidSize":"1","askPrice":"60001","askSize":"2","timestamp":1719600000000}]}}`))
		},
		"/api/v1/common/symbols": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"data":{"symbols":[{"symbol":"BTC_USDT_PERP","name":"BTC USDT PERP","type":"PERP","baseCurrency":"BTC","quoteCurrency":"USDT","baseStep":"0.001","quoteStep":"0.1","minSizeLimit":"0.001","status":"TRADING"}]}}`))
		},
		"/api/v1/common/riskTable": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"symbols":[{"symbol":"BTC_USDT_PERP","rows":[{"rowNum":1,"maxLeverage":"75"}]}]}}`))
		},
		"/api/v1/market/indexes": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"data":{"indexes":[{"symbol":"BTC_USDT_PERP","nextFundingRate":"0.0001","nextFundingTime":1719600000000}]}}`))
		},
		"/uapi/v1/account/positions": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"data":{"positions":[{"positionId":"p123","symbol":"BTC_USDT_PERP","isolatedMode":"CROSS","positionSide":"LONG","netSize":"1.5","avgPrice":"60000.0","leverage":"10"}]}}`))
		},
		"/uapi/v1/account/historyPositions": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"positions":[]}}`))
		},
		"/uapi/v1/account/leverage": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"symbol":"BTC_USDT_PERP","leverage":"10"}}`))
		},
		"/uapi/v1/account/positionMode": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000}`))
		},
		"/uapi/v1/trade/isolatedMode": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000}`))
		},
		"/uapi/v1/trade/order": func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost:
				_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"orderId":123456}}`))
			case http.MethodDelete:
				_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000}`))
			default:
				_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"orderId":123456,"symbol":"BTC_USDT_PERP","type":"LIMIT","positionMode":"OPENCLOSE","isolatedMode":"CROSS","side":"BUY","positionSide":"LONG","price":"60000.0","origSize":"1.0","size":"1.0","filledSize":"0.0","filledAmount":"0.0","status":"OPEN","reduceOnly":false,"clientOrderId":"ext123","createTime":1719600000000,"updateTime":1719600000000}}`))
			}
		},
		"/uapi/v1/trade/orderByClientOrderId": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"orderId":123456,"symbol":"BTC_USDT_PERP","type":"LIMIT","positionMode":"OPENCLOSE","isolatedMode":"CROSS","side":"BUY","positionSide":"LONG","price":"60000.0","origSize":"1.0","size":"1.0","filledSize":"0.0","filledAmount":"0.0","status":"OPEN","reduceOnly":false,"clientOrderId":"ext123","createTime":1719600000000,"updateTime":1719600000000}}`))
		},
		"/uapi/v1/trade/openOrders": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"orders":[{"orderId":123456,"symbol":"BTC_USDT_PERP","type":"LIMIT","positionMode":"OPENCLOSE","isolatedMode":"CROSS","side":"BUY","positionSide":"LONG","price":"60000.0","origSize":"1.0","size":"1.0","filledSize":"0.0","filledAmount":"0.0","status":"OPEN","reduceOnly":false,"clientOrderId":"ext123","createTime":1719600000000,"updateTime":1719600000000}]}}`))
		},
		"/uapi/v1/trade/historyOrders": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"orders":[]}}`))
		},
		"/uapi/v1/trade/allOrders": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000}`))
		},
		"/uapi/v1/trade/fundingFee": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":true,"timestamp":1719600000000,"data":{"fundings":[]}}`))
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if handler, ok := handlers[path]; ok {
			if strings.HasPrefix(path, "/uapi/") {
				if !requireAuth(w, r) {
					return
				}
			}
			handler(w, r)
		} else {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"result":false,"error":{"message":"not found"}}`))
		}
	}))
}

func TestGetServerTime(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	res, err := client.GetServerTime(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res != 1719600000000 {
		t.Errorf("expected server time 1719600000000, got %d", res)
	}
}

func TestGetTickers(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tickers) != 1 {
		t.Fatalf("expected 1 ticker, got %d", len(tickers))
	}

	if tickers[0].Symbol != "BTC_USDT_PERP" {
		t.Errorf("expected BTC_USDT_PERP, got %s", tickers[0].Symbol)
	}
	if tickers[0].LastPrice != 60000 {
		t.Errorf("expected lastPrice 60000, got %f", tickers[0].LastPrice)
	}
	if tickers[0].Bid1 != 59999 {
		t.Errorf("expected bid1 59999, got %f", tickers[0].Bid1)
	}
	if tickers[0].Ask1 != 60001 {
		t.Errorf("expected ask1 60001, got %f", tickers[0].Ask1)
	}
}

func TestGetContractDetails(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(details) != 1 {
		t.Fatalf("expected 1 contract detail, got %d", len(details))
	}

	d := details[0]
	if d.Symbol != "BTC_USDT_PERP" {
		t.Errorf("expected symbol BTC_USDT_PERP, got %s", d.Symbol)
	}
	if d.BaseCoin != "BTC" {
		t.Errorf("expected base BTC, got %s", d.BaseCoin)
	}
	if d.QuoteCoin != "USDT" {
		t.Errorf("expected quote USDT, got %s", d.QuoteCoin)
	}
	if d.PriceUnit != 0.1 {
		t.Errorf("expected PriceUnit 0.1, got %f", d.PriceUnit)
	}
	if d.MaxLeverage != 75 {
		t.Errorf("expected MaxLeverage 75, got %d", d.MaxLeverage)
	}
}

func TestGetFundingRates(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	rates, err := client.GetFundingRates(context.Background(), []string{"BTC_USDT_PERP"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rates) != 1 {
		t.Fatalf("expected 1 rate, got %d", len(rates))
	}

	if rates[0].Symbol != "BTC_USDT_PERP" {
		t.Errorf("expected symbol BTC_USDT_PERP, got %s", rates[0].Symbol)
	}
	if rates[0].Rate != 0.0001 {
		t.Errorf("expected rate 0.0001, got %f", rates[0].Rate)
	}
}

func TestPrivateRawRequestSignature(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "myApiKey", "myApiSecret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Date(2024, 6, 28, 12, 0, 0, 0, time.UTC)}) // Fixed mock timestamp: 1719576000000

	raw, ok := exchange.Client(client).(exchange.RawRequest)
	if !ok {
		t.Fatalf("client does not implement exchange.RawRequest")
	}

	body, err := raw.GetOpenPositionsRaw(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(body), `"positions"`) {
		t.Errorf("expected positions response, got %s", string(body))
	}
}

func TestChangeLeverage(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTC_USDT_PERP",
		Leverage: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSwitchPositionMode(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	err := client.SwitchPositionMode(context.Background(), "BTC_USDT_PERP", domain.PositionModeHedge)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSwitchMarginMode(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	err := client.SwitchMarginMode(context.Background(), "BTC_USDT_PERP", domain.MarginModeCross, 10, domain.SideOpenLong)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateOrder(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:       "BTC_USDT_PERP",
		Price:        60000.0,
		Vol:          1.0,
		Side:         domain.SideOpenLong,
		Type:         domain.OrderTypeLimit,
		PositionMode: domain.PositionModeHedge,
		ExternalOID:  "ext123",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OrderID != "123456" {
		t.Errorf("expected orderId 123456, got %s", res.OrderID)
	}
}

func TestGetFundingFeeRaw(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	body, err := client.GetFundingFeeRaw(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(body), `"fundings"`) {
		t.Errorf("expected fundings, got %s", string(body))
	}
}

func TestGetRiskTableRaw(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	body, err := client.GetRiskTableRaw(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(body), `"symbols"`) {
		t.Errorf("expected symbols in risk table, got %s", string(body))
	}
}

func TestGetSymbolsRaw(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	body, err := client.GetSymbolsRaw(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(body), `"symbols"`) {
		t.Errorf("expected symbols, got %s", string(body))
	}
}

func TestGetOrder(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	res, err := client.GetOrder(context.Background(), "BTC_USDT_PERP", "123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OrderID != "123456" {
		t.Errorf("expected orderId 123456, got %s", res.OrderID)
	}
	if res.Side != domain.SideOpenLong {
		t.Errorf("expected SideOpenLong, got %v", res.Side)
	}
}

func TestGetOrderByExternalID(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	res, err := client.GetOrderByExternalID(context.Background(), "BTC_USDT_PERP", "ext123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OrderID != "123456" {
		t.Errorf("expected orderId 123456, got %s", res.OrderID)
	}
	if res.ExternalOID != "ext123" {
		t.Errorf("expected ext123, got %s", res.ExternalOID)
	}
}

func TestCancelOrder(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	err := client.CancelOrder(context.Background(), "BTC_USDT_PERP", "123456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetOpenOrders(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	res, err := client.GetOpenOrders(context.Background(), "BTC_USDT_PERP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 order, got %d", len(res))
	}
	if res[0].OrderID != "123456" {
		t.Errorf("expected orderId 123456, got %s", res[0].OrderID)
	}
}

func TestGetHistoryOrdersRaw(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	res, err := client.GetHistoryOrdersRaw(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(res), `"orders"`) {
		t.Errorf("expected orders in history orders response, got %s", string(res))
	}
}

func TestCancelAllOpenOrders(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	err := client.CancelAllOpenOrders(context.Background(), "BTC_USDT_PERP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetOpenPositions(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	res, err := client.GetOpenPositions(context.Background(), "BTC_USDT_PERP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 position, got %d", len(res))
	}
	if res[0].Symbol != "BTC_USDT_PERP" {
		t.Errorf("expected BTC_USDT_PERP, got %s", res[0].Symbol)
	}
	if res[0].HoldVolCoin != 1.5 {
		t.Errorf("expected holdVolCoin 1.5, got %v", res[0].HoldVolCoin)
	}
	if res[0].PositionType != exchange.PositionTypeLong {
		t.Errorf("expected PositionTypeLong, got %v", res[0].PositionType)
	}
}

func TestGetHistoryPositionsRaw(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	res, err := client.GetHistoryPositionsRaw(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(res), `"positions"`) {
		t.Errorf("expected positions key in history positions response, got %s", string(res))
	}
}

func TestCancelOrders(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	err := client.CancelOrders(context.Background(), []string{"123456", "789012"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClosePosition(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	err := client.ClosePosition(context.Background(), "BTC_USDT_PERP", domain.SideCloseLong, 1.0, domain.PositionModeHedge, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCloseAllPositions(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()

	client := pionex.NewClient(http.DefaultClient, server.URL, "apiKey", "apiSecret", config.LoggingConfig{})
	err := client.CloseAllPositions(context.Background(), "BTC_USDT_PERP")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWsAdapter_GetPrivateURLFunc(t *testing.T) {
	t.Parallel()
	client := pionex.NewClient(http.DefaultClient, "https://api.pionex.com", "my_api_key", "my_api_secret", config.LoggingConfig{})
	// Mock clock to keep timestamp stable
	now := time.Unix(1719600000, 0)
	client.SetClock(stubClock{now: now})

	adapter := pionex.NewWsAdapter(client, "wss://ws.pionex.com/wsUA")
	urlFunc := adapter.GetPrivateURLFunc(context.Background())
	resolvedURL, err := urlFunc()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	u, err := url.Parse(resolvedURL)
	if err != nil {
		t.Fatalf("failed to parse resolved URL: %v", err)
	}

	q := u.Query()
	if q.Get("key") != "my_api_key" {
		t.Errorf("expected key to be my_api_key, got %s", q.Get("key"))
	}
	if q.Get("timestamp") != "1719600000000" {
		t.Errorf("expected timestamp to be 1719600000000, got %s", q.Get("timestamp"))
	}
	if q.Get("signature") == "" {
		t.Errorf("expected non-empty signature")
	}
}

func TestGetContractDetails_RiskTableEdgeCases(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		riskResponse string
		statusCode   int
	}{
		{
			name:         "HTTP 500 error",
			riskResponse: `{"result":false}`,
			statusCode:   http.StatusInternalServerError,
		},
		{
			name:         "Invalid JSON",
			riskResponse: `{"invalid-json"`,
			statusCode:   http.StatusOK,
		},
		{
			name:         "Result false response",
			riskResponse: `{"result":false,"timestamp":1719600000000,"data":{"symbols":[]}}`,
			statusCode:   http.StatusOK,
		},
		{
			name:         "Invalid maxLeverage string value",
			riskResponse: `{"result":true,"timestamp":1719600000000,"data":{"symbols":[{"symbol":"BTC_USDT_PERP","rows":[{"rowNum":1,"maxLeverage":"abc"}]}]}}`,
			statusCode:   http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/common/symbols", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"result":true,"data":{"symbols":[{"symbol":"BTC_USDT_PERP","name":"BTC USDT PERP","type":"PERP","baseCurrency":"BTC","quoteCurrency":"USDT","baseStep":"0.001","quoteStep":"0.1","minSizeLimit":"0.001","status":"TRADING"}]}}`))
			})
			mux.HandleFunc("/api/v1/common/riskTable", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
				_, _ = w.Write([]byte(tc.riskResponse))
			})

			ts := httptest.NewServer(mux)
			defer ts.Close()

			client := pionex.NewClient(http.DefaultClient, ts.URL, "apiKey", "apiSecret", config.LoggingConfig{})
			details, err := client.GetContractDetails(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(details) != 1 {
				t.Fatalf("expected 1 contract detail, got %d", len(details))
			}

			d := details[0]
			if d.MaxLeverage != 100 {
				t.Errorf("expected fallback MaxLeverage 100, got %d", d.MaxLeverage)
			}
		})
	}
}

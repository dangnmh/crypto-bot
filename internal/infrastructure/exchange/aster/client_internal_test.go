package aster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
)

type mockClock struct{}

func (mockClock) Now() time.Time {
	return time.Unix(1600000000, 0)
}

func TestAsterEIP712Signing(t *testing.T) {
	t.Parallel()
	client := NewClient(nil, "http://localhost", "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	params := map[string]string{
		"symbol": "BTCUSDT",
		"side":   "BUY",
		"nonce":  "1600000000000000",
	}

	sig, err := client.signParams(params)
	if err != nil {
		t.Fatalf("signParams failed: %v", err)
	}

	if len(sig) != 130 {
		t.Errorf("expected 130 character hex signature, got length %d", len(sig))
	}
}

func TestAsterGetTickers(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/ticker/24hr") {
			_, _ = w.Write([]byte(`[{"symbol":"BTCUSDT","lastPrice":"25000.5","volume":"10.5","quoteVolume":"262500","closeTime":1600000000000}]`))
			return
		}
		if strings.Contains(r.URL.Path, "/ticker/bookTicker") {
			_, _ = w.Write([]byte(`[{"symbol":"BTCUSDT","bidPrice":"25000.0","askPrice":"25001.0","time":1600000000000}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	if err != nil {
		t.Fatalf("GetTickers failed: %v", err)
	}
	if len(tickers) != 1 || tickers[0].Symbol != "BTCUSDT" || tickers[0].LastPrice != 25000.5 || tickers[0].Bid1 != 25000.0 || tickers[0].Ask1 != 25001.0 {
		t.Errorf("unexpected tickers value: %+v", tickers)
	}
}

func TestAsterGetContractDetails(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/exchangeInfo") {
			_, _ = w.Write([]byte(`{
				"symbols": [{
					"symbol": "BTCUSDT",
					"baseAsset": "BTC",
					"quoteAsset": "USDT",
					"marginAsset": "USDT",
					"pricePrecision": 2,
					"quantityPrecision": 3,
					"filters": [
						{"filterType": "PRICE_FILTER", "tickSize": "0.1"},
						{"filterType": "LOT_SIZE", "minQty": "0.001", "stepSize": "0.001"}
					]
				}]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	if err != nil {
		t.Fatalf("GetContractDetails failed: %v", err)
	}
	if len(details) != 1 || details[0].Symbol != "BTCUSDT" || details[0].PriceScale != 2 || details[0].PriceUnit != 0.1 || details[0].MinVol != 0 {
		t.Errorf("unexpected details value: %+v", details)
	}
}

func TestAsterGetFundingRates(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/premiumIndex") {
			_, _ = w.Write([]byte(`[{"symbol":"BTCUSDT","lastFundingRate":"0.0001","nextFundingTime":1600003600000}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
	if err != nil {
		t.Fatalf("GetFundingRates failed: %v", err)
	}
	if len(rates) != 1 || rates[0].Rate != 0.0001 || rates[0].SettleTime != 1600003600000 {
		t.Errorf("unexpected rates value: %+v", rates)
	}
}

func TestAsterPrivateSettings(t *testing.T) {
	t.Parallel()
	marginModeTries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/positionSide/dual") {
			_, _ = w.Write([]byte(`{"code":200,"msg":"success"}`))
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/marginType") {
			marginModeTries++
			if marginModeTries == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":-4168,"msg":"Unable to adjust to isolated-margin mode under the Multi-Assets mode."}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":200,"msg":"success"}`))
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/multiAssetsMargin") {
			_, _ = w.Write([]byte(`{"code":200,"msg":"success"}`))
			return
		}
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/leverage") {
			_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","leverage":10,"maxNotionalValue":"1000000"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	ctx := context.Background()
	if err := client.SwitchPositionMode(ctx, "BTCUSDT", domain.PositionModeHedge); err != nil {
		t.Fatalf("SwitchPositionMode failed: %v", err)
	}
	if err := client.SwitchMarginMode(ctx, "BTCUSDT", domain.MarginModeIsolated, 10, domain.SideOpenLong); err != nil {
		t.Fatalf("SwitchMarginMode failed: %v", err)
	}
	if err := client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{Symbol: "BTCUSDT", Leverage: 10}); err != nil {
		t.Fatalf("ChangeLeverage failed: %v", err)
	}
}

func TestAsterOrderLifecycle(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order") {
			_, _ = w.Write([]byte(`{"orderId":123456,"clientOrderId":"test-client-id","status":"NEW"}`))
			return
		}
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/order") {
			_, _ = w.Write([]byte(`{"orderId":123456,"clientOrderId":"test-client-id","status":"CANCELED"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	ctx := context.Background()
	res, err := client.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol: "BTCUSDT",
		Price:  25000,
		Vol:    1.5,
		Side:   domain.SideOpenLong,
		Type:   domain.OrderTypeLimit,
	})
	if err != nil {
		t.Fatalf("CreateOrder failed: %v", err)
	}
	if res.OrderID != "123456" {
		t.Errorf("unexpected order id: %s", res.OrderID)
	}

	if err := client.CancelOrder(ctx, "BTCUSDT", "123456"); err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}
}

func TestAsterPlaceTPSL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/order") {
			_ = r.ParseForm()
			if r.FormValue("type") == "TAKE_PROFIT_MARKET" || r.FormValue("type") == "STOP_MARKET" {
				if r.FormValue("closePosition") != "true" || r.FormValue("stopPrice") == "" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				_, _ = w.Write([]byte(`{"orderId":999999,"clientOrderId":"tpsl-123","status":"NEW"}`))
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	err := client.PlaceTPSL(context.Background(), exchange.TPSLRequest{
		Symbol:          "BTCUSDT",
		Side:            domain.SideCloseLong,
		TakeProfitPrice: 26000,
		StopLossPrice:   24000,
	})
	if err != nil {
		t.Fatalf("PlaceTPSL failed: %v", err)
	}
}

func TestAsterGetOrderPNL(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/order") {
			_, _ = w.Write([]byte(`{
				"orderId": 123456,
				"clientOrderId": "test-client-id",
				"status": "FILLED",
				"symbol": "BTCUSDT",
				"origQty": "1.5",
				"executedQty": "1.5",
				"price": "25000",
				"avgPrice": "25000",
				"updateTime": 1600000000000
			}`))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/userTrades") {
			_, _ = w.Write([]byte(`[
				{
					"orderId": 123456,
					"symbol": "BTCUSDT",
					"price": "25000",
					"qty": "1.5",
					"commission": "15",
					"realizedPnl": "0",
					"side": "BUY",
					"positionSide": "LONG",
					"time": 1600000000000
				},
				{
					"orderId": 789012,
					"symbol": "BTCUSDT",
					"price": "26000",
					"qty": "1.5",
					"commission": "15",
					"realizedPnl": "1500",
					"side": "SELL",
					"positionSide": "LONG",
					"time": 1600000005000
				}
			]`))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/income") {
			_, _ = w.Write([]byte(`[
				{"symbol":"BTCUSDT","incomeType":"FUNDING_FEE","income":"-5.0","time":1600000002000}
			]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	pnl, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "123456")
	if err != nil {
		t.Fatalf("GetOrderPNL failed: %v", err)
	}
	if pnl.GrossPnL != 1500 || pnl.Fee != 30 || pnl.FundingFee != -5.0 || pnl.NetPnl != 1465.0 {
		t.Errorf("unexpected PNL calculation values: %+v", pnl)
	}
}

func setupAsterMockServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case strings.Contains(path, "/time"):
			_, _ = w.Write([]byte(`{"serverTime": 1783042200000}`))
		case strings.Contains(path, "/positionRisk"):
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"positionAmt": "1.5",
					"entryPrice": "25000",
					"unrealizedProfit": "100",
					"positionSide": "LONG",
					"leverage": "20",
					"marginType": "crossed"
				},
				{
					"symbol": "ETHUSDT",
					"positionAmt": "-2.0",
					"entryPrice": "1500",
					"unrealizedProfit": "-50",
					"positionSide": "SHORT",
					"leverage": "10",
					"marginType": "isolated"
				},
				{
					"symbol": "XRPUSDT",
					"positionAmt": "0",
					"entryPrice": "0",
					"unrealizedProfit": "0",
					"positionSide": "BOTH",
					"leverage": "20",
					"marginType": "crossed"
				}
			]`))
		case r.Method == http.MethodPost && strings.Contains(path, "/order"):
			_, _ = w.Write([]byte(`{
				"orderId": 999999,
				"clientOrderId": "close-order-id",
				"status": "NEW",
				"symbol": "BTCUSDT"
			}`))
		case r.Method == http.MethodDelete && path == "/fapi/v3/order":
			_, _ = w.Write([]byte(`{
				"orderId": 111111,
				"clientOrderId": "external-id-1",
				"status": "CANCELED",
				"symbol": "BTCUSDT"
			}`))
		case r.Method == http.MethodGet && path == "/fapi/v3/order":
			_, _ = w.Write([]byte(`{
				"orderId": 111111,
				"clientOrderId": "external-id-1",
				"symbol": "BTCUSDT",
				"price": "24000",
				"origQty": "0.5",
				"executedQty": "0",
				"status": "NEW",
				"type": "LIMIT",
				"side": "BUY",
				"positionSide": "LONG",
				"updateTime": 1600000000000
			}`))
		case strings.Contains(path, "/allOpenOrders"):
			_, _ = w.Write([]byte(`{"code": 200, "msg": "success"}`))
		case strings.Contains(path, "/openOrders"):
			_, _ = w.Write([]byte(`[
				{
					"orderId": 111111,
					"clientOrderId": "external-id-1",
					"symbol": "BTCUSDT",
					"price": "24000",
					"origQty": "0.5",
					"executedQty": "0",
					"status": "NEW",
					"type": "LIMIT",
					"side": "BUY",
					"positionSide": "LONG",
					"time": 1600000000000
				}
			]`))
		case strings.Contains(path, "/leverageBracket"):
			_, _ = w.Write([]byte(`[
				{
					"symbol": "BTCUSDT",
					"brackets": [
						{"bracket": 1, "initialLeverage": 125}
					]
				}
			]`))
		case strings.Contains(path, "/exchangeInfo"):
			_, _ = w.Write([]byte(`{
				"symbols": [{
					"symbol": "BTCUSDT",
					"baseAsset": "BTC",
					"quoteAsset": "USDT",
					"marginAsset": "USDT",
					"pricePrecision": 2,
					"quantityPrecision": 3,
					"filters": []
				}]
			}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestAsterAdditionalCoverage_System(t *testing.T) {
	t.Parallel()
	server := setupAsterMockServer()
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})

	st, err := client.GetServerTime(context.Background())
	if err != nil || st != 1783042200000 {
		t.Errorf("GetServerTime failed or unexpected: %v, st=%d", err, st)
	}
	if client.SupportLeverageOnOrder() {
		t.Errorf("SupportLeverageOnOrder should be false")
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	client.WarmUp(ctx, 100*time.Millisecond)
}

func TestAsterAdditionalCoverage_Positions(t *testing.T) {
	t.Parallel()
	server := setupAsterMockServer()
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})

	pos, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	if err != nil || len(pos) != 2 {
		t.Fatalf("GetOpenPositions failed or unexpected length: %v, len=%d", err, len(pos))
	}
	if pos[0].Symbol != "BTCUSDT" || pos[0].PositionType != exchange.PositionTypeLong || pos[0].HoldVolContract != 1.5 {
		t.Errorf("unexpected long position mapping: %+v", pos[0])
	}
	if pos[1].Symbol != "ETHUSDT" || pos[1].PositionType != exchange.PositionTypeShort || pos[1].HoldVolContract != 2.0 {
		t.Errorf("unexpected short position mapping: %+v", pos[1])
	}

	err = client.ClosePosition(context.Background(), "BTCUSDT", domain.SideCloseLong, 1.5, domain.PositionModeHedge, 20)
	if err != nil {
		t.Errorf("ClosePosition long failed: %v", err)
	}

	err = client.ClosePosition(context.Background(), "BTCUSDT", domain.SideCloseShort, 1.5, domain.PositionModeHedge, 20)
	if err != nil {
		t.Errorf("ClosePosition short failed: %v", err)
	}

	err = client.CloseAllPositions(context.Background(), "BTCUSDT")
	if err != nil {
		t.Errorf("CloseAllPositions failed: %v", err)
	}
}

func TestAsterAdditionalCoverage_Orders(t *testing.T) {
	t.Parallel()
	server := setupAsterMockServer()
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})

	err := client.CancelAllOpenOrders(context.Background(), "BTCUSDT")
	if err != nil {
		t.Errorf("CancelAllOpenOrders failed: %v", err)
	}

	err = client.CancelOrders(context.Background(), []string{"111111", "222222"})
	if err != nil {
		t.Errorf("CancelOrders failed: %v", err)
	}

	orders, err := client.GetOpenOrders(context.Background(), "BTCUSDT")
	if err != nil || len(orders) != 1 || orders[0].OrderID != "111111" {
		t.Errorf("GetOpenOrders failed or unexpected: %v, orders=%+v", err, orders)
	}

	order, err := client.GetOrderByExternalID(context.Background(), "BTCUSDT", "external-id-1")
	if err != nil || order.OrderID != "111111" {
		t.Errorf("GetOrderByExternalID failed or unexpected: %v, order=%+v", err, order)
	}
}

func TestAsterAdditionalCoverage_ContractDetails(t *testing.T) {
	t.Parallel()
	server := setupAsterMockServer()
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})

	details, err := client.GetContractDetails(context.Background())
	if err != nil || len(details) != 1 {
		t.Fatalf("GetContractDetails failed: %v", err)
	}
	if details[0].MaxLeverage != 125 {
		t.Errorf("expected MaxLeverage 125, got %d", details[0].MaxLeverage)
	}
}

func TestAsterPotentialFundingSymbols(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/ticker/24hr") {
			_, _ = w.Write([]byte(`[
				{"symbol": "BTCUSDT", "lastPrice": "25000", "volume": "1000", "quoteVolume": "25000000", "closeTime": 1600000000000},
				{"symbol": "ETHUSDT", "lastPrice": "1500", "volume": "500", "quoteVolume": "750000", "closeTime": 1600000000000}
			]`))
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/premiumIndex") {
			_, _ = w.Write([]byte(`[
				{"symbol": "BTCUSDT", "lastFundingRate": "0.0001", "nextFundingTime": 1600003600000},
				{"symbol": "ETHUSDT", "lastFundingRate": "0.0002", "nextFundingTime": 1600003600000}
			]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "", "", "", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 100000, 50000000, []string{"BTCUSDT", "ETHUSDT"}, []string{})
	if err != nil || len(res) != 2 {
		t.Fatalf("GetPotentialFundingSymbols failed: %v, len=%d", err, len(res))
	}
	if res[0].Symbol != "BTCUSDT" || res[0].Rate != 0.0001 || res[1].Symbol != "ETHUSDT" || res[1].Rate != 0.0002 {
		t.Errorf("unexpected scan results: %+v", res)
	}
}

func TestAsterRawRequests(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"raw": "response"}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	ctx := context.Background()
	_, err := client.GetFundingRateRaw(ctx, nil)
	if err != nil {
		t.Errorf("GetFundingRateRaw failed: %v", err)
	}
	_, err = client.GetTickersRaw(ctx, nil)
	if err != nil {
		t.Errorf("GetTickersRaw failed: %v", err)
	}
	_, err = client.GetOpenPositionsRaw(ctx, nil)
	if err != nil {
		t.Errorf("GetOpenPositionsRaw failed: %v", err)
	}
	_, err = client.GetHistoryPositionsRaw(ctx, nil)
	if err != nil {
		t.Errorf("GetHistoryPositionsRaw failed: %v", err)
	}
	_, err = client.GetOrderDetailRaw(ctx, "123", nil)
	if err != nil {
		t.Errorf("GetOrderDetailRaw failed: %v", err)
	}
	_, err = client.GetHistoryOrdersRaw(ctx, nil)
	if err != nil {
		t.Errorf("GetHistoryOrdersRaw failed: %v", err)
	}
	_, err = client.GetOrderPNLRaw(ctx, nil)
	if err != nil {
		t.Errorf("GetOrderPNLRaw failed: %v", err)
	}

	// Test RawRequest with body
	_, err = client.RawRequest(ctx, "POST", "/fapi/v3/test", nil, []byte(`{"key":"val"}`))
	if err != nil {
		t.Errorf("RawRequest with body failed: %v", err)
	}
}

func TestAsterSwitchPositionMode_IgnoreAlreadySet(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":-4059,"msg":"No need to change position side."}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	err := client.SwitchPositionMode(context.Background(), "BTCUSDT", domain.PositionModeHedge)
	if err != nil {
		t.Errorf("expected SwitchPositionMode to ignore -4059 error, but got: %v", err)
	}
}

func TestAsterSwitchMarginMode_IgnoreAlreadySet(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":-4046,"msg":"No need to change margin type."}`))
	}))
	defer server.Close()

	client := NewClient(server.Client(), server.URL, "0x6E2435798939aE93815647457989381564745798", "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", "0x6E2435798939aE93815647457989381564745798", config.LoggingConfig{})
	client.SetClock(mockClock{})

	err := client.SwitchMarginMode(context.Background(), "BTCUSDT", domain.MarginModeIsolated, 10, domain.SideOpenLong)
	if err != nil {
		t.Errorf("expected SwitchMarginMode to ignore -4046 error, but got: %v", err)
	}
}

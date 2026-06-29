package orangex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/orangex"
)

type stubClock struct {
	now time.Time
}

func (s stubClock) Now() time.Time {
	return s.now
}

func setupMockServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/public/auth":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"mock_tok","expires_in":3600}}`))
		case "/public/time":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"1719600000"}`))
		case "/public/tickers":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"instrument_name":"BTC-USDT-PERPETUAL","best_bid_price":"60000","best_ask_price":"60005","last_price":"60002","volume_24h":"10"}]}`))
		case "/public/get_instruments":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"instrument_name":"BTC-USDT-PERPETUAL","base_currency":"BTC","quote_currency":"USDT","tick_size":"0.1","min_trade_amount":"0.001","funding_rate":"0.0001","next_funding_rate_timestamp":1719600000000}]}`))
		case "/public/coin_gecko_contracts":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"ticker_id":"BTC-USDT-PERPETUAL","product_type":"perpetual","target_currency":"USDT","target_volume":"10000000.0","last_price":"60000.0","funding_rate":"0.0001","next_funding_rate_timestamp":1719600000,"base_volume":"166.666","bid":"59999.0","ask":"60001.0","high":"61000.0","low":"59000.0","open_interest":"5.0","index_price":"60000.5","index_name":"BTC-USDT","index_currency":"BTC","start_timestamp":1719600000,"end_timestamp":1719600000,"next_funding_rate":"0.0002","contract_type":"Quanto","contract_price":"60000.0","contract_price_currency":"USDT"}]}`))
		case "/private/buy", "/private/sell":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"order":{"order_id":"ord123"}}}`))
		case "/private/get_order_state":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"order_id":"ord123","order_state":"filled","amount":"1.0","price":"60000.0","filled_amount":"1.0","average_price":"60000.0","creation_timestamp":1719600000000,"instrument_name":"BTC-USDT-PERPETUAL","direction":"buy","custom_order_id":"cust123"}}`))
		case "/private/cancel", "/private/cancel_all_by_instrument":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
		case "/private/get_open_orders_by_instrument":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"order_id":"ord123","order_state":"untriggered","amount":"1.0","price":"60000.0","filled_amount":"0.0","average_price":"0.0","creation_timestamp":1719600000000,"instrument_name":"BTC-USDT-PERPETUAL","direction":"buy","custom_order_id":"cust123"}]}`))
		case "/private/get_positions":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"instrument_name":"BTC-USDT-PERPETUAL","direction":"buy","size":"1.0","average_price":"60000.0","leverage":"10","margin":"6000.0"}]}`))
		case "/private/close_position":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"order":{"order_id":"ordclose"}}}`))
		case "/private/adjust_perpetual_leverage", "/private/adjust_perpetual_margin_type":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
		case "/private/get_user_trades_by_order":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"trades":[{"amount":"1.0","direction":"buy","fee":"1.2","fee_coin_type":"USDT","instrument_name":"BTC-USDT-PERPETUAL","order_id":"ord123","price":"60000.0","timestamp":1719600001000}],"has_more":false}}`))
		}
	}))
}

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	ts, err := client.GetServerTime(context.Background())
	if err != nil || ts != 1719600000000 {
		t.Fatalf("GetServerTime failed: %v, ts=%d", err, ts)
	}
}

func TestClient_GetAccessToken(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	token, err := client.GetAccessToken(context.Background())
	if err != nil || token != "mock_tok" {
		t.Fatalf("GetAccessToken failed: %v, token=%s", err, token)
	}
}

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	ctx := context.Background()

	tickers, err := client.GetTickers(ctx, "BTC-USDT-PERPETUAL")
	if err != nil || len(tickers) != 1 || tickers[0].LastPrice != 60002.0 {
		t.Fatalf("GetTickers failed: %v, tickers=%+v", err, tickers)
	}
	if tickers[0].AmountUSDT24 != 10.0*60002.0 {
		t.Fatalf("AmountUSDT24 for single symbol not set correctly: got %f, want %f", tickers[0].AmountUSDT24, 10.0*60002.0)
	}

	allTickers, err := client.GetTickers(ctx, "")
	if err != nil || len(allTickers) != 1 {
		t.Fatalf("GetTickers (all) failed: %v, tickers=%+v", err, allTickers)
	}
	if allTickers[0].Symbol != "BTC-USDT-PERPETUAL" ||
		allTickers[0].LastPrice != 60000.0 ||
		allTickers[0].Bid1 != 59999.0 ||
		allTickers[0].Ask1 != 60001.0 ||
		allTickers[0].Volume24 != 166.666 ||
		allTickers[0].AmountUSDT24 != 10000000.0 {
		t.Fatalf("GetTickers values mapped incorrectly: %+v", allTickers[0])
	}
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	ctx := context.Background()

	details, err := client.GetContractDetails(ctx)
	if err != nil || len(details) != 1 || details[0].PriceScale != 1 {
		t.Fatalf("GetContractDetails failed: %v, details=%+v", err, details)
	}
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	ctx := context.Background()

	rates, err := client.GetFundingRates(ctx, []string{"BTC-USDT-PERPETUAL"})
	if err != nil || len(rates) != 1 || rates[0].Rate != 0.0001 {
		t.Fatalf("GetFundingRates failed: %v, rates=%+v", err, rates)
	}
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	ctx := context.Background()

	potSymbols, err := client.GetPotentialFundingSymbols(ctx, 1000000.0, 0.0, nil, nil)
	if err != nil || len(potSymbols) != 1 || potSymbols[0].Symbol != "BTC-USDT-PERPETUAL" {
		t.Fatalf("GetPotentialFundingSymbols failed: %v, symbols=%+v", err, potSymbols)
	}
}

func TestClient_OrderManagement(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	ctx := context.Background()

	res, err := client.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol: "BTC-USDT-PERPETUAL",
		Side:   domain.SideOpenLong,
		Vol:    1.0,
		Price:  60000.0,
	})
	if err != nil || res.OrderID != "ord123" {
		t.Fatalf("CreateOrder failed: %v, res=%+v", err, res)
	}

	order, err := client.GetOrder(ctx, "BTC-USDT-PERPETUAL", "ord123")
	if err != nil || order.OrderID != "ord123" || order.State != domain.OrderStateFilled {
		t.Fatalf("GetOrder failed: %v, order=%+v", err, order)
	}

	orderExt, err := client.GetOrderByExternalID(ctx, "BTC-USDT-PERPETUAL", "cust123")
	if err != nil || orderExt.ExternalOID != "cust123" {
		t.Fatalf("GetOrderByExternalID failed: %v, orderExt=%+v", err, orderExt)
	}

	err = client.CancelOrder(ctx, "BTC-USDT-PERPETUAL", "ord123")
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}

	err = client.CancelOrders(ctx, []string{"ord123"})
	if err != nil {
		t.Fatalf("CancelOrders failed: %v", err)
	}

	err = client.CancelAllOpenOrders(ctx, "BTC-USDT-PERPETUAL")
	if err != nil {
		t.Fatalf("CancelAllOpenOrders failed: %v", err)
	}

	openOrders, err := client.GetOpenOrders(ctx, "BTC-USDT-PERPETUAL")
	if err != nil || len(openOrders) != 1 {
		t.Fatalf("GetOpenOrders failed: %v, err", err)
	}
}

func TestClient_PositionAndMargin(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	ctx := context.Background()

	positions, err := client.GetOpenPositions(ctx, "BTC-USDT-PERPETUAL")
	if err != nil || len(positions) != 1 || positions[0].HoldVol != 1.0 {
		t.Fatalf("GetOpenPositions failed: %v, positions=%+v", err, positions)
	}

	err = client.ClosePosition(ctx, "BTC-USDT-PERPETUAL", domain.SideCloseLong, 1.0, domain.PositionModeHedge, 0)
	if err != nil {
		t.Fatalf("ClosePosition failed: %v", err)
	}

	err = client.CloseAllPositions(ctx, "BTC-USDT-PERPETUAL")
	if err != nil {
		t.Fatalf("CloseAllPositions failed: %v", err)
	}

	err = client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{
		Symbol:   "BTC-USDT-PERPETUAL",
		Leverage: 5,
	})
	if err != nil {
		t.Fatalf("ChangeLeverage failed: %v", err)
	}

	err = client.SwitchMarginMode(ctx, "BTC-USDT-PERPETUAL", "isolated", 5, domain.SideOpenLong)
	if err != nil {
		t.Fatalf("SwitchMarginMode failed: %v", err)
	}
}

func TestClient_PnLAndWarmup(t *testing.T) {
	t.Parallel()
	srv := setupMockServer()
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	client.SetClock(stubClock{now: time.Unix(1719600000, 0)})

	ctx := context.Background()

	pnl, err := client.GetOrderPNL(ctx, "BTC-USDT-PERPETUAL", "ord123")
	if err != nil || pnl.Fee != 1.2 || pnl.NetPnl != -1.2 {
		t.Fatalf("GetOrderPNL failed: %v, pnl=%+v", err, pnl)
	}

	client.WarmUp(ctx, time.Second)
	if client.SupportLeverageOnOrder() {
		t.Error("expected SupportLeverageOnOrder to be false")
	}

	bgCtx, cancel := context.WithCancel(ctx)
	client.StartBackgroundTasks(bgCtx)
	cancel()
}

func TestClient_Errors(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/private/get_positions" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":401,"message":"unauthorized"}}`))
			return
		}
		if r.URL.Path == "/public/tickers" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":10001,"message":"mock bad request"}}`))
			return
		}
		if r.URL.Path == "/public/time" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("internal server error"))
			return
		}
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := orangex.NewClient(nil, srv.URL, "key", "secret", config.LoggingConfig{})
	ctx := context.Background()

	_, err := client.GetOpenPositions(ctx, "")
	if err == nil {
		t.Error("expected error for unauthorized request")
	}

	_, err = client.GetTickers(ctx, "")
	if err == nil {
		t.Error("expected error for RPC error response")
	}

	_, err = client.GetServerTime(ctx)
	if err == nil {
		t.Error("expected error for InternalServerError status")
	}
}

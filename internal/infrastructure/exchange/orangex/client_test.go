package orangex_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	handlers := map[string]func(w http.ResponseWriter, r *http.Request){
		"/public/auth": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"access_token":"mock_tok","expires_in":3600}}`))
		},
		"/public/time": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"1719600000"}`))
		},
		"/public/tickers": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"instrument_name":"BTC-USDT-PERPETUAL","best_bid_price":"60000","best_ask_price":"60005","last_price":"60002","volume_24h":"10"}]}`))
		},
		"/public/get_instruments": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"instrument_name":"BTC-USDT-PERPETUAL","base_currency":"BTC","quote_currency":"USDT","tick_size":"0.1","min_trade_amount":"0.001","funding_rate":"0.0001","next_funding_rate_timestamp":1719600000000,"leverage":75}]}`))
		},
		"/public/coin_gecko_contracts": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"ticker_id":"BTC-USDT-PERPETUAL","product_type":"perpetual","target_currency":"USDT","target_volume":"10000000.0","last_price":"60000.0","funding_rate":"0.0001","next_funding_rate_timestamp":1719600000,"base_volume":"166.666","bid":"59999.0","ask":"60001.0","high":"61000.0","low":"59000.0","open_interest":"5.0","index_price":"60000.5","index_name":"BTC-USDT","index_currency":"BTC","start_timestamp":1719600000,"end_timestamp":1719600000,"next_funding_rate":"0.0002","contract_type":"Quanto","contract_price":"60000.0","contract_price_currency":"USDT"}]}`))
		},
		"/private/buy": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"order":{"order_id":"ord123"}}}`))
		},
		"/private/sell": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"order":{"order_id":"ord123"}}}`))
		},
		"/private/get_order_state": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"order_id":"ord123","order_state":"filled","amount":"1.0","price":"60000.0","filled_amount":"1.0","average_price":"60000.0","creation_timestamp":1719600000000,"instrument_name":"BTC-USDT-PERPETUAL","direction":"buy","custom_order_id":"cust123"}}`))
		},
		"/private/cancel": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
		},
		"/private/cancel_all_by_instrument": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
		},
		"/private/get_open_orders_by_instrument": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"order_id":"ord123","order_state":"untriggered","amount":"1.0","price":"60000.0","filled_amount":"0.0","average_price":"0.0","creation_timestamp":1719600000000,"instrument_name":"BTC-USDT-PERPETUAL","direction":"buy","custom_order_id":"cust123"}]}`))
		},
		"/private/get_positions": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":[{"instrument_name":"BTC-USDT-PERPETUAL","direction":"buy","size":"1.0","average_price":"60000.0","leverage":"10","margin":"6000.0"}]}`))
		},
		"/private/close_position": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"order":{"order_id":"ordclose"}}}`))
		},
		"/private/adjust_perpetual_leverage": func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req struct {
				Params struct {
					InstrumentName string `json:"instrument_name"`
				} `json:"params"`
			}
			_ = json.Unmarshal(bodyBytes, &req)
			if req.Params.InstrumentName == "ERROR-5147" {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":5147,"message":"Unsupported operation on current position mode"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
		},
		"/private/adjust_perpetual_margin_type": func(w http.ResponseWriter, r *http.Request) {
			bodyBytes, _ := io.ReadAll(r.Body)
			var req struct {
				Params struct {
					MarginType string `json:"margin_type"`
				} `json:"params"`
			}
			_ = json.Unmarshal(bodyBytes, &req)
			if req.Params.MarginType != "isolate" && req.Params.MarginType != "cross" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32602,"message":"Invalid params: missing or invalid margin_type"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
		},
		"/private/get_user_trades_by_instrument": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"trades":[
				{"amount":"1.0","direction":"buy","fee":"1.2","fee_coin_type":"USDT","instrument_name":"BTC-USDT-PERPETUAL","order_id":"ord123","price":"60000.0","timestamp":1719600001000},
				{"amount":"1.0","direction":"sell","fee":"1.3","fee_coin_type":"USDT","instrument_name":"BTC-USDT-PERPETUAL","order_id":"ordclose","price":"61000.0","timestamp":1719600005000}
			],"has_more":false}}`))
		},
		"/private/get_transaction_log": func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":{"total":1,"logs":[
				{"id":"818164928862748672","type":"perpetual_funding","change":"-0.0145","coin_type":"USDT","asset_type":"PERPETUAL","create_time":"1719600000500"}
			]}}`))
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if handler, ok := handlers[r.URL.Path]; ok {
			handler(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
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
	if details[0].PriceUnit != 0.1 {
		t.Errorf("expected PriceUnit 0.1, got %f", details[0].PriceUnit)
	}
	if details[0].ContractSize != 1.0 {
		t.Errorf("expected ContractSize 1.0, got %f", details[0].ContractSize)
	}
	if details[0].MinVol != 0 {
		t.Errorf("expected MinVol 0, got %d", details[0].MinVol)
	}
	if details[0].MaxLeverage != 75 {
		t.Errorf("expected MaxLeverage 75, got %d", details[0].MaxLeverage)
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
		Symbol:       "BTC-USDT-PERPETUAL",
		Leverage:     5,
		PositionType: exchange.PositionTypeShort,
	})
	if err != nil {
		t.Fatalf("ChangeLeverage failed: %v", err)
	}

	// Test ChangeLeverage returning error 5147, which should be ignored (nil returned)
	err = client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{
		Symbol:       "ERROR-5147",
		Leverage:     5,
		PositionType: exchange.PositionTypeShort,
	})
	if err != nil {
		t.Fatalf("expected ChangeLeverage to ignore error 5147, got: %v", err)
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
	if err != nil {
		t.Fatalf("GetOrderPNL failed: %v", err)
	}
	if pnl.EntryPrice != 60000.0 {
		t.Errorf("expected EntryPrice 60000.0, got %f", pnl.EntryPrice)
	}
	if pnl.ExitPrice != 61000.0 {
		t.Errorf("expected ExitPrice 61000.0, got %f", pnl.ExitPrice)
	}
	if pnl.ClosedSize != 1.0 {
		t.Errorf("expected ClosedSize 1.0, got %f", pnl.ClosedSize)
	}
	if pnl.Fee != 2.5 {
		t.Errorf("expected Fee 2.5, got %f", pnl.Fee)
	}
	if pnl.NetPnl != 997.4855 {
		t.Errorf("expected NetPnl 997.4855, got %f", pnl.NetPnl)
	}
	if pnl.FundingFee != 0.0145 {
		t.Errorf("expected FundingFee 0.0145, got %f", pnl.FundingFee)
	}
	if pnl.DurationMs != 5000 {
		t.Errorf("expected DurationMs 5000, got %d", pnl.DurationMs)
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

func TestClient_GetTransactionLog(t *testing.T) {
	t.Parallel()
	server := setupMockServer()
	defer server.Close()
	client := orangex.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})
	res, err := client.GetTransactionLogRaw(context.Background(), map[string]string{"currency": "USDT"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(res), `"logs"`) {
		t.Errorf("expected response to contain logs, got %s", string(res))
	}
}

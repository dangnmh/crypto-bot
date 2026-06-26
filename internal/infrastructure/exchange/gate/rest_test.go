package gate_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/gate"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_MarketAndAccountEndpoints(t *testing.T) {
	t.Parallel()

	server := newGateServer(t)
	client := gate.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ctx := context.Background()

	serverTime, err := client.GetServerTime(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1700000000000), serverTime)

	contracts, err := client.GetContractDetails(ctx)
	require.NoError(t, err)
	require.Len(t, contracts, 1)
	assert.Equal(t, "BTC_USDT", contracts[0].Symbol)
	assert.Equal(t, 0.0001, contracts[0].PriceUnit)
	assert.Equal(t, 100, contracts[0].MaxLeverage)
	assert.Equal(t, 1, contracts[0].MinVol)
	assert.Equal(t, 10000, contracts[0].MaxVol)
	assert.Equal(t, 0, contracts[0].VolScale)

	tickers, err := client.GetTickers(ctx, "BTC_USDT")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, 100.5, tickers[0].LastPrice)

	fundingRates, err := client.GetFundingRates(ctx, []string{"BTC_USDT"})
	require.NoError(t, err)
	require.Len(t, fundingRates, 1)
	assert.Equal(t, "BTC_USDT", fundingRates[0].Symbol)
	assert.Equal(t, 0.001, fundingRates[0].Rate)

	positions, err := client.GetOpenPositions(ctx, "BTC_USDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, 2.0, positions[0].HoldVol)
	assert.Equal(t, exchange.PositionTypeLong, positions[0].PositionType)

	positions, err = client.GetOpenPositions(ctx, "")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "BTC_USDT", positions[0].Symbol)
}

func TestClient_OrderEndpoints(t *testing.T) {
	t.Parallel()

	server := newGateServer(t)
	client := gate.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ctx := context.Background()

	require.NoError(t, client.CancelOrder(ctx, "BTC_USDT", "42"))
	require.NoError(t, client.CancelOrders(ctx, []string{"42", "43"}))
	require.NoError(t, client.CancelAllOpenOrders(ctx, "BTC_USDT"))

	order, err := client.GetOrder(ctx, "BTC_USDT", "42")
	require.NoError(t, err)
	require.NotNil(t, order)
	assert.Equal(t, "42", order.OrderID)
	assert.Equal(t, exchange.OrderStateFilled, order.State)
	assert.Equal(t, "ext", order.ExternalOID)

	openOrders, err := client.GetOpenOrders(ctx, "BTC_USDT")
	require.NoError(t, err)
	require.Len(t, openOrders, 1)
	assert.Equal(t, exchange.OrderStatePartiallyFilled, openOrders[0].State)

	// Test GetOrderByExternalID direct query success
	orderDirect, err := client.GetOrderByExternalID(ctx, "BTC_USDT", "direct")
	require.NoError(t, err)
	require.NotNil(t, orderDirect)
	assert.Equal(t, "42", orderDirect.OrderID)
	assert.Equal(t, exchange.OrderStateFilled, orderDirect.State)

	// Test GetOrderByExternalID with fallback to finished orders list
	orderFinished, err := client.GetOrderByExternalID(ctx, "BTC_USDT", "finished")
	require.NoError(t, err)
	require.NotNil(t, orderFinished)
	assert.Equal(t, "42", orderFinished.OrderID)
	assert.Equal(t, exchange.OrderStateFilled, orderFinished.State)

	// Test GetOrderByExternalID with fallback to orders_timerange
	orderByExt, err := client.GetOrderByExternalID(ctx, "BTC_USDT", "ext")
	require.NoError(t, err)
	require.NotNil(t, orderByExt)
	assert.Equal(t, "42", orderByExt.OrderID)
	assert.Equal(t, exchange.OrderStateFilled, orderByExt.State)

	require.NoError(t, client.ClosePosition(ctx, "BTC_USDT", domain.SideCloseLong, 2, 1, 20))
	require.NoError(t, client.CloseAllPositions(ctx, "BTC_USDT"))
	require.NoError(t, client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{Symbol: "BTC_USDT", Leverage: 20}))
}

func TestClient_LatencyWarmUpAndRESTErrors(t *testing.T) {
	t.Parallel()

	server := newGateServer(t)
	client := gate.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})

	latency, err := client.Latency(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, latency, int64(0))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.WarmUp(ctx, time.Hour)

	errServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	t.Cleanup(errServer.Close)
	errClient := gate.NewClient(errServer.Client(), errServer.URL, "key", "secret", config.LoggingConfig{})

	_, err = errClient.GetServerTime(context.Background())
	require.Error(t, err)
	_, err = errClient.GetContractDetails(context.Background())
	require.Error(t, err)
	_, err = errClient.GetTickers(context.Background(), "BTC_USDT")
	require.Error(t, err)
	_, err = errClient.GetFundingRates(context.Background(), []string{"BTC_USDT"})
	require.Error(t, err)

	_, err = errClient.GetOpenPositions(context.Background(), "BTC_USDT")
	require.Error(t, err)
	_, err = errClient.CreateOrder(context.Background(), exchange.SubmitOrderRequest{Symbol: "BTC_USDT", Vol: 1})
	require.Error(t, err)
	require.Error(t, errClient.CancelOrder(context.Background(), "BTC_USDT", "42"))
	require.Error(t, errClient.CancelOrders(context.Background(), []string{"42"}))
	require.Error(t, errClient.CancelAllOpenOrders(context.Background(), "BTC_USDT"))
	_, err = errClient.GetOrder(context.Background(), "BTC_USDT", "42")
	require.Error(t, err)
	_, err = errClient.GetOpenOrders(context.Background(), "BTC_USDT")
	require.Error(t, err)
	require.Error(t, errClient.ClosePosition(context.Background(), "BTC_USDT", domain.SideCloseLong, 1, 1, 10))
	require.Error(t, errClient.CloseAllPositions(context.Background(), "BTC_USDT"))
	require.Error(t, errClient.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{Symbol: "BTC_USDT", Leverage: 10}))
	_, err = errClient.Latency(context.Background())
	require.Error(t, err)
}

//nolint:cyclop,gocognit // Single test server switch keeps endpoint fixtures local and readable.
func newGateServer(t *testing.T) *httptest.Server {
	t.Helper()
	positionsCount := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/spot/time":
			writeJSON(t, w, map[string]int64{"server_time": 1700000000000})
		case r.URL.Path == "/futures/usdt/contracts":
			writeJSON(t, w, []map[string]any{{
				"name": "BTC_USDT", "quanto_multiplier": "0.0001", "leverage_min": "1",
				"leverage_max": "100", "order_price_round": "0.0001",
				"maker_fee_rate": "0.0002", "taker_fee_rate": "0.0006",
				"funding_rate": "0.001", "funding_next_apply": 1700000000,
				"order_size_min": 1, "order_size_max": 10000,
			}})
		case r.URL.Path == "/futures/usdt/tickers":
			writeJSON(t, w, []map[string]string{{
				"contract": "BTC_USDT", "last": "100.5", "highest_bid": "100",
				"lowest_ask": "101", "volume_24h": "10", "volume_24h_quote": "1000",
				"index_price": "100.2", "mark_price": "100.3", "funding_rate": "0.001",
			}})
		case r.URL.Path == "/futures/usdt/candlesticks":
			writeJSON(t, w, []map[string]any{{
				"t": 1700000000, "o": "99", "c": "100", "h": "101", "l": "98", "v": 12, "sum": "1200",
			}})
		case strings.HasSuffix(r.URL.Path, "/order_book"):
			writeJSON(t, w, map[string]any{
				"id":   123,
				"asks": []map[string]any{{"p": "101", "s": 2}, {"p": "0", "s": 1}},
				"bids": []map[string]any{{"p": "100", "s": 3}},
			})
		case r.URL.Path == "/futures/usdt/accounts":
			writeJSON(t, w, map[string]string{
				"currency": "USDT", "total": "1000", "unrealised_pnl": "10",
				"position_margin": "100", "available": "900",
			})
		case strings.HasPrefix(r.URL.Path, "/futures/usdt/dual_comp/positions/"):
			parts := strings.Split(r.URL.Path, "/")
			sym := parts[len(parts)-1]
			positionsCount[sym]++
			if sym == "BTC_USDT" && positionsCount[sym] == 1 {
				writeJSON(t, w, []map[string]any{
					gatePosition(2, "dual_long"),
					gatePosition(0, "dual_short"),
				})
			} else {
				writeJSON(t, w, []map[string]any{
					gatePosition(0, "dual_long"),
					gatePosition(0, "dual_short"),
				})
			}
		case r.URL.Path == "/futures/usdt/positions":
			writeJSON(t, w, []map[string]any{gatePosition(2), gatePosition(0)})
		case r.URL.Path == "/futures/usdt/orders" && r.Method == http.MethodGet:
			statusVal := r.URL.Query().Get("status")
			contractVal := r.URL.Query().Get("contract")
			size := int64(5)
			if contractVal == "ETH_USDT" {
				size = -5
			}
			if statusVal == "finished" {
				writeJSON(t, w, []map[string]any{
					gateOrder(42, "finished", "filled", size, 0, "t-finished"),
				})
			} else {
				writeJSON(t, w, []map[string]any{gateOrder(43, "open", "", size, 2, "raw")})
			}
		case r.URL.Path == "/futures/usdt/orders_timerange" && r.Method == http.MethodGet:
			contractVal := r.URL.Query().Get("contract")
			size := int64(5)
			if contractVal == "ETH_USDT" {
				size = -5
			}
			writeJSON(t, w, []map[string]any{
				gateOrderTimerangeString(43, "open", "", "5", "2", "raw"),
				gateOrderTimerange(42, "finished", "filled", size, 0, "t-ext"),
			})
		case r.URL.Path == "/futures/usdt/orders" && r.Method == http.MethodPost:
			writeJSON(t, w, map[string]int64{"id": 99})
		case r.URL.Path == "/futures/usdt/orders" && r.Method == http.MethodDelete:
			writeJSON(t, w, []map[string]any{})
		case strings.HasPrefix(r.URL.Path, "/futures/usdt/orders/"):
			parts := strings.Split(r.URL.Path, "/")
			lastPart := parts[len(parts)-1]
			if lastPart != "t-direct" && lastPart != "t-ext_short" {
				if _, err := strconv.ParseInt(lastPart, 10, 64); err != nil {
					w.WriteHeader(http.StatusNotFound)
					writeJSON(t, w, map[string]string{
						"label":   "ORDER_NOT_FOUND",
						"message": "order not found",
					})
					return
				}
			}
			textVal := lastPart
			size := int64(5)
			if strings.Contains(textVal, "short") || textVal == "43" {
				size = -5
			}
			if _, err := strconv.ParseInt(lastPart, 10, 64); err == nil {
				textVal = "t-ext"
			}
			idVal := int64(42)
			if lastPart == "43" {
				idVal = 43
			}
			writeJSON(t, w, gateOrder(idVal, "finished", "filled", size, 0, textVal))
		case r.URL.Path == "/futures/usdt/dual_mode" && r.Method == http.MethodPost:
			dualModeStr := r.URL.Query().Get("dual_mode")
			if dualModeStr == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			writeJSON(t, w, map[string]any{
				"in_dual_mode": dualModeStr == "true",
			})
		case strings.Contains(r.URL.Path, "/leverage"):
			writeJSON(t, w, []map[string]any{gatePosition(2)})
		case r.URL.Path == "/futures/usdt/my_trades":
			contract := r.URL.Query().Get("contract")
			orderIDStr := r.URL.Query().Get("order")
			if contract == "XRP_USDT" || contract == "DOGE_USDT" {
				writeJSON(t, w, []any{})
				return
			}
			if orderIDStr == "42" || orderIDStr == "43" {
				if contract == "ETH_USDT" {
					writeJSON(t, w, []map[string]any{
						{
							"id":          1001,
							"create_time": 1700000000.0,
							"contract":    "ETH_USDT",
							"order_id":    orderIDStr,
							"size":        "-2.0",
							"close_size":  "0",
							"price":       "101.0",
							"fee":         "0.0",
						},
					})
				} else {
					writeJSON(t, w, []map[string]any{
						{
							"id":          1001,
							"create_time": 1700000000.0,
							"contract":    contract,
							"order_id":    orderIDStr,
							"size":        "2.0",
							"close_size":  "0",
							"price":       "100.0",
							"fee":         "0.0",
						},
					})
				}
				return
			}

			switch contract {
			case "ETH_USDT":
				writeJSON(t, w, []map[string]any{
					{
						"id":          1002,
						"create_time": 1700000005.0,
						"contract":    "ETH_USDT",
						"order_id":    "44",
						"size":        "2.0",
						"close_size":  "2",
						"price":       "100.0",
						"fee":         "-0.04",
					},
				})
			case "LTC_USDT":
				writeJSON(t, w, []map[string]any{
					{
						"id":          1001,
						"create_time": 1700000000.0,
						"contract":    "LTC_USDT",
						"order_id":    "42",
						"size":        "2.0",
						"close_size":  "0",
						"price":       "100.0",
						"fee":         "0.0",
					},
					{
						"id":          1002,
						"create_time": 1700000005.0,
						"contract":    "LTC_USDT",
						"order_id":    "43",
						"size":        "-2.0",
						"close_size":  "-2",
						"price":       "101.0",
						"fee":         "-0.04",
					},
				})
			default:
				writeJSON(t, w, []map[string]any{
					{
						"id":          1002,
						"create_time": 1700000005.0,
						"contract":    "BTC_USDT",
						"order_id":    "43",
						"size":        "-2.0",
						"close_size":  "-2",
						"price":       "101.0",
						"fee":         "-0.04",
					},
				})
			}
		case r.URL.Path == "/futures/usdt/account_book":
			contract := r.URL.Query().Get("contract")
			if contract == "XRP_USDT" || contract == "DOGE_USDT" {
				writeJSON(t, w, []any{})
				return
			}
			writeJSON(t, w, []map[string]any{
				{
					"time":    1700000005.0,
					"change":  "1.0",
					"balance": "1001.0",
					"type":    "pnl",
					"text":    "t-ext",
				},
				{
					"time":    1700000005.0,
					"change":  "-0.01",
					"balance": "1000.99",
					"type":    "fund",
					"text":    "t-ext",
				},
			})
		default:
			t.Fatalf("unhandled %s %s", r.Method, r.URL.String())
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func gateOrder(id int64, status, finishAs string, size, left int64, text string) map[string]any {
	return map[string]any{
		"id": id, "contract": "BTC_USDT", "price": "100", "size": size, "left": left,
		"fill_price": "100.5", "status": status, "finish_as": finishAs, "text": text,
		"create_time": 1700000000, "finish_time": 1700000001,
	}
}

func gateOrderTimerange(id int64, status, finishAs string, size, left int64, text string) map[string]any {
	return map[string]any{
		"id": id, "user": 1, "contract": "BTC_USDT", "price": "100", "size": size, "left": left,
		"fill_price": "100.5", "status": status, "finish_as": finishAs, "text": text,
		"create_time": 1700000000.0, "finish_time": "1700000001.0", "update_time": "1700000001.0",
		"is_close": false, "is_reduce_only": false, "is_liq": false, "tif": "gtc",
	}
}

func gateOrderTimerangeString(id int64, status, finishAs, size, left, text string) map[string]any {
	return map[string]any{
		"id": id, "user": 1, "contract": "BTC_USDT", "price": "100", "size": size, "left": left,
		"fill_price": "100.5", "status": status, "finish_as": finishAs, "text": text,
		"create_time": 1700000000.0, "finish_time": "1700000001.0", "update_time": "1700000001.0",
		"is_close": false, "is_reduce_only": false, "is_liq": false, "tif": "gtc",
	}
}

//nolint:misspell // Gate.io uses the British spelling in the API field name.
func gatePosition(size int64, modeOpt ...string) map[string]any {
	mode := "dual_long"
	if len(modeOpt) > 0 {
		mode = modeOpt[0]
	}
	return map[string]any{
		"contract": "BTC_USDT", "size": size, "entry_price": "100", "liq_price": "50",
		"realised_pnl": "1.5", "leverage": "20",
		"mode":           mode,
		"pnl_pnl":        "1.0",
		"pnl_fee":        "-0.04",
		"pnl_fund":       "-0.01",
		"last_close_pnl": "0.95",
		"update_time":    1700000005,
		"open_time":      1700000000,
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := newGateServer(t)
	client := gate.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ctx := context.Background()

	assertPnLInfo := func(t *testing.T, res *exchange.ClosedPnLInfo, symbol string) {
		t.Helper()
		assert.Equal(t, symbol, res.Symbol)
		assert.Equal(t, 100.0, res.EntryPrice)
		assert.Equal(t, 101.0, res.ExitPrice)
		assert.Equal(t, 2.0, res.ClosedSize)
		assert.Equal(t, 1.0, res.GrossPnL)
		assert.Equal(t, 0.04, res.Fee)
		assert.Equal(t, -0.01, res.FundingFee)
		assert.Equal(t, int64(5000), res.DurationMs)
		assert.Equal(t, 0.95, res.NetPnl)
		assert.Equal(t, 1.0, res.PnLRate)
	}

	t.Run("long side position close matched", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetOrderPNL(ctx, "BTC_USDT", "42")
		require.NoError(t, err)
		require.NotNil(t, res)
		assertPnLInfo(t, res, "BTC_USDT")
	})

	t.Run("short side position close matched", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetOrderPNL(ctx, "ETH_USDT", "43")
		require.NoError(t, err)
		require.NotNil(t, res)

		assert.Equal(t, "ETH_USDT", res.Symbol)
		assert.Equal(t, 101.0, res.EntryPrice)
		assert.Equal(t, 100.0, res.ExitPrice)
		assert.Equal(t, 2.0, res.ClosedSize)
		assert.Equal(t, 1.0, res.GrossPnL)
		assert.Equal(t, 0.04, res.Fee)
		assert.Equal(t, -0.01, res.FundingFee)
		assert.Equal(t, int64(5000), res.DurationMs)
		assert.Equal(t, 0.95, res.NetPnl)
		expectedPnLRate := ((101.0 - 100.0) / 101.0) * 100.0
		assert.Equal(t, expectedPnLRate, res.PnLRate)
	})

	t.Run("ignore opening trade if returned in trades query", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetOrderPNL(ctx, "LTC_USDT", "42")
		require.NoError(t, err)
		require.NotNil(t, res)
		assertPnLInfo(t, res, "LTC_USDT")
	})

	t.Run("position close history unmatched error", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetOrderPNL(ctx, "XRP_USDT", "42")
		require.Error(t, err)
		assert.Nil(t, res)
		assert.Contains(t, err.Error(), "no closing trades found for symbol")
	})

	t.Run("no close records found error", func(t *testing.T) {
		t.Parallel()
		res, err := client.GetOrderPNL(ctx, "DOGE_USDT", "42")
		require.Error(t, err)
		assert.Nil(t, res)
		assert.Contains(t, err.Error(), "no closing trades found for symbol")
	})
}

func TestClient_SetPositionModeAndChangeLeverage(t *testing.T) {
	t.Parallel()

	server := newGateServer(t)
	client := gate.NewClient(server.Client(), server.URL, "key", "secret", config.LoggingConfig{})
	ctx := context.Background()

	t.Run("set position mode successfully", func(t *testing.T) {
		t.Parallel()
		err := client.SetPositionMode(ctx, "usdt", true)
		require.NoError(t, err)

		err = client.SetPositionMode(ctx, "usdt", false)
		require.NoError(t, err)
	})

	t.Run("change leverage cross mode", func(t *testing.T) {
		t.Parallel()
		err := client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{
			Symbol:   "BTC_USDT",
			Leverage: 10,
			OpenType: exchange.OpenTypeCross,
		})
		require.NoError(t, err)
	})

	t.Run("change leverage isolated mode", func(t *testing.T) {
		t.Parallel()
		err := client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{
			Symbol:   "BTC_USDT",
			Leverage: 15,
			OpenType: exchange.OpenTypeIsolated,
		})
		require.NoError(t, err)
	})
}

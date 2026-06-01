package bybit_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_CreateOrder_Mapping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		req        exchange.SubmitOrderRequest
		wantSide   string
		wantIdx    float64
		wantPrice  string
		wantTif    string
		wantType   string
		wantReduce bool
	}{
		{
			name: "Open Long Limit (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          2.5,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeLimit,
				Price:        50000.0,
				PositionMode: 1, // Hedge
			},
			wantSide:  "Buy",
			wantIdx:   1,
			wantPrice: "50000",
			wantTif:   "GTC",
			wantType:  "Limit",
		},
		{
			name: "Open Short Market (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          1.5,
				Side:         exchange.SideOpenShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSide: "Sell",
			wantIdx:  2,
			wantTif:  "IOC",
			wantType: "Market",
		},
		{
			name: "Close Long (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          2.0,
				Side:         exchange.SideCloseLong,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSide:   "Sell",
			wantIdx:    1,
			wantTif:    "IOC",
			wantType:   "Market",
			wantReduce: true,
		},
		{
			name: "Open Short (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          2.0,
				Side:         exchange.SideCloseShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1, // Hedge
			},
			wantSide:   "Buy",
			wantIdx:    2,
			wantTif:    "IOC",
			wantType:   "Market",
			wantReduce: true,
		},
		{
			name: "Open Long with Leverage (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          2.5,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeLimit,
				Price:        50000.0,
				PositionMode: 1, // Hedge
				Leverage:     5,
			},
			wantSide:  "Buy",
			wantIdx:   1,
			wantPrice: "50000",
			wantTif:   "GTC",
			wantType:  "Limit",
		},
		{
			name: "Open Long Market (OneWay)",

			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          3.0,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 2, // OneWay
				ReduceOnly:   true,
			},
			wantSide:   "Buy",
			wantIdx:    0,
			wantTif:    "IOC",
			wantType:   "Market",
			wantReduce: true,
		},
		{
			name: "PostOnly Order Type",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          1.0,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypePostOnly,
				Price:        49000.0,
				PositionMode: 2,
			},
			wantSide:  "Buy",
			wantIdx:   0,
			wantTif:   "PostOnly",
			wantType:  "Limit",
			wantPrice: "49000",
		},
		{
			name: "FOK Order Type",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          1.0,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeFOK,
				Price:        49000.0,
				PositionMode: 2,
			},
			wantSide:  "Buy",
			wantIdx:   0,
			wantTif:   "FOK",
			wantType:  "Limit",
			wantPrice: "49000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Contains(t, r.URL.Path, "/v5/order/create")

				var body map[string]any
				err := json.NewDecoder(r.Body).Decode(&body)
				require.NoError(t, err)

				assert.Equal(t, tt.req.Symbol, body["symbol"])
				assert.Equal(t, "linear", body["category"])
				assert.Equal(t, tt.wantSide, body["side"])
				assert.Equal(t, tt.wantIdx, body["positionIdx"])
				assert.Equal(t, tt.wantType, body["orderType"])
				assert.Equal(t, tt.wantTif, body["timeInForce"])

				if tt.wantPrice != "" {
					assert.Equal(t, tt.wantPrice, body["price"])
				}

				if tt.wantReduce {
					assert.Equal(t, true, body["reduceOnly"])
				}

				if tt.req.Leverage > 0 {
					assert.Equal(t, fmt.Sprintf("%d", tt.req.Leverage), body["leverage"])
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"retCode": 0,
					"retMsg": "OK",
					"result": {
						"orderId": "bybit-ord-987654",
						"orderLinkId": "link-123"
					}
				}`))
			}))
			defer server.Close()

			client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

			res, err := client.CreateOrder(context.Background(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, "bybit-ord-987654", res.OrderID)
		})
	}
}

func TestClient_GetAssets(t *testing.T) {
	t.Parallel()

	t.Run("Standard Account", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/v5/account/wallet-balance")
			assert.Equal(t, "CONTRACT", r.URL.Query().Get("accountType"))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"retCode": 0,
				"retMsg": "OK",
				"result": {
					"list": [{
						"coin": [{
							"coin": "USDT",
							"equity": "10000",
							"unrealisedPnl": "100",
							"walletBalance": "9900"
						}]
					}]
				}
			}`))
		}))
		defer server.Close()

		client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})
		assets, err := client.GetAssets(context.Background())
		require.NoError(t, err)
		require.Len(t, assets, 1)
		assert.Equal(t, "USDT", assets[0].Currency)
		assert.Equal(t, 10000.0, assets[0].Equity)
		assert.Equal(t, 100.0, assets[0].Unrealized)
		assert.Equal(t, 10000.0, assets[0].AvailableBalance) // walletBalance + unrealized = 9900 + 100 = 10000

		// Test GetAssetByCurrency helper
		asset, err := client.GetAssetByCurrency(context.Background(), "USDT")
		require.NoError(t, err)
		assert.Equal(t, "USDT", asset.Currency)
		assert.Equal(t, 10000.0, asset.Equity)

		// Test non-existent currency defaults to empty zero asset info
		asset2, err := client.GetAssetByCurrency(context.Background(), "BTC")
		require.NoError(t, err)
		assert.Equal(t, "BTC", asset2.Currency)
		assert.Equal(t, 0.0, asset2.Equity)
	})

	t.Run("Unified Account", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/v5/account/wallet-balance")
			assert.Equal(t, "UNIFIED", r.URL.Query().Get("accountType"))

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"retCode": 0,
				"retMsg": "OK",
				"result": {
					"list": []
				}
			}`))
		}))
		defer server.Close()

		client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "unified", config.LoggingConfig{})
		assets, err := client.GetAssets(context.Background())
		require.NoError(t, err)
		// Empty balance list returns default USDT asset info
		require.Len(t, assets, 1)
		assert.Equal(t, "USDT", assets[0].Currency)
		assert.Equal(t, 0.0, assets[0].Equity)
	})
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	t.Run("Successful Cancel", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/v5/order/cancel")

			var body map[string]any
			err := json.NewDecoder(r.Body).Decode(&body)
			require.NoError(t, err)

			assert.Equal(t, "linear", body["category"])
			assert.Equal(t, "BTCUSDT", body["symbol"])
			assert.Equal(t, "bybit-ord-987654", body["orderId"])

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"retCode": 0, "retMsg": "OK"}`))
		}))
		defer server.Close()

		client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})
		err := client.CancelOrder(context.Background(), "BTCUSDT", "bybit-ord-987654")
		require.NoError(t, err)
	})

	t.Run("Ignore Already Canceled/Filled Errors", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"retCode": 110001,
				"retMsg": "order already cancelled or filled"
			}`))
		}))
		defer server.Close()

		client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})
		err := client.CancelOrder(context.Background(), "BTCUSDT", "bybit-ord-987654")
		require.NoError(t, err) // returns nil
	})

	t.Run("CancelOrders Helper and CancelAllOpenOrders", func(t *testing.T) {
		t.Parallel()
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch r.URL.Path {
			case "/v5/order/cancel":
				calls++
				_, _ = w.Write([]byte(`{"retCode": 0, "retMsg": "OK"}`))
			case "/v5/order/cancel-all":
				_, _ = w.Write([]byte(`{"retCode": 0, "retMsg": "OK"}`))
			}
		}))
		defer server.Close()

		client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})
		err := client.CancelOrders(context.Background(), []string{"id1", "id2"})
		require.NoError(t, err)
		assert.Equal(t, 2, calls)

		err = client.CancelAllOpenOrders(context.Background(), "BTCUSDT")
		require.NoError(t, err)
	})
}

func TestClient_GetOrder_And_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v5/order/realtime")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"category": "linear",
				"list": [{
					"symbol": "BTCUSDT",
					"orderId": "bybit-ord-987654",
					"orderLinkId": "link-123",
					"price": "50000",
					"qty": "2.5",
					"side": "Buy",
					"orderStatus": "Filled",
					"orderType": "Limit",
					"cumExecQty": "2.5",
					"avgPrice": "50000",
					"createdTime": "1672531200000",
					"updatedTime": "1672531205000",
					"positionIdx": 1
				}]
			}
		}`))
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	// GetOrder
	info, err := client.GetOrder(context.Background(), "BTCUSDT", "bybit-ord-987654")
	require.NoError(t, err)
	assert.Equal(t, "bybit-ord-987654", info.OrderID)
	assert.Equal(t, "BTCUSDT", info.Symbol)
	assert.Equal(t, 50000.0, info.Price)
	assert.Equal(t, 2.5, info.Vol)
	assert.Equal(t, 2.5, info.DealVol)
	assert.Equal(t, 50000.0, info.DealAvgPrice)
	assert.Equal(t, exchange.OrderStateFilled, info.State)
	assert.Equal(t, exchange.SideOpenLong, info.Side)
	assert.Equal(t, 1, info.PositionMode) // Hedge mode

	// GetOpenOrders
	infos, err := client.GetOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, "bybit-ord-987654", infos[0].OrderID)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v5/position/list")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"category": "linear",
				"list": [{
					"symbol": "BTCUSDT",
					"size": "1.5",
					"entryPrice": "50000",
					"liqPrice": "45000",
					"unrealisedPnl": "200",
					"leverage": "10",
					"positionIdx": 1,
					"side": "Buy"
				}]
			}
		}`))
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	p := positions[0]
	assert.Equal(t, "BTCUSDT", p.Symbol)
	assert.Equal(t, 1.5, p.HoldVol)
	assert.Equal(t, 50000.0, p.HoldAvgPrice)
	assert.Equal(t, 1, p.PositionType) // Long
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v5/market/instruments-info")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"category": "linear",
				"list": [
					{
						"symbol": "BTCUSDT",
						"status": "Trading",
						"baseCoin": "BTC",
						"quoteCoin": "USDT",
						"settleCoin": "USDT",
						"lotSizeFilter": {
							"maxOrderQty": "100",
							"minOrderQty": "0.001",
							"qtyStep": "0.001"
						},
						"priceFilter": {
							"tickSize": "0.1"
						},
						"leverageFilter": {
							"minLeverage": "1",
							"maxLeverage": "100"
						}
					},
					{
						"symbol": "ETHUSDT",
						"status": "Closed",
						"baseCoin": "ETH",
						"quoteCoin": "USDT",
						"settleCoin": "USDT",
						"lotSizeFilter": {
							"maxOrderQty": "1000",
							"minOrderQty": "0.01",
							"qtyStep": "0.01"
						},
						"priceFilter": {
							"tickSize": "0.01"
						},
						"leverageFilter": {
							"minLeverage": "1",
							"maxLeverage": "50"
						}
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	// Status != "Trading" (ETHUSDT) should be filtered out
	require.Len(t, details, 1)
	d := details[0]
	assert.Equal(t, "BTCUSDT", d.Symbol)
	assert.Equal(t, "BTC", d.BaseCoin)
	assert.Equal(t, "USDT", d.QuoteCoin)
	assert.Equal(t, "USDT", d.SettleCoin)
	assert.Equal(t, 1, d.MinLeverage)
	assert.Equal(t, 100, d.MaxLeverage)
	assert.Equal(t, 0.1, d.PriceUnit)
	assert.Equal(t, 1, d.MinVol)
	assert.Equal(t, 1, d.VolUnit)
	assert.Equal(t, 1, d.PriceScale)
	assert.Equal(t, 3, d.VolScale)
}

func TestClient_GetTickers_And_GetFundingRate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v5/market/tickers")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"category": "linear",
				"list": [{
					"symbol": "BTCUSDT",
					"lastPrice": "50000",
					"bid1Price": "49999",
					"ask1Price": "50001",
					"volume24h": "1000",
					"turnover24h": "50000000",
					"indexPrice": "50000",
					"markPrice": "50000",
					"fundingRate": "0.0001",
					"nextFundingTime": "1672531200000"
				}]
			}
		}`))
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	tickers, err := client.GetTickers(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	t0 := tickers[0]
	assert.Equal(t, "BTCUSDT", t0.Symbol)
	assert.Equal(t, 50000.0, t0.LastPrice)
	assert.Equal(t, 49999.0, t0.Bid1)
	assert.Equal(t, 50001.0, t0.Ask1)

	rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
	require.NoError(t, err)
	require.Len(t, rates, 1)
	assert.Equal(t, "BTCUSDT", rates[0].Symbol)
	assert.Equal(t, 0.0001, rates[0].Rate)
	assert.Equal(t, int64(1672531200000), rates[0].SettleTime)
}

func TestClient_GetKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v5/market/kline")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"category": "linear",
				"symbol": "BTCUSDT",
				"list": [
					["1672531260000", "50050", "50150", "50000", "50100", "15", "750000"],
					["1672531200000", "50000", "50100", "49900", "50050", "10", "500000"]
				]
			}
		}`))
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	klines, err := client.GetKlines(context.Background(), "BTCUSDT", "Min1", 1672531100000, 1672531300000)
	require.NoError(t, err)
	// Klines must be reversed: oldest first
	require.Len(t, klines, 2)
	assert.Equal(t, int64(1672531200000), klines[0].Timestamp)
	assert.Equal(t, 50000.0, klines[0].Open)
	assert.Equal(t, int64(1672531260000), klines[1].Timestamp)
	assert.Equal(t, 50050.0, klines[1].Open)
}

func TestClient_GetDepthSnapshot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/v5/market/orderbook")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"s": "BTCUSDT",
				"b": [["49999", "1.5"]],
				"a": [["50001", "2.5"]],
				"ts": 1672531200000,
				"u": 12345
			}
		}`))
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	book, err := client.GetDepthSnapshot(context.Background(), "BTCUSDT", 50)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", book.Symbol)
	assert.Equal(t, int64(12345), book.Version)
	require.Len(t, book.Bids, 1)
	require.Len(t, book.Asks, 1)
	assert.Equal(t, 49999.0, book.Bids[0].Price)
	assert.Equal(t, 1.5, book.Bids[0].Volume)
	assert.Equal(t, 50001.0, book.Asks[0].Price)
	assert.Equal(t, 2.5, book.Asks[0].Volume)

	commits, err := client.GetDepthCommits(context.Background(), "BTCUSDT", 50)
	require.NoError(t, err)
	assert.Nil(t, commits)
}

func TestClient_ClosePosition_And_ChangeLeverage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v5/order/create":
			_, _ = w.Write([]byte(`{"retCode": 0, "result": {"orderId": "close-1"}}`))
		case "/v5/position/list":
			_, _ = w.Write([]byte(`{
				"retCode": 0,
				"result": {
					"list": [
						{"symbol": "BTCUSDT", "size": "1.0", "positionIdx": 1}
					]
				}
			}`))
		case "/v5/position/set-leverage":
			// Leverage set error we ignore
			_, _ = w.Write([]byte(`{"retCode": 110043, "retMsg": "leverage already set"}`))
		}
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	// ClosePosition
	err := client.ClosePosition(context.Background(), "BTCUSDT", domain.SideCloseLong, 1.0, 1)
	require.NoError(t, err)

	// CloseAllPositions
	err = client.CloseAllPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)

	// ChangeLeverage
	err = client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTCUSDT",
		Leverage: 10,
	})
	require.NoError(t, err) // code 110043 ignored
}

func TestWsAdapter_HooksAndParsing(t *testing.T) {
	t.Parallel()

	adapter := bybit.NewWsAdapter()
	require.NotNil(t, adapter)

	// Check extract ping config
	ping, interval := adapter.GetPingConfig()
	assert.Equal(t, 20*time.Second, interval)
	pingMap, ok := ping.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ping", pingMap["op"])

	// Check parse kline topic extraction
	rawKline := []byte(`{
		"topic": "kline.1.BTCUSDT",
		"data": [{
			"start": 1672531200000,
			"open": "50000",
			"close": "50050",
			"high": "50100",
			"low": "49950",
			"volume": "10.5"
		}]
	}`)
	sym, k, err := adapter.ParseKline(rawKline)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, int64(1672531200000), k.Timestamp)
	assert.Equal(t, 50000.0, k.Open)
	assert.Equal(t, 50050.0, k.Close)

	// Check parse depth
	rawDepth := []byte(`{
		"topic": "orderbook.50.BTCUSDT",
		"data": {
			"s": "BTCUSDT",
			"b": [["49999", "1.5"]],
			"a": [["50001", "2.5"]],
			"ts": 1672531200000,
			"u": 12345
		}
	}`)
	sym, ob, err := adapter.ParseDepth(rawDepth)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	require.Len(t, ob.Bids, 1)
	assert.Equal(t, 49999.0, ob.Bids[0].Price)

	// Check parse ticker
	rawTicker := []byte(`{
		"topic": "tickers.BTCUSDT",
		"data": {
			"symbol": "BTCUSDT",
			"lastPrice": "50000",
			"bid1Price": "49999",
			"ask1Price": "50001",
			"volume24h": "100"
		}
	}`)
	sym, pd, err := adapter.ParseTicker(rawTicker)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 50000.0, pd.LastPrice)
	assert.Equal(t, 49999.0, pd.BestBid)

	// Check parse order
	rawOrder := []byte(`{
		"topic": "order",
		"data": [{
			"symbol": "BTCUSDT",
			"orderId": "bybit-ord-987654",
			"orderStatus": "Filled",
			"side": "Buy"
		}]
	}`)
	deal, err := adapter.ParseOrder(rawOrder)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", deal.Symbol)
	assert.Equal(t, exchange.OrderStateFilled, deal.State)

	// Check parse position
	rawPos := []byte(`{
		"topic": "position",
		"data": [{
			"symbol": "BTCUSDT",
			"size": "2.0",
			"positionIdx": 1
		}]
	}`)
	pos, err := adapter.ParsePosition(rawPos)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", pos.Symbol)
	assert.Equal(t, 2.0, pos.HoldVol)
	assert.Equal(t, 1, pos.PositionType)

	// Check extractor routing
	extractor := adapter.GetChannelExtractor()
	assert.Equal(t, "ticker", extractor([]byte(`{"topic": "tickers.BTCUSDT"}`)))
	assert.Equal(t, "depth", extractor([]byte(`{"topic": "orderbook.50.BTCUSDT"}`)))
	assert.Equal(t, "kline", extractor([]byte(`{"topic": "kline.1.BTCUSDT"}`)))
	assert.Equal(t, "personal.order", extractor([]byte(`{"topic": "order"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"topic": "position"}`)))

	// Check auth hook creation
	hook := adapter.GetAuthHook("key", "secret")
	assert.NotNil(t, hook)
	// Execute the auth hook on a dummy Client to cover the code block
	dummyClient := pkgws.NewClient("ws://127.0.0.1", slog.Default())
	hook(dummyClient)

	// Stub checks for cover
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pool := pkgws.NewPool("ws://127.0.0.1:1", 1, logger)
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.SubscribeKline(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeKline(ctx, "BTCUSDT")
	_ = adapter.SubscribeDepth(ctx, "BTCUSDT", "1")
	_ = adapter.UnsubscribeDepth(ctx, "BTCUSDT", "1")
	_ = adapter.SubscribePersonal(ctx)
	d, _ := adapter.ParseOrderDeal(nil)
	assert.Nil(t, d)
	tr, _ := adapter.ParseTrackOrder(nil)
	assert.Nil(t, tr)

	// Additional parsing error cases
	_, _, err = adapter.ParseTicker([]byte("{"))
	assert.Error(t, err)
	_, _, err = adapter.ParseTicker([]byte(`{"topic":"tickers","data":[]}`))
	assert.Error(t, err)

	_, _, err = adapter.ParseDepth([]byte("{"))
	assert.Error(t, err)
	_, ob, err = adapter.ParseDepth([]byte(`{"topic":"orderbook","data":{"s":"BTCUSDT","b":[["49000"]],"a":[["50000"]]}}`)) // less than 2 items in entries
	require.NoError(t, err)
	assert.Len(t, ob.Bids, 0)
	assert.Len(t, ob.Asks, 0)

	_, _, err = adapter.ParseKline([]byte("{"))
	assert.Error(t, err)
	_, _, err = adapter.ParseKline([]byte(`{"topic":"kline","data":[]}`))
	assert.Error(t, err)

	_, err = adapter.ParseOrder([]byte("{"))
	assert.Error(t, err)
	_, err = adapter.ParseOrder([]byte(`{"topic":"order","data":[]}`))
	assert.Error(t, err)

	_, err = adapter.ParsePosition([]byte("{"))
	assert.Error(t, err)
	_, err = adapter.ParsePosition([]byte(`{"topic":"position","data":[]}`))
	assert.Error(t, err)
}

func TestClient_GetServerTime_WarmUp_OtherCases(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v5/market/time":
			_, _ = w.Write([]byte(`{
				"retCode": 0,
				"retMsg": "OK",
				"time": 1672531200000
			}`))
		case "/v5/market/kline":
			_, _ = w.Write([]byte(`{
				"retCode": 0,
				"retMsg": "OK",
				"result": {
					"category": "linear",
					"symbol": "BTCUSDT",
					"list": []
				}
			}`))
		case "/v5/position/list":
			_, _ = w.Write([]byte(`{
				"retCode": 0,
				"retMsg": "OK",
				"result": {
					"category": "linear",
					"list": [
						{"symbol": "BTCUSDT", "size": "1.0", "positionIdx": 2, "side": "Sell"},
						{"symbol": "BTCUSDT", "size": "1.0", "positionIdx": 0, "side": "Buy"},
						{"symbol": "BTCUSDT", "size": "1.0", "positionIdx": 0, "side": "Sell"}
					]
				}
			}`))
		}
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})

	// Test GetServerTime
	serverTime, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1672531200000), serverTime)

	// Test WarmUp with cancelled context to exit immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client.WarmUp(ctx, 10*time.Millisecond)

	// Test CreateTrackOrder returns error
	_, err = client.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{})
	assert.Error(t, err)

	// Test mapInterval fallbacks in GetKlines
	intervals := []string{"Min5", "Min15", "Min30", "Hour1", "Hour4", "Day1", "Unknown"}
	for _, iv := range intervals {
		_, _ = client.GetKlines(context.Background(), "BTCUSDT", iv, 0, 0)
	}

	// Test other positionIdx / Side mappings in GetOpenPositions
	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 3)
	assert.Equal(t, 2, positions[0].PositionType) // Short (Idx=2)
	assert.Equal(t, 1, positions[1].PositionType) // Long (Idx=0, buy)
	assert.Equal(t, 2, positions[2].PositionType) // Short (Idx=0, sell)
}

func TestClient_ErrorAndEdgeCases(t *testing.T) {
	t.Parallel()

	// 1. Test error responses from server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Always return an error retCode = 50001
		_, _ = w.Write([]byte(`{
			"retCode": 50001,
			"retMsg": "Internal Mock Error",
			"result": {}
		}`))
	}))
	defer server.Close()

	client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})
	ctx := context.Background()

	// Test GetServerTime error
	_, err := client.GetServerTime(ctx)
	assert.Error(t, err)

	// Test GetContractDetails error
	_, err = client.GetContractDetails(ctx)
	assert.Error(t, err)

	// Test GetTickers error
	_, err = client.GetTickers(ctx, "BTCUSDT")
	assert.Error(t, err)

	// Test GetFundingRates error
	_, err = client.GetFundingRates(ctx, []string{"BTCUSDT"})
	assert.Error(t, err)

	// Test GetKlines symbol empty
	_, err = client.GetKlines(ctx, "", "Min1", 0, 0)
	assert.Error(t, err)

	// Test GetKlines server error
	_, err = client.GetKlines(ctx, "BTCUSDT", "Min1", 0, 0)
	assert.Error(t, err)

	// Test GetDepthSnapshot symbol empty
	_, err = client.GetDepthSnapshot(ctx, "", 50)
	assert.Error(t, err)

	// Test GetDepthSnapshot server error
	_, err = client.GetDepthSnapshot(ctx, "BTCUSDT", 50)
	assert.Error(t, err)

	// Test CreateOrder server error
	_, err = client.CreateOrder(ctx, exchange.SubmitOrderRequest{Symbol: "BTCUSDT"})
	assert.Error(t, err)

	// Test CancelOrder server error
	err = client.CancelOrder(ctx, "BTCUSDT", "id")
	assert.Error(t, err)

	// Test CancelAllOpenOrders server error
	err = client.CancelAllOpenOrders(ctx, "BTCUSDT")
	assert.Error(t, err)

	// Test GetOrder server error
	_, err = client.GetOrder(ctx, "BTCUSDT", "id")
	assert.Error(t, err)

	// Test GetOpenOrders server error
	_, err = client.GetOpenOrders(ctx, "BTCUSDT")
	assert.Error(t, err)

	// Test ChangeLeverage server error
	err = client.ChangeLeverage(ctx, exchange.ChangeLeverageRequest{Symbol: "BTCUSDT", Leverage: 10})
	assert.Error(t, err)

	// 2. Test GetAuthHook with empty credentials
	adapter := bybit.NewWsAdapter()
	hook := adapter.GetAuthHook("", "")
	assert.Nil(t, hook)

	// 3. Test mapOrderInfo other status states
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {
				"category": "linear",
				"list": [{
					"symbol": "BTCUSDT",
					"orderId": "bybit-ord-987654",
					"orderStatus": "rejected",
					"side": "Unknown"
				}]
			}
		}`))
	}))
	defer server2.Close()

	client2 := bybit.NewClient(server2.Client(), server2.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})
	info, err := client2.GetOrder(ctx, "BTCUSDT", "bybit-ord-987654")
	require.NoError(t, err)
	assert.Equal(t, exchange.OrderStateCanceled, info.State)
	assert.Equal(t, 0, info.Side) // Unknown side is mapped to 0
}

func TestClient_GetRecentClosedPnL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		response   string
		wantErr    string
		wantSymbol string
	}{
		{
			name: "Successful query",
			response: `{
				"retCode": 0,
				"retMsg": "OK",
				"result": {
					"list": [{
						"symbol": "BTCUSDT",
						"side": "Buy",
						"qty": "1.0",
						"orderPrice": "50000",
						"orderType": "Limit",
						"closedSize": "1.0",
						"avgEntryPrice": "50000",
						"avgExitPrice": "51000",
						"closedPnl": "1000",
						"openFee": "10",
						"closeFee": "11",
						"createdTime": "{{CREATED_TIME}}",
						"updatedTime": "{{UPDATED_TIME}}"
					}]
				}
			}`,
			wantSymbol: "BTCUSDT",
		},
		{
			name: "Empty response",
			response: `{
				"retCode": 0,
				"retMsg": "OK",
				"result": {
					"list": []
				}
			}`,
			wantErr: "no closed pnl records found",
		},
		{
			name: "API Error in Order Lookup",
			response: `{
				"retCode": 50002,
				"retMsg": "Mock error"
			}`,
			wantErr: "bybit get order by external ID error: retCode=50002",
		},
		{
			name: "API Error in Closed PnL",
			response: `{
				"retCode": 50002,
				"retMsg": "Mock error"
			}`,
			wantErr: "bybit get closed pnl error: retCode=50002",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "/v5/order/realtime") {
					if tt.name == "API Error in Order Lookup" {
						_, _ = w.Write([]byte(`{
							"retCode": 50002,
							"retMsg": "Mock error"
						}`))
						return
					}
					_, _ = w.Write([]byte(`{
						"retCode": 0,
						"retMsg": "OK",
						"result": {
							"list": [{
								"orderId": "bybit-ord-987654",
								"orderLinkId": "ext-123"
							}]
						}
					}`))
					return
				}
				if strings.Contains(r.URL.Path, "transaction-log") {
					_, _ = w.Write([]byte(`{
						"retCode": 0,
						"retMsg": "OK",
						"result": {
							"list": [{
								"symbol": "BTCUSDT",
								"type": "FUNDING",
								"change": "-0.05"
							}]
						}
					}`))
					return
				}
				now := time.Now().UnixMilli()
				resStr := strings.ReplaceAll(tt.response, "{{UPDATED_TIME}}", fmt.Sprintf("%d", now))
				resStr = strings.ReplaceAll(resStr, "{{CREATED_TIME}}", fmt.Sprintf("%d", now-5000))
				_, _ = w.Write([]byte(resStr))
			}))
			defer server.Close()

			client := bybit.NewClient(server.Client(), server.URL, "api_key", "api_secret", "standard", config.LoggingConfig{})
			info, err := client.GetRecentClosedPnL(context.Background(), "BTCUSDT", "ext-123", time.Time{})

			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			} else {
				require.NoError(t, err)
				require.NotNil(t, info)
				assert.Equal(t, tt.wantSymbol, info.Symbol)
				assert.Equal(t, 50000.0, info.EntryPrice)
				assert.Equal(t, 51000.0, info.ExitPrice)
				assert.Equal(t, 1.0, info.ClosedSize)
				assert.Equal(t, 1021.05, info.GrossPnL)
				assert.Equal(t, 21.0, info.Fee)
				assert.Equal(t, -0.05, info.FundingFee)
				assert.Equal(t, int64(5000), info.DurationMs)
				assert.Equal(t, 1000.0, info.NetPnl)
			}
		})
	}
}

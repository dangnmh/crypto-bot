package binance_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/binance"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/ping":
			_, _ = w.Write([]byte(`{}`))
		case "/fapi/v1/time":
			_, _ = w.Write([]byte(`{"serverTime": 1672531200000}`))
		}
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	timeVal, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1672531200000), timeVal)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v1/exchangeInfo")

		_, _ = w.Write([]byte(`{
			"symbols": [
				{
					"symbol": "BTCUSDT",
					"status": "TRADING",
					"contractType": "PERPETUAL",
					"baseAsset": "BTC",
					"quoteAsset": "USDT",
					"marginAsset": "USDT",
					"pricePrecision": 2,
					"quantityPrecision": 3,
					"filters": [
						{
							"filterType": "PRICE_FILTER",
							"tickSize": "0.1"
						},
						{
							"filterType": "LOT_SIZE",
							"minQty": "0.001",
							"stepSize": "0.001"
						}
					]
				},
				{
					"symbol": "ETHUSDT",
					"status": "CLOSED",
					"contractType": "PERPETUAL"
				}
			]
		}`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)

	d := details[0]
	assert.Equal(t, "BTCUSDT", d.Symbol)
	assert.Equal(t, "BTC", d.BaseCoin)
	assert.Equal(t, "USDT", d.QuoteCoin)
	assert.Equal(t, "USDT", d.SettleCoin)
	assert.Equal(t, 0.1, d.PriceUnit)
	assert.Equal(t, 2, d.PriceScale)
	assert.Equal(t, 3, d.VolScale)
}

func TestClient_GetTickers_And_FundingRate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/fapi/v1/ticker/24hr":
			_, _ = w.Write([]byte(`[{
				"symbol": "BTCUSDT",
				"lastPrice": "50000.0",
				"volume": "1000.0",
				"quoteVolume": "50000000.0"
			}]`))
		case "/fapi/v1/ticker/bookTicker":
			_, _ = w.Write([]byte(`[{
				"symbol": "BTCUSDT",
				"bidPrice": "49999.0",
				"askPrice": "50001.0"
			}]`))
		case "/fapi/v1/premiumIndex":
			_, _ = w.Write([]byte(`[{
				"symbol": "BTCUSDT",
				"markPrice": "50000.0",
				"indexPrice": "49998.0",
				"lastFundingRate": "0.0001",
				"nextFundingTime": 1672531200000
			}]`))
		}
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	tickers, err := client.GetTickers(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, tickers, 1)

	t0 := tickers[0]
	assert.Equal(t, "BTCUSDT", t0.Symbol)
	assert.Equal(t, 50000.0, t0.LastPrice)
	assert.Equal(t, 49999.0, t0.Bid1)
	assert.Equal(t, 50001.0, t0.Ask1)
	assert.Equal(t, 0.0001, t0.FundingRate)
	assert.Equal(t, int64(1672531200000), t0.NextSettleTime)

	rate, err := client.GetFundingRate(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", rate.Symbol)
	assert.Equal(t, 0.0001, rate.FundingRate)
	assert.Equal(t, int64(1672531200000), rate.NextSettleTime)
}

func TestClient_GetKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v1/klines")

		_, _ = w.Write([]byte(`[
			[1672531200000, "50000", "50100", "49900", "50050", "10", 1672531259999, "500000", 100, "5", "250000", "0"]
		]`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	klines, err := client.GetKlines(context.Background(), "BTCUSDT", "Min1", 0, 0)
	require.NoError(t, err)
	require.Len(t, klines, 1)

	k := klines[0]
	assert.Equal(t, int64(1672531200000), k.Timestamp)
	assert.Equal(t, 50000.0, k.Open)
	assert.Equal(t, 50050.0, k.Close)
	assert.Equal(t, 50100.0, k.High)
	assert.Equal(t, 49900.0, k.Low)
	assert.Equal(t, 10.0, k.Volume)
	assert.Equal(t, 500000.0, k.Amount)
}

func TestClient_GetDepthSnapshot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v1/depth")

		_, _ = w.Write([]byte(`{
			"lastUpdateId": 987654321,
			"bids": [["49999", "1.5"]],
			"asks": [["50001", "2.5"]]
		}`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	book, err := client.GetDepthSnapshot(context.Background(), "BTCUSDT", 20)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", book.Symbol)
	assert.Equal(t, int64(987654321), book.Version)
	require.Len(t, book.Bids, 1)
	require.Len(t, book.Asks, 1)
	assert.Equal(t, 49999.0, book.Bids[0].Price)
	assert.Equal(t, 1.5, book.Bids[0].Volume)
	assert.Equal(t, 50001.0, book.Asks[0].Price)
	assert.Equal(t, 2.5, book.Asks[0].Volume)

	commits, err := client.GetDepthCommits(context.Background(), "BTCUSDT", 20)
	require.NoError(t, err)
	assert.Nil(t, commits)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		req         exchange.SubmitOrderRequest
		wantSide    string
		wantType    string
		wantTif     string
		wantPosSide string
		wantPrice   float32
		wantReduce  string
	}{
		{
			name: "Open Long Limit (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          0.5,
				Side:         exchange.SideOpenLong,
				Type:         exchange.OrderTypeLimit,
				Price:        50000.0,
				PositionMode: 1, // Hedge
			},
			wantSide:    "BUY",
			wantType:    "LIMIT",
			wantTif:     "GTC",
			wantPosSide: "LONG",
			wantPrice:   50000.0,
		},
		{
			name: "Close Short (Hedge)",
			req: exchange.SubmitOrderRequest{
				Symbol:       "BTCUSDT",
				Vol:          0.5,
				Side:         exchange.SideCloseShort,
				Type:         exchange.OrderTypeMarket,
				PositionMode: 1,
			},
			wantSide:    "BUY",
			wantType:    "MARKET",
			wantPosSide: "SHORT",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "POST", r.Method)
				assert.Contains(t, r.URL.Path, "/fapi/v1/order")

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{
					"orderId": 1234567,
					"symbol": "BTCUSDT",
					"status": "NEW",
					"clientOrderId": "external_123"
				}`))
			}))
			defer server.Close()

			client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

			orderID, err := client.CreateOrder(context.Background(), tt.req)
			require.NoError(t, err)
			assert.Equal(t, "1234567", orderID)
		})
	}
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Contains(t, r.URL.Path, "/fapi/v1/order")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"orderId": 1234567,
			"status": "CANCELED"
		}`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	err := client.CancelOrder(context.Background(), "BTCUSDT", "1234567")
	require.NoError(t, err)
}

func TestClient_GetAssets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v2/balance")

		_, _ = w.Write([]byte(`[
			{
				"asset": "USDT",
				"balance": "1000.0",
				"crossUnPnl": "50.0",
				"availableBalance": "950.0"
			}
		]`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	assets, err := client.GetAssets(context.Background())
	require.NoError(t, err)
	require.Len(t, assets, 1)

	assert.Equal(t, "USDT", assets[0].Currency)
	assert.Equal(t, 1000.0, assets[0].CashBalance)
	assert.Equal(t, 1050.0, assets[0].Equity)
	assert.Equal(t, 50.0, assets[0].Unrealized)
	assert.Equal(t, 950.0, assets[0].AvailableBalance)

	asset, err := client.GetAssetByCurrency(context.Background(), "USDT")
	require.NoError(t, err)
	assert.Equal(t, "USDT", asset.Currency)
	assert.Equal(t, 1000.0, asset.CashBalance)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		assert.Contains(t, r.URL.Path, "/fapi/v2/positionRisk")

		_, _ = w.Write([]byte(`[
			{
				"symbol": "BTCUSDT",
				"positionAmt": "0.5",
				"entryPrice": "50000.0",
				"liquidationPrice": "45000.0",
				"unRealizedProfit": "100.0",
				"leverage": "10",
				"positionSide": "LONG",
				"marginType": "cross"
			}
		]`))
	}))
	defer server.Close()

	client := binance.NewClient(server.Client(), server.URL, "api_key", "api_secret", config.LoggingConfig{})

	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)

	p := positions[0]
	assert.Equal(t, "BTCUSDT", p.Symbol)
	assert.Equal(t, 0.5, p.HoldVol)
	assert.Equal(t, 50000.0, p.HoldAvgPrice)
	assert.Equal(t, 45000.0, p.LiquidatePrice)
	assert.Equal(t, 100.0, p.Realised)
	assert.Equal(t, 10, p.Leverage)
	assert.Equal(t, 1, p.PositionType) // Long
}

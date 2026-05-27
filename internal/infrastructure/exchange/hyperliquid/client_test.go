package hyperliquid_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/hyperliquid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	client := hyperliquid.NewClient(context.Background(), http.DefaultClient, "https://api.hyperliquid.xyz", "", "", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Greater(t, ts, int64(0))
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/info", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"universe": [
				{
					"name": "BTC",
					"szDecimals": 4,
					"maxLeverage": 50,
					"marginTableId": 0,
					"onlyIsolated": false,
					"isDelisted": false
				}
			],
			"marginTables": []
		}`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)
	assert.Equal(t, "BTC", details[0].Symbol)
	assert.Equal(t, 50, details[0].MaxLeverage)
	assert.Equal(t, 4, details[0].VolScale)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/info", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"universe": [
					{
						"name": "BTC",
						"szDecimals": 4,
						"maxLeverage": 50,
						"marginTableId": 0,
						"onlyIsolated": false,
						"isDelisted": false
					}
				],
				"marginTables": []
			},
			[
				{
					"funding": "0.0001",
					"openInterest": "100.0",
					"prevDayPx": "50000.0",
					"dayNtlVlm": "1000000.0",
					"premium": "0.0",
					"oraclePx": "50000.0",
					"markPx": "50000.5",
					"midPx": "50000.2"
				}
			]
		]`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)
	assert.Equal(t, "BTC", tickers[0].Symbol)
	assert.Equal(t, 50000.2, tickers[0].LastPrice)
	assert.Equal(t, 1000000.0, tickers[0].Volume24)
	assert.Equal(t, 0.0001, tickers[0].FundingRate)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetFundingRate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"universe": [
					{"name": "BTC", "szDecimals": 4, "maxLeverage": 50, "isDelisted": false}
				],
				"marginTables": []
			},
			[
				{"funding": "0.000125", "dayNtlVlm": "1000.0", "markPx": "50000"}
			]
		]`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "", config.LoggingConfig{})
	fr, err := client.GetFundingRate(context.Background(), "BTC")
	require.NoError(t, err)
	assert.Equal(t, "BTC", fr.Symbol)
	assert.Equal(t, 0.000125, fr.FundingRate)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetKlines(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"t": 1672531200000,
				"T": 1672531259999,
				"i": "1m",
				"n": 10,
				"o": "50000.0",
				"h": "50100.0",
				"l": "49900.0",
				"c": "50050.0",
				"s": "BTC",
				"v": "5.0"
			}
		]`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "", config.LoggingConfig{})
	klines, err := client.GetKlines(context.Background(), "BTC", "1m", 0, 0)
	require.NoError(t, err)
	require.Len(t, klines, 1)
	assert.Equal(t, int64(1672531200000), klines[0].Timestamp)
	assert.Equal(t, 50000.0, klines[0].Open)
	assert.Equal(t, 50050.0, klines[0].Close)
	assert.Equal(t, 5.0, klines[0].Volume)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetDepthSnapshot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"coin": "BTC",
			"levels": [
				[{"px": "49999.0", "sz": "1.5", "n": 2}],
				[{"px": "50001.0", "sz": "2.5", "n": 3}]
			],
			"time": 1672531200000
		}`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "", config.LoggingConfig{})
	ob, err := client.GetDepthSnapshot(context.Background(), "BTC", 5)
	require.NoError(t, err)
	assert.Equal(t, "BTC", ob.Symbol)
	require.Len(t, ob.Bids, 1)
	require.Len(t, ob.Asks, 1)
	assert.Equal(t, 49999.0, ob.Bids[0].Price)
	assert.Equal(t, 1.5, ob.Bids[0].Volume)
	assert.Equal(t, 50001.0, ob.Asks[0].Price)
	assert.Equal(t, 2.5, ob.Asks[0].Volume)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetAssets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"marginSummary": {
				"accountValue": "1000.5",
				"totalMarginUsed": "200.0"
			},
			"withdrawable": "800.5",
			"assetPositions": []
		}`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", config.LoggingConfig{})
	assets, err := client.GetAssets(context.Background())
	require.NoError(t, err)
	require.Len(t, assets, 1)
	assert.Equal(t, "USDC", assets[0].Currency)
	assert.Equal(t, 1000.5, assets[0].Equity)
	assert.Equal(t, 800.5, assets[0].AvailableBalance)
	assert.Equal(t, 200.0, assets[0].FrozenBalance)

	asset, err := client.GetAssetByCurrency(context.Background(), "USDC")
	require.NoError(t, err)
	assert.Equal(t, 1000.5, asset.Equity)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"marginSummary": {
				"accountValue": "1000.5",
				"totalMarginUsed": "200.0"
			},
			"withdrawable": "800.5",
			"assetPositions": [
				{
					"position": {
						"coin": "BTC",
						"entryPx": "50000.0",
						"leverage": {
							"type": "isolated",
							"value": 10
						},
						"liquidationPx": "45000.0",
						"marginUsed": "100.0",
						"positionValue": "500.0",
						"szi": "0.01",
						"unrealizedPnl": "5.5"
					},
					"type": "one-way"
				}
			]
		}`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, positions, 1)
	assert.Equal(t, "BTC", positions[0].Symbol)
	assert.Equal(t, 0.01, positions[0].HoldVol)
	assert.Equal(t, 10, positions[0].Leverage)
	assert.Equal(t, 1, positions[0].PositionType)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_ClosePosition(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/info" {
			_, _ = w.Write([]byte(`{
				"universe": [
					{
						"name": "BTC",
						"szDecimals": 4,
						"maxLeverage": 50,
						"marginTableId": 0,
						"onlyIsolated": false,
						"isDelisted": false
					}
				],
				"marginTables": []
			}`))
			return
		}

		_, _ = w.Write([]byte(`{
			"status": "ok",
			"response": {
				"type": "order",
				"data": {
					"statuses": [
						{
							"resting": {
								"oid": 12345,
								"cloid": null,
								"status": "resting"
							}
						}
					]
				}
			}
		}`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "0x0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", config.LoggingConfig{})
	err := client.ClosePosition(context.Background(), "BTC", domain.SideCloseLong, 0.01, 1)
	require.NoError(t, err)
}

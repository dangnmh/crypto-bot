package hyperliquid_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
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
	assert.Equal(t, 19.999920000319998, tickers[0].Volume24)
	assert.Equal(t, 1000000.0, tickers[0].AmountUSDT24)
}

//nolint:dupl // standard mock setup contains high structural similarity
func TestClient_GetFundingRate(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			[
				"BTC",
				[
					[
						"HlPerp",
						{"fundingRate": "0.000125", "nextFundingTime": 1609459200000}
					]
				]
			]
		]`))
	}))
	defer server.Close()

	client := hyperliquid.NewClient(context.Background(), server.Client(), server.URL, "", "", config.LoggingConfig{})
	frs, err := client.GetFundingRates(context.Background(), []string{"BTC"})
	require.NoError(t, err)
	require.Len(t, frs, 1)
	assert.Equal(t, "BTC", frs[0].Symbol)
	assert.Equal(t, 0.000125, frs[0].Rate)
}

//nolint:dupl // standard mock setup contains high structural similarity
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
	assert.Equal(t, exchange.PositionTypeLong, positions[0].PositionType)
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
	err := client.ClosePosition(context.Background(), "BTC", domain.SideCloseLong, 0.01, 1, 10)
	require.NoError(t, err)
}

func TestClient_PrivateTrading_and_Orders(t *testing.T) {
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

		bodyBytes, _ := io.ReadAll(r.Body)
		bodyStr := string(bodyBytes)
		if strings.Contains(bodyStr, "cancel") {
			_, _ = w.Write([]byte(`{
				"status": "ok",
				"response": {
					"type": "cancel",
					"data": {
						"statuses": ["success"]
					}
				}
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

	// 1. CreateOrder
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:      "BTC",
		Vol:         0.01,
		Price:       50000.0,
		Side:        exchange.SideOpenLong,
		Type:        exchange.OrderTypeLimit,
		ExternalOID: "0x00000000000000000000000000000001",
	})
	require.NoError(t, err)
	assert.Equal(t, "12345", res.OrderID)

	// 2. CancelOrder
	err = client.CancelOrder(context.Background(), "BTC", "12345")
	require.NoError(t, err)

	// 3. ChangeLeverage
	err = client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTC",
		Leverage: 20,
		OpenType: exchange.OpenTypeCross,
	})
	require.NoError(t, err)
}

func TestClient_ErrorPaths(t *testing.T) {
	t.Parallel()

	// 1. Unimplemented errors
	client := hyperliquid.NewClient(context.Background(), nil, "http://127.0.0.1", "", "", config.LoggingConfig{})
	_, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{})
	assert.ErrorContains(t, err, "exchange signer is not configured")

	err = client.CancelOrder(context.Background(), "BTC", "12345")
	assert.ErrorContains(t, err, "exchange signer is not configured")

	err = client.CancelOrders(context.Background(), []string{"1"})
	assert.ErrorContains(t, err, "batch cancel not supported")

	err = client.CloseAllPositions(context.Background(), "BTC")
	require.NoError(t, err)
}

package xt_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/xt"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Signature(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "my-api-key", r.Header.Get("validate-appkey"))
		assert.NotEmpty(t, r.Header.Get("validate-timestamp"))
		assert.NotEmpty(t, r.Header.Get("validate-signature"))
		assert.Equal(t, "HmacSHA256", r.Header.Get("validate-algorithms"))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"returnCode":0,"msgInfo":"success","result":"12345678"}`))
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})

	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTCUSDT",
		Price:  60000,
		Vol:    0.1,
		Side:   domain.SideOpenLong,
		Type:   domain.OrderTypeLimit,
	})
	require.NoError(t, err)
	assert.Equal(t, "12345678", res.OrderID)
}

func TestClient_GetOrderPNL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/future/trade/v1/order/detail" {
			// Mock order detail
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": {
					"orderId": "o999",
					"clientOrderId": "ext999",
					"symbol": "btc_usdt",
					"orderSide": "SELL",
					"positionSide": "LONG",
					"orderType": "LIMIT",
					"price": "60000.0",
					"origQty": "1.5",
					"executedQty": "1.5",
					"avgPrice": "61000.0",
					"state": "FILLED",
					"createdTime": 1782570000000,
					"updateTime": 1782570005000
				}
			}`))
			return
		}

		if r.URL.Path == "/future/trade/v1/position/list-history" {
			// Mock position list history
			_, _ = w.Write([]byte(`{
				"returnCode": 0,
				"msgInfo": "success",
				"result": {
					"hasNext": false,
					"hasPrev": false,
					"items": [
						{
							"id": "1987654321098765432",
							"positionSide": "LONG",
							"contractType": "PERPETUAL",
							"symbol": "BTCUSDT",
							"positionType": 2,
							"closeProfit": "1500.0",
							"closePositionSize": "1.5",
							"closeOpenPrice": "60666.67",
							"closePrice": "61000.0",
							"maxPositionSize": "1.5",
							"openTime": 1782570000000,
							"closeTime": 1782570005000,
							"startLeverage": 5,
							"endLeverage": 5,
							"working": false,
							"force": false,
							"forceMarkPrice": null,
							"totalFee": "4.5",
							"totalFundFee": "-2.5",
							"welfareAccount": false
						}
					]
				}
			}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := xt.NewClient(server.Client(), server.URL, "my-api-key", "my-api-secret", config.LoggingConfig{})

	pnl, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "o999")
	require.NoError(t, err)

	assert.Equal(t, "xt", pnl.Exchange)
	assert.Equal(t, "BTCUSDT", pnl.Symbol)
	// Entry price weighted: ((60000 * 0.5) + (61000 * 1.0)) / 1.5 = 60666.666...
	assert.InDelta(t, 60666.67, pnl.EntryPrice, 0.01)
	assert.Equal(t, 61000.0, pnl.ExitPrice)
	assert.Equal(t, 1.5, pnl.ClosedSize)
	assert.Equal(t, 1500.0, pnl.GrossPnL)
	assert.Equal(t, 4.5, pnl.Fee)
	assert.Equal(t, -2.5, pnl.FundingFee) // sumFunding is 2.5, so fundingFee is -2.5 (credit received, increasing profit)
	// netPnL = grossPnL + fee - fundingFee = 1500 + 4.5 - (-2.5) = 1507.0
	assert.Equal(t, 1507.0, pnl.NetPnl)
}

//nolint:misspell // API compatibility requires "realised" spelling
package bitmart_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitmart"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GetTickers(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/public/details", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": {
				"symbols": [
					{
						"symbol": "BTCUSDT",
						"last_price": "60000.5",
						"volume_24h": "120",
						"turnover_24h": "7200000",
						"contract_size": "0.001",
						"funding_rate": "0.0001",
						"funding_time": 1719273600000
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	tickers, err := client.GetTickers(context.Background(), "")
	require.NoError(t, err)
	require.Len(t, tickers, 1)

	assert.Equal(t, "BTCUSDT", tickers[0].Symbol)
	assert.Equal(t, 60000.5, tickers[0].LastPrice)
	assert.Equal(t, 0.12, tickers[0].Volume24)
	assert.Equal(t, 7200000.0, tickers[0].AmountUSDT24)
}

func TestClient_GetContractDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/public/details", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": {
				"symbols": [
					{
						"symbol": "BTCUSDT",
						"base_currency": "BTC",
						"quote_currency": "USDT",
						"contract_size": "0.001",
						"min_leverage": "1",
						"max_leverage": "100",
						"price_precision": "0.1",
						"vol_precision": "1",
						"min_volume": "10",
						"status": "Trading"
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	details, err := client.GetContractDetails(context.Background())
	require.NoError(t, err)
	require.Len(t, details, 1)

	assert.Equal(t, "BTCUSDT", details[0].Symbol)
	assert.Equal(t, 0.001, details[0].ContractSize)
	assert.Equal(t, 1, details[0].MinLeverage)
	assert.Equal(t, 100, details[0].MaxLeverage)
	assert.Equal(t, 0.1, details[0].PriceUnit)
	assert.Equal(t, 10, details[0].MinVol)
	assert.Equal(t, 1, details[0].VolUnit)
	assert.Equal(t, 1, details[0].PriceScale)
	assert.Equal(t, 0, details[0].VolScale)
	assert.Equal(t, 1, details[0].State)
}

func TestClient_GetServerTime(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/system/time", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": {
				"server_time": 1719273600123
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	ts, err := client.GetServerTime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(1719273600123), ts)
}

func TestClient_CreateOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/submit-order", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var bodyReq struct {
			Symbol        string `json:"symbol"`
			ClientOrderID string `json:"client_order_id"`
			Type          string `json:"type"`
			Side          int    `json:"side"`
			Leverage      string `json:"leverage"`
			OpenType      string `json:"open_type"`
			Mode          int    `json:"mode"`
			Price         string `json:"price"`
			Size          int    `json:"size"`
		}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&bodyReq)
		require.NoError(t, err)

		assert.LessOrEqual(t, len(bodyReq.ClientOrderID), 32)
		assert.Equal(t, "BTCUSDT", bodyReq.Symbol)
		assert.Equal(t, "limit", bodyReq.Type)
		assert.Equal(t, 1, bodyReq.Side)
		assert.Equal(t, "10", bodyReq.Leverage)
		assert.Equal(t, "isolated", bodyReq.OpenType)
		assert.Equal(t, 1, bodyReq.Mode)
		assert.Equal(t, "60000", bodyReq.Price)
		assert.Equal(t, 5, bodyReq.Size)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": {
				"order_id": 9876543210
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:   "BTCUSDT",
		Price:    60000.0,
		Vol:      5.0,
		Leverage: 10,
		Side:     domain.SideOpenLong,
		Type:     domain.OrderTypeLimit,
		OpenType: domain.OpenTypeIsolated,
	})
	require.NoError(t, err)
	assert.Equal(t, "9876543210", res.OrderID)
	assert.False(t, res.TPSLSubmitted)
}

func TestClient_CreateOrder_LongClientOrderID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/submit-order", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var bodyReq struct {
			ClientOrderID string `json:"client_order_id"`
		}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&bodyReq)
		require.NoError(t, err)

		// Assert that the client_order_id is exactly truncated to 32 characters
		assert.Equal(t, "12345678901234567890123456789012", bodyReq.ClientOrderID)
		assert.Equal(t, 32, len(bodyReq.ClientOrderID))

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": {
				"order_id": 9876543210
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	_, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:      "BTCUSDT",
		Price:       60000.0,
		Vol:         5.0,
		Leverage:    10,
		Side:        domain.SideOpenLong,
		Type:        domain.OrderTypeLimit,
		OpenType:    domain.OpenTypeIsolated,
		ExternalOID: "123456789012345678901234567890123456", // 36 characters
	})
	require.NoError(t, err)
}

func TestClient_CreateOrder_WithTPSL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/submit-order", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var bodyReq struct {
			Symbol                    string `json:"symbol"`
			ClientOrderID             string `json:"client_order_id"`
			Type                      string `json:"type"`
			Side                      int    `json:"side"`
			Leverage                  string `json:"leverage"`
			OpenType                  string `json:"open_type"`
			Mode                      int    `json:"mode"`
			Price                     string `json:"price"`
			Size                      int    `json:"size"`
			PresetTakeProfitPriceType int    `json:"preset_take_profit_price_type"`
			PresetStopLossPriceType   int    `json:"preset_stop_loss_price_type"`
			PresetTakeProfitPrice     string `json:"preset_take_profit_price"`
			PresetStopLossPrice       string `json:"preset_stop_loss_price"`
		}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&bodyReq)
		require.NoError(t, err)

		assert.Equal(t, "ETHUSDT", bodyReq.Symbol)
		assert.Equal(t, "BM1234", bodyReq.ClientOrderID)
		assert.Equal(t, "limit", bodyReq.Type)
		assert.Equal(t, 1, bodyReq.Side)
		assert.Equal(t, "10", bodyReq.Leverage)
		assert.Equal(t, "isolated", bodyReq.OpenType)
		assert.Equal(t, 1, bodyReq.Mode)
		assert.Equal(t, "2000", bodyReq.Price)
		assert.Equal(t, 5, bodyReq.Size)
		assert.Equal(t, 1, bodyReq.PresetTakeProfitPriceType)
		assert.Equal(t, 1, bodyReq.PresetStopLossPriceType)
		assert.Equal(t, "2100", bodyReq.PresetTakeProfitPrice)
		assert.Equal(t, "1900", bodyReq.PresetStopLossPrice)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": {
				"order_id": 9876543211
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	res, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol:          "ETHUSDT",
		Price:           2000.0,
		Vol:             5.0,
		Leverage:        10,
		Side:            domain.SideOpenLong,
		Type:            domain.OrderTypeLimit,
		OpenType:        domain.OpenTypeIsolated,
		ExternalOID:     "BM1234",
		TakeProfitPrice: 2100.0,
		StopLossPrice:   1900.0,
	})
	require.NoError(t, err)
	assert.Equal(t, "9876543211", res.OrderID)
	assert.True(t, res.TPSLSubmitted)
}

func TestClient_CancelOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/cancel-order", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var bodyReq struct {
			Symbol  string `json:"symbol"`
			OrderID string `json:"order_id"`
		}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&bodyReq)
		require.NoError(t, err)
		assert.Equal(t, "BTCUSDT", bodyReq.Symbol)
		assert.Equal(t, "9876543210", bodyReq.OrderID)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok"
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	err := client.CancelOrder(context.Background(), "BTCUSDT", "9876543210")
	require.NoError(t, err)
}

func TestClient_GetOpenPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/position-v2", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": [
				{
					"symbol": "BTCUSDT",
					"position_amt": "1.5",
					"avg_entry_price": "50000.0",
					"unrealized_pnl": "100.5",
					"leverage": "10",
					"open_type": "isolated",
					"position_side": "long"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)

	assert.Equal(t, "BTCUSDT", positions[0].Symbol)
	assert.Equal(t, 1.5, positions[0].HoldVol)
	assert.Equal(t, exchange.PositionTypeLong, positions[0].PositionType)
	assert.Equal(t, 50000.0, positions[0].HoldAvgPrice)
	assert.Equal(t, 100.5, positions[0].CloseProfitLoss)
}

func TestClient_GetOpenPositions_DirectArray(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/position-v2", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": [
				{
					"symbol": "REUSDT",
					"position_amt": "2.0",
					"avg_entry_price": "0.57456",
					"unrealized_pnl": "-0.0244",
					"leverage": "5",
					"open_type": "isolated",
					"position_side": "short"
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	positions, err := client.GetOpenPositions(context.Background(), "REUSDT")
	require.NoError(t, err)
	require.Len(t, positions, 1)

	assert.Equal(t, "REUSDT", positions[0].Symbol)
	assert.Equal(t, 2.0, positions[0].HoldVol)
	assert.Equal(t, exchange.PositionTypeShort, positions[0].PositionType)
	assert.Equal(t, 0.57456, positions[0].HoldAvgPrice)
	assert.Equal(t, -0.0244, positions[0].CloseProfitLoss)
}

func TestClient_ChangeLeverage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/submit-leverage", r.URL.Path)
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok"
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	err := client.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTCUSDT",
		Leverage: 10,
		OpenType: domain.OpenTypeIsolated,
	})
	require.NoError(t, err)
}

func TestClient_SwitchPositionMode(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/set-position-mode", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var bodyReq struct {
			PositionMode string `json:"position_mode"`
		}
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&bodyReq)
		require.NoError(t, err)
		assert.Equal(t, "hedge_mode", bodyReq.PositionMode)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok"
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	err := client.SwitchPositionMode(context.Background(), "BTCUSDT", domain.PositionModeHedge)
	require.NoError(t, err)
}

func TestClient_Helpers(t *testing.T) {
	t.Parallel()

	client := bitmart.NewClient(nil, "https://api-cloud-v2.bitmart.com", "key", "secret", "passphrase", config.LoggingConfig{})
	assert.False(t, client.SupportLeverageOnOrder())

	client.SetClock(exchange.RealClock{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":1000,"data":{"server_time":12345}}`))
	}))
	defer server.Close()

	clientWarm := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	clientWarm.WarmUp(context.Background(), time.Second)
}

func TestClient_GetFundingRates(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"data": {
				"symbols": [
					{
						"symbol": "BTCUSDT",
						"funding_rate": "0.0001",
						"funding_time": 1719273600000
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	rates, err := client.GetFundingRates(context.Background(), []string{"BTCUSDT"})
	require.NoError(t, err)
	require.Len(t, rates, 1)
	assert.Equal(t, "BTCUSDT", rates[0].Symbol)
	assert.Equal(t, 0.0001, rates[0].Rate)

	ratesEmpty, err := client.GetFundingRates(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, ratesEmpty)
}

func TestClient_GetPotentialFundingSymbols(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"data": {
				"symbols": [
					{
						"symbol": "BTCUSDT",
						"last_price": "60000",
						"turnover_24h": "10000000",
						"funding_rate": "0.0001",
						"funding_time": 1719273600000
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	res, err := client.GetPotentialFundingSymbols(context.Background(), 100000, 0, nil, nil)
	require.NoError(t, err)
	require.Len(t, res, 1)
	assert.Equal(t, "BTCUSDT", res[0].Symbol)
}

func TestClient_CancelAllOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":1000,"message":"Ok"}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	err := client.CancelAllOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
}

func TestClient_GetOrder_And_GetOrderByExternalID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"data": {
				"order_id": "9876543210",
				"client_order_id": "my_client_id",
				"symbol": "BTCUSDT",
				"side": 1,
				"type": "limit",
				"price": "60000.0",
				"size": "5",
				"deal_size": "0",
				"state": 2,
				"create_time": 1719273600000
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	order, err := client.GetOrder(context.Background(), "BTCUSDT", "9876543210")
	require.NoError(t, err)
	assert.Equal(t, "9876543210", order.OrderID)
	assert.Equal(t, "my_client_id", order.ExternalOID)

	order2, err := client.GetOrderByExternalID(context.Background(), "BTCUSDT", "my_client_id")
	require.NoError(t, err)
	assert.Equal(t, "9876543210", order2.OrderID)
}

func TestClient_GetOpenOrders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"data": [
				{
					"order_id": "9876543210",
					"symbol": "BTCUSDT",
					"side": 1,
					"type": "limit",
					"price": "60000.0",
					"size": "5",
					"deal_size": "0",
					"state": 2
				}
			]
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	orders, err := client.GetOpenOrders(context.Background(), "BTCUSDT")
	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "9876543210", orders[0].OrderID)
}

func TestClient_ClosePosition_CloseAllPositions(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"data": [
					{
						"symbol": "BTCUSDT",
						"position_amt": "1.5",
						"avg_entry_price": "50000.0",
						"unrealized_pnl": "100.5",
						"leverage": "10",
						"position_side": "long"
					}
				]
			}`))
		} else {
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"data": {
					"order_id": 9876543210
				}
			}`))
		}
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	err := client.ClosePosition(context.Background(), "BTCUSDT", domain.SideCloseLong, 1.5, domain.PositionModeHedge, 10)
	require.NoError(t, err)

	err = client.CloseAllPositions(context.Background(), "BTCUSDT")
	require.NoError(t, err)
}

func TestClient_RawRequest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"raw": "data"}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})

	resp, err := client.GetFundingRateRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(resp), "raw")

	resp, err = client.GetTickersRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(resp), "raw")

	resp, err = client.GetOpenPositionsRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(resp), "raw")

	_, err = client.GetHistoryPositionsRaw(context.Background(), nil)
	require.Error(t, err)

	resp, err = client.GetOrderDetailRaw(context.Background(), "123", nil)
	require.NoError(t, err)
	assert.Contains(t, string(resp), "raw")

	resp, err = client.GetHistoryOrdersRaw(context.Background(), nil)
	require.NoError(t, err)
	assert.Contains(t, string(resp), "raw")

	_, err = client.GetOrderPNLRaw(context.Background(), nil)
	require.Error(t, err)
}

func TestClient_CancelOrders(t *testing.T) {
	t.Parallel()

	client := bitmart.NewClient(nil, "https://api-cloud-v2.bitmart.com", "key", "secret", "passphrase", config.LoggingConfig{})
	err := client.CancelOrders(context.Background(), []string{"123"})
	require.Error(t, err)
}

func TestClient_SwitchMarginMode(t *testing.T) {
	t.Parallel()

	client := bitmart.NewClient(nil, "https://api-cloud-v2.bitmart.com", "key", "secret", "passphrase", config.LoggingConfig{})
	err := client.SwitchMarginMode(context.Background(), "BTCUSDT", "isolated", 10, domain.SideOpenLong)
	require.NoError(t, err)
}

func TestClient_GetOrderPNL_Canceled(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/contract/private/order", r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"code": 1000,
			"message": "Ok",
			"data": {
				"order_id": "12345",
				"symbol": "BTCUSDT",
				"state": 6,
				"deal_size": "0"
			}
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	res, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "12345")
	require.NoError(t, err)
	assert.Equal(t, "bitmart", res.Exchange)
	assert.Equal(t, "BTCUSDT", res.Symbol)
	assert.Equal(t, 0.0, res.ClosedSize)
}

func TestClient_GetOrderPNL_Filled_OpenLong(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/contract/private/order":
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"message": "Ok",
				"data": {
					"order_id": "12345",
					"symbol": "BTCUSDT",
					"side": 1,
					"deal_size": "10",
					"deal_avg_price": "50000",
					"state": 4,
					"create_time": 1719273600000,
					"update_time": 1719273605000
				}
			}`))
		case "/contract/private/trades":
			assert.Equal(t, "1719273540", r.URL.Query().Get("start_time"))
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"message": "Ok",
				"data": [
					{
						"order_id": "12345",
						"symbol": "BTCUSDT",
						"side": 1,
						"price": "50000",
						"vol": "10",
						"realised_profit": "0",
						"paid_fees": "1.0",
						"create_time": 1719273600000
					},
					{
						"order_id": "12346",
						"symbol": "BTCUSDT",
						"side": 3,
						"price": "60000",
						"vol": "10",
						"realised_profit": "100.0",
						"paid_fees": "1.0",
						"create_time": 1719273605000
					}
				]
			}`))
		case "/contract/private/transaction-history":
			assert.Equal(t, "1719273600000", r.URL.Query().Get("start_time"))
			assert.Empty(t, r.URL.Query().Get("end_time"))
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"message": "Ok",
				"data": [
					{
						"amount": "-0.5"
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	res, err := client.GetOrderPNL(context.Background(), "BTCUSDT", "12345")
	require.NoError(t, err)

	assert.Equal(t, "bitmart", res.Exchange)
	assert.Equal(t, 10.0, res.ClosedSize)
	assert.InDelta(t, 60000.0, res.ExitPrice, 1e-9)
	assert.Equal(t, 100.0, res.GrossPnL)
	assert.Equal(t, 2.0, res.Fee)
	assert.Equal(t, -0.5, res.FundingFee)
	assert.Equal(t, 97.5, res.NetPnl)
	assert.InDelta(t, 50000.0, res.EntryPrice, 1e-9)
	assert.InDelta(t, 20.0, res.PnLRate, 1e-6)
	assert.Equal(t, int64(5000), res.DurationMs)

	// Test GetOrderPNLRaw
	rawBytes, err := client.GetOrderPNLRaw(context.Background(), map[string]string{
		"symbol":   "BTCUSDT",
		"order_id": "12345",
	})
	require.NoError(t, err)
	var rawInfo exchange.ClosedPnLInfo
	err = json.Unmarshal(rawBytes, &rawInfo)
	require.NoError(t, err)
	assert.Equal(t, 97.5, rawInfo.NetPnl)
}

func TestClient_GetOrder_FallbackToHistory(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contract/private/order":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{
				"code": 40035,
				"message": "Order Not Exist"
			}`))
		case "/contract/private/order-history":
			assert.Equal(t, "BTCUSDT", r.URL.Query().Get("symbol"))
			assert.Equal(t, "12345", r.URL.Query().Get("order_id"))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"message": "Ok",
				"data": [
					{
						"order_id": "12345",
						"client_order_id": "client-123",
						"symbol": "BTCUSDT",
						"side": 1,
						"type": "limit",
						"price": "60000",
						"size": "10",
						"deal_size": "10",
						"state": 4,
						"create_time": 1719273600000,
						"update_time": 1719273605000
					}
				]
			}`))
		}
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	info, err := client.GetOrder(context.Background(), "BTCUSDT", "12345")
	require.NoError(t, err)
	assert.Equal(t, "12345", info.OrderID)
	assert.Equal(t, domain.OrderStateFilled, info.State)
}

func TestClient_APIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{
			"code": 50001,
			"message": "Invalid Parameter"
		}`))
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	_, err := client.GetOrder(context.Background(), "BTCUSDT", "12345")
	require.Error(t, err)

	apiErr, ok := exchange.IsAPIError(err)
	require.True(t, ok)
	assert.Equal(t, 400, apiErr.StatusCode)
	assert.Equal(t, 50001, apiErr.Code)
	assert.Equal(t, "Invalid Parameter", apiErr.Message)
	assert.Contains(t, apiErr.Path, "/contract/private/order")
}

func TestClient_GetOrderPNL_OpeningOrder(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		switch r.URL.Path {
		case "/contract/private/order":
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"message": "Ok",
				"data": {
					"order_id": "10001",
					"symbol": "PORTALUSDT",
					"side": 4,
					"deal_size": "10423",
					"deal_avg_price": "0.01439",
					"state": 4,
					"create_time": 1782483900000,
					"update_time": 1782483900258
				}
			}`))
		case "/contract/private/trades":
			assert.Empty(t, r.URL.Query().Get("order_id"))
			assert.Equal(t, "1782483840", r.URL.Query().Get("start_time"))
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"message": "Ok",
				"data": [
					{
						"order_id": "10001",
						"symbol": "PORTALUSDT",
						"side": 4,
						"price": "0.01439",
						"vol": "10423",
						"realised_profit": "0",
						"paid_fees": "0.00899921",
						"create_time": 1782483900258
					},
					{
						"order_id": "10002",
						"symbol": "PORTALUSDT",
						"side": 2,
						"price": "0.01442",
						"vol": "10423",
						"realised_profit": "-0.31269",
						"paid_fees": "0.00899921",
						"create_time": 1782483910636
					}
				]
			}`))
		case "/contract/private/transaction-history":
			_, _ = w.Write([]byte(`{
				"code": 1000,
				"message": "Ok",
				"data": []
			}`))
		}
	}))
	defer server.Close()

	client := bitmart.NewClient(server.Client(), server.URL, "key", "secret", "passphrase", config.LoggingConfig{})
	res, err := client.GetOrderPNL(context.Background(), "PORTALUSDT", "10001")
	require.NoError(t, err)

	assert.Equal(t, "bitmart", res.Exchange)
	assert.Equal(t, 10423.0, res.ClosedSize)
	assert.InDelta(t, 0.01442, res.ExitPrice, 1e-9)
	assert.Equal(t, -0.31269, res.GrossPnL)
	assert.Equal(t, 0.01799842, res.Fee)
	assert.Equal(t, 0.0, res.FundingFee)
	assert.InDelta(t, 0.01439, res.EntryPrice, 1e-9)
	assert.InDelta(t, ((0.01439-0.01442)/0.01439)*100.0, res.PnLRate, 1e-6)
	assert.Equal(t, int64(10636), res.DurationMs)
}

package bybit_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/bybit"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type dummyResult struct {
	OrderID string `json:"orderId"`
}

func TestParseResponseEnvelope(t *testing.T) {
	t.Parallel()

	t.Run("success with time", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{
			"retCode": 0,
			"retMsg": "OK",
			"result": {"orderId": "ord-123"},
			"time": 1672217377164
		}`)

		resp, err := bybit.ParseResponseEnvelope[dummyResult](body, "create order")
		require.NoError(t, err)
		assert.Equal(t, 0, resp.RetCode)
		assert.Equal(t, "OK", resp.RetMsg)
		assert.Equal(t, "ord-123", resp.Result.OrderID)
		assert.Equal(t, int64(1672217377164), resp.Time)
		assert.Equal(t, int64(1672217377164), time.UnixMilli(resp.Time).UnixMilli())
	})

	t.Run("non-zero retCode", func(t *testing.T) {
		t.Parallel()

		body := []byte(`{
			"retCode": 10001,
			"retMsg": "params error",
			"result": null,
			"time": 1672217377164
		}`)

		resp, err := bybit.ParseResponseEnvelope[dummyResult](body, "create order")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "retCode=10001")
		assert.Contains(t, err.Error(), "params error")
		assert.Equal(t, 10001, resp.RetCode)
	})

	t.Run("invalid json", func(t *testing.T) {
		t.Parallel()

		body := []byte(`invalid json`)
		_, err := bybit.ParseResponseEnvelope[dummyResult](body, "create order")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json unmarshal")
	})
}

func TestParseResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"retCode": 0,
		"retMsg": "OK",
		"result": {"orderId": "ord-456"},
		"time": 1672217377164
	}`)

	res, err := bybit.ParseResponse[dummyResult](body, "test")
	require.NoError(t, err)
	assert.Equal(t, "ord-456", res.OrderID)

	errBody := []byte(`{"retCode": 10002, "retMsg": "unauthorized"}`)
	_, err = bybit.ParseResponse[dummyResult](errBody, "test")
	require.Error(t, err)
}

func TestDecodeListResponse(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"retCode": 0,
		"retMsg": "OK",
		"result": {
			"list": [{"orderId": "item-1"}, {"orderId": "item-2"}]
		}
	}`)

	items, err := bybit.DecodeListResponse[dummyResult](body, "list")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "item-1", items[0].OrderID)
	assert.Equal(t, "item-2", items[1].OrderID)
}

func TestBaseClient_PrepareRequestAndExecute(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-api-key", r.Header.Get("X-BAPI-API-KEY"))
		assert.NotEmpty(t, r.Header.Get("X-BAPI-SIGN"))
		assert.NotEmpty(t, r.Header.Get("X-BAPI-TIMESTAMP"))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"orderId":"123"},"time":1672217377164}`))
	}))
	defer server.Close()

	client := bybit.NewBaseClient(server.Client(), server.URL, "test-api-key", "test-api-secret", "unified", config.LoggingConfig{})
	dispatch, err := client.PrepareRequest(context.Background(), http.MethodPost, "/v5/order/create", nil, []byte(`{"category":"linear"}`))
	require.NoError(t, err)

	respBody, err := dispatch(context.Background())
	require.NoError(t, err)

	resp, err := bybit.ParseResponseEnvelope[dummyResult](respBody, "prepare")
	require.NoError(t, err)
	assert.Equal(t, "123", resp.Result.OrderID)
	assert.Equal(t, int64(1672217377164), resp.Time)
}

func TestBaseClient_PreWarm_And_OrderHTTPClient(t *testing.T) {
	t.Parallel()

	var pingCount int
	var orderCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v5/market/time":
			pingCount++
			_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"timeSecond":"1672217377"}}`))
		case "/v5/order/create":
			orderCount++
			assert.Equal(t, "test-api-key", r.Header.Get("X-BAPI-API-KEY"))
			assert.NotEmpty(t, r.Header.Get("X-BAPI-SIGN"))
			_, _ = w.Write([]byte(`{"retCode":0,"retMsg":"OK","result":{"orderId":"123"},"time":1672217377164}`))
		}
	}))
	defer server.Close()

	client := bybit.NewBaseClient(server.Client(), server.URL, "test-api-key", "test-api-secret", "unified", config.LoggingConfig{})
	orderClient := server.Client()
	client.SetOrderHTTPClient(orderClient)
	assert.NotNil(t, client.OrderHTTPClient())

	err := client.PreWarm(context.Background())
	require.NoError(t, err)

	dispatch, err := client.PrepareRequest(context.Background(), http.MethodPost, "/v5/order/create", nil, []byte(`{"category":"linear"}`))
	require.NoError(t, err)

	respBody, err := dispatch(context.Background())
	require.NoError(t, err)

	resp, err := bybit.ParseResponseEnvelope[dummyResult](respBody, "prepare")
	require.NoError(t, err)
	assert.Equal(t, "123", resp.Result.OrderID)

	assert.Equal(t, 1, pingCount)
	assert.Equal(t, 1, orderCount)
}

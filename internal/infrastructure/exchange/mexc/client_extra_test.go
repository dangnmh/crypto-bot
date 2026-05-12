package mexc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/mexc"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_Post(t *testing.T) {
	t.Parallel()
	resp := exchange.CreateOrderResponse{OrderID: "post123"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.NotEmpty(t, r.Header.Get("ApiKey"))
		assert.NotEmpty(t, r.Header.Get("Request-Time"))
		assert.NotEmpty(t, r.Header.Get("Signature"))
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[exchange.CreateOrderResponse]{Success: true, Code: 0, Data: resp}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	body, err := client.Post(context.Background(), "/api/v1/private/order/create", map[string]string{"symbol": "BTC_USDT"})
	require.NoError(t, err)
	require.NotEmpty(t, body)
}

func TestClient_Post_NilBody(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	body, err := client.Post(context.Background(), "/api/v1/private/test", nil)
	require.NoError(t, err)
	require.NotEmpty(t, body)
}

func TestClient_WarmUp(t *testing.T) {
	t.Parallel()
	pingCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pingCount++
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[int64]{Success: true, Code: 0, Data: time.Now().UnixMilli()}))
	}))
	defer srv.Close()

	client := newTestClient(srv)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	client.WarmUp(ctx, time.Hour) // RunImmediate: fires once then ctx expires
	assert.GreaterOrEqual(t, pingCount, 1, "WarmUp should have pinged at least once")
}

func TestClient_Latency_Error(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`error`))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.Latency(context.Background())
	assert.Error(t, err)
}

func TestClient_GetCtx_CancelledContext(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	client := newTestClient(srv)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	_, err := client.Get(ctx, "/api/v1/contract/ping", nil)
	assert.Error(t, err)
}

func TestClient_doRequest_NonOK_NonRateLimit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.Get(context.Background(), "/api/v1/contract/ping", nil)
	assert.Error(t, err)
	assert.False(t, exchange.IsRateLimitError(err))
}

func TestClient_Get_WithParams(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.RawQuery, "symbol=BTC_USDT")
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.Get(context.Background(), "/api/v1/contract/detail", map[string]string{"symbol": "BTC_USDT"})
	assert.NoError(t, err)
}

func TestClient_GetCtx_PrivateEndpoint(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Private endpoints should have auth headers.
		assert.NotEmpty(t, r.Header.Get("ApiKey"))
		assert.NotEmpty(t, r.Header.Get("Request-Time"))
		assert.NotEmpty(t, r.Header.Get("Signature"))
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.GetCtx(context.Background(), "/api/v1/private/account/assets", nil)
	assert.NoError(t, err)
}

func TestClient_NewClient_WithHTTPLogging(t *testing.T) {
	t.Parallel()
	// Ensure no panic when creating client with HTTP logging and nil transport.
	client := mexc.NewClient(&http.Client{}, "http://localhost", "k", "s", config.LoggingConfig{HTTP: true})
	assert.NotNil(t, client)
}

package mexc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/exchange/mexc"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Account API error branches ──────────────────────────────────────.

func TestClient_GetOpenPositions_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]exchange.Position]{
			Success: false,
			Code:    1003,
			Message: "position error",
		}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.GetOpenPositions(context.Background(), "BTC_USDT")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "position error")
}

func TestClient_GetOpenPositions_NoSymbol(t *testing.T) {
	t.Parallel()
	positions := []exchange.Position{{Symbol: "BTC_USDT", HoldVol: 10}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify no symbol param is sent when empty.
		assert.Empty(t, r.URL.Query().Get("symbol"))
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]exchange.Position]{Success: true, Code: 0, Data: positions}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	got, err := client.GetOpenPositions(context.Background(), "")
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

// ── Market API error branches ───────────────────────────────────────.

func TestClient_GetContractDetails_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[[]exchange.ContractDetail]{
			Success: false,
			Code:    2001,
			Message: "service unavailable",
		}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.GetContractDetails(context.Background())
	assert.Error(t, err)
}

func TestClient_GetTickers_InvalidDataFormat(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return invalid raw data that can't be parsed as array or single.
		raw := json.RawMessage(`"just a string"`)
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[json.RawMessage]{Success: true, Code: 0, Data: raw}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.GetTickers(context.Background(), "INVALID")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse ticker data")
}

// ── Order API error branches ────────────────────────────────────────.

func TestClient_CreateOrder_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[exchange.CreateOrderResponse]{
			Success: false,
			Code:    3001,
			Message: "insufficient balance",
		}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.CreateOrder(context.Background(), exchange.SubmitOrderRequest{Symbol: "BTC_USDT"})
	assert.Error(t, err)
}

func TestClient_GetOrder_APIError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(mustJSON(t, mexc.APIResponse[exchange.OrderInfo]{
			Success: false,
			Code:    3003,
			Message: "order not found",
		}))
	}))
	defer srv.Close()

	client := newTestClient(srv)
	_, err := client.GetOrder(context.Background(), "BTC_USDT", "nonexistent")
	assert.Error(t, err)
}

// ── WsAdapter GetAuthHook inner function ────────────────────────────.

func TestWsAdapter_ParseTicker_InvalidDataField(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()

	// Valid outer JSON but invalid data field.
	input := `{"channel":"push.ticker","symbol":"BTC_USDT","data":"not_valid_json"}`
	_, _, err := a.ParseTicker([]byte(input))
	assert.Error(t, err)
}

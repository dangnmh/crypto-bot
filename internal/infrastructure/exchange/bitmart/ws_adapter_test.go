package bitmart_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bitmart"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockClock struct {
	now time.Time
}

func (m mockClock) Now() time.Time {
	return m.now
}

func TestWsAdapter_Lifecycle(t *testing.T) {
	t.Parallel()

	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	// SetClient
	client := bitmart.NewClient(nil, "https://api-cloud-v2.bitmart.com", "key", "secret", "passphrase", config.LoggingConfig{})
	adapter.SetClient(client)

	// Ping config
	ping, interval := adapter.GetPingConfig()
	assert.NotNil(t, ping)
	assert.Equal(t, 30*time.Second, interval)

	// Channel extractor
	extractor := adapter.GetChannelExtractor()
	require.NotNil(t, extractor)

	assert.Equal(t, "ping", extractor([]byte(`{"ping": 123}`)))
	assert.Equal(t, "pong", extractor([]byte(`{"pong": 123}`)))
	assert.Equal(t, "ticker", extractor([]byte(`{"group": "futures/ticker"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"group": "futures/position"}`)))
	assert.Equal(t, "personal.position", extractor([]byte(`{"event": "futures/position"}`)))
}

func TestWsAdapter_Subscriptions(t *testing.T) {
	t.Parallel()

	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = adapter.SubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.UnsubscribeTicker(ctx, "BTCUSDT")
	_ = adapter.SubscribePersonal(ctx)
}

func TestWsAdapter_AuthHook(t *testing.T) {
	t.Parallel()

	clk := mockClock{now: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)}
	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")
	adapter.SetClock(clk)

	hook := adapter.GetAuthHook("mykey", "mysecret")
	assert.NotNil(t, hook)

	// Hook is not null, execute it with a dummy ws client
	wsClient := pkgws.NewClient("ws://127.0.0.1:1", slog.Default())
	hook(wsClient)
}

func TestWsAdapter_ParseTicker(t *testing.T) {
	t.Parallel()

	clk := mockClock{now: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)}
	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")
	adapter.SetClock(clk)

	// 1. Single object ticker data
	tickerData := []byte(`{
		"group": "futures/ticker",
		"data": {
			"symbol": "BTCUSDT",
			"last_price": "60000.5",
			"ask_price": "60001.5",
			"bid_price": "59999.5",
			"volume_24h": "120.5"
		}
	}`)

	sym, pd, err := adapter.ParseTicker(tickerData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 60000.5, pd.LastPrice)
	assert.Equal(t, 60001.5, pd.BestAsk)
	assert.Equal(t, 59999.5, pd.BestBid)
	assert.Equal(t, 60000.5, pd.FairPrice)
	assert.Equal(t, 120.5, pd.Volume24)

	// 2. Array wrapper ticker data
	tickerArrayData := []byte(`{
		"group": "futures/ticker",
		"data": [
			{
				"symbol": "ETHUSDT",
				"last_price": "3500.0",
				"ask_price": "3500.5",
				"bid_price": "3499.5",
				"volume_24h": "500"
			}
		]
	}`)

	sym, pd, err = adapter.ParseTicker(tickerArrayData)
	require.NoError(t, err)
	assert.Equal(t, "ETHUSDT", sym)
	assert.Equal(t, 3500.0, pd.LastPrice)

	// 3. futures/ticker with volume_24
	tickerVolume24Data := []byte(`{
		"group": "futures/ticker:BTCUSDT",
		"data": {
			"symbol": "BTCUSDT",
			"last_price": "97153.6",
			"volume_24": "25502894",
			"ask_price": "97153.9",
			"bid_price": "97153.4"
		}
	}`)

	sym, pd, err = adapter.ParseTicker(tickerVolume24Data)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", sym)
	assert.Equal(t, 97153.6, pd.LastPrice)
	assert.Equal(t, 97153.9, pd.BestAsk)
	assert.Equal(t, 97153.4, pd.BestBid)
	assert.Equal(t, 25502894.0, pd.Volume24)

	// 4. futures/bookticker
	bookTickerData := []byte(`{
		"group": "futures/bookticker:LTCUSDT",
		"data": {
			"symbol": "LTCUSDT",
			"best_bid_price": "97315",
			"best_bid_vol": "156",
			"best_ask_price": "97315.4",
			"best_ask_vol": "333",
			"ms_t": 1733891542244
		}
	}`)

	sym, pd, err = adapter.ParseTicker(bookTickerData)
	require.NoError(t, err)
	assert.Equal(t, "LTCUSDT", sym)
	assert.Equal(t, 97315.4, pd.BestAsk)
	assert.Equal(t, 97315.0, pd.BestBid)
	assert.Equal(t, 97315.2, pd.LastPrice)
}

func TestWsAdapter_ParseTicker_Errors(t *testing.T) {
	t.Parallel()

	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")

	// Invalid JSON
	_, _, err := adapter.ParseTicker([]byte("{invalid"))
	require.Error(t, err)

	// Empty ticker data
	_, _, err = adapter.ParseTicker([]byte(`{"group": "futures/ticker", "data": ""}`))
	require.Error(t, err)

	// Missing symbol
	_, _, err = adapter.ParseTicker([]byte(`{"group": "futures/ticker", "data": {}}`))
	require.Error(t, err)

	// Empty array
	_, _, err = adapter.ParseTicker([]byte(`{"group": "futures/ticker", "data": []}`))
	require.Error(t, err)
}

func TestWsAdapter_ParsePosition(t *testing.T) {
	t.Parallel()

	clk := mockClock{now: time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)}
	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")
	adapter.SetClock(clk)

	// 1. Array style position
	posData := []byte(`{
		"group": "futures/position",
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
	}`)

	update, err := adapter.ParsePosition(posData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", update.Symbol)
	assert.Equal(t, 1.5, update.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeLong, update.PositionType)
	assert.Equal(t, 50000.0, update.HoldAvgPrice)
	assert.Equal(t, 100.5, update.CloseProfitLoss)
	assert.Equal(t, 10, update.Leverage)

	// 2. Direct object style position
	posDirectData := []byte(`{
		"group": "futures/position",
		"data": {
			"symbol": "BTCUSDT",
			"position_amount": "2.5",
			"open_avg_price": "51000.0",
			"unrealized_pnl": "-50.0",
			"leverage": "20",
			"position_side": "2"
		}
	}`)

	update, err = adapter.ParsePosition(posDirectData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", update.Symbol)
	assert.Equal(t, 2.5, update.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, update.PositionType)
	assert.Equal(t, 51000.0, update.HoldAvgPrice)
	assert.Equal(t, -50.0, update.CloseProfitLoss)
	assert.Equal(t, 20, update.Leverage)

	// 3. Official documentation style position (with hold_volume, hold_avg_price, position_type)
	posDocData := []byte(`{
		"group": "futures/position",
		"data": [
			{
				"symbol": "BTCUSDT",
				"hold_volume": "2000",
				"position_type": 2,
				"open_type": 1,
				"frozen_volume": "0",
				"close_volume": "0",
				"hold_avg_price": "19406.2092",
				"close_avg_price": "0",
				"open_avg_price": "19406.2092",
				"liquidate_price": "15621.998406",
				"create_time": 1662692862255,
				"update_time": 1662692862255,
				"position_mode": "hedge_mode"
			}
		]
	}`)

	update, err = adapter.ParsePosition(posDocData)
	require.NoError(t, err)
	assert.Equal(t, "BTCUSDT", update.Symbol)
	assert.Equal(t, 2000.0, update.HoldVolContract)
	assert.Equal(t, exchange.PositionTypeShort, update.PositionType)
	assert.Equal(t, 19406.2092, update.HoldAvgPrice)
	assert.Equal(t, 0.0, update.CloseVolContract)
	assert.Equal(t, 0.0, update.CloseAvgPrice)

	// 4. Position closed style position
	posClosedData := []byte(`{
		"group": "futures/position",
		"data": [
			{
				"symbol": "PORTALUSDT",
				"hold_volume": "0",
				"position_type": 2,
				"open_type": 1,
				"frozen_volume": "0",
				"close_volume": "10423",
				"hold_avg_price": "0.01439",
				"close_avg_price": "0.01442",
				"open_avg_price": "0.01439",
				"liquidate_price": "0",
				"create_time": 1782483900258,
				"update_time": 1782483910636,
				"position_mode": "hedge_mode"
			}
		]
	}`)

	update, err = adapter.ParsePosition(posClosedData)
	require.NoError(t, err)
	assert.Equal(t, "PORTALUSDT", update.Symbol)
	assert.Equal(t, 0.0, update.HoldVolContract)
	assert.Equal(t, 10423.0, update.CloseVolContract)
	assert.Equal(t, 0.01442, update.CloseAvgPrice)
}

func TestWsAdapter_ParsePosition_Errors(t *testing.T) {
	t.Parallel()

	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")

	// Invalid JSON
	_, err := adapter.ParsePosition([]byte("{invalid"))
	require.Error(t, err)

	// Empty data list
	_, err = adapter.ParsePosition([]byte(`{"group": "futures/position", "data": []}`))
	require.Error(t, err)
}

func TestWsAdapter_AuthSynchronization(t *testing.T) {
	t.Parallel()

	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	// Before login, SubscribePersonal should block.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := adapter.SubscribePersonal(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Re-run with a mock client and trigger the login success.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()

	// Initialize login hook so the channel is re-created
	hook := adapter.GetAuthHook("mykey", "mysecret")
	wsClient := pkgws.NewClient("ws://127.0.0.1:1", slog.Default())
	hook(wsClient)

	// In a separate goroutine, call SubscribePersonal
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- adapter.SubscribePersonal(ctx2)
	}()

	// Ensure it hasn't completed yet
	select {
	case err := <-doneCh:
		t.Fatalf("SubscribePersonal completed prematurely: %v", err)
	case <-time.After(50 * time.Millisecond):
		// OK, still blocking
	}

	// Trigger login success frame
	extractor := adapter.GetChannelExtractor()
	action := extractor([]byte(`{"action":"access","success":true}`))
	assert.Equal(t, "access", action)

	// It should now unblock and try to send the private message.
	select {
	case err := <-doneCh:
		assert.NoError(t, err)
	case <-time.After(200 * time.Millisecond):
		t.Fatal("SubscribePersonal did not unblock after login success")
	}
}

func TestWsAdapter_AuthHook_EmptyKey(t *testing.T) {
	t.Parallel()

	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	hook := adapter.GetAuthHook("", "")
	assert.Nil(t, hook)

	// Since apiKey is empty, SubscribePersonal should unblock immediately
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := adapter.SubscribePersonal(ctx)
	assert.NoError(t, err)
}

func TestWsAdapter_AuthFailure(t *testing.T) {
	t.Parallel()

	adapter := bitmart.NewWsAdapter("wss://openapi-ws-v2.bitmart.com/user?protocol=1.1", "mypassphrase")
	pool := pkgws.NewPool("ws://127.0.0.1:1", 30, slog.Default())
	adapter.SetPool(pool)

	hook := adapter.GetAuthHook("mykey", "mysecret")
	wsClient := pkgws.NewClient("ws://127.0.0.1:1", slog.Default())
	hook(wsClient)

	// In a separate goroutine, call SubscribePersonal
	ctx := context.Background()
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- adapter.SubscribePersonal(ctx)
	}()

	// Trigger login failure frame
	extractor := adapter.GetChannelExtractor()
	action := extractor([]byte(`{"action":"access","success":false,"error":"SIGN_ERROR"}`))
	assert.Equal(t, "access", action)

	// SubscribePersonal should unblock and return an error
	select {
	case err := <-doneCh:
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "login failed: SIGN_ERROR")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("SubscribePersonal did not unblock after login failure")
	}
}

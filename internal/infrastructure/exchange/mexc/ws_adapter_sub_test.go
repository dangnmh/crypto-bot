package mexc_test

import (
	"context"
	"log/slog"
	"testing"

	"net/http"
	"net/http/httptest"
	"strings"

	"crypto-bot/internal/infrastructure/exchange/mexc"
	pkgws "crypto-bot/pkg/ws"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var upgrader = websocket.Upgrader{}

// newTestWSPool creates a real local WS server and pool connected to it.
func newTestWSPool(t *testing.T) (*pkgws.Pool, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				break
			}
		}
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	pool := pkgws.NewPool(wsURL, 30, slog.Default())

	return pool, func() {
		pool.Close()
		srv.Close()
	}
}

func TestWsAdapter_SetPool(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()

	a.SetPool(pool)
}

func TestWsAdapter_GetAuthHook_EmptyKey(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	hook := a.GetAuthHook("", "secret")
	assert.Nil(t, hook, "empty API key should return nil hook")
}

func TestWsAdapter_GetAuthHook_ValidKey(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	hook := a.GetAuthHook("myKey", "mySecret")
	require.NotNil(t, hook, "valid API key should return non-nil hook")
}

func TestWsAdapter_SubscribeTicker(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.SubscribeTicker(context.Background(), "BTC_USDT")
	assert.NoError(t, err)
}

func TestWsAdapter_UnsubscribeTicker(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.UnsubscribeTicker(context.Background(), "BTC_USDT")
	assert.NoError(t, err)
}

func TestWsAdapter_SubscribeKline(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.SubscribeKline(context.Background(), "BTC_USDT")
	assert.NoError(t, err)
}

func TestWsAdapter_UnsubscribeKline(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.UnsubscribeKline(context.Background(), "BTC_USDT")
	assert.NoError(t, err)
}

func TestWsAdapter_SubscribeDepth_Full(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.SubscribeDepth(context.Background(), "BTC_USDT", "")
	assert.NoError(t, err)
}

func TestWsAdapter_SubscribeDepth_Step(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.SubscribeDepth(context.Background(), "BTC_USDT", "step0")
	assert.NoError(t, err)
}

func TestWsAdapter_UnsubscribeDepth_Full(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.UnsubscribeDepth(context.Background(), "BTC_USDT", "")
	assert.NoError(t, err)
}

func TestWsAdapter_UnsubscribeDepth_Step(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.UnsubscribeDepth(context.Background(), "BTC_USDT", "step0")
	assert.NoError(t, err)
}

func TestWsAdapter_SubscribePersonal(t *testing.T) {
	t.Parallel()
	a := mexc.NewWsAdapter()
	pool, cleanup := newTestWSPool(t)
	defer cleanup()
	a.SetPool(pool)

	err := a.SubscribePersonal(context.Background())
	assert.NoError(t, err)
}

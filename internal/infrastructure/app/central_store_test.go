package app_test

import (
	"context"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/app"
	"time"

	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Construction ─────────────────────────────────────────────────────.

func TestNewCentralStore_NoOptions(t *testing.T) {
	t.Parallel()
	cs := app.NewCentralStore()

	assert.Nil(t, cs.Ticker(), "Ticker should be nil without app.WithTicker")
	assert.Nil(t, cs.Contract(), "Contract should be nil without app.WithContract")
	assert.Nil(t, cs.Price(), "Price should be nil without app.WithPrice")
	assert.Nil(t, cs.Depth(), "Depth should be nil without app.WithDepth")
	assert.Nil(t, cs.Funding(), "Funding should be nil without app.WithFunding")
	assert.Nil(t, cs.Kline(), "Kline should be nil without app.WithKline")
}

func TestNewCentralStore_WSOnlyStores(t *testing.T) {
	t.Parallel()
	cs := app.NewCentralStore(app.WithLogger(testLogger()), app.WithPrice(), app.WithDepth(), app.WithKline())

	assert.NotNil(t, cs.Price())
	assert.NotNil(t, cs.Depth())
	assert.NotNil(t, cs.Kline())

	// REST stores remain nil.
	assert.Nil(t, cs.Ticker())
	assert.Nil(t, cs.Contract())
	assert.Nil(t, cs.Funding())
}

func TestNewCentralStore_RESTStores(t *testing.T) {
	t.Parallel()
	client := &dummyClient{}

	cs := app.NewCentralStore(app.WithLogger(testLogger()),
		app.WithTicker(client, time.Second),
		app.WithContract(client, time.Second),
		app.WithFunding(client, time.Second, []string{"BTC_USDT"}),
	)

	assert.NotNil(t, cs.Ticker())
	assert.NotNil(t, cs.Contract())
	assert.NotNil(t, cs.Funding())
}

func TestNewCentralStore_AllOptions(t *testing.T) {
	t.Parallel()
	client := &dummyClient{}

	cs := app.NewCentralStore(app.WithLogger(testLogger()),
		app.WithTicker(client, time.Second),
		app.WithContract(client, time.Second),
		app.WithFunding(client, time.Second, []string{"BTC_USDT"}),
		app.WithPrice(),
		app.WithDepth(),
		app.WithKline(),
	)

	assert.NotNil(t, cs.Ticker())
	assert.NotNil(t, cs.Contract())
	assert.NotNil(t, cs.Funding())
	assert.NotNil(t, cs.Price())
	assert.NotNil(t, cs.Depth())
	assert.NotNil(t, cs.Kline())
}

// ── Lifecycle ────────────────────────────────────────────────────────.

func TestCentralStore_Start_CancelledContext(t *testing.T) {
	t.Parallel()
	client := &dummyClient{}

	cs := app.NewCentralStore(app.WithLogger(testLogger()),
		app.WithTicker(client, time.Second),
		app.WithContract(client, time.Second),
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancel

	assert.NotPanics(t, func() {
		cs.Start(ctx)
	})
}

func TestCentralStore_WaitReady_NoSyncTasks(t *testing.T) {
	t.Parallel()
	// Only WS stores — no readyWG increments, so WaitReady returns immediately.
	cs := app.NewCentralStore(app.WithLogger(testLogger()), app.WithPrice(), app.WithDepth())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := cs.WaitReady(ctx)
	assert.NoError(t, err)
}

func TestCentralStore_WaitReady_ContextTimeout(t *testing.T) {
	t.Parallel()
	client := &dummyClient{}

	// Ticker registers a readyWG entry that will never complete (no sync runs).
	cs := app.NewCentralStore(app.WithLogger(testLogger()), app.WithTicker(client, time.Hour))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := cs.WaitReady(ctx)
	assert.Error(t, err, "WaitReady should return error on context timeout")
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

// ── WireWS ───────────────────────────────────────────────────────────.

func TestCentralStore_WireWS_NilPoolOrAdapter(t *testing.T) {
	t.Parallel()
	cs := app.NewCentralStore(app.WithLogger(testLogger()), app.WithPrice(), app.WithDepth(), app.WithKline())

	// Should not panic with nil args.
	assert.NotPanics(t, func() {
		cs.WireWS(nil, nil)
		cs.WireWS(nil, &mockAdapter{})
	})
}

func TestCentralStore_WireWS_RegistersHandlers(t *testing.T) {
	t.Parallel()
	cs := app.NewCentralStore(app.WithLogger(testLogger()), app.WithPrice(), app.WithDepth(), app.WithKline())

	adapter := &mockAdapter{}
	pool := newTestPool()

	// Should not panic and should register handlers for all enabled WS stores.
	assert.NotPanics(t, func() {
		cs.WireWS(pool, adapter)
	})
}

func TestCentralStore_WireWS_SkipsDisabledStores(t *testing.T) {
	t.Parallel()
	// Only enable Price — no depth or kline.
	cs := app.NewCentralStore(app.WithLogger(testLogger()), app.WithPrice())

	adapter := &mockAdapter{}
	pool := newTestPool()

	// Should not panic — only ticker handler registered (for Price), not depth/kline.
	assert.NotPanics(t, func() {
		cs.WireWS(pool, adapter)
	})
}

// ── SyncTasks registration ───────────────────────────────────────────.

func TestCentralStore_SyncTaskCount(t *testing.T) {
	t.Parallel()
	client := &dummyClient{}

	cs := app.NewCentralStore(app.WithLogger(testLogger()),
		app.WithTicker(client, time.Second),
		app.WithContract(client, time.Second),
		app.WithFunding(client, time.Second, []string{"BTC_USDT"}),
		app.WithPrice(), // no sync task
		app.WithDepth(), // no sync task
	)

	require.Len(t, cs.SyncTaskNamesForTest(), 3, "expected 3 sync tasks (ticker, contract, funding)")
	assert.Equal(t, "ticker", cs.SyncTaskNamesForTest()[0])
	assert.Equal(t, "contract", cs.SyncTaskNamesForTest()[1])
	assert.Equal(t, "funding", cs.SyncTaskNamesForTest()[2])
}

// ── test helpers ─────────────────────────────────────────────────────.

// newTestPool creates a minimal Pool for handler registration tests.
func newTestPool() *pkgws.Pool {
	return pkgws.NewPool("wss://test.example.com", 10, slog.Default())
}

package exchange_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsOrderDeal_GetOrderID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		orderID any
		want    string
	}{
		{"string ID", "abc123", "abc123"},
		{"numeric ID", float64(12345), "12345"},
		{"int ID", 42, "42"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &exchange.WsOrderDeal{OrderID: tt.orderID}
			assert.Equal(t, tt.want, d.GetOrderID())
		})
	}
}

func TestDryRunClient_CreateTrackOrder(t *testing.T) {
	t.Parallel()

	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	id, err := dry.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{Symbol: "BTC"})
	require.NoError(t, err)
	assert.NotEmpty(t, id)
}

func TestDryRunClient_CancelOrders_NoRealCall(t *testing.T) {
	t.Parallel()

	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)
	err := dry.CancelOrders(context.Background(), []string{"o1", "o2"})
	require.NoError(t, err)
}

func TestDryRunClient_CancelAllOpenOrders_NoRealCall(t *testing.T) {
	t.Parallel()

	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)
	err := dry.CancelAllOpenOrders(context.Background(), "BTC_USDT")
	require.NoError(t, err)
}

func TestDryRunClient_ChangeLeverage_NoRealCall(t *testing.T) {
	t.Parallel()

	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)
	err := dry.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{Symbol: "BTC", Leverage: 20})
	require.NoError(t, err)
}

func TestDryRunClient_AccountProvider_Delegates(t *testing.T) {
	t.Parallel()

	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	_, err := dry.GetAssets(context.Background())
	require.NoError(t, err)

	_, err = dry.GetAssetByCurrency(context.Background(), "USDT")
	require.NoError(t, err)

	_, err = dry.GetOpenPositions(context.Background(), "BTC_USDT")
	require.NoError(t, err)
}

func TestDryRunClient_GetOrder_Delegates(t *testing.T) {
	t.Parallel()

	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	_, err := dry.GetOrder(context.Background(), "BTC_USDT", "order_123")
	require.NoError(t, err)

	_, err = dry.GetOpenOrders(context.Background(), "BTC_USDT")
	require.NoError(t, err)
}

func TestDryRunClient_WarmUp(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)
	// Should not panic.
	assert.NotPanics(t, func() {
		dry.WarmUp(context.Background(), 0)
	})
}

func TestDryRunClient_GetFundingRates_Delegates(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)
	_, err := dry.GetFundingRates(context.Background())
	require.NoError(t, err)
}

func TestDryRunClient_GetKlines_Delegates(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)
	_, err := dry.GetKlines(context.Background(), "BTC_USDT", "1m", 0, 0)
	require.NoError(t, err)
}

func TestDryRunClient_UniqueOrderIDs(t *testing.T) {
	t.Parallel()

	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	ids := make(map[string]bool)
	for range 50 {
		res, err := dry.CreateOrder(context.Background(), exchange.SubmitOrderRequest{})
		require.NoError(t, err)
		assert.False(t, ids[res.OrderID], "duplicate order ID: %s", res.OrderID)
		ids[res.OrderID] = true
	}
}

type stubClosedPnLClient struct {
	stubClient
}

func (s *stubClosedPnLClient) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	return &exchange.ClosedPnLInfo{Symbol: symbol, EntryPrice: 123}, nil
}

func TestDryRunClient_GetRecentClosedPnL_Supported(t *testing.T) {
	t.Parallel()

	inner := &stubClosedPnLClient{}
	dry := exchange.NewDryRunClient(inner)

	info, err := dry.GetRecentClosedPnL(context.Background(), "BTC", "ord123", time.Time{})
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "BTC", info.Symbol)
	assert.Equal(t, 123.0, info.EntryPrice)
}

func TestDryRunClient_GetRecentClosedPnL_NotSupported(t *testing.T) {
	t.Parallel()

	inner := &stubClient{}
	dry := exchange.NewDryRunClient(inner)

	_, err := dry.GetRecentClosedPnL(context.Background(), "BTC", "ord123", time.Time{})
	require.ErrorIs(t, err, exchange.ErrNotSupported)
}

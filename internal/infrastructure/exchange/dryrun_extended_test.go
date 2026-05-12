package exchange_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWsOrderDeal_GetOrderID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		orderID interface{}
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

	_, err := dry.GetOrder(context.Background(), "order_123")
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

func TestDryRunClient_GetFundingRate_Delegates(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)
	_, err := dry.GetFundingRate(context.Background(), "BTC_USDT")
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
	for i := 0; i < 50; i++ {
		id, err := dry.CreateOrder(context.Background(), exchange.SubmitOrderRequest{})
		require.NoError(t, err)
		assert.False(t, ids[id], "duplicate order ID: %s", id)
		ids[id] = true
	}
}

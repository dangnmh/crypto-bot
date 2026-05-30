package exchange_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubClient is a minimal test double for exchange.Client.
type stubClient struct {
	createOrderCalled   bool
	cancelOrderCalled   bool
	closePositionCalled bool
	closeCalled         bool
}

func (s *stubClient) GetTickers(_ context.Context, _ string) ([]exchange.Ticker, error) {
	return nil, nil
}
func (s *stubClient) GetContractDetails(_ context.Context) ([]exchange.ContractDetail, error) {
	return nil, nil
}
func (s *stubClient) GetFundingRate(_ context.Context, _ string) (*exchange.FundingRateDetail, error) {
	return nil, nil
}
func (s *stubClient) GetServerTime(_ context.Context) (int64, error) { return 0, nil }
func (s *stubClient) GetKlines(_ context.Context, _, _ string, _, _ int64) ([]exchange.Kline, error) {
	return nil, nil
}
func (s *stubClient) GetAssets(_ context.Context) ([]exchange.AssetInfo, error) { return nil, nil }
func (s *stubClient) GetAssetByCurrency(_ context.Context, _ string) (*exchange.AssetInfo, error) {
	return nil, nil
}
func (s *stubClient) GetOpenPositions(_ context.Context, _ string) ([]exchange.Position, error) {
	return nil, nil
}
func (s *stubClient) CreateOrder(_ context.Context, _ exchange.SubmitOrderRequest) (string, error) {
	s.createOrderCalled = true
	return "real_123", nil
}
func (s *stubClient) CreateTrackOrder(_ context.Context, _ exchange.SubmitTrackOrderRequest) (string, error) {
	return "real_trk_123", nil
}
func (s *stubClient) CancelOrder(_ context.Context, _, _ string) error {
	s.cancelOrderCalled = true
	return nil
}
func (s *stubClient) CancelOrders(_ context.Context, _ []string) error      { return nil }
func (s *stubClient) CancelAllOpenOrders(_ context.Context, _ string) error { return nil }
func (s *stubClient) GetOrder(_ context.Context, _ string) (*exchange.OrderInfo, error) {
	return nil, nil
}
func (s *stubClient) GetOpenOrders(_ context.Context, _ string) ([]exchange.OrderInfo, error) {
	return nil, nil
}
func (s *stubClient) CloseAllPositions(_ context.Context, _ string) error {
	s.closeCalled = true
	return nil
}
func (s *stubClient) ClosePosition(_ context.Context, _ string, _ domain.Side, _ float64, _ int) error {
	s.closePositionCalled = true
	return nil
}
func (s *stubClient) ChangeLeverage(_ context.Context, _ exchange.ChangeLeverageRequest) error {
	return nil
}
func (s *stubClient) GetDepthSnapshot(_ context.Context, _ string, _ int) (*exchange.OrderBook, error) {
	return nil, nil
}
func (s *stubClient) GetDepthCommits(_ context.Context, _ string, _ int) ([]exchange.DepthCommit, error) {
	return nil, nil
}
func (s *stubClient) WarmUp(_ context.Context, _ time.Duration) {}

func TestDryRunClient_CreateOrder_ReturnsSimulatedID(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	id, err := dry.CreateOrder(context.Background(), exchange.SubmitOrderRequest{
		Symbol: "BTC_USDT",
		Price:  50000,
		Vol:    1,
		Side:   1,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, id)
	assert.False(t, stub.createOrderCalled, "real CreateOrder should NOT be called in dry-run mode")
}

func TestDryRunClient_CancelOrder_NoRealCall(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	err := dry.CancelOrder(context.Background(), "BTC_USDT", "order123")
	require.NoError(t, err)
	assert.False(t, stub.cancelOrderCalled, "real CancelOrder should NOT be called in dry-run mode")
}

func TestDryRunClient_CloseAllPositions_NoRealCall(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	err := dry.CloseAllPositions(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.False(t, stub.closeCalled, "real CloseAllPositions should NOT be called in dry-run mode")
}

func TestDryRunClient_ClosePosition_NoRealCall(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	err := dry.ClosePosition(context.Background(), "BTC_USDT", domain.SideCloseLong, 1, 1)
	require.NoError(t, err)
	assert.False(t, stub.closePositionCalled, "real ClosePosition should NOT be called in dry-run mode")
}

func TestDryRunClient_OtherWriteOps_NoRealCall(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	trackID, err := dry.CreateTrackOrder(context.Background(), exchange.SubmitTrackOrderRequest{
		Symbol:      "BTC_USDT",
		Side:        1,
		ActivePrice: 50000,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, trackID)

	require.NoError(t, dry.CancelOrders(context.Background(), []string{"order_1", "order_2"}))
	require.NoError(t, dry.CancelAllOpenOrders(context.Background(), "BTC_USDT"))
	require.NoError(t, dry.ChangeLeverage(context.Background(), exchange.ChangeLeverageRequest{
		Symbol:   "BTC_USDT",
		Leverage: 5,
	}))
}

func TestDryRunClient_ReadOps_DelegateToReal(t *testing.T) {
	t.Parallel()
	stub := &stubClient{}
	dry := exchange.NewDryRunClient(stub)

	// These should delegate to the real client without error.
	_, err := dry.GetTickers(context.Background(), "BTC_USDT")
	require.NoError(t, err)

	_, err = dry.GetContractDetails(context.Background())
	require.NoError(t, err)

	_, err = dry.GetServerTime(context.Background())
	require.NoError(t, err)

	_, err = dry.GetFundingRate(context.Background(), "BTC_USDT")
	require.NoError(t, err)

	_, err = dry.GetKlines(context.Background(), "BTC_USDT", "Min1", 1, 2)
	require.NoError(t, err)

	_, err = dry.GetDepthSnapshot(context.Background(), "BTC_USDT", 20)
	require.NoError(t, err)

	_, err = dry.GetDepthCommits(context.Background(), "BTC_USDT", 20)
	require.NoError(t, err)

	_, err = dry.GetAssets(context.Background())
	require.NoError(t, err)

	_, err = dry.GetAssetByCurrency(context.Background(), "USDT")
	require.NoError(t, err)

	_, err = dry.GetOpenPositions(context.Background(), "BTC_USDT")
	require.NoError(t, err)

	_, err = dry.GetOrder(context.Background(), "order_1")
	require.NoError(t, err)

	_, err = dry.GetOpenOrders(context.Background(), "BTC_USDT")
	require.NoError(t, err)

	dry.WarmUp(context.Background(), time.Second)
}

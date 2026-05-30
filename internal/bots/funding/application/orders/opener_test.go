package orders_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/orders"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestExternalOrderID(t *testing.T) {
	t.Parallel()

	id := orders.ExternalOrderID("ioc", "BTC_USDT")
	assert.True(t, strings.HasPrefix(id, "ioc_"))
	assert.LessOrEqual(t, len(id), 30)
}

func TestOrderResultIsSuccess(t *testing.T) {
	t.Parallel()

	assert.True(t, (&orders.OrderResult{OrderID: "1"}).IsSuccess())
	assert.False(t, (&orders.OrderResult{}).IsSuccess())
	assert.False(t, (&orders.OrderResult{OrderID: "1", Error: errors.New("x")}).IsSuccess())
}

func TestFireIOC(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	candidate := testCandidate(shared.SideOpenLong)

	clock.EXPECT().GetServerTime().Return(time.Unix(100, 0).UnixMilli())
	clock.EXPECT().Offset().Return(int64(7))
	client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.Equal(t, "BTC_USDT", req.Symbol)
			assert.Equal(t, exchange.OrderTypeIOC, req.Type)
			assert.Equal(t, int(shared.SideOpenLong), req.Side)
			assert.Equal(t, 2.0, req.Vol)
			assert.NotZero(t, req.Price)
			assert.True(t, strings.HasPrefix(req.ExternalOID, "ioc_"))
			return "order-1", nil
		},
	)

	got := orders.FireIOC(context.Background(), client, &candidate, clock, testLogger())
	require.NoError(t, got.Error)
	assert.True(t, got.IsSuccess())
	assert.Equal(t, "order-1", got.OrderID)
	assert.Equal(t, 2.0, got.Volume)
}

func TestFireIOCValidationAndSubmitError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)

	invalid := testCandidate(shared.SideOpenLong)
	invalid.BestAsk = 0
	got := orders.FireIOC(context.Background(), client, &invalid, clock, testLogger())
	require.Error(t, got.Error)

	valid := testCandidate(shared.SideOpenShort)
	clock.EXPECT().GetServerTime().Return(time.Unix(100, 0).UnixMilli())
	clock.EXPECT().Offset().Return(int64(0))
	wantErr := errors.New("exchange down")
	client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("", wantErr)

	got = orders.FireIOC(context.Background(), client, &valid, clock, testLogger())
	require.ErrorIs(t, got.Error, wantErr)
	assert.False(t, got.IsSuccess())
}

func TestFireLimitTrap(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	candidate := testCandidate(shared.SideOpenLong)

	client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.Equal(t, exchange.OrderTypeLimit, req.Type)
			assert.Equal(t, int(shared.SideOpenShort), req.Side)
			assert.True(t, strings.HasPrefix(req.ExternalOID, "trp_"))
			assert.NotZero(t, req.TakeProfitPrice)
			assert.NotZero(t, req.StopLossPrice)
			return "trap-1", nil
		},
	)

	got := orders.FireLimitTrap(context.Background(), client, &candidate, clock, testLogger())
	require.NoError(t, got.Error)
	assert.Equal(t, "trap-1", got.OrderID)
	assert.Equal(t, shared.SideOpenShort, got.Candidate.Side)
}

func TestFireLimitTrapInvalidPrice(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	candidate := testCandidate(shared.SideOpenLong)
	candidate.Config.FundingTrap.DepthPct = 0

	got := orders.FireLimitTrap(context.Background(), client, &candidate, clock, testLogger())
	require.Error(t, got.Error)
}

func testCandidate(side shared.Side) fundingdomain.Candidate {
	closeSide := shared.CloseSideFor(side)
	ref := "bestAsk"
	if side == shared.SideOpenShort {
		ref = "bestBid"
	}
	return fundingdomain.Candidate{
		Config: fundingdomain.TradeConfig{
			Symbol:              "BTC_USDT",
			MaxPriceDiffPercent: 1,
			MarginUSDT:          100,
			Leverage:            10,
			ParsedOpenType:      1,
			ParsedPositionMode:  1,
			FundingReversion: fundingdomain.FundingReversionConfig{
				TakeProfitPct:     0.02,
				StopLossPct:       0.01,
				PostSettleTimeout: types.Duration(time.Second),
			},
			FundingTrap: fundingdomain.FundingTrapConfig{
				Enabled:         true,
				SizeRatio:       0.5,
				MaxNotionalUSDT: 50,
				DepthPct:        0.01,
				TakeProfitPct:   0.02,
				StopLossPct:     0.01,
			},
		},
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:       "BTC_USDT",
			Side:         side,
			CloseSide:    closeSide,
			RefPriceType: ref,
			ExternalID:   "ioc_btc_usdt",
		},
		ContractSpec: fundingdomain.ContractSpec{
			PriceUnit:    0.1,
			PriceScale:   1,
			VolScale:     0,
			MinVol:       1,
			ContractSize: 0.01,
		},
		MarketData: fundingdomain.MarketData{
			LastPrice: 100,
			BestBid:   99.9,
			BestAsk:   100.1,
		},
		TradePlan: fundingdomain.TradePlan{Volume: 2},
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

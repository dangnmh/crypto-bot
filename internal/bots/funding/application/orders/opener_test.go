//nolint:testpackage // These tests exercise unexported helper branches directly.
package orders

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestFireIOCSubmitsOrder(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	candidate := testCandidate(shared.SideOpenShort)

	clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	clock.EXPECT().Offset().Return(int64(0)).AnyTimes()
	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
		DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.Equal(t, "BTC_USDT", req.Symbol)
			assert.Equal(t, int(shared.SideOpenShort), req.Side)
			assert.Equal(t, exchange.OrderTypeIOC, req.Type)
			assert.Equal(t, 10.0, req.Vol)
			assert.Greater(t, req.Price, 0.0)
			assert.Greater(t, req.TakeProfitPrice, 0.0)
			assert.Greater(t, req.StopLossPrice, 0.0)
			return "ioc-1", nil
		})

	res := FireIOC(context.Background(), client, &candidate, clock, discardLogger())

	require.NoError(t, res.Error)
	assert.Equal(t, "ioc-1", res.OrderID)
	assert.True(t, res.IsSuccess())
}

func TestFireIOCReturnsCalcError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	candidate := testCandidate(shared.SideOpenShort)
	candidate.PriceUnit = 0

	res := FireIOC(context.Background(), client, &candidate, clock, discardLogger())

	require.Error(t, res.Error)
	assert.False(t, res.IsSuccess())
}

func TestFireIOCDropsInvalidTakeProfitForLongAndShort(t *testing.T) {
	t.Parallel()

	for _, side := range []shared.Side{shared.SideOpenLong, shared.SideOpenShort} {
		ctrl := gomock.NewController(t)
		client := mocks.NewMockClient(ctrl)
		clock := mocks.NewMockClock(ctrl)
		candidate := testCandidate(side)
		candidate.Config.FundingReversion.TakeProfitPct = 0.001

		clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
		clock.EXPECT().Offset().Return(int64(0)).AnyTimes()
		client.EXPECT().
			CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
			DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
				assert.Zero(t, req.TakeProfitPrice)
				assert.Greater(t, req.StopLossPrice, 0.0)
				return "ioc-drop", nil
			})

		res := FireIOC(context.Background(), client, &candidate, clock, discardLogger())

		require.NoError(t, res.Error)
		assert.Equal(t, "ioc-drop", res.OrderID)
	}
}

func TestFireLimitTrapSubmitsOppositeSideOrder(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	candidate := testCandidate(shared.SideOpenShort)

	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
		DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.Equal(t, int(shared.SideOpenLong), req.Side)
			assert.Equal(t, exchange.OrderTypeLimit, req.Type)
			assert.Greater(t, req.Price, 0.0)
			assert.Greater(t, req.TakeProfitPrice, 0.0)
			assert.Greater(t, req.StopLossPrice, 0.0)
			return "trap-1", nil
		})

	res := FireLimitTrap(context.Background(), client, &candidate, clock, discardLogger())

	require.NoError(t, res.Error)
	assert.Equal(t, "trap-1", res.OrderID)
	assert.Equal(t, shared.SideOpenLong, res.Candidate.Side)
	assert.Equal(t, shared.SideCloseLong, res.Candidate.CloseSide)
}

func TestFireLimitTrapFromLongUsesShortSideAndValidatesVolume(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	candidate := testCandidate(shared.SideOpenLong)

	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.AssignableToTypeOf(exchange.SubmitOrderRequest{})).
		DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.Equal(t, int(shared.SideOpenShort), req.Side)
			assert.Equal(t, exchange.OrderTypeLimit, req.Type)
			return "trap-short", nil
		})

	res := FireLimitTrap(context.Background(), client, &candidate, clock, discardLogger())
	require.NoError(t, res.Error)
	assert.Equal(t, shared.SideOpenShort, res.Candidate.Side)
	assert.Equal(t, shared.SideCloseShort, res.Candidate.CloseSide)

	invalid := testCandidate(shared.SideOpenLong)
	invalid.Config.MarginUSDT = 0
	res = FireLimitTrap(context.Background(), client, &invalid, clock, discardLogger())
	require.Error(t, res.Error)
	assert.Equal(t, "trap volume <= 0", res.Error.Error())
}

func TestFireLimitTrapHandlesInvalidAndExchangeError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)
	invalid := testCandidate(shared.SideOpenShort)
	invalid.BestBid = 0

	res := FireLimitTrap(context.Background(), client, &invalid, clock, discardLogger())
	require.Error(t, res.Error)
	assert.Equal(t, "trap price <= 0", res.Error.Error())

	valid := testCandidate(shared.SideOpenShort)
	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.Any()).
		Return("", errors.New("exchange down"))

	res = FireLimitTrap(context.Background(), client, &valid, clock, discardLogger())
	require.Error(t, res.Error)
	assert.Equal(t, "exchange down", res.Error.Error())
}

func TestCalcSpreadPctHandlesZeroBid(t *testing.T) {
	t.Parallel()

	assert.Zero(t, calcSpreadPct(0, 100))
}

func testCandidate(side shared.Side) fundingdomain.Candidate {
	closeSide := shared.CloseSideFor(side)
	return fundingdomain.Candidate{
		Config: fundingdomain.TradeConfig{
			Symbol:              "BTC_USDT",
			MaxPriceDiffPercent: 0.2,
			MarginUSDT:          100,
			Leverage:            5,
			ParsedOpenType:      exchange.OpenTypeIsolated,
			ParsedPositionMode:  1,
			FundingReversion: fundingdomain.FundingReversionConfig{
				TakeProfitPct: 0.01,
				StopLossPct:   0.01,
			},
			FundingTrap: fundingdomain.FundingTrapConfig{
				SizeRatio:     0.5,
				DepthPct:      0.01,
				TakeProfitPct: 0.01,
				StopLossPct:   0.01,
			},
		},
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:    "BTC_USDT",
			Side:      side,
			CloseSide: closeSide,
		},
		ContractSpec: fundingdomain.ContractSpec{
			PriceUnit:    0.1,
			PriceScale:   1,
			VolScale:     0,
			MinVol:       1,
			ContractSize: 1,
		},
		MarketData: fundingdomain.MarketData{
			LastPrice: 100,
			BestBid:   100,
			BestAsk:   101,
		},
		TradePlan: fundingdomain.TradePlan{
			Volume: 10,
		},
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

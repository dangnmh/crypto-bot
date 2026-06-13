package orders_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
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

	settleTime := time.Date(2026, 6, 9, 11, 4, 1, 0, time.UTC)
	id := orders.ExternalOrderID("BTC_USDT", settleTime, "bybit")
	assert.Equal(t, "BTCUSDT09062026180401BYBIT", id)
	assert.LessOrEqual(t, len(id), 32)

	idLong := orders.ExternalOrderID("ALONGANDVERYCOMPLEXSYMBOLNAMEHERE", settleTime, "bybit")
	assert.Equal(t, "ALONGANDVERYCOMPLEXSYMBOLNAMEHER", idLong)
	assert.Equal(t, 32, len(idLong))

	idGate := orders.ExternalOrderID("ALONGANDVERYCOMPLEXSYMBOLNAMEHERE", settleTime, "gate")
	assert.Equal(t, "ALONGANDVERYCOMPLEXSYMBOLNAM", idGate)
	assert.Equal(t, 28, len(idGate))
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
		func(_ context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
			assert.Equal(t, "BTC_USDT", req.Symbol)
			assert.Equal(t, exchange.OrderTypeIOC, req.Type)
			assert.Equal(t, shared.SideOpenLong, req.Side)
			assert.Equal(t, 2.0, req.Vol)
			assert.NotZero(t, req.Price)
			expectedOID := orders.ExternalOrderID(candidate.Symbol, candidate.SettleTime, candidate.Config.Exchange)
			assert.Equal(t, expectedOID, req.ExternalOID)
			return exchange.CreateOrderResult{OrderID: "order-1", TPSLSubmitted: false}, nil
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
	client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return(exchange.CreateOrderResult{}, wantErr)

	got = orders.FireIOC(context.Background(), client, &valid, clock, testLogger())
	require.ErrorIs(t, got.Error, wantErr)
	assert.False(t, got.IsSuccess())
}

func testCandidate(side shared.Side) fundingdomain.Candidate {
	closeSide := shared.CloseSideFor(side)
	ref := "bestAsk"
	if side == shared.SideOpenShort {
		ref = "bestBid"
	}
	settleTime := time.Date(2026, 6, 9, 11, 4, 1, 0, time.UTC)
	return fundingdomain.Candidate{
		Config: fundingdomain.TradeConfig{
			Symbol:              "BTC_USDT",
			Exchange:            "bybit",
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
		},
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:       "BTC_USDT",
			Side:         side,
			CloseSide:    closeSide,
			RefPriceType: ref,
			ExternalID:   orders.ExternalOrderID("BTC_USDT", settleTime, "bybit"),
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
		TradePlan:  fundingdomain.TradePlan{Volume: 2},
		SettleTime: settleTime,
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

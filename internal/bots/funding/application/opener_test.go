package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestOrderResultIsSuccess(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		give application.OrderResult
		want bool
	}{
		{
			name: "Success",
			give: application.OrderResult{OrderID: "12345"},
			want: true,
		},
		{
			name: "Error present",
			give: application.OrderResult{OrderID: "12345", Error: errors.New("API error")},
			want: false,
		},
		{
			name: "Empty OrderID",
			give: application.OrderResult{},
			want: false,
		},
		{
			name: "Empty OrderID and Error",
			give: application.OrderResult{Error: errors.New("API error")},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.give.IsSuccess())
		})
	}
}

func TestFireIOC_ReturnsSubmittedPrices(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:             "BTC_USDT",
			Leverage:           5,
			ParsedOpenType:     exchange.OpenTypeIsolated,
			ParsedPositionMode: 1,
			FundingReversion: domain.FundingReversionConfig{
				TakeProfitPct: 0.03,
				StopLossPct:   0.03,
			},
		},
		TradeIntent: domain.TradeIntent{
			Symbol:    "BTC_USDT",
			Side:      shared.SideOpenShort,
			CloseSide: shared.SideCloseShort,
		},
		MarketData: domain.MarketData{
			LastPrice: 100.5,
			BestBid:   100,
			BestAsk:   101,
		},
		ContractSpec: domain.ContractSpec{
			PriceUnit:  0.01,
			PriceScale: 2,
			VolScale:   0,
		},
		TradePlan: domain.TradePlan{Volume: 1},
	}

	clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli())
	clock.EXPECT().Offset().Return(int64(0))
	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.InDelta(t, 99.98, req.Price, 1e-9)
			assert.InDelta(t, 97.00, req.TakeProfitPrice, 1e-9)
			assert.InDelta(t, 103.00, req.StopLossPrice, 1e-9)
			return "ioc-1", nil
		})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := application.FireIOC(context.Background(), client, &candidate, clock, logger)

	require.True(t, result.IsSuccess())
	assert.InDelta(t, 99.98, result.Price, 1e-9)
	assert.InDelta(t, 97.00, result.TakeProfitPrice, 1e-9)
	assert.InDelta(t, 103.00, result.StopLossPrice, 1e-9)
	assert.InDelta(t, 1.0, result.Volume, 1e-9)
}

func TestFireLimitTrap_SubmitsTPAndSL(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	clock := mocks.NewMockClock(ctrl)

	candidate := domain.Candidate{
		Config: domain.TradeConfig{
			Symbol:             "BTC_USDT",
			Leverage:           5,
			ParsedOpenType:     exchange.OpenTypeIsolated,
			ParsedPositionMode: 1,
			FundingTrap: domain.FundingTrapConfig{
				DepthPct:      0.025,
				TakeProfitPct: 0.015,
				StopLossPct:   0.015,
			},
		},
		TradeIntent: domain.TradeIntent{
			Symbol:    "BTC_USDT",
			Side:      shared.SideOpenLong,
			CloseSide: shared.SideCloseLong,
		},
		MarketData: domain.MarketData{
			BestAsk: 101,
		},
		ContractSpec: domain.ContractSpec{
			PriceUnit:  0.01,
			PriceScale: 2,
			VolScale:   0,
		},
		TradePlan: domain.TradePlan{Volume: 1},
	}

	client.EXPECT().
		CreateOrder(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, req exchange.SubmitOrderRequest) (string, error) {
			assert.Equal(t, int(shared.SideOpenShort), req.Side)
			assert.InDelta(t, 103.53, req.Price, 1e-9)
			assert.InDelta(t, 101.98, req.TakeProfitPrice, 1e-9)
			assert.InDelta(t, 105.08, req.StopLossPrice, 1e-9)
			return "trap-1", nil
		})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	result := application.FireLimitTrap(context.Background(), client, &candidate, clock, logger)

	require.True(t, result.IsSuccess())
	assert.Equal(t, shared.SideOpenShort, result.Candidate.Side)
	assert.InDelta(t, 103.53, result.Price, 1e-9)
	assert.InDelta(t, 101.98, result.TakeProfitPrice, 1e-9)
	assert.InDelta(t, 105.08, result.StopLossPrice, 1e-9)
}

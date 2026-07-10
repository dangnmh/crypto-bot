package application_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	exchange "crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

type mockReportRepo struct {
	pendingPreReports   []domain.SymbolFundingReport
	pendingAfterReports []domain.SymbolFundingReport
	preFetchedUpdates   map[uint]bool
	afterFetchedUpdates map[uint]bool
	err                 error
}

func (m *mockReportRepo) SaveBatch(ctx context.Context, reports []domain.SymbolFundingReport) error {
	return nil
}

func (m *mockReportRepo) GetPendingPreFunding(ctx context.Context, settleTimeThreshold time.Time) ([]domain.SymbolFundingReport, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pendingPreReports, nil
}

func (m *mockReportRepo) GetPendingAfterFunding(ctx context.Context, settleTimeThreshold time.Time) ([]domain.SymbolFundingReport, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pendingAfterReports, nil
}

func (m *mockReportRepo) UpdatePreFunding(ctx context.Context, id uint, fetched bool) error {
	m.preFetchedUpdates[id] = fetched
	return m.err
}

func (m *mockReportRepo) UpdateAfterFunding(ctx context.Context, id uint, fetched bool) error {
	m.afterFetchedUpdates[id] = fetched
	return m.err
}

type mockTickRepo struct {
	savedTicks []domain.FundingPriceTick
	err        error
}

func (m *mockTickRepo) SaveBatch(ctx context.Context, ticks []domain.FundingPriceTick) error {
	m.savedTicks = append(m.savedTicks, ticks...)
	return m.err
}

func (m *mockTickRepo) GetTicksForSettle(ctx context.Context, exchangeName, symbol string, settleTime time.Time) ([]domain.FundingPriceTick, error) {
	return nil, nil
}

type mockKlineClient struct {
	*mocks.MockClient
	fetchKlinesFn func(ctx context.Context, symbol string, interval exchange.Interval, startTime, endTime time.Time) ([]shared.Kline, error)
}

func (m *mockKlineClient) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, startTime, endTime time.Time) ([]shared.Kline, error) {
	if m.fetchKlinesFn != nil {
		return m.fetchKlinesFn(ctx, symbol, interval, startTime, endTime)
	}
	return nil, nil
}

func TestPriceTrackJob_TrackPrePrices_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	settleTime := time.Now().Truncate(time.Hour)
	reports := []domain.SymbolFundingReport{
		{
			ID:         101,
			Exchange:   "mexc",
			Symbol:     "BTC-USDT-SWAP",
			SettleTime: settleTime,
		},
	}

	reportRepo := &mockReportRepo{
		pendingPreReports: reports,
		preFetchedUpdates: make(map[uint]bool),
	}
	tickRepo := &mockTickRepo{}

	baseMock := mocks.NewMockClient(ctrl)
	mockClient := &mockKlineClient{
		MockClient: baseMock,
		fetchKlinesFn: func(ctx context.Context, symbol string, interval exchange.Interval, startTime, endTime time.Time) ([]shared.Kline, error) {
			assert.Equal(t, exchange.Interval1m, interval)
			assert.Equal(t, "BTC-USDT-SWAP", symbol)
			assert.Equal(t, settleTime.Add(-20*time.Minute), startTime)
			assert.Equal(t, settleTime, endTime)
			return []shared.Kline{
				{Timestamp: settleTime.Add(-10 * time.Minute).UnixMilli(), Close: 60100.0},
				{Timestamp: settleTime.UnixMilli(), Close: 60200.0},
			}, nil
		},
	}

	engine := &infraapp.Engine{
		Providers: map[string]*infraapp.ExchangeProvider{
			"mexc": {
				Name:   "mexc",
				Client: mockClient,
			},
		},
	}

	sysCfg := &fundingconfig.SystemConfig{}
	job := application.NewPriceTrackJob(reportRepo, sysCfg, engine, tickRepo, nil, slog.Default())

	job.TrackPrePrices(context.Background())

	assert.Equal(t, 2, len(tickRepo.savedTicks))
	assert.True(t, reportRepo.preFetchedUpdates[101])
}

func TestPriceTrackJob_TrackPrePrices_Fallback(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	settleTime := time.Now().Truncate(time.Hour)
	reports := []domain.SymbolFundingReport{
		{
			ID:         102,
			Exchange:   "mexc",
			Symbol:     "ETH-USDT-SWAP",
			SettleTime: settleTime,
		},
	}

	reportRepo := &mockReportRepo{
		pendingPreReports: reports,
		preFetchedUpdates: make(map[uint]bool),
	}
	tickRepo := &mockTickRepo{}

	baseMock := mocks.NewMockClient(ctrl)
	mockClient := &mockKlineClient{
		MockClient: baseMock,
		fetchKlinesFn: func(ctx context.Context, symbol string, interval exchange.Interval, startTime, endTime time.Time) ([]shared.Kline, error) {
			// Return ticks, but none at exactly settleTime
			return []shared.Kline{
				{Timestamp: settleTime.Add(-5 * time.Minute).UnixMilli(), Close: 3000.0},
				{Timestamp: settleTime.Add(-1 * time.Minute).UnixMilli(), Close: 3050.0},
			}, nil
		},
	}

	engine := &infraapp.Engine{
		Providers: map[string]*infraapp.ExchangeProvider{
			"mexc": {
				Name:   "mexc",
				Client: mockClient,
			},
		},
	}

	sysCfg := &fundingconfig.SystemConfig{}
	job := application.NewPriceTrackJob(reportRepo, sysCfg, engine, tickRepo, nil, slog.Default())

	job.TrackPrePrices(context.Background())

	assert.True(t, reportRepo.preFetchedUpdates[102])
}

func TestPriceTrackJob_TrackPrePrices_UnsupportedError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	settleTime := time.Now().Truncate(time.Hour)
	reports := []domain.SymbolFundingReport{
		{
			ID:         103,
			Exchange:   "unsupported-exchange",
			Symbol:     "SOL-USDT",
			SettleTime: settleTime,
		},
	}

	reportRepo := &mockReportRepo{
		pendingPreReports: reports,
		preFetchedUpdates: make(map[uint]bool),
	}
	tickRepo := &mockTickRepo{}

	engine := &infraapp.Engine{
		Providers: map[string]*infraapp.ExchangeProvider{},
	}

	sysCfg := &fundingconfig.SystemConfig{}
	job := application.NewPriceTrackJob(reportRepo, sysCfg, engine, tickRepo, nil, slog.Default())

	job.TrackPrePrices(context.Background())

	assert.True(t, reportRepo.preFetchedUpdates[103])
}

func TestPriceTrackJob_TrackAfterPrices_Success(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	settleTime := time.Now().Truncate(time.Hour)
	reports := []domain.SymbolFundingReport{
		{
			ID:         201,
			Exchange:   "mexc",
			Symbol:     "BTC-USDT-SWAP",
			SettleTime: settleTime,
		},
	}

	reportRepo := &mockReportRepo{
		pendingAfterReports: reports,
		afterFetchedUpdates: make(map[uint]bool),
	}
	tickRepo := &mockTickRepo{}

	baseMock := mocks.NewMockClient(ctrl)
	mockClient := &mockKlineClient{
		MockClient: baseMock,
		fetchKlinesFn: func(ctx context.Context, symbol string, interval exchange.Interval, startTime, endTime time.Time) ([]shared.Kline, error) {
			assert.Equal(t, exchange.Interval1m, interval)
			assert.Equal(t, "BTC-USDT-SWAP", symbol)
			assert.Equal(t, settleTime, startTime)
			assert.Equal(t, settleTime.Add(20*time.Minute), endTime)
			return []shared.Kline{
				{Timestamp: settleTime.UnixMilli(), Close: 60200.0},
				{Timestamp: settleTime.Add(20 * time.Minute).UnixMilli(), Close: 60300.0},
			}, nil
		},
	}

	engine := &infraapp.Engine{
		Providers: map[string]*infraapp.ExchangeProvider{
			"mexc": {
				Name:   "mexc",
				Client: mockClient,
			},
		},
	}

	sysCfg := &fundingconfig.SystemConfig{}
	job := application.NewPriceTrackJob(reportRepo, sysCfg, engine, tickRepo, nil, slog.Default())

	job.TrackAfterPrices(context.Background())

	assert.Equal(t, 2, len(tickRepo.savedTicks))
	assert.True(t, reportRepo.afterFetchedUpdates[201])
}

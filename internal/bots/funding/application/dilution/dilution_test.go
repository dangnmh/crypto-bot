package dilution_test

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/dilution"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/internal/trading/ordermanager/futures"
	"crypto-bot/pkg/types"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type mockEngineGetter struct {
	prov *infraapp.AccountProvider
}

func testConfigWithDilution(dil *fundingconfig.DilutionConfig) *fundingconfig.Config {
	return &fundingconfig.Config{
		Accounts: map[string]*fundingconfig.AccountBotConfig{
			"mexc_main": {
				Account:  sysconfig.AccountConfig{ID: "mexc_main", Exchange: "mexc_futures"},
				Dilution: dil,
			},
		},
	}
}

func (m *mockEngineGetter) GetAccountProvider(name string) (*infraapp.AccountProvider, error) {
	return m.prov, nil
}

type cancelCall struct {
	AccountID string
	Exchange  string
	Symbol    string
}

type mockDispatcher struct {
	dispatched []futures.OrderEvent
	canceled   []cancelCall
}

func (m *mockDispatcher) Dispatch(ctx context.Context, intent futures.OrderEvent) error {
	m.dispatched = append(m.dispatched, intent)
	return nil
}

func (m *mockDispatcher) CancelOpenOrders(ctx context.Context, accountID, exchangeName, symbol string) error {
	m.canceled = append(m.canceled, cancelCall{
		AccountID: accountID,
		Exchange:  exchangeName,
		Symbol:    symbol,
	})
	return nil
}

type mockClock struct {
	now time.Time
}

func (m mockClock) Now() time.Time                                   { return m.now }
func (m mockClock) Until(t time.Time) time.Duration                  { return t.Sub(m.now) }
func (m mockClock) GetServerTime() int64                             { return m.now.UnixMilli() }
func (m mockClock) LatencyMs() int64                                 { return 5 }
func (m mockClock) Offset() int64                                    { return 0 }
func (m mockClock) IsHealthy() bool                                  { return true }
func (m mockClock) MsUntilTarget(target int64) int64                 { return target - m.now.UnixMilli() }
func (m mockClock) Sleep(ctx context.Context, d time.Duration) error { return nil }

func TestDilutionMaker_GenerateQuotes_FlatPosition(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)

	mockClient.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{
			Symbol:       "BTC_USDT",
			ContractSize: 1.0,
			MinVol:       1,
			VolScale:     3,
			PriceUnit:    0.1,
			PriceScale:   1,
		},
	}, nil).AnyTimes()

	mockClient.EXPECT().GetTickers(gomock.Any(), "BTC_USDT").Return([]exchange.Ticker{
		{
			Symbol:    "BTC_USDT",
			Bid1:      50000.0,
			Ask1:      50000.1,
			LastPrice: 50000.0,
		},
	}, nil).AnyTimes()

	engineGetter := &mockEngineGetter{
		prov: &infraapp.AccountProvider{
			Client: mockClient,
		},
	}

	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)

	cfg := fundingconfig.ExchangeDilutionCfg{
		Enabled:              true,
		Symbol:               "BTC_USDT",
		MaxPositionUSD:       1000,
		Leverage:             20,
		MarginUSD:            25,
		PositionCloseTimeout: types.Duration(180 * time.Second),
		SpreadOffsetTicks:    0,
	}

	// Case 1: Flat position -> generates both Buy (SideOpenLong) and Sell (SideOpenShort)
	specs, err := maker.GenerateQuotes(context.Background(), "mexc_main", "mexc_futures", cfg, dilution.PositionSummary{})
	require.NoError(t, err)
	require.Len(t, specs, 2)

	for _, s := range specs {
		_, parseErr := uuid.Parse(s.ReqID)
		require.NoError(t, parseErr, "expected ReqID to be a valid UUID")
		expectedLen := exchange.MaxClientOrderIDLength(s.Exchange)
		assert.Len(t, s.ClientOrderID, expectedLen)
		assert.True(t, regexp.MustCompile(fmt.Sprintf(`^[a-zA-Z0-9]{%d}$`, expectedLen)).MatchString(s.ClientOrderID), "expected ClientOrderID to be alphanumeric")
	}
	assert.NotEqual(t, specs[0].ClientOrderID, specs[1].ClientOrderID, "quotes should have distinct client order IDs")

	assert.Equal(t, "mexc_main", specs[0].AccountID)
	assert.Equal(t, shared.SideOpenLong, specs[0].Side)
	assert.Equal(t, 50000.0, specs[0].Price)
	assert.Equal(t, futures.OrderTypePostOnly, specs[0].OrderType)

	assert.Equal(t, shared.SideOpenShort, specs[1].Side)
	assert.Equal(t, 50000.1, specs[1].Price)
	assert.Equal(t, futures.OrderTypePostOnly, specs[1].OrderType)
}

func TestDilutionMaker_GenerateQuotes_WithTPSL(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)

	mockClient.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{
			Symbol:       "BTC_USDT",
			ContractSize: 1.0,
			MinVol:       1,
			VolScale:     3,
			PriceUnit:    0.1,
			PriceScale:   1,
		},
	}, nil).AnyTimes()

	mockClient.EXPECT().GetTickers(gomock.Any(), "BTC_USDT").Return([]exchange.Ticker{
		{
			Symbol:    "BTC_USDT",
			Bid1:      50000.0,
			Ask1:      50000.1,
			LastPrice: 50000.0,
		},
	}, nil).AnyTimes()

	engineGetter := &mockEngineGetter{
		prov: &infraapp.AccountProvider{
			Client: mockClient,
		},
	}

	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)

	cfg := fundingconfig.ExchangeDilutionCfg{
		Enabled:              true,
		Symbol:               "BTC_USDT",
		MaxPositionUSD:       1000,
		Leverage:             20,
		MarginUSD:            25,
		PositionCloseTimeout: types.Duration(180 * time.Second),
		TakeProfitPct:        0.2, // +0.2%
		StopLossPct:          0.5, // -0.5%
		SpreadOffsetTicks:    0,
	}

	specs, err := maker.GenerateQuotes(context.Background(), "mexc_main", "mexc_futures", cfg, dilution.PositionSummary{})
	require.NoError(t, err)
	require.Len(t, specs, 2)
	assert.Equal(t, "mexc_main", specs[0].AccountID)

	// Long open: Price 50000.0, TP = 50000 * 1.002 = 50100.0, SL = 50000 * 0.995 = 49750.0
	assert.Equal(t, shared.SideOpenLong, specs[0].Side)
	assert.Equal(t, 50000.0, specs[0].Price)
	assert.Equal(t, 50100.0, specs[0].TakeProfitPrice)
	assert.Equal(t, 49750.0, specs[0].StopLossPrice)

	// Short open: Price 50000.1, TP = 50000.1 * 0.998 = 49900.0, SL = 50000.1 * 1.005 = 50250.2
	assert.Equal(t, shared.SideOpenShort, specs[1].Side)
	assert.Equal(t, 50000.1, specs[1].Price)
	assert.True(t, specs[1].TakeProfitPrice < specs[1].Price)
	assert.True(t, specs[1].StopLossPrice > specs[1].Price)
}

func TestDilutionMaker_GenerateQuotes_LongPosition(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)

	mockClient.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{
			Symbol:       "BTC_USDT",
			ContractSize: 1.0,
			MinVol:       1,
			VolScale:     3,
			PriceUnit:    0.1,
			PriceScale:   1,
		},
	}, nil).AnyTimes()

	mockClient.EXPECT().GetTickers(gomock.Any(), "BTC_USDT").Return([]exchange.Ticker{
		{
			Symbol:    "BTC_USDT",
			Bid1:      50000.0,
			Ask1:      50000.1,
			LastPrice: 50000.0,
		},
	}, nil).AnyTimes()

	engineGetter := &mockEngineGetter{
		prov: &infraapp.AccountProvider{
			Client: mockClient,
		},
	}

	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)

	cfg := fundingconfig.ExchangeDilutionCfg{
		Enabled:              true,
		Symbol:               "BTC_USDT",
		MaxPositionUSD:       1000,
		Leverage:             20,
		MarginUSD:            25,
		PositionCloseTimeout: types.Duration(180 * time.Second),
		SpreadOffsetTicks:    0,
	}

	// Long position -> Must quote SideCloseLong at BestAsk
	specs, err := maker.GenerateQuotes(context.Background(), "mexc_main", "mexc_futures", cfg, dilution.PositionSummary{
		LongVol:  0.01,
		LongUSD:  500,
		NetUSD:   500,
		GrossUSD: 500,
	})
	require.NoError(t, err)
	require.Len(t, specs, 1)
	assert.Equal(t, "mexc_main", specs[0].AccountID)
	assert.Equal(t, shared.SideCloseLong, specs[0].Side)
	assert.Equal(t, 50000.1, specs[0].Price)
	assert.Equal(t, 0.01, specs[0].Volume)
}

func TestDilutionMaker_GenerateQuotes_ShortPosition(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)

	mockClient.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{
			Symbol:       "BTC_USDT",
			ContractSize: 1.0,
			MinVol:       1,
			VolScale:     3,
			PriceUnit:    0.1,
			PriceScale:   1,
		},
	}, nil).AnyTimes()

	mockClient.EXPECT().GetTickers(gomock.Any(), "BTC_USDT").Return([]exchange.Ticker{
		{
			Symbol:    "BTC_USDT",
			Bid1:      50000.0,
			Ask1:      50000.1,
			LastPrice: 50000.0,
		},
	}, nil).AnyTimes()

	engineGetter := &mockEngineGetter{
		prov: &infraapp.AccountProvider{
			Client: mockClient,
		},
	}

	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)

	cfg := fundingconfig.ExchangeDilutionCfg{
		Enabled:              true,
		Symbol:               "BTC_USDT",
		MaxPositionUSD:       1000,
		Leverage:             20,
		MarginUSD:            25,
		PositionCloseTimeout: types.Duration(180 * time.Second),
		SpreadOffsetTicks:    0,
	}

	// Short position -> Must quote SideCloseShort at BestBid
	specs, err := maker.GenerateQuotes(context.Background(), "mexc_main", "mexc_futures", cfg, dilution.PositionSummary{
		ShortVol: 0.01,
		ShortUSD: 500,
		NetUSD:   -500,
		GrossUSD: 500,
	})
	require.NoError(t, err)
	require.Len(t, specs, 1)
	assert.Equal(t, "mexc_main", specs[0].AccountID)
	assert.Equal(t, shared.SideCloseShort, specs[0].Side)
	assert.Equal(t, 50000.0, specs[0].Price)
	assert.Equal(t, 0.01, specs[0].Volume)
}

func TestDilutionMaker_GenerateQuotes_DualPosition(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)

	mockClient.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{
			Symbol:       "BTC_USDT",
			ContractSize: 1.0,
			MinVol:       1,
			VolScale:     3,
			PriceUnit:    0.1,
			PriceScale:   1,
		},
	}, nil).AnyTimes()

	mockClient.EXPECT().GetTickers(gomock.Any(), "BTC_USDT").Return([]exchange.Ticker{
		{
			Symbol:    "BTC_USDT",
			Bid1:      50000.0,
			Ask1:      50000.1,
			LastPrice: 50000.0,
		},
	}, nil).AnyTimes()

	engineGetter := &mockEngineGetter{
		prov: &infraapp.AccountProvider{
			Client: mockClient,
		},
	}

	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)

	cfg := fundingconfig.ExchangeDilutionCfg{
		Enabled:              true,
		Symbol:               "BTC_USDT",
		MaxPositionUSD:       1000,
		Leverage:             20,
		MarginUSD:            25,
		PositionCloseTimeout: types.Duration(180 * time.Second),
		SpreadOffsetTicks:    0,
	}

	// Dual position -> Must quote BOTH SideCloseLong and SideCloseShort to liquidate
	specs, err := maker.GenerateQuotes(context.Background(), "mexc_main", "mexc_futures", cfg, dilution.PositionSummary{
		LongVol:  0.01,
		LongUSD:  500,
		ShortVol: 0.01,
		ShortUSD: 500,
		NetUSD:   0,
		GrossUSD: 1000,
	})
	require.NoError(t, err)
	require.Len(t, specs, 2)
	assert.Equal(t, "mexc_main", specs[0].AccountID)
	assert.Equal(t, "mexc_main", specs[1].AccountID)
	assert.Equal(t, shared.SideCloseLong, specs[0].Side)
	assert.Equal(t, 50000.1, specs[0].Price)
	assert.Equal(t, 0.01, specs[0].Volume)

	assert.Equal(t, shared.SideCloseShort, specs[1].Side)
	assert.Equal(t, 50000.0, specs[1].Price)
	assert.Equal(t, 0.01, specs[1].Volume)
}

func TestDilutionJob_Tick_QuotesPositionExit(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)

	startTime := time.Date(2026, 1, 1, 12, 15, 0, 0, time.UTC)
	clock := &mockTimeClock{currentTime: startTime}

	// Ticks: position exists (LongVol: 0.01)
	mockClient.EXPECT().GetOpenPositions(gomock.Any(), "BTC_USDT").Return([]exchange.Position{
		{
			Symbol:          "BTC_USDT",
			PositionType:    exchange.PositionTypeLong,
			HoldVolContract: 0.01,
			HoldAvgPrice:    50000.0,
		},
	}, nil).Times(2)

	mockClient.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{Symbol: "BTC_USDT", ContractSize: 1.0, MinVol: 1, VolScale: 3, PriceUnit: 0.1, PriceScale: 1},
	}, nil).AnyTimes()

	mockClient.EXPECT().GetTickers(gomock.Any(), "BTC_USDT").Return([]exchange.Ticker{
		{Symbol: "BTC_USDT", Bid1: 50000.0, Ask1: 50000.1, LastPrice: 50000.0},
	}, nil).AnyTimes()

	engineGetter := &mockEngineGetter{
		prov: &infraapp.AccountProvider{
			Client: mockClient,
		},
	}

	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)

	dispatcher := &mockDispatcher{}
	runner, err := dilution.NewDilutionRunner(dispatcher, clock, slog.Default())
	require.NoError(t, err)

	rootCfg := testConfigWithDilution(&fundingconfig.DilutionConfig{
		Enabled:              true,
		PollInterval:         types.Duration(10 * time.Second),
		Symbol:               "BTC_USDT",
		MaxPositionUSD:       1000,
		Leverage:             20,
		MarginUSD:            25,
		PositionCloseTimeout: types.Duration(60 * time.Second),
		SpreadOffsetTicks:    0,
	})

	dilCfg, err := rootCfg.DilutionForAccount("mexc_main")
	require.NoError(t, err)
	job, err := dilution.NewDilutionJobForAccount("mexc_main", "mexc_futures", dilCfg, engineGetter, maker, runner, clock, slog.Default())
	require.NoError(t, err)

	// Tick 1 at startTime: cancels stale orders and executes exit quote
	err = job.Tick(context.Background())
	require.NoError(t, err)
	require.Len(t, dispatcher.dispatched, 1)

	// Tick 2: continues exit quote management
	err = job.Tick(context.Background())
	require.NoError(t, err)
	require.Len(t, dispatcher.dispatched, 2)
}

func TestDilutionJob_StartStopLifecycle(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)

	startTime := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	clock := &mockTimeClock{currentTime: startTime}
	engineGetter := &mockEngineGetter{
		prov: &infraapp.AccountProvider{
			Client: mockClient,
		},
	}
	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)
	dispatcher := &mockDispatcher{}
	runner, err := dilution.NewDilutionRunner(dispatcher, clock, slog.Default())
	require.NoError(t, err)

	t.Run("Start when disabled returns nil", func(t *testing.T) {
		t.Parallel()
		rootCfg := testConfigWithDilution(&fundingconfig.DilutionConfig{
			Enabled: false,
		})
		dilCfg, err := rootCfg.DilutionForAccount("mexc_main")
		require.NoError(t, err)
		job, err := dilution.NewDilutionJobForAccount("mexc_main", "mexc_futures", dilCfg, engineGetter, maker, runner, clock, slog.Default())
		require.NoError(t, err)
		err = job.Start(context.Background(), nil)
		require.NoError(t, err)
		err = job.Stop(context.Background())
		require.NoError(t, err)
	})

	t.Run("Start and Stop lifecycle without jitter", func(t *testing.T) {
		t.Parallel()
		rootCfg := testConfigWithDilution(&fundingconfig.DilutionConfig{
			Enabled:      true,
			PollInterval: types.Duration(10 * time.Second),
		})
		dilCfg, err := rootCfg.DilutionForAccount("mexc_main")
		require.NoError(t, err)
		job, err := dilution.NewDilutionJobForAccount("mexc_main", "mexc_futures", dilCfg, engineGetter, maker, runner, clock, slog.Default())
		require.NoError(t, err)
		err = job.Start(context.Background(), nil)
		require.NoError(t, err)
		err = job.Stop(context.Background())
		require.NoError(t, err)
	})

	t.Run("Start and Stop lifecycle with jitter", func(t *testing.T) {
		t.Parallel()
		rootCfg := testConfigWithDilution(&fundingconfig.DilutionConfig{
			Enabled:      true,
			PollInterval: types.Duration(10 * time.Second),
			Jitter:       types.Duration(2 * time.Second),
		})
		dilCfg, err := rootCfg.DilutionForAccount("mexc_main")
		require.NoError(t, err)
		job, err := dilution.NewDilutionJobForAccount("mexc_main", "mexc_futures", dilCfg, engineGetter, maker, runner, clock, slog.Default())
		require.NoError(t, err)
		err = job.Start(context.Background(), nil)
		require.NoError(t, err)
		err = job.Stop(context.Background())
		require.NoError(t, err)
	})
}

type mockTimeClock struct {
	currentTime time.Time
}

func (m *mockTimeClock) Now() time.Time                                   { return m.currentTime }
func (m *mockTimeClock) Until(t time.Time) time.Duration                  { return t.Sub(m.currentTime) }
func (m *mockTimeClock) GetServerTime() int64                             { return m.currentTime.UnixMilli() }
func (m *mockTimeClock) LatencyMs() int64                                 { return 5 }
func (m *mockTimeClock) Offset() int64                                    { return 0 }
func (m *mockTimeClock) IsHealthy() bool                                  { return true }
func (m *mockTimeClock) MsUntilTarget(target int64) int64                 { return target - m.currentTime.UnixMilli() }
func (m *mockTimeClock) Sleep(ctx context.Context, d time.Duration) error { return nil }

func TestDilutionRunner_Execute(t *testing.T) {
	t.Parallel()

	dispatcher := &mockDispatcher{}
	clock := mockClock{now: time.Date(2026, 1, 1, 12, 10, 0, 0, time.UTC)}

	runner, err := dilution.NewDilutionRunner(dispatcher, clock, slog.Default())
	require.NoError(t, err)

	spec := &dilution.DilutionSpec{
		ReqID:                 "test-dilution-req-id",
		ClientOrderID:         "testdilutionclientorderid12345",
		Exchange:              "mexc_futures",
		Symbol:                "BTC_USDT",
		Side:                  shared.SideCloseLong,
		NotionalUSDT:          500,
		MarginUSDT:            25,
		Leverage:              20,
		Price:                 50000,
		Volume:                0.01,
		ContractSize:          1.0,
		PositionCloseTimeout:  180 * time.Second,
		UnfilledCancelTimeout: 60 * time.Second,
		OrderType:             futures.OrderTypePostOnly,
	}

	err = runner.Execute(context.Background(), spec)
	require.NoError(t, err)
	require.Len(t, dispatcher.dispatched, 1)

	evt, ok := dispatcher.dispatched[0].(futures.OrderIntentEvent)
	require.True(t, ok)
	assert.Equal(t, "test-dilution-req-id", evt.ReqID)
	assert.Equal(t, "test-dilution-req-id", evt.RefID)
	assert.Equal(t, "testdilutionclientorderid12345", evt.ClientOrderID)
	assert.Equal(t, "mexc_futures", evt.Exchange)
	assert.Equal(t, "BTC_USDT", evt.Symbol)
	assert.Equal(t, futures.StrategyDilution, evt.StrategyType)
	assert.Equal(t, futures.OrderTypePostOnly, evt.OrderType)
	assert.Equal(t, shared.SideCloseLong, evt.Side)
	assert.Equal(t, 50000.0, evt.Price)

	t.Run("uses provided ReqID and ClientOrderID when present", func(t *testing.T) {
		t.Parallel()
		customDisp := &mockDispatcher{}
		customRunner, err := dilution.NewDilutionRunner(customDisp, clock, slog.Default())
		require.NoError(t, err)

		customSpec := &dilution.DilutionSpec{
			ReqID:         "custom-req-id-123",
			ClientOrderID: "customclientorderid",
			Exchange:      "mexc_futures",
			Symbol:        "BTC_USDT",
			Side:          shared.SideCloseLong,
			Price:         50000,
			ContractSize:  1.0,
		}

		err = customRunner.Execute(context.Background(), customSpec)
		require.NoError(t, err)
		require.Len(t, customDisp.dispatched, 1)

		customEvt, ok := customDisp.dispatched[0].(futures.OrderIntentEvent)
		require.True(t, ok)
		assert.Equal(t, "custom-req-id-123", customEvt.ReqID)
		assert.Equal(t, "custom-req-id-123", customEvt.RefID)
		assert.Equal(t, "customclientorderid", customEvt.ClientOrderID)
	})

	t.Run("fails when ReqID is empty", func(t *testing.T) {
		t.Parallel()
		specMissingReqID := &dilution.DilutionSpec{
			ReqID:         "",
			ClientOrderID: "someclientorderid123",
		}
		err := runner.Execute(context.Background(), specMissingReqID)
		require.ErrorContains(t, err, "ReqID cannot be empty")
	})

	t.Run("fails when ClientOrderID is empty", func(t *testing.T) {
		t.Parallel()
		specMissingCOID := &dilution.DilutionSpec{
			ReqID:         "some-req-id",
			ClientOrderID: "",
		}
		err := runner.Execute(context.Background(), specMissingCOID)
		require.ErrorContains(t, err, "ClientOrderID cannot be empty")
	})
}

func TestNewDilutionJobForAccount_Validation(t *testing.T) {
	t.Parallel()

	engineGetter := &mockEngineGetter{}
	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)
	runner, err := dilution.NewDilutionRunner(&mockDispatcher{}, &mockTimeClock{}, slog.Default())
	require.NoError(t, err)

	dilCfg := &fundingconfig.DilutionConfig{Enabled: true}

	t.Run("missing accountID", func(t *testing.T) {
		t.Parallel()
		job, err := dilution.NewDilutionJobForAccount("", "mexc_futures", dilCfg, engineGetter, maker, runner, &mockTimeClock{}, slog.Default())
		require.ErrorContains(t, err, "missing required accountID")
		require.Nil(t, job)
	})

	t.Run("missing accountExchange", func(t *testing.T) {
		t.Parallel()
		job, err := dilution.NewDilutionJobForAccount("mexc_main", "", dilCfg, engineGetter, maker, runner, &mockTimeClock{}, slog.Default())
		require.ErrorContains(t, err, "missing required accountExchange")
		require.Nil(t, job)
	})

	t.Run("nil dilution config", func(t *testing.T) {
		t.Parallel()
		job, err := dilution.NewDilutionJobForAccount("mexc_main", "mexc_futures", nil, engineGetter, maker, runner, &mockTimeClock{}, slog.Default())
		require.ErrorContains(t, err, "missing required dilution config")
		require.Nil(t, job)
	})

	t.Run("nil dependencies", func(t *testing.T) {
		t.Parallel()
		job, err := dilution.NewDilutionJobForAccount("mexc_main", "mexc_futures", dilCfg, engineGetter, nil, runner, &mockTimeClock{}, slog.Default())
		require.ErrorContains(t, err, "missing required dependencies")
		require.Nil(t, job)
	})
}

func TestNewDilutionJobs_MultipleAccounts(t *testing.T) {
	t.Parallel()

	engineGetter := &mockEngineGetter{}
	maker, err := dilution.NewDilutionMaker(engineGetter)
	require.NoError(t, err)
	runner, err := dilution.NewDilutionRunner(&mockDispatcher{}, &mockTimeClock{}, slog.Default())
	require.NoError(t, err)

	t.Run("nil root config", func(t *testing.T) {
		t.Parallel()
		jobs, err := dilution.NewDilutionJobs(nil, engineGetter, maker, runner, &mockTimeClock{}, slog.Default())
		require.ErrorContains(t, err, "missing required root config")
		require.Nil(t, jobs)
	})

	t.Run("empty accounts", func(t *testing.T) {
		t.Parallel()
		emptyCfg := &fundingconfig.Config{
			Accounts: map[string]*fundingconfig.AccountBotConfig{},
		}
		jobs, err := dilution.NewDilutionJobs(emptyCfg, engineGetter, maker, runner, &mockTimeClock{}, slog.Default())
		require.NoError(t, err)
		require.Nil(t, jobs)
	})

	t.Run("multiple accounts initialization and sorting", func(t *testing.T) {
		t.Parallel()
		cfg := &fundingconfig.Config{
			Accounts: map[string]*fundingconfig.AccountBotConfig{
				"acc_b": {
					Account:  sysconfig.AccountConfig{ID: "acc_b", Exchange: "mexc_futures"},
					Dilution: &fundingconfig.DilutionConfig{Enabled: false},
				},
				"acc_a": {
					Account:  sysconfig.AccountConfig{ID: "acc_a", Exchange: "mexc_futures"},
					Dilution: &fundingconfig.DilutionConfig{Enabled: true},
				},
				"acc_c": {
					Account:  sysconfig.AccountConfig{ID: "acc_c", Exchange: "mexc_futures"},
					Dilution: nil,
				},
			},
		}
		jobs, err := dilution.NewDilutionJobs(cfg, engineGetter, maker, runner, &mockTimeClock{}, slog.Default())
		require.NoError(t, err)
		require.Len(t, jobs, 2)
		assert.Equal(t, "acc_a", jobs[0].AccountID())
		assert.True(t, jobs[0].Enabled())
		assert.Equal(t, "acc_b", jobs[1].AccountID())
		assert.False(t, jobs[1].Enabled())
	})
}

func TestDilutionRunner_CancelOpenOrders(t *testing.T) {
	t.Parallel()

	disp := &mockDispatcher{}
	runner, err := dilution.NewDilutionRunner(disp, &mockTimeClock{}, slog.Default())
	require.NoError(t, err)

	err = runner.CancelOpenOrders(context.Background(), "acc_test", "mexc_futures", "BTC_USDT")
	require.NoError(t, err)

	require.Len(t, disp.canceled, 1)
	assert.Equal(t, "acc_test", disp.canceled[0].AccountID)
	assert.Equal(t, "mexc_futures", disp.canceled[0].Exchange)
	assert.Equal(t, "BTC_USDT", disp.canceled[0].Symbol)
}

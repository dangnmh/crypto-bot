package obfuscator_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/obfuscator"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/trading/ordermanager"
	ordermanagerpersistence "crypto-bot/internal/trading/ordermanager/persistence"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDispatcher struct {
	events []ordermanager.OrderEvent
	err    error
}

func (m *mockDispatcher) Dispatch(ctx context.Context, evt ordermanager.OrderEvent) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, evt)
	return nil
}

type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time {
	if m.now.IsZero() {
		return time.Date(2026, 8, 15, 12, 15, 0, 0, time.UTC)
	}
	return m.now
}

func (m *mockClock) GetServerTime() int64 {
	return m.Now().UnixMilli()
}

func (m *mockClock) Until(target time.Time) time.Duration {
	return target.Sub(m.Now())
}

func (m *mockClock) LatencyMs() int64 {
	return 0
}

func (m *mockClock) Offset() int64 {
	return 0
}

func (m *mockClock) IsHealthy() bool {
	return true
}

func (m *mockClock) MsUntilTarget(targetServerTimeMs int64) int64 {
	return targetServerTimeMs - m.GetServerTime()
}

func (m *mockClock) Sleep(ctx context.Context, d time.Duration) error {
	return nil
}

type mockPnLReader struct {
	records []ordermanagerpersistence.ProfitableTradeRecord
	err     error
}

func (m *mockPnLReader) GetProfitableTradeRecords(ctx context.Context, exchangeName string, threshold float64, since time.Time) ([]ordermanagerpersistence.ProfitableTradeRecord, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.records, nil
}

type mockExchangeClient struct {
	exchange.Client
	ob         *shared.OrderBook
	obErr      error
	detail     *exchange.ContractDetail
	detailErr  error
	tickers    []exchange.Ticker
	tickersErr error
	rates      []exchange.FundingRateResult
	ratesErr   error
}

func (m *mockExchangeClient) GetDepth(ctx context.Context, symbol string) (*shared.OrderBook, error) {
	if m.obErr != nil {
		return nil, m.obErr
	}
	return m.ob, nil
}

func (m *mockExchangeClient) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	if m.detailErr != nil {
		return nil, m.detailErr
	}
	if m.detail != nil {
		return []exchange.ContractDetail{*m.detail}, nil
	}
	return nil, nil
}

func (m *mockExchangeClient) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	if m.tickersErr != nil {
		return nil, m.tickersErr
	}
	return m.tickers, nil
}

func (m *mockExchangeClient) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if m.ratesErr != nil {
		return nil, m.ratesErr
	}
	return m.rates, nil
}

type mockEngineProviderGetter struct {
	client      *mockExchangeClient
	providerErr error
}

func (m *mockEngineProviderGetter) GetProvider(name string) (*infraapp.ExchangeProvider, error) {
	if m.providerErr != nil {
		return nil, m.providerErr
	}
	if m.client == nil {
		return nil, nil
	}
	return &infraapp.ExchangeProvider{
		Name:   name,
		Client: m.client,
	}, nil
}

type mockEventPublisher struct {
	publishedTopics   []string
	publishedPayloads []any
	err               error
}

func (p *mockEventPublisher) Publish(topic string, payload any) error {
	if p.err != nil {
		return p.err
	}
	p.publishedTopics = append(p.publishedTopics, topic)
	p.publishedPayloads = append(p.publishedPayloads, payload)
	return nil
}

func TestOrderGenerator(t *testing.T) {
	t.Parallel()

	ob := &shared.OrderBook{
		Bids: []shared.OrderBookEntry{{Price: 100, Volume: 10}},
		Asks: []shared.OrderBookEntry{{Price: 101, Volume: 5}},
	}
	mockClient := &mockExchangeClient{
		ob: ob,
		detail: &exchange.ContractDetail{
			Symbol:       "ETHUSDT",
			ContractSize: 1.0,
			MinVol:       1,
			VolScale:     2,
			PriceUnit:    0.01,
			PriceScale:   2,
		},
	}
	mockEngine := &mockEngineProviderGetter{client: mockClient}

	gen, err := obfuscator.NewOrderGenerator(mockEngine)
	require.NoError(t, err)

	cfg := fundingconfig.ExchangeObfuscationCfg{
		Enabled:             true,
		NetPnLThresholdUSDT: 5.0,
		VolumeScalePct:      50.0,
		MinUSDT:             10.0,
		MaxUSDT:             100.0,
		MarginUSDT:          5.0,
		TakeProfitPct:       1.5,
		StopLossPct:         1.0,
		MinHoldSec:          10,
		MaxHoldSec:          20,
	}

	record := &ordermanagerpersistence.ProfitableTradeRecord{
		ReqID:      "req-123",
		Exchange:   "binance",
		Symbol:     "ETHUSDT",
		Side:       shared.SideOpenLong,
		NetProfit:  30.0, // 50% = 15.0 USDT
		MarginUSDT: 10.0,
		Leverage:   5,
	}

	spec, err := gen.GenerateSpec(context.Background(), cfg, record)
	require.NoError(t, err)
	assert.Equal(t, "req-123", spec.OriginReqID)
	assert.Equal(t, "binance", spec.Exchange)
	assert.Equal(t, "ETHUSDT", spec.Symbol)
	assert.Equal(t, 15.0, spec.NotionalUSDT)
	assert.Equal(t, 5.0, spec.MarginUSDT)
	assert.Equal(t, shared.SideOpenLong, spec.Side)
	assert.Greater(t, spec.Volume, 0.0)
	assert.Greater(t, spec.Price, 0.0)
	assert.Greater(t, spec.TakeProfitPrice, 0.0)
	assert.Greater(t, spec.StopLossPrice, 0.0)

	t.Run("returns error when engine is nil", func(t *testing.T) {
		t.Parallel()
		_, err := obfuscator.NewOrderGenerator(nil)
		require.Error(t, err)
	})
}

func TestOrderGenerator_DepthScenarios(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		bids       []shared.OrderBookEntry
		asks       []shared.OrderBookEntry
		prevSide   shared.Side
		netProfit  float64
		minUSDT    float64
		maxUSDT    float64
		scalePct   float64
		wantSide   shared.Side
		wantNotion float64
	}{
		{
			name:       "ask volume > bid volume triggers Short side",
			bids:       []shared.OrderBookEntry{{Price: 100, Volume: 5}},
			asks:       []shared.OrderBookEntry{{Price: 101, Volume: 15}},
			prevSide:   shared.SideOpenLong,
			netProfit:  40.0,
			minUSDT:    10.0,
			maxUSDT:    100.0,
			scalePct:   50.0,
			wantSide:   shared.SideOpenShort,
			wantNotion: 20.0,
		},
		{
			name:       "equal volume triggers fallback inverse side of Long to Short",
			bids:       []shared.OrderBookEntry{{Price: 100, Volume: 10}},
			asks:       []shared.OrderBookEntry{{Price: 101, Volume: 10}},
			prevSide:   shared.SideOpenLong,
			netProfit:  40.0,
			minUSDT:    10.0,
			maxUSDT:    100.0,
			scalePct:   50.0,
			wantSide:   shared.SideOpenShort,
			wantNotion: 20.0,
		},
		{
			name:       "equal volume triggers fallback inverse side of Short to Long",
			bids:       []shared.OrderBookEntry{{Price: 100, Volume: 10}},
			asks:       []shared.OrderBookEntry{{Price: 101, Volume: 10}},
			prevSide:   shared.SideOpenShort,
			netProfit:  40.0,
			minUSDT:    10.0,
			maxUSDT:    100.0,
			scalePct:   50.0,
			wantSide:   shared.SideOpenLong,
			wantNotion: 20.0,
		},
		{
			name:       "notional clamped to MinUSDT when scaled profit is small",
			bids:       []shared.OrderBookEntry{{Price: 100, Volume: 10}},
			asks:       []shared.OrderBookEntry{{Price: 101, Volume: 5}},
			prevSide:   shared.SideOpenLong,
			netProfit:  2.0, // 50% = 1.0 < MinUSDT 10.0
			minUSDT:    10.0,
			maxUSDT:    100.0,
			scalePct:   50.0,
			wantSide:   shared.SideOpenLong,
			wantNotion: 10.0,
		},
		{
			name:       "notional clamped to MaxUSDT when scaled profit is large",
			bids:       []shared.OrderBookEntry{{Price: 100, Volume: 10}},
			asks:       []shared.OrderBookEntry{{Price: 101, Volume: 5}},
			prevSide:   shared.SideOpenLong,
			netProfit:  1000.0, // 50% = 500.0 > MaxUSDT 100.0
			minUSDT:    10.0,
			maxUSDT:    100.0,
			scalePct:   50.0,
			wantSide:   shared.SideOpenLong,
			wantNotion: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mockClient := &mockExchangeClient{
				ob: &shared.OrderBook{Bids: tt.bids, Asks: tt.asks},
				detail: &exchange.ContractDetail{
					Symbol:       "BTCUSDT",
					ContractSize: 1.0,
					PriceUnit:    0.01,
					PriceScale:   2,
					MinVol:       1,
					VolScale:     3,
				},
			}
			mockEngine := &mockEngineProviderGetter{client: mockClient}
			gen, err := obfuscator.NewOrderGenerator(mockEngine)
			require.NoError(t, err)

			cfg := fundingconfig.ExchangeObfuscationCfg{
				Enabled:             true,
				NetPnLThresholdUSDT: 5.0,
				VolumeScalePct:      tt.scalePct,
				MinUSDT:             tt.minUSDT,
				MaxUSDT:             tt.maxUSDT,
				MarginUSDT:          5.0,
				TakeProfitPct:       1.0,
				StopLossPct:         1.0,
				MinHoldSec:          10,
				MaxHoldSec:          20,
			}
			record := &ordermanagerpersistence.ProfitableTradeRecord{
				ReqID:     "req-scenario",
				Exchange:  "mexc",
				Symbol:    "BTCUSDT",
				Side:      tt.prevSide,
				NetProfit: tt.netProfit,
			}

			spec, err := gen.GenerateSpec(context.Background(), cfg, record)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSide, spec.Side)
			assert.Equal(t, tt.wantNotion, spec.NotionalUSDT)
		})
	}
}

func TestOrderGenerator_EdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("provider not found gracefully fallbacks", func(t *testing.T) {
		t.Parallel()
		mockEngine := &mockEngineProviderGetter{providerErr: errors.New("provider not found")}
		gen, err := obfuscator.NewOrderGenerator(mockEngine)
		require.NoError(t, err)

		cfg := fundingconfig.ExchangeObfuscationCfg{
			Enabled:    true,
			MinUSDT:    10.0,
			MaxUSDT:    50.0,
			MarginUSDT: 5.0,
			MinHoldSec: 5,
			MaxHoldSec: 5,
		}
		record := &ordermanagerpersistence.ProfitableTradeRecord{
			ReqID:    "req-noprov",
			Exchange: "unknown",
			Symbol:   "BTCUSDT",
			Side:     shared.SideOpenLong,
		}
		spec, err := gen.GenerateSpec(context.Background(), cfg, record)
		require.NoError(t, err)
		assert.Equal(t, shared.SideOpenShort, spec.Side)
		assert.Equal(t, 10.0, spec.NotionalUSDT)
	})

	t.Run("orderbook error gracefully fallbacks", func(t *testing.T) {
		t.Parallel()
		mockClient := &mockExchangeClient{
			obErr: errors.New("depth error"),
			detail: &exchange.ContractDetail{
				Symbol:       "BTCUSDT",
				ContractSize: 1.0,
				PriceUnit:    0.01,
			},
		}
		mockEngine := &mockEngineProviderGetter{client: mockClient}
		gen, err := obfuscator.NewOrderGenerator(mockEngine)
		require.NoError(t, err)

		cfg := fundingconfig.ExchangeObfuscationCfg{
			Enabled:    true,
			MinUSDT:    10.0,
			MaxUSDT:    50.0,
			MarginUSDT: 5.0,
			MinHoldSec: 5,
			MaxHoldSec: 5,
		}
		record := &ordermanagerpersistence.ProfitableTradeRecord{
			ReqID:    "req-oberr",
			Exchange: "binance",
			Symbol:   "BTCUSDT",
			Side:     shared.SideOpenLong,
		}
		spec, err := gen.GenerateSpec(context.Background(), cfg, record)
		require.NoError(t, err)
		assert.Equal(t, shared.SideOpenShort, spec.Side)
	})

	t.Run("contract details cached properly", func(t *testing.T) {
		t.Parallel()
		mockClient := &mockExchangeClient{
			detail: &exchange.ContractDetail{
				Symbol:       "BTCUSDT",
				ContractSize: 2.5,
				PriceUnit:    0.05,
			},
		}
		mockEngine := &mockEngineProviderGetter{client: mockClient}
		gen, err := obfuscator.NewOrderGenerator(mockEngine)
		require.NoError(t, err)

		cfg := fundingconfig.ExchangeObfuscationCfg{
			Enabled:    true,
			MinUSDT:    10.0,
			MaxUSDT:    50.0,
			MarginUSDT: 5.0,
			MinHoldSec: 5,
			MaxHoldSec: 5,
		}
		record := &ordermanagerpersistence.ProfitableTradeRecord{
			ReqID:    "req-cache",
			Exchange: "bybit",
			Symbol:   "BTCUSDT",
		}
		spec1, err := gen.GenerateSpec(context.Background(), cfg, record)
		require.NoError(t, err)
		assert.Equal(t, 2.5, spec1.ContractSize)

		// Second call uses cached contract detail
		spec2, err := gen.GenerateSpec(context.Background(), cfg, record)
		require.NoError(t, err)
		assert.Equal(t, 2.5, spec2.ContractSize)
	})

	t.Run("uses live ticker price when depth provider is unavailable", func(t *testing.T) {
		t.Parallel()
		mockClient := &mockExchangeClient{
			detail: &exchange.ContractDetail{
				Symbol:       "BLUR-SWAP-USDT",
				ContractSize: 1.0,
				PriceUnit:    0.0001,
				PriceScale:   4,
				MinVol:       1,
				VolScale:     0,
			},
			tickers: []exchange.Ticker{
				{
					Symbol:       "BLUR-SWAP-USDT",
					LastPrice:    0.20,
					Bid1:         0.199,
					Ask1:         0.201,
					AmountUSDT24: 500000.0,
				},
			},
			rates: []exchange.FundingRateResult{
				{
					Symbol: "BLUR-SWAP-USDT",
					Rate:   -0.0015,
				},
			},
		}
		mockEngine := &mockEngineProviderGetter{client: mockClient}
		gen, err := obfuscator.NewOrderGenerator(mockEngine)
		require.NoError(t, err)

		cfg := fundingconfig.ExchangeObfuscationCfg{
			Enabled:        true,
			MinUSDT:        10.0,
			MaxUSDT:        100.0,
			MarginUSDT:     10.0,
			VolumeScalePct: 100.0,
			MinHoldSec:     5,
			MaxHoldSec:     5,
		}
		record := &ordermanagerpersistence.ProfitableTradeRecord{
			ReqID:     "req-blur",
			Exchange:  "toobit_futures",
			Symbol:    "BLUR-SWAP-USDT",
			Side:      shared.SideOpenShort,
			NetProfit: 10.0, // notional = 10 USDT
		}
		spec, err := gen.GenerateSpec(context.Background(), cfg, record)
		require.NoError(t, err)
		assert.Equal(t, 0.201, spec.Price) // ask price for Long
		assert.Equal(t, 49.0, spec.Volume) // 10 / 0.201 = 49.75 -> 49 contracts (volScale=0)
		assert.Equal(t, 10.0, spec.NotionalUSDT)
		assert.Equal(t, 500000.0, spec.Vol24hUSDT)
		assert.Equal(t, -0.0015, spec.FundingRate)
	})
}

func TestObfuscatorRunner(t *testing.T) {
	t.Parallel()
	disp := &mockDispatcher{}
	clock := &mockClock{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	runner, err := obfuscator.NewObfuscatorRunner(disp, clock, logger)
	require.NoError(t, err)

	spec := &obfuscator.ObfuscationSpec{
		OriginReqID:     "orig-999",
		Exchange:        "bybit",
		Symbol:          "SOLUSDT",
		Side:            shared.SideOpenShort,
		NotionalUSDT:    25.0,
		MarginUSDT:      5.0,
		Leverage:        5,
		Price:           150.0,
		Volume:          0.166,
		ContractSize:    1.0,
		TakeProfitPrice: 147.0,
		StopLossPrice:   151.5,
		TakeProfitPct:   2.0,
		StopLossPct:     1.0,
		HoldDuration:    15 * time.Second,
		OrderType:       ordermanager.OrderTypeMarket,
		Vol24hUSDT:      1500000.0,
		FundingRate:     0.0001,
	}

	err = runner.Execute(context.Background(), spec)
	require.NoError(t, err)
	require.Len(t, disp.events, 1)

	evt, ok := disp.events[0].(ordermanager.OrderIntentEvent)
	require.True(t, ok)
	assert.Equal(t, "orig-999", evt.RefID)
	assert.Equal(t, "bybit", evt.Exchange)
	assert.Equal(t, "SOLUSDT", evt.Symbol)
	assert.Equal(t, shared.SideOpenShort, evt.Side)
	assert.Equal(t, ordermanager.StrategyObfuscator, evt.StrategyType)
	assert.Equal(t, 0.166, evt.Volume)
	assert.Equal(t, 150.0, evt.Price)
	assert.Equal(t, 1.0, evt.ContractSize)
	assert.Equal(t, 5.0, evt.MarginUSDT)
	assert.Equal(t, 5, evt.Leverage)
	assert.Equal(t, 147.0, evt.TakeProfitPrice)
	assert.Equal(t, 151.5, evt.StopLossPrice)
	assert.Equal(t, 15*time.Second, evt.TimeoutDuration)
	assert.Equal(t, 1500000.0, evt.Vol24hUSDT)
	assert.Equal(t, 0.0001, evt.FundingRate)

	t.Run("returns error when nil spec", func(t *testing.T) {
		t.Parallel()
		err := runner.Execute(context.Background(), nil)
		require.Error(t, err)
	})

	t.Run("returns error when missing dependencies", func(t *testing.T) {
		t.Parallel()
		_, err := obfuscator.NewObfuscatorRunner(nil, clock, logger)
		require.Error(t, err)
		_, err = obfuscator.NewObfuscatorRunner(disp, nil, logger)
		require.Error(t, err)
	})
}

func TestEventBusDispatcher(t *testing.T) {
	t.Parallel()

	pub := &mockEventPublisher{}
	disp, err := obfuscator.NewEventBusDispatcher(pub)
	require.NoError(t, err)

	t.Run("returns error when publisher is nil", func(t *testing.T) {
		t.Parallel()
		_, err := obfuscator.NewEventBusDispatcher(nil)
		require.Error(t, err)
	})

	t.Run("dispatch nil event returns nil", func(t *testing.T) {
		t.Parallel()
		err := disp.Dispatch(context.Background(), nil)
		require.NoError(t, err)
	})

	t.Run("dispatch valid event publishes to eventbus", func(t *testing.T) {
		t.Parallel()
		evt := ordermanager.OrderIntentEvent{
			BaseExecutionEvent: ordermanager.BaseExecutionEvent{
				ReqID: "req-disp-001",
			},
		}
		err := disp.Dispatch(context.Background(), evt)
		require.NoError(t, err)
		assert.Contains(t, pub.publishedTopics, ordermanager.TopicOrderIntent)
	})

	t.Run("dispatch returns error when publisher fails", func(t *testing.T) {
		t.Parallel()
		errPub := &mockEventPublisher{err: errors.New("publish error")}
		errDisp, err := obfuscator.NewEventBusDispatcher(errPub)
		require.NoError(t, err)
		err = errDisp.Dispatch(context.Background(), ordermanager.OrderIntentEvent{})
		require.Error(t, err)
	})
}

func TestObfuscatorJob(t *testing.T) {
	t.Parallel()
	disp := &mockDispatcher{}
	clock := &mockClock{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	mockEngine := &mockEngineProviderGetter{
		client: &mockExchangeClient{
			ob: &shared.OrderBook{
				Bids: []shared.OrderBookEntry{{Price: 100, Volume: 10}},
				Asks: []shared.OrderBookEntry{{Price: 101, Volume: 5}},
			},
			detail: &exchange.ContractDetail{
				Symbol:       "BTCUSDT",
				ContractSize: 1.0,
				PriceUnit:    0.01,
			},
		},
	}

	runner, err := obfuscator.NewObfuscatorRunner(disp, clock, logger)
	require.NoError(t, err)
	gen, err := obfuscator.NewOrderGenerator(mockEngine)
	require.NoError(t, err)

	pnlReader := &mockPnLReader{
		records: []ordermanagerpersistence.ProfitableTradeRecord{
			{
				ReqID:     "trade-1",
				Exchange:  "binance",
				Symbol:    "BTCUSDT",
				Side:      shared.SideOpenLong,
				NetProfit: 50.0,
			},
		},
	}

	cfg := fundingconfig.ObfuscatorConfig{
		Enabled:        true,
		PollInterval:   types.Duration(1 * time.Minute),
		LookbackWindow: types.Duration(1 * time.Hour),
		Exchanges: map[string]fundingconfig.ExchangeObfuscationCfg{
			"binance": {
				Enabled:             true,
				NetPnLThresholdUSDT: 10.0,
				VolumeScalePct:      100.0,
				MinUSDT:             10.0,
				MaxUSDT:             50.0,
				MarginUSDT:          10.0,
				MinHoldSec:          5,
				MaxHoldSec:          10,
			},
		},
	}

	job, err := obfuscator.NewObfuscatorJob(cfg, pnlReader, gen, runner, clock, logger)
	require.NoError(t, err)

	err = job.Tick(context.Background())
	require.NoError(t, err)
	assert.Len(t, disp.events, 1)

	// Second tick should skip already processed trade
	err = job.Tick(context.Background())
	require.NoError(t, err)
	assert.Len(t, disp.events, 1)

	t.Run("NewObfuscatorJob dependency validation", func(t *testing.T) {
		t.Parallel()
		_, err := obfuscator.NewObfuscatorJob(cfg, nil, gen, runner, clock, logger)
		require.Error(t, err)
		_, err = obfuscator.NewObfuscatorJob(cfg, pnlReader, nil, runner, clock, logger)
		require.Error(t, err)
		_, err = obfuscator.NewObfuscatorJob(cfg, pnlReader, gen, nil, clock, logger)
		require.Error(t, err)
		_, err = obfuscator.NewObfuscatorJob(cfg, pnlReader, gen, runner, nil, logger)
		require.Error(t, err)
	})

	t.Run("Start when disabled returns nil", func(t *testing.T) {
		t.Parallel()
		disabledCfg := cfg
		disabledCfg.Enabled = false
		disJob, err := obfuscator.NewObfuscatorJob(disabledCfg, pnlReader, gen, runner, clock, logger)
		require.NoError(t, err)
		err = disJob.Start(context.Background(), nil)
		require.NoError(t, err)
	})

	t.Run("Start and Stop lifecycle", func(t *testing.T) {
		t.Parallel()
		startJob, err := obfuscator.NewObfuscatorJob(cfg, pnlReader, gen, runner, clock, logger)
		require.NoError(t, err)
		err = startJob.Start(context.Background(), nil)
		require.NoError(t, err)
		err = startJob.Stop(context.Background())
		require.NoError(t, err)
	})

	t.Run("Tick handles PnL reader query error gracefully", func(t *testing.T) {
		t.Parallel()
		errReader := &mockPnLReader{err: errors.New("db query error")}
		errJob, err := obfuscator.NewObfuscatorJob(cfg, errReader, gen, runner, clock, logger)
		require.NoError(t, err)
		err = errJob.Tick(context.Background())
		require.NoError(t, err)
	})

	t.Run("Tick handles Runner execution error and clears cache", func(t *testing.T) {
		t.Parallel()
		errDisp := &mockDispatcher{err: errors.New("dispatch error")}
		errRunner, err := obfuscator.NewObfuscatorRunner(errDisp, clock, logger)
		require.NoError(t, err)

		oneReader := &mockPnLReader{
			records: []ordermanagerpersistence.ProfitableTradeRecord{
				{
					ReqID:     "trade-err-runner",
					Exchange:  "binance",
					Symbol:    "BTCUSDT",
					NetProfit: 20.0,
				},
			},
		}
		errJob, err := obfuscator.NewObfuscatorJob(cfg, oneReader, gen, errRunner, clock, logger)
		require.NoError(t, err)
		err = errJob.Tick(context.Background())
		require.NoError(t, err)
	})

	t.Run("Tick skips disabled exchange", func(t *testing.T) {
		t.Parallel()
		disExchCfg := cfg
		disExchCfg.Exchanges = map[string]fundingconfig.ExchangeObfuscationCfg{
			"binance": {Enabled: false},
		}
		disExchJob, err := obfuscator.NewObfuscatorJob(disExchCfg, pnlReader, gen, runner, clock, logger)
		require.NoError(t, err)
		err = disExchJob.Tick(context.Background())
		require.NoError(t, err)
	})

	t.Run("Tick respects MaxActiveOrders", func(t *testing.T) {
		t.Parallel()
		maxCfg := cfg
		maxCfg.Exchanges = map[string]fundingconfig.ExchangeObfuscationCfg{
			"binance": {
				Enabled:         true,
				MinUSDT:         10.0,
				MaxUSDT:         50.0,
				MarginUSDT:      5.0,
				MaxActiveOrders: 1,
			},
		}
		multiReader := &mockPnLReader{
			records: []ordermanagerpersistence.ProfitableTradeRecord{
				{ReqID: "trade-max-1", Exchange: "binance", Symbol: "BTCUSDT", NetProfit: 20.0},
				{ReqID: "trade-max-2", Exchange: "binance", Symbol: "BTCUSDT", NetProfit: 30.0},
			},
		}
		countDisp := &mockDispatcher{}
		countRunner, err := obfuscator.NewObfuscatorRunner(countDisp, clock, logger)
		require.NoError(t, err)
		maxJob, err := obfuscator.NewObfuscatorJob(maxCfg, multiReader, gen, countRunner, clock, logger)
		require.NoError(t, err)
		err = maxJob.Tick(context.Background())
		require.NoError(t, err)
		assert.Len(t, countDisp.events, 1) // only 1 order executed due to MaxActiveOrders = 1
	})

	t.Run("Tick skips execution during settlement blackout window (-5m to +5m)", func(t *testing.T) {
		t.Parallel()
		testTimes := []struct {
			name        string
			time        time.Time
			expectEvent bool
		}{
			{"at 11:54:00 (outside window)", time.Date(2026, 8, 15, 11, 54, 0, 0, time.UTC), true},
			{"at 11:55:00 (-5m boundary)", time.Date(2026, 8, 15, 11, 55, 0, 0, time.UTC), false},
			{"at 11:58:00 (inside window)", time.Date(2026, 8, 15, 11, 58, 0, 0, time.UTC), false},
			{"at 12:00:00 (hour mark)", time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC), false},
			{"at 12:03:00 (inside window)", time.Date(2026, 8, 15, 12, 3, 0, 0, time.UTC), false},
			{"at 12:05:00 (+5m boundary)", time.Date(2026, 8, 15, 12, 5, 0, 0, time.UTC), false},
			{"at 12:06:00 (outside window)", time.Date(2026, 8, 15, 12, 6, 0, 0, time.UTC), true},
		}

		for _, tt := range testTimes {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				tClock := &mockClock{now: tt.time}
				tDisp := &mockDispatcher{}
				tRunner, err := obfuscator.NewObfuscatorRunner(tDisp, tClock, logger)
				require.NoError(t, err)

				tReader := &mockPnLReader{
					records: []ordermanagerpersistence.ProfitableTradeRecord{
						{ReqID: "trade-blackout-" + tt.name, Exchange: "binance", Symbol: "BTCUSDT", NetProfit: 25.0},
					},
				}
				tJob, err := obfuscator.NewObfuscatorJob(cfg, tReader, gen, tRunner, tClock, logger)
				require.NoError(t, err)

				err = tJob.Tick(context.Background())
				require.NoError(t, err)

				if tt.expectEvent {
					assert.Len(t, tDisp.events, 1, "expected event to dispatch at %v", tt.time)
				} else {
					assert.Empty(t, tDisp.events, "expected event to be skipped at %v", tt.time)
				}
			})
		}
	})
}

func TestIsSettlementBlackout(t *testing.T) {
	t.Parallel()

	assert.False(t, obfuscator.IsSettlementBlackout(time.Date(2026, 8, 15, 10, 54, 0, 0, time.UTC)))
	assert.True(t, obfuscator.IsSettlementBlackout(time.Date(2026, 8, 15, 10, 55, 0, 0, time.UTC)))
	assert.True(t, obfuscator.IsSettlementBlackout(time.Date(2026, 8, 15, 10, 58, 0, 0, time.UTC)))
	assert.True(t, obfuscator.IsSettlementBlackout(time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)))
	assert.True(t, obfuscator.IsSettlementBlackout(time.Date(2026, 8, 15, 11, 2, 0, 0, time.UTC)))
	assert.True(t, obfuscator.IsSettlementBlackout(time.Date(2026, 8, 15, 11, 5, 0, 0, time.UTC)))
	assert.False(t, obfuscator.IsSettlementBlackout(time.Date(2026, 8, 15, 11, 6, 0, 0, time.UTC)))
	assert.False(t, obfuscator.IsSettlementBlackout(time.Date(2026, 8, 15, 11, 30, 0, 0, time.UTC)))
}

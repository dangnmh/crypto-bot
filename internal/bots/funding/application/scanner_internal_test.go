package application

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestConfiguredScanner_Scan_MissingStore(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc"},
		},
	}

	scanner := NewConfiguredScanner(
		cfg,
		nil,
		map[string]strategy.FundingStoreSet{}, // Empty stores
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Empty(t, opportunities)
}

func TestConfiguredScanner_Scan_NilTicker(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	fundings := mocks.NewMockFundingReader(ctrl)
	fundings.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(time.Now().Add(30*time.Second), nil).AnyTimes()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc"},
		},
	}

	scanner := NewConfiguredScanner(
		cfg,
		nil,
		map[string]strategy.FundingStoreSet{
			"mexc": fakeFundingStoreSet{
				funding: fundings,
				ticker:  nil, // Nil ticker store
			},
		},
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Empty(t, opportunities)
}

func TestConfiguredScanner_Scan_GetTickerError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tickers := mocks.NewMockTickerReader(ctrl)
	fundings := mocks.NewMockFundingReader(ctrl)

	fundings.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(time.Now().Add(30*time.Second), nil).AnyTimes()
	tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(nil, errors.New("ticker error")).AnyTimes()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc"},
		},
	}

	scanner := NewConfiguredScanner(
		cfg,
		nil,
		map[string]strategy.FundingStoreSet{
			"mexc": fakeFundingStoreSet{
				funding: fundings,
				ticker:  tickers,
			},
		},
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Empty(t, opportunities)
}

func TestConfiguredScanner_Scan_NilContract(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tickers := mocks.NewMockTickerReader(ctrl)
	fundings := mocks.NewMockFundingReader(ctrl)

	tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol: "BTC_USDT",
	}, nil).AnyTimes()
	fundings.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(time.Now().Add(30*time.Second), nil).AnyTimes()
	fundings.EXPECT().GetFunding(gomock.Any(), "BTC_USDT").Return(&store.FundingData{
		Symbol:      "BTC_USDT",
		FundingRate: -0.01,
	}, nil).AnyTimes()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc"},
		},
	}

	scanner := NewConfiguredScanner(
		cfg,
		nil,
		map[string]strategy.FundingStoreSet{
			"mexc": fakeFundingStoreSet{
				funding:  fundings,
				ticker:   tickers,
				contract: nil, // Nil contract store
			},
		},
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Empty(t, opportunities)
}

func TestConfiguredScanner_Scan_GetContractError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tickers := mocks.NewMockTickerReader(ctrl)
	fundings := mocks.NewMockFundingReader(ctrl)
	contracts := mocks.NewMockContractReader(ctrl)

	tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol: "BTC_USDT",
	}, nil).AnyTimes()
	fundings.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(time.Now().Add(30*time.Second), nil).AnyTimes()
	fundings.EXPECT().GetFunding(gomock.Any(), "BTC_USDT").Return(&store.FundingData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.01,
	}, nil).AnyTimes()
	contracts.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(nil, errors.New("contract error")).AnyTimes()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc"},
		},
	}

	scanner := NewConfiguredScanner(
		cfg,
		nil,
		map[string]strategy.FundingStoreSet{
			"mexc": fakeFundingStoreSet{
				funding:  fundings,
				ticker:   tickers,
				contract: contracts,
			},
		},
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Empty(t, opportunities)
}

func TestScannerJob_ScanError(t *testing.T) {
	t.Parallel()

	mScanner := &mockScanner{
		err: errors.New("scan error"),
	}

	job := NewScannerJob(
		[]Scanner{mScanner},
		&app.Engine{Providers: map[string]*app.ExchangeProvider{}},
		nil,
		sniperTestLogger(),
	)

	job.tick(context.Background())

	mScanner.mu.Lock()
	assert.Equal(t, 1, mScanner.scanCalled)
	mScanner.mu.Unlock()
}

func TestScannerJob_DoubleTriggerAndPublishError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	bus := eventbus.New(sniperTestLogger())
	t.Cleanup(func() { _ = bus.Close() })

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetServerTime(gomock.Any()).Return(time.Now().UnixMilli(), nil).AnyTimes()
	client.EXPECT().SupportLeverageOnOrder().Return(true).AnyTimes()

	ts := timesync.New(client, slog.Default(), time.Second)
	ctxSync, cancelSync := context.WithCancel(context.Background())
	cancelSync()
	ts.Start(ctxSync)

	engine := &app.Engine{
		Bus: bus,
		Providers: map[string]*app.ExchangeProvider{
			"mexc": {
				Name:     "mexc",
				Client:   client,
				TimeSync: ts,
			},
		},
	}

	now := time.Now()
	opp := ScanOpportunity{
		Candidate: domain.Candidate{
			Config: domain.TradeConfig{
				Symbol:   "BTC_USDT",
				Exchange: "mexc",
			},
			TradeIntent: domain.TradeIntent{
				Symbol:      "BTC_USDT",
				FundingRate: 0.01,
			},
		},
		SettleTime: now.Add(30 * time.Second),
	}

	mScanner := &mockScanner{
		opportunities: []ScanOpportunity{opp},
	}

	job := NewScannerJob(
		[]Scanner{mScanner},
		engine,
		nil,
		sniperTestLogger(),
	)

	subCtx := t.Context()
	ch, err := bus.Subscribe(subCtx, reversion.TopicReversionCandidate)
	require.NoError(t, err)

	job.tick(context.Background())

	select {
	case msg := <-ch:
		require.NotNil(t, msg)
		msg.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for event")
	}

	mScanner.mu.Lock()
	mScanner.scanCalled = 0
	mScanner.mu.Unlock()

	job.tick(context.Background())

	select {
	case <-ch:
		t.Fatal("Received unexpected duplicate trigger event")
	case <-time.After(200 * time.Millisecond):
	}

	_ = bus.Close()

	opp2 := opp
	opp2.SettleTime = now.Add(60 * time.Second)
	mScanner2 := &mockScanner{
		opportunities: []ScanOpportunity{opp2},
	}
	job2 := NewScannerJob(
		[]Scanner{mScanner2},
		engine,
		nil,
		sniperTestLogger(),
	)

	job2.tick(context.Background())
}

func TestConfiguredScanner_Scan_Blacklisted(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc"},
		},
		Blacklist: &config.BlacklistConfig{
			"common": []string{"BTC_USDT"},
		},
	}

	scanner := NewConfiguredScanner(
		cfg,
		nil,
		map[string]strategy.FundingStoreSet{
			"mexc": fakeFundingStoreSet{},
		},
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	assert.Empty(t, opportunities)
}

func TestScheduleScanner_Scan(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)

	settleTime := time.Now().Add(4 * time.Hour).UnixMilli()

	// Mock GetPotentialFundingSymbols
	client.EXPECT().GetPotentialFundingSymbols(gomock.Any(), 1000000.0, 0.0, nil, gomock.Any()).Return([]exchange.PotentialFundingResult{
		{
			Symbol:     "BTC_USDT",
			Rate:       0.004,
			SettleTime: settleTime,
			Volume24h:  2000000,
		},
	}, nil)

	// Mock GetTickers: returns tickers for BTC_USDT
	client.EXPECT().GetTickers(gomock.Any(), "").Return([]exchange.Ticker{
		{
			Symbol:       "BTC_USDT",
			LastPrice:    50000,
			Bid1:         49990,
			Ask1:         50010,
			Volume24:     40,
			AmountUSDT24: 2000000,
		},
	}, nil)

	// Mock GetContractDetails
	client.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{
			Symbol:       "BTC_USDT",
			PriceUnit:    0.1,
			VolUnit:      1,
			MinVol:       1,
			PriceScale:   1,
			VolScale:     0,
			ContractSize: 0.001,
			TakerFeeRate: 0.0006,
			MakerFeeRate: 0.0002,
		},
	}, nil)

	// Setup minimal config
	cfg := &config.Config{
		Reversion: &config.ReversionConfig{
			RawFundingReversionConfig: config.RawFundingReversionConfig{
				Default: config.ExchangeReversionConfig{
					MinVol24USD: 1000000,
				},
			},
		},
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc", MarginUSDT: 5.0},
		},
	}

	scanner := NewScheduleScanner(
		"mexc",
		cfg,
		client,
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)
	require.Len(t, opportunities, 1)

	opp := opportunities[0]
	assert.Equal(t, "BTC_USDT", opp.Candidate.Symbol)
	assert.Equal(t, 0.004, opp.Candidate.FundingRate)
	assert.Equal(t, 5.0, opp.Candidate.Config.MarginUSDT)
	assert.Equal(t, "mexc", opp.Candidate.Config.Exchange)
}

func TestScheduleScanner_Scan_BestOpportunityFiltering(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		tickers        []exchange.Ticker
		rates          []exchange.FundingRateResult
		expectedSymbol string
		expectedRate   float64
		expectedVolume float64
	}{
		{
			name: "highest absolute funding rate",
			tickers: []exchange.Ticker{
				{Symbol: "BTC_USDT", LastPrice: 50000, Bid1: 49990, Ask1: 50010, Volume24: 40, AmountUSDT24: 2000000},
				{Symbol: "ETH_USDT", LastPrice: 3000, Bid1: 2999, Ask1: 3001, Volume24: 1000, AmountUSDT24: 3000000},
			},
			rates: []exchange.FundingRateResult{
				{Symbol: "BTC_USDT", Rate: 0.004, SettleTime: time.Now().Add(4 * time.Hour).UnixMilli()},
				{Symbol: "ETH_USDT", Rate: -0.008, SettleTime: time.Now().Add(4 * time.Hour).UnixMilli()}, // chosen (0.008 > 0.004)
			},
			expectedSymbol: "ETH_USDT",
			expectedRate:   -0.008,
			expectedVolume: 3000000.0,
		},
		{
			name: "same absolute funding rate - pick higher volume",
			tickers: []exchange.Ticker{
				{Symbol: "BTC_USDT", LastPrice: 50100, Bid1: 50090, Ask1: 50110, Volume24: 45, AmountUSDT24: 2200000},
				{Symbol: "ETH_USDT", LastPrice: 3050, Bid1: 3049, Ask1: 3051, Volume24: 1500, AmountUSDT24: 4500000}, // chosen (4.5M > 2.2M)
				{Symbol: "LTC_USDT", LastPrice: 150, Bid1: 149, Ask1: 151, Volume24: 100, AmountUSDT24: 15000},
			},
			rates: []exchange.FundingRateResult{
				{Symbol: "BTC_USDT", Rate: 0.005, SettleTime: time.Now().Add(4 * time.Hour).UnixMilli()},
				{Symbol: "ETH_USDT", Rate: -0.005, SettleTime: time.Now().Add(4 * time.Hour).UnixMilli()},
				{Symbol: "LTC_USDT", Rate: 0.001, SettleTime: time.Now().Add(4 * time.Hour).UnixMilli()},
			},
			expectedSymbol: "ETH_USDT",
			expectedRate:   -0.005,
			expectedVolume: 4500000.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			client := mocks.NewMockClient(ctrl)

			var potentialResults []exchange.PotentialFundingResult
			tickerMap := make(map[string]exchange.Ticker)
			for _, tk := range tt.tickers {
				tickerMap[tk.Symbol] = tk
			}
			for _, rt := range tt.rates {
				tk := tickerMap[rt.Symbol]
				if tk.AmountUSDT24 >= 1000000 {
					potentialResults = append(potentialResults, exchange.PotentialFundingResult{
						Symbol:     rt.Symbol,
						Rate:       rt.Rate,
						SettleTime: rt.SettleTime,
						Volume24h:  tk.AmountUSDT24,
					})
				}
			}

			client.EXPECT().GetPotentialFundingSymbols(gomock.Any(), 1000000.0, 0.0, nil, gomock.Any()).Return(potentialResults, nil)
			client.EXPECT().GetTickers(gomock.Any(), "").Return(tt.tickers, nil)
			client.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
				{Symbol: "BTC_USDT", PriceUnit: 0.1, VolUnit: 1, MinVol: 1, PriceScale: 1, VolScale: 0, ContractSize: 0.001},
				{Symbol: "ETH_USDT", PriceUnit: 0.01, VolUnit: 1, MinVol: 1, PriceScale: 2, VolScale: 0, ContractSize: 0.01},
			}, nil)

			cfg := &config.Config{
				Reversion: &config.ReversionConfig{
					RawFundingReversionConfig: config.RawFundingReversionConfig{
						Default: config.ExchangeReversionConfig{
							MinVol24USD: 1000000,
						},
					},
				},
				Symbols: []config.SymbolConfig{
					{Symbol: "BTC_USDT", Exchange: "mexc", MarginUSDT: 5.0},
					{Symbol: "ETH_USDT", Exchange: "mexc", MarginUSDT: 10.0},
				},
			}

			scanner := NewScheduleScanner(
				"mexc",
				cfg,
				client,
				sniperTestLogger(),
				func(string) (string, bool) { return "", false },
			)

			opportunities, err := scanner.Scan(context.Background())
			require.NoError(t, err)
			require.Len(t, opportunities, 1)

			opp := opportunities[0]
			assert.Equal(t, tt.expectedSymbol, opp.Candidate.Symbol)
			assert.Equal(t, tt.expectedRate, opp.Candidate.FundingRate)
			assert.Equal(t, tt.expectedVolume, opp.Candidate.Vol24USDT)
		})
	}
}

func TestScannerJob_ShouldTrigger_Filters(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Blacklist: &config.BlacklistConfig{
			"mexc": []string{"XRP_USDT"},
		},
	}

	engine := &app.Engine{Providers: map[string]*app.ExchangeProvider{}}
	job := NewScannerJob(nil, engine, cfg, sniperTestLogger())

	// Candidate meets all thresholds
	candOk := domain.Candidate{
		Config: domain.TradeConfig{
			Exchange:       "mexc",
			Symbol:         "BTC_USDT",
			MinFundingRate: 0.001,
			MinVol24USD:    1000000,
		},
		TradeIntent: domain.TradeIntent{
			Symbol:      "BTC_USDT",
			FundingRate: 0.005,
		},
		MarketData: domain.MarketData{
			Vol24USDT: 2000000,
		},
	}
	assert.True(t, job.shouldTrigger(candOk, time.Now().Add(10*time.Minute)))

	// Candidate fails funding rate check
	candLowFR := candOk
	candLowFR.FundingRate = 0.0005
	assert.False(t, job.shouldTrigger(candLowFR, time.Now().Add(10*time.Minute)))

	// Candidate fails volume check
	candLowVol := candOk
	candLowVol.Vol24USDT = 500000
	assert.False(t, job.shouldTrigger(candLowVol, time.Now().Add(10*time.Minute)))

	// Candidate is blacklisted
	candBlacklisted := candOk
	candBlacklisted.Symbol = "XRP_USDT"
	candBlacklisted.Config.Symbol = "XRP_USDT"
	assert.False(t, job.shouldTrigger(candBlacklisted, time.Now().Add(10*time.Minute)))
}

func TestConfiguredScanner_BuildCandidate_FundingRateRounding(t *testing.T) {
	t.Parallel()
	scanner := &ConfiguredScanner{}
	sc := config.SymbolConfig{Symbol: "BTC_USDT", Exchange: "mexc"}
	td := &store.TickerData{Symbol: "BTC_USDT", LastPrice: 50000}

	// Test positive funding rate: e.g. 0.00125 (0.125%) -> 0.001 (0.1%)
	cand1 := scanner.buildCandidate(sc, td, 0.00125)
	assert.Equal(t, 0.001, cand1.FundingRate)

	// Test negative funding rate: e.g. -0.00175 (-0.175%) -> -0.002 (-0.2%)
	cand2 := scanner.buildCandidate(sc, td, -0.00175)
	assert.Equal(t, -0.002, cand2.FundingRate)
}

func TestScheduleScanner_BuildCandidate_FundingRateRounding(t *testing.T) {
	t.Parallel()
	scanner := &ScheduleScanner{}
	sc := config.SymbolConfig{Symbol: "BTC_USDT", Exchange: "mexc"}
	td := exchange.Ticker{Symbol: "BTC_USDT", LastPrice: 50000}

	// Test positive funding rate: e.g. 0.00125 (0.125%) -> 0.001 (0.1%)
	cand1 := scanner.buildCandidate(sc, td, 0.00125)
	assert.Equal(t, 0.001, cand1.FundingRate)

	// Test negative funding rate: e.g. -0.00175 (-0.175%) -> -0.002 (-0.2%)
	cand2 := scanner.buildCandidate(sc, td, -0.00175)
	assert.Equal(t, -0.002, cand2.FundingRate)
}

func TestScanner_TradeSideFilter(t *testing.T) {
	t.Parallel()

	// 1. Test ConfiguredScanner filtering
	t.Run("ConfiguredScanner filtering", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		tickers := mocks.NewMockTickerReader(ctrl)
		fundings := mocks.NewMockFundingReader(ctrl)
		contracts := mocks.NewMockContractReader(ctrl)

		// Mock settle time
		fundings.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(time.Now().Add(30*time.Second), nil).AnyTimes()

		// Mock ticker data
		tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
			Symbol: "BTC_USDT",
		}, nil).AnyTimes()

		// Mock negative funding rate: -0.01 (means candidate.Side is SHORT)
		fundings.EXPECT().GetFunding(gomock.Any(), "BTC_USDT").Return(&store.FundingData{
			Symbol:      "BTC_USDT",
			FundingRate: -0.01,
		}, nil).AnyTimes()

		contracts.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
			Symbol: "BTC_USDT",
		}, nil).AnyTimes()

		// Config with tradeSide set to "long" (so SHORT candidate should be skipped)
		cfg := &config.Config{
			Reversion: &config.ReversionConfig{
				RawFundingReversionConfig: config.RawFundingReversionConfig{
					TradeSide: "long",
				},
			},
			Symbols: []config.SymbolConfig{
				{Symbol: "BTC_USDT", Exchange: "mexc"},
			},
		}

		scanner := NewConfiguredScanner(
			cfg,
			nil,
			map[string]strategy.FundingStoreSet{
				"mexc": fakeFundingStoreSet{
					funding:  fundings,
					ticker:   tickers,
					contract: contracts,
				},
			},
			sniperTestLogger(),
			func(string) (string, bool) { return "", false },
		)

		opportunities, err := scanner.Scan(context.Background())
		require.NoError(t, err)
		assert.Empty(t, opportunities) // Skipped because it's a SHORT candidate and we only want LONG
	})

	// 2. Test ScheduleScanner filtering
	t.Run("ScheduleScanner filtering", func(t *testing.T) {
		t.Parallel()
		ctrl := gomock.NewController(t)
		client := mocks.NewMockClient(ctrl)

		settleTime := time.Now().Add(4 * time.Hour).UnixMilli()

		// Mock GetPotentialFundingSymbols with negative rate (SHORT side candidate)
		client.EXPECT().GetPotentialFundingSymbols(gomock.Any(), 1000000.0, 0.0, nil, gomock.Any()).Return([]exchange.PotentialFundingResult{
			{
				Symbol:     "BTC_USDT",
				Rate:       -0.004,
				SettleTime: settleTime,
				Volume24h:  2000000,
			},
		}, nil)

		client.EXPECT().GetTickers(gomock.Any(), "").Return([]exchange.Ticker{
			{
				Symbol:       "BTC_USDT",
				LastPrice:    50000,
				Bid1:         49990,
				Ask1:         50010,
				Volume24:     40,
				AmountUSDT24: 2000000,
			},
		}, nil)

		client.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
			{Symbol: "BTC_USDT", PriceUnit: 0.1, VolUnit: 1, MinVol: 1, PriceScale: 1, VolScale: 0, ContractSize: 0.001},
		}, nil)

		// Config with tradeSide = "long"
		cfg := &config.Config{
			Reversion: &config.ReversionConfig{
				RawFundingReversionConfig: config.RawFundingReversionConfig{
					TradeSide: "long",
					Default: config.ExchangeReversionConfig{
						MinVol24USD: 1000000,
					},
				},
			},
		}

		scanner := NewScheduleScanner(
			"mexc",
			cfg,
			client,
			sniperTestLogger(),
			func(string) (string, bool) { return "", false },
		)

		opportunities, err := scanner.Scan(context.Background())
		require.NoError(t, err)
		assert.Empty(t, opportunities) // Skipped because it's a SHORT candidate and config is LONG
	})
}

func TestScheduleScanner_MaxCandidateTrade(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)

	settleTime := time.Now().Add(4 * time.Hour).UnixMilli()

	// Mock GetPotentialFundingSymbols to return 3 symbols
	client.EXPECT().GetPotentialFundingSymbols(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]exchange.PotentialFundingResult{
		{Symbol: "SOL_USDT", Rate: 0.003, SettleTime: settleTime, Volume24h: 1000000},
		{Symbol: "BTC_USDT", Rate: 0.005, SettleTime: settleTime, Volume24h: 2000000},
		{Symbol: "ETH_USDT", Rate: 0.004, SettleTime: settleTime, Volume24h: 1500000},
	}, nil)

	// Mock GetTickers
	client.EXPECT().GetTickers(gomock.Any(), "").Return([]exchange.Ticker{
		{Symbol: "BTC_USDT", LastPrice: 50000, AmountUSDT24: 2000000},
		{Symbol: "ETH_USDT", LastPrice: 3000, AmountUSDT24: 1500000},
		{Symbol: "SOL_USDT", LastPrice: 100, AmountUSDT24: 1000000},
	}, nil)

	// Mock GetContractDetails
	client.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{Symbol: "BTC_USDT", PriceUnit: 0.1, VolUnit: 1, MinVol: 1},
		{Symbol: "ETH_USDT", PriceUnit: 0.1, VolUnit: 1, MinVol: 1},
		{Symbol: "SOL_USDT", PriceUnit: 0.1, VolUnit: 1, MinVol: 1},
	}, nil)

	// Configure total marginUSD = 11 and maxCandidateTrade = 2
	cfg := &config.Config{
		Reversion: &config.ReversionConfig{
			RawFundingReversionConfig: config.RawFundingReversionConfig{
				Default: config.ExchangeReversionConfig{
					MinVol24USD:       1000000,
					MarginUSD:         11.0,
					MaxCandidateTrade: 2,
				},
			},
		},
	}

	scanner := NewScheduleScanner(
		"mexc",
		cfg,
		client,
		sniperTestLogger(),
		func(string) (string, bool) { return "", false },
	)

	opportunities, err := scanner.Scan(context.Background())
	require.NoError(t, err)

	// We have maxCandidateTrade = 2, so it must return 2 opportunities (BTC_USDT and ETH_USDT) sorted by funding rate
	require.Len(t, opportunities, 2)
	assert.Equal(t, "BTC_USDT", opportunities[0].Candidate.Symbol)
	assert.Equal(t, "ETH_USDT", opportunities[1].Candidate.Symbol)

	// Allocated margin: int(11.0 / 2) = 5.0
	assert.Equal(t, 5.0, opportunities[0].Candidate.Config.MarginUSDT)
	assert.Equal(t, 5.0, opportunities[1].Candidate.Config.MarginUSDT)
}

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
			Common: []string{"BTC_USDT"},
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

	// Mock GetTickers: returns tickers for BTC_USDT and ETH_USDT
	// BTC_USDT has large volume: 2,000,000 USD
	// ETH_USDT has low volume: 500,000 USD
	client.EXPECT().GetTickers(gomock.Any(), "").Return([]exchange.Ticker{
		{
			Symbol:    "BTC_USDT",
			LastPrice: 50000,
			Bid1:      49990,
			Ask1:      50010,
			Volume24:  40,
			Amount24:  2000000, // 2M volume > 1M threshold
		},
		{
			Symbol:    "ETH_USDT",
			LastPrice: 3000,
			Bid1:      2999,
			Ask1:      3001,
			Volume24:  166,
			Amount24:  500000, // 500k volume < 1M threshold
		},
	}, nil).Times(2) // Once inside FundingService, once inside ScheduleScanner.Scan

	// Mock GetFundingRates: called only for BTC_USDT because ETH_USDT is filtered by volume
	client.EXPECT().GetFundingRates(gomock.Any(), []string{"BTC_USDT"}).Return([]exchange.FundingRateResult{
		{
			Symbol:     "BTC_USDT",
			Rate:       0.004, // 0.4% > 0.3% threshold
			SettleTime: time.Now().Add(4 * time.Hour).UnixMilli(),
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
		System: &config.SystemConfig{
			Safety: config.SafetyConfig{
				MinVol24USD: 1000000,
			},
		},
		Symbols: []config.SymbolConfig{
			{Symbol: "BTC_USDT", Exchange: "mexc", MarginUSDT: 5.0},
		},
	}

	scanner := NewScheduleScanner(
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
				{Symbol: "BTC_USDT", LastPrice: 50000, Bid1: 49990, Ask1: 50010, Volume24: 40, Amount24: 2000000},
				{Symbol: "ETH_USDT", LastPrice: 3000, Bid1: 2999, Ask1: 3001, Volume24: 1000, Amount24: 3000000},
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
				{Symbol: "BTC_USDT", LastPrice: 50000, Bid1: 49990, Ask1: 50010, Volume24: 40, Amount24: 2000000},
				{Symbol: "ETH_USDT", LastPrice: 3000, Bid1: 2999, Ask1: 3001, Volume24: 1333, Amount24: 4000000}, // chosen (4M > 2M)
			},
			rates: []exchange.FundingRateResult{
				{Symbol: "BTC_USDT", Rate: 0.005, SettleTime: time.Now().Add(4 * time.Hour).UnixMilli()},
				{Symbol: "ETH_USDT", Rate: -0.005, SettleTime: time.Now().Add(4 * time.Hour).UnixMilli()},
			},
			expectedSymbol: "ETH_USDT",
			expectedRate:   -0.005,
			expectedVolume: 4000000.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			client := mocks.NewMockClient(ctrl)

			client.EXPECT().GetTickers(gomock.Any(), "").Return(tt.tickers, nil).Times(2)
			client.EXPECT().GetFundingRates(gomock.Any(), gomock.Any()).Return(tt.rates, nil)
			client.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
				{Symbol: "BTC_USDT", PriceUnit: 0.1, VolUnit: 1, MinVol: 1, PriceScale: 1, VolScale: 0, ContractSize: 0.001},
				{Symbol: "ETH_USDT", PriceUnit: 0.01, VolUnit: 1, MinVol: 1, PriceScale: 2, VolScale: 0, ContractSize: 0.01},
			}, nil)

			cfg := &config.Config{
				System: &config.SystemConfig{
					Safety: config.SafetyConfig{
						MinVol24USD: 1000000,
					},
				},
				Symbols: []config.SymbolConfig{
					{Symbol: "BTC_USDT", Exchange: "mexc", MarginUSDT: 5.0},
					{Symbol: "ETH_USDT", Exchange: "mexc", MarginUSDT: 10.0},
				},
			}

			scanner := NewScheduleScanner(
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
			assert.Equal(t, tt.expectedVolume, opp.Candidate.Amount24)
		})
	}
}

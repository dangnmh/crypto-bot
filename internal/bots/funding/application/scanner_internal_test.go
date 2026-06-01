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
		Symbol:      "BTC_USDT",
		FundingRate: -0.01,
	}, nil).AnyTimes()
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
		Symbol:      "BTC_USDT",
		FundingRate: 0.01,
	}, nil).AnyTimes()
	fundings.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(time.Now().Add(30*time.Second), nil).AnyTimes()
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

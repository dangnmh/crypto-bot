package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/config"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type orchestratorStrategy struct {
	enabled  bool
	execErr  error
	execN    int
	cleanupN int
}

func (s *orchestratorStrategy) Flow() string { return "test" }
func (s *orchestratorStrategy) Enabled(config.SymbolConfig) bool {
	return s.enabled
}
func (s *orchestratorStrategy) Execute(context.Context, time.Time, fundingdomain.Candidate) error {
	s.execN++
	return s.execErr
}
func (s *orchestratorStrategy) CleanupOpenExposure(context.Context) error {
	s.cleanupN++
	return nil
}

func TestOrchestratorBuildCandidateAndEnrich(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	contracts := mocks.NewMockContractReader(ctrl)
	contracts.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		Symbol:       "BTC_USDT",
		PriceUnit:    0.1,
		VolUnit:      1,
		MinVol:       1,
		PriceScale:   1,
		VolScale:     0,
		ContractSize: 0.01,
		TakerFeeRate: 0.0006,
		MakerFeeRate: 0.0002,
	}, nil)

	o := application.NewOrchestrator(symbolConfig(), globalConfig(), application.Deps{
		ContractStore: contracts,
		Log:           testAppLogger(),
	})
	candidate := o.BuildCandidate(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.01,
		LastPrice:   100,
		BestBid:     99,
		BestAsk:     101,
		Volume24:    1000,
		Amount24:    100000,
	})
	assert.Equal(t, shared.SideOpenLong, candidate.Side)
	assert.Equal(t, "bestAsk", candidate.RefPriceType)

	require.True(t, o.Enrich(context.Background(), &candidate))
	assert.Equal(t, 0.1, candidate.PriceUnit)
	assert.Equal(t, 0.01, candidate.ContractSize)
}

func TestOrchestratorRunScansAndExecutesStrategies(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tickers := mocks.NewMockTickerReader(ctrl)
	contracts := mocks.NewMockContractReader(ctrl)
	tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: -0.01,
		LastPrice:   100,
		BestBid:     99,
		BestAsk:     101,
		Amount24:    100000,
	}, nil)
	contracts.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		Symbol: "BTC_USDT",
	}, nil)
	testStrategy := &orchestratorStrategy{enabled: true}

	o := application.NewOrchestrator(symbolConfig(), globalConfig(), application.Deps{
		TickerStore:   tickers,
		ContractStore: contracts,
		Log:           testAppLogger(),
	}, testStrategy)
	o.Run(context.Background(), time.Now().Add(time.Hour))
	assert.Equal(t, 1, testStrategy.execN)
}

func TestOrchestratorRunSkipsAndCleansUpOnError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tickers := mocks.NewMockTickerReader(ctrl)
	contracts := mocks.NewMockContractReader(ctrl)
	tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.01,
		BestBid:     99,
		BestAsk:     101,
		Amount24:    100000,
	}, nil)
	contracts.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{}, nil)
	testStrategy := &orchestratorStrategy{enabled: true, execErr: errors.New("execute")}

	o := application.NewOrchestrator(symbolConfig(), globalConfig(), application.Deps{
		TickerStore:   tickers,
		ContractStore: contracts,
		Log:           testAppLogger(),
	}, testStrategy)
	o.Run(context.Background(), time.Now().Add(time.Hour))
	assert.Equal(t, 1, testStrategy.execN)
	assert.Equal(t, 1, testStrategy.cleanupN)

	noTicker := application.NewOrchestrator(symbolConfig(), globalConfig(), application.Deps{Log: testAppLogger()}, testStrategy)
	noTicker.Run(context.Background(), time.Now())
}

func TestOrchestratorScanSkipsInvalidMarketData(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	tickers := mocks.NewMockTickerReader(ctrl)
	tickers.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.0001,
		Amount24:    100000,
	}, nil)
	testStrategy := &orchestratorStrategy{enabled: true}
	o := application.NewOrchestrator(symbolConfig(), globalConfig(), application.Deps{
		TickerStore: tickers,
		Log:         testAppLogger(),
	}, testStrategy)
	o.Run(context.Background(), time.Now())
	assert.Zero(t, testStrategy.execN)
}

func symbolConfig() config.SymbolConfig {
	return config.SymbolConfig{
		Symbol:         "BTC_USDT",
		Exchange:       "mexc",
		MarginUSDT:     100,
		Leverage:       10,
		MinFundingRate: 0.001,
	}
}

func globalConfig() *config.Config {
	return &config.Config{System: &config.SystemConfig{Safety: config.SafetyConfig{MinVol24USD: 10}}}
}

func testAppLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

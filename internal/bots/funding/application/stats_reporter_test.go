package application_test

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/decmath"

	"github.com/stretchr/testify/assert"
)

type mockScannerClient struct {
	results   []exchange.PotentialFundingResult
	err       error
	blacklist []string // Captured blacklist parameter
}

func (m *mockScannerClient) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	m.blacklist = blacklist
	if m.err != nil {
		return nil, m.err
	}
	var filtered []exchange.PotentialFundingResult
	for _, r := range m.results {
		if minVol24h > 0 && r.Volume24h < minVol24h {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered, nil
}

type mockReportRepository struct {
	mu      sync.Mutex
	reports []domain.SymbolFundingReport
	err     error
}

func (m *mockReportRepository) SaveBatch(ctx context.Context, reports []domain.SymbolFundingReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reports = append(m.reports, reports...)
	return m.err
}

func (m *mockReportRepository) GetPendingPreFunding(ctx context.Context, settleTimeThreshold time.Time) ([]domain.SymbolFundingReport, error) {
	return nil, nil
}

func (m *mockReportRepository) GetPendingAfterFunding(ctx context.Context, settleTimeThreshold time.Time) ([]domain.SymbolFundingReport, error) {
	return nil, nil
}

func (m *mockReportRepository) UpdatePreFunding(ctx context.Context, id uint, fetched bool) error {
	return nil
}

func (m *mockReportRepository) UpdateAfterFunding(ctx context.Context, id uint, fetched bool) error {
	return nil
}

type mockNotifier struct {
	mu     sync.Mutex
	events []notifier.Event
	err    error
}

func (m *mockNotifier) Send(ctx context.Context, evt notifier.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evt)
	return m.err
}

func (m *mockNotifier) Start(ctx context.Context) error { return nil }
func (m *mockNotifier) Stop(ctx context.Context) error  { return nil }

func TestStatsReportJob_Tick(t *testing.T) {
	t.Parallel()

	now := time.Now()

	// Opportunity 1: Matches all criteria
	r1 := exchange.PotentialFundingResult{
		Symbol:     "TAIKO-USDT",
		Rate:       -0.009,                                // -0.9% (>= 0.8% absolute)
		Volume24h:  15000000.0,                            // 15M (>= 10M)
		SettleTime: now.Add(10 * time.Minute).UnixMilli(), // <= 15 minutes
	}

	// Opportunity 2: Settle time in the past
	r2 := exchange.PotentialFundingResult{
		Symbol:     "BTC-USDT",
		Rate:       0.015,
		Volume24h:  50000000.0,
		SettleTime: now.Add(-5 * time.Minute).UnixMilli(),
	}

	// Opportunity 3: Settle time too far in the future (> 15m)
	r3 := exchange.PotentialFundingResult{
		Symbol:     "ETH-USDT",
		Rate:       0.02,
		Volume24h:  30000000.0,
		SettleTime: now.Add(30 * time.Minute).UnixMilli(),
	}

	// Opportunity 4: Funding rate too low (< 0.8%)
	r4 := exchange.PotentialFundingResult{
		Symbol:     "SOL-USDT",
		Rate:       0.005, // 0.5%
		Volume24h:  20000000.0,
		SettleTime: now.Add(5 * time.Minute).UnixMilli(),
	}

	// Opportunity 5: Volume too low (< 10M)
	r5 := exchange.PotentialFundingResult{
		Symbol:     "DOGE-USDT",
		Rate:       -0.012,
		Volume24h:  5000000.0, // 5M
		SettleTime: now.Add(5 * time.Minute).UnixMilli(),
	}

	client := &mockScannerClient{
		results: []exchange.PotentialFundingResult{r1, r2, r3, r4, r5},
	}

	repo := &mockReportRepository{}
	noti := &mockNotifier{}

	cfg := &fundingconfig.Config{
		Blacklist: &fundingconfig.BlacklistConfig{
			"common": []string{"BTC-USDT"},
		},
	}

	job := application.NewStatsReportJob(
		cfg,
		&fundingconfig.SystemConfig{},
		&http.Client{},
		repo,
		noti,
		testLogger(),
	)

	// Clean out real clients and inject mock
	for k := range job.Clients {
		delete(job.Clients, k)
	}
	job.Clients["mexc"] = client

	// Execute manual stats collection
	job.CollectStats(context.Background())

	// Verify database persistence
	repo.mu.Lock()
	assert.Len(t, repo.reports, 1)
	saved := repo.reports[0]
	repo.mu.Unlock()

	assert.Equal(t, "mexc", saved.Exchange)
	assert.Equal(t, "TAIKO-USDT", saved.Symbol)
	assert.Equal(t, "TAIKO", saved.NormalizedSymbol)
	assert.Equal(t, decmath.RoundToScale(r1.Rate, 3), saved.FundingRate)
	assert.Equal(t, 15000000.0, saved.Volume24h)
	assert.True(t, math.Abs(float64(saved.SettleTime.UnixMilli()-r1.SettleTime)) < 1000)
	assert.Contains(t, client.blacklist, "BTC-USDT")
}

func testLogger() *slog.Logger {
	return slog.Default()
}

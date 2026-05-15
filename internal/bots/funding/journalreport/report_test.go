package journalreport_test

import (
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/bots/funding/journalreport"
)

func TestDecodeFiltersBySymbol(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(`
{"req_id":"1","symbol":"BTC_USDT","outcome":"profit"}
{"req_id":"2","symbol":"ETH_USDT","outcome":"loss"}
`)

	records, err := journalreport.Decode(input, "BTC_USDT")
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records len = %d, want 1", len(records))
	}
	if records[0].ReqID != "1" {
		t.Fatalf("req_id = %q, want 1", records[0].ReqID)
	}
}

func TestBuildAggregatesDailyMetrics(t *testing.T) {
	t.Parallel()

	date := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	records := []domain.CycleRecord{
		{
			ReqID:   "a",
			Symbol:  "BTC_USDT",
			Outcome: domain.OutcomeProfit,
			Decision: domain.DecisionSnapshot{
				FRAtScan: 0.003,
			},
			IOC: domain.IOCSnapshot{
				Filled:         true,
				SlippagePct:    0.2,
				SettleOffsetMs: -200,
			},
			Trap: domain.TrapSnapshot{
				Enabled: true,
				Filled:  true,
				Source:  "static_limit",
				Excursion: domain.ExcursionSnapshot{
					MFEPct: 2,
					MAEPct: 3,
				},
			},
			Exit: domain.ExitSnapshot{
				TPPctConfigured: 3,
				SLPctConfigured: 2,
			},
			Excursion: domain.ExcursionSnapshot{
				MFEPct: 4,
				MAEPct: 1,
			},
			IOCExcursion: domain.ExcursionSnapshot{
				MFEPct: 4,
				MAEPct: 1,
			},
		},
		{
			ReqID:   "b",
			Symbol:  "BTC_USDT",
			Outcome: domain.OutcomeNoFill,
			IOC: domain.IOCSnapshot{
				SettleOffsetMs: 100,
			},
			Trap: domain.TrapSnapshot{
				Enabled: true,
				Source:  "ob_monitor",
			},
			Exit: domain.ExitSnapshot{
				TPPctConfigured: 0.03,
			},
		},
	}

	report := journalreport.Build(date, "BTC_USDT", records)

	if report.Cycles != 2 {
		t.Fatalf("cycles = %d, want 2", report.Cycles)
	}
	if report.Outcomes[string(domain.OutcomeProfit)] != 1 {
		t.Fatalf("profit outcomes = %d, want 1", report.Outcomes[string(domain.OutcomeProfit)])
	}
	if report.IOC.FillRatePct != 50 {
		t.Fatalf("IOC fill rate = %.2f, want 50", report.IOC.FillRatePct)
	}
	if report.IOC.SettleOffsetMedianMs != -50 {
		t.Fatalf("median offset = %.2f, want -50", report.IOC.SettleOffsetMedianMs)
	}
	if report.Trap.FillRatePct != 50 {
		t.Fatalf("Trap fill rate = %.2f, want 50", report.Trap.FillRatePct)
	}
	if report.IOC.AvgMFEPct != 4 {
		t.Fatalf("IOC avg MFE = %.2f, want 4", report.IOC.AvgMFEPct)
	}
	if report.Trap.AvgMFEPct != 2 {
		t.Fatalf("Trap avg MFE = %.2f, want 2", report.Trap.AvgMFEPct)
	}
	if len(report.UnitWarnings) == 0 {
		t.Fatal("expected unit warning for decimal-like TP")
	}
}

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
				Outcome: domain.TrapOutcomeClosed,
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
			ReqID:      "b",
			Symbol:     "BTC_USDT",
			Outcome:    domain.OutcomeNoFill,
			AbortTopic: eventsTopicReversionAbort,
			ErrorTopic: eventsTopicReversionError,
			IOC: domain.IOCSnapshot{
				SettleOffsetMs: 100,
			},
			Trap: domain.TrapSnapshot{
				Enabled:    true,
				Outcome:    domain.TrapOutcomeSkipped,
				SkipReason: domain.TrapSkipReasonWallNotVerified,
				Source:     "ob_monitor",
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
	if report.Trap.Outcomes[string(domain.TrapOutcomeClosed)] != 1 {
		t.Fatalf("closed trap outcomes = %d, want 1", report.Trap.Outcomes[string(domain.TrapOutcomeClosed)])
	}
	if report.Trap.Outcomes[string(domain.TrapOutcomeSkipped)] != 1 {
		t.Fatalf("skipped trap outcomes = %d, want 1", report.Trap.Outcomes[string(domain.TrapOutcomeSkipped)])
	}
	if report.Trap.SkipReasons[string(domain.TrapSkipReasonWallNotVerified)] != 1 {
		t.Fatalf("wall-not-verified skip reasons = %d, want 1", report.Trap.SkipReasons[string(domain.TrapSkipReasonWallNotVerified)])
	}
	if report.AbortTopics[eventsTopicReversionAbort] != 1 {
		t.Fatalf("abort topic count = %d, want 1", report.AbortTopics[eventsTopicReversionAbort])
	}
	if report.ErrorTopics[eventsTopicReversionError] != 1 {
		t.Fatalf("error topic count = %d, want 1", report.ErrorTopics[eventsTopicReversionError])
	}
	if len(report.UnitWarnings) == 0 {
		t.Fatal("expected unit warning for decimal-like TP")
	}
}

func TestBuildAggregatesFRBucketsByAbsoluteFundingRate(t *testing.T) {
	t.Parallel()

	date := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	records := []domain.CycleRecord{
		{
			ReqID:   "low",
			Outcome: domain.OutcomeProfit,
			Decision: domain.DecisionSnapshot{
				FRAtScan: -0.0029,
			},
			IOC: domain.IOCSnapshot{
				Filled: true,
			},
		},
		{
			ReqID:   "mid",
			Outcome: domain.OutcomeProfit,
			Decision: domain.DecisionSnapshot{
				FRAtScan: 0.008,
			},
			Trap: domain.TrapSnapshot{
				Enabled: true,
				Outcome: domain.TrapOutcomeFilled,
				Filled:  true,
				Source:  "ob_monitor",
			},
		},
		{
			ReqID:   "boundary",
			Outcome: domain.OutcomeNoFill,
			Decision: domain.DecisionSnapshot{
				FRAtScan: 0.003,
			},
			IOC: domain.IOCSnapshot{
				Filled: true,
			},
		},
		{
			ReqID:   "high",
			Outcome: domain.OutcomeLoss,
			Decision: domain.DecisionSnapshot{
				FRAtScan: -0.025,
			},
			Trap: domain.TrapSnapshot{
				Enabled: true,
				Outcome: domain.TrapOutcomeSkipped,
				Source:  "static_limit",
			},
		},
	}

	report := journalreport.Build(date, "", records)

	if len(report.FRBuckets) != 4 {
		t.Fatalf("FR bucket count = %d, want 4", len(report.FRBuckets))
	}
	if report.FRBuckets[0].Bucket != "<0.3%" {
		t.Fatalf("first bucket = %q, want <0.3%%", report.FRBuckets[0].Bucket)
	}
	lowerMid := findFRBucket(t, report, "0.3%-0.6%")
	if lowerMid.Cycles != 1 || lowerMid.IOC.FillRatePct != 100 {
		t.Fatalf("0.3%%-0.6%% metrics = cycles %d fill %.2f, want cycles 1 fill 100", lowerMid.Cycles, lowerMid.IOC.FillRatePct)
	}
	mid := findFRBucket(t, report, "0.6%-1.2%")
	if mid.Cycles != 1 {
		t.Fatalf("0.6%%-1.2%% cycles = %d, want 1", mid.Cycles)
	}
	if mid.Trap.EnabledCycles != 1 || mid.Trap.FillRatePct != 100 {
		t.Fatalf("0.6%%-1.2%% trap metrics = enabled %d fill %.2f, want enabled 1 fill 100", mid.Trap.EnabledCycles, mid.Trap.FillRatePct)
	}
	if mid.Trap.BySource["ob_monitor"] != 1 {
		t.Fatalf("0.6%%-1.2%% ob_monitor count = %d, want 1", mid.Trap.BySource["ob_monitor"])
	}
	high := findFRBucket(t, report, ">2.0%")
	if high.MinAbsFRPct != 2.0 || high.MaxAbsFRPct != 0 {
		t.Fatalf(">2.0%% bounds = %.1f %.1f, want 2.0 0", high.MinAbsFRPct, high.MaxAbsFRPct)
	}
	if high.Trap.Outcomes[string(domain.TrapOutcomeSkipped)] != 1 {
		t.Fatalf(">2.0%% skipped outcomes = %d, want 1", high.Trap.Outcomes[string(domain.TrapOutcomeSkipped)])
	}
}

func findFRBucket(t *testing.T, report journalreport.Report, bucket string) journalreport.FRBucket {
	t.Helper()
	for i := range report.FRBuckets {
		if report.FRBuckets[i].Bucket == bucket {
			return report.FRBuckets[i]
		}
	}
	t.Fatalf("FR bucket %q not found in %#v", bucket, report.FRBuckets)
	return journalreport.FRBucket{}
}

const (
	eventsTopicReversionAbort = "funding.reversion.abort"
	eventsTopicReversionError = "funding.reversion.error"
)

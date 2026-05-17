package journalreport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/pkg/decmath"
)

const flowTrap = "trap"

// Query selects a daily JSONL journal window.
type Query struct {
	Dir    string
	Date   time.Time
	Symbol string
}

// Report contains daily aggregate metrics for funding cycle records.
type Report struct {
	Date            string         `json:"date"`
	Symbol          string         `json:"symbol,omitempty"`
	Cycles          int            `json:"cycles"`
	Outcomes        map[string]int `json:"outcomes"`
	AbortTopics     map[string]int `json:"abort_topics,omitempty"`
	ErrorTopics     map[string]int `json:"error_topics,omitempty"`
	AbortReasons    map[string]int `json:"abort_reasons,omitempty"`
	IOC             IOCMetrics     `json:"ioc"`
	Trap            TrapMetrics    `json:"trap"`
	FRBuckets       []FRBucket     `json:"fr_buckets,omitempty"`
	UnitWarnings    []string       `json:"unit_warnings,omitempty"`
	Recommendations []string       `json:"recommendations,omitempty"`
}

// FRBucket summarizes comparable cycles by absolute funding-rate bucket.
type FRBucket struct {
	Bucket      string         `json:"bucket"`
	MinAbsFRPct float64        `json:"min_abs_fr_pct"`
	MaxAbsFRPct float64        `json:"max_abs_fr_pct,omitempty"`
	Cycles      int            `json:"cycles"`
	Outcomes    map[string]int `json:"outcomes"`
	IOC         IOCMetrics     `json:"ioc"`
	Trap        TrapMetrics    `json:"trap"`
}

// IOCMetrics summarizes Reversion execution quality.
type IOCMetrics struct {
	FillRatePct          float64 `json:"fill_rate_pct"`
	AvgSlippagePct       float64 `json:"avg_slippage_pct"`
	AvgMFEPct            float64 `json:"avg_mfe_pct"`
	AvgMAEPct            float64 `json:"avg_mae_pct"`
	SettleOffsetAvgMs    float64 `json:"settle_offset_avg_ms"`
	SettleOffsetMedianMs float64 `json:"settle_offset_median_ms"`
	SettleOffsetMinMs    int64   `json:"settle_offset_min_ms"`
	SettleOffsetMaxMs    int64   `json:"settle_offset_max_ms"`
}

// TrapMetrics summarizes Trap execution quality.
type TrapMetrics struct {
	EnabledCycles int            `json:"enabled_cycles"`
	FillRatePct   float64        `json:"fill_rate_pct"`
	AvgMFEPct     float64        `json:"avg_mfe_pct"`
	AvgMAEPct     float64        `json:"avg_mae_pct"`
	Outcomes      map[string]int `json:"outcomes,omitempty"`
	SkipReasons   map[string]int `json:"skip_reasons,omitempty"`
	BySource      map[string]int `json:"by_source,omitempty"`
}

type reportAccumulator struct {
	offsets    []int64
	slippages  []float64
	iocMFE     []float64
	iocMAE     []float64
	trapMFE    []float64
	trapMAE    []float64
	iocFilled  int
	trapFilled int
}

type frBucketDef struct {
	label string
	min   float64
	max   float64
}

type frBucketAccumulator struct {
	bucket FRBucket
	acc    reportAccumulator
}

var frBuckets = []frBucketDef{
	{label: "<0.3%", min: 0, max: 0.3},
	{label: "0.3%-0.6%", min: 0.3, max: 0.6},
	{label: "0.6%-1.2%", min: 0.6, max: 1.2},
	{label: "1.2%-2.0%", min: 1.2, max: 2.0},
	{label: ">2.0%", min: 2.0, max: 0},
}

// Load reads the selected daily JSONL file and returns matching records.
func Load(q Query) ([]domain.CycleRecord, error) {
	path := filepath.Join(q.Dir, fmt.Sprintf("cycles-%s.jsonl", q.Date.Format("2006-01-02")))
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open journal file: %w", err)
	}
	defer func() { _ = f.Close() }()

	return Decode(f, q.Symbol)
}

// Decode parses JSONL cycle records from r, optionally filtering by symbol.
func Decode(r io.Reader, symbol string) ([]domain.CycleRecord, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var records []domain.CycleRecord
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var rec domain.CycleRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("decode journal line %d: %w", lineNo, err)
		}
		if symbol != "" && rec.Symbol != symbol {
			continue
		}
		records = append(records, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan journal: %w", err)
	}

	return records, nil
}

// Build aggregates records into a daily report.
func Build(date time.Time, symbol string, records []domain.CycleRecord) Report {
	r := Report{
		Date:         date.Format("2006-01-02"),
		Symbol:       symbol,
		Outcomes:     make(map[string]int),
		AbortTopics:  make(map[string]int),
		ErrorTopics:  make(map[string]int),
		AbortReasons: make(map[string]int),
		Trap: TrapMetrics{
			Outcomes:    make(map[string]int),
			SkipReasons: make(map[string]int),
			BySource:    make(map[string]int),
		},
	}

	var acc reportAccumulator
	bucketAccs := make(map[string]*frBucketAccumulator, len(frBuckets))

	for i := range records {
		rec := &records[i]
		r.Cycles++
		r.addOutcome(rec)
		acc.addIOC(rec)
		acc.addTrap(rec, &r.Trap)
		r.addUnitWarnings(rec)
		addFRBucketRecord(bucketAccs, rec)
	}

	finalizeMetrics(r.Cycles, &r.IOC, &r.Trap, &acc)
	setTiming(&r.IOC, acc.offsets)
	r.FRBuckets = buildFRBuckets(bucketAccs)
	r.Recommendations = recommendations(r)

	return r
}

func (r *Report) addOutcome(rec *domain.CycleRecord) {
	addOutcome(r.Outcomes, rec)
	if rec.AbortTopic != "" {
		r.AbortTopics[rec.AbortTopic]++
	}
	if rec.ErrorTopic != "" {
		r.ErrorTopics[rec.ErrorTopic]++
	}
	if rec.AbortReason != "" {
		r.AbortReasons[rec.AbortReason]++
	}
}

func addOutcome(outcomes map[string]int, rec *domain.CycleRecord) {
	outcomes[string(rec.Outcome)]++
}

func (a *reportAccumulator) addIOC(rec *domain.CycleRecord) {
	if rec.IOC.Filled {
		a.iocFilled++
	}
	if rec.IOC.SlippagePct > 0 {
		a.slippages = append(a.slippages, rec.IOC.SlippagePct)
	}
	if rec.IOC.SettleOffsetMs != 0 {
		a.offsets = append(a.offsets, rec.IOC.SettleOffsetMs)
	}

	excursion := iocExcursionFor(rec)
	if excursion.MFEPct > 0 {
		a.iocMFE = append(a.iocMFE, excursion.MFEPct)
	}
	if excursion.MAEPct > 0 {
		a.iocMAE = append(a.iocMAE, excursion.MAEPct)
	}
}

func (a *reportAccumulator) addTrap(rec *domain.CycleRecord, trap *TrapMetrics) {
	if !rec.Trap.Enabled {
		return
	}

	trap.EnabledCycles++
	outcome := trapOutcomeFor(rec)
	if outcome != "" {
		trap.Outcomes[string(outcome)]++
	}
	if rec.Trap.SkipReason != "" {
		trap.SkipReasons[string(rec.Trap.SkipReason)]++
	}
	if rec.Trap.Filled {
		a.trapFilled++
		a.addTrapExcursion(rec.Trap.Excursion)
	}
	if rec.Trap.Source != "" {
		trap.BySource[rec.Trap.Source]++
	}
}

func trapOutcomeFor(rec *domain.CycleRecord) domain.TrapOutcome {
	if rec.Trap.Outcome != "" {
		return rec.Trap.Outcome
	}
	if rec.AbortFlow == flowTrap {
		return domain.TrapOutcomeAborted
	}
	if rec.Timeout.Flow == flowTrap && rec.Timeout.Triggered {
		return domain.TrapOutcomeTimeout
	}
	if rec.Trap.Filled {
		return domain.TrapOutcomeFilled
	}
	if rec.Trap.OrderID != "" {
		return domain.TrapOutcomePlaced
	}
	return ""
}

func (a *reportAccumulator) addTrapExcursion(excursion domain.ExcursionSnapshot) {
	if excursion.MFEPct > 0 {
		a.trapMFE = append(a.trapMFE, excursion.MFEPct)
	}
	if excursion.MAEPct > 0 {
		a.trapMAE = append(a.trapMAE, excursion.MAEPct)
	}
}

func iocExcursionFor(rec *domain.CycleRecord) domain.ExcursionSnapshot {
	if rec.IOC.Excursion.MFEPct != 0 || rec.IOC.Excursion.MAEPct != 0 {
		return rec.IOC.Excursion
	}
	if rec.IOCExcursion.MFEPct != 0 || rec.IOCExcursion.MAEPct != 0 {
		return rec.IOCExcursion
	}
	return rec.Excursion
}

func (r *Report) addUnitWarnings(rec *domain.CycleRecord) {
	checks := []struct {
		condition bool
		message   string
	}{
		{rec.Exit.TPPctConfigured > 0 && rec.Exit.TPPctConfigured < 0.2,
			fmt.Sprintf("%s TP looks decimal-like: %.6f", rec.ReqID, rec.Exit.TPPctConfigured)},
		{rec.Exit.SLPctConfigured > 0 && rec.Exit.SLPctConfigured < 0.2,
			fmt.Sprintf("%s SL looks decimal-like: %.6f", rec.ReqID, rec.Exit.SLPctConfigured)},
		{math.Abs(rec.Decision.FRAtScan) > 0.2,
			fmt.Sprintf("%s funding rate suspicious: %.6f", rec.ReqID, rec.Decision.FRAtScan)},
		{rec.IOC.SlippagePct > 20,
			fmt.Sprintf("%s IOC slippage suspicious: %.6f", rec.ReqID, rec.IOC.SlippagePct)},
		{rec.Outcome != domain.OutcomeAborted && rec.IOC.Filled && isZeroExcursion(iocExcursionFor(rec)),
			fmt.Sprintf("%s missing MFE/MAE", rec.ReqID)},
		{rec.Trap.Filled && isZeroExcursion(rec.Trap.Excursion),
			fmt.Sprintf("%s missing Trap MFE/MAE", rec.ReqID)},
		{rec.Trap.Enabled && trapOutcomeFor(rec) == "",
			fmt.Sprintf("%s missing Trap terminal outcome", rec.ReqID)},
		{rec.Trap.Outcome == domain.TrapOutcomeSkipped && rec.Trap.SkipReason == "",
			fmt.Sprintf("%s missing Trap skip reason", rec.ReqID)},
	}
	for _, check := range checks {
		if check.condition {
			r.UnitWarnings = append(r.UnitWarnings, check.message)
		}
	}
}

func isZeroExcursion(excursion domain.ExcursionSnapshot) bool {
	return excursion.MFEPct == 0 && excursion.MAEPct == 0
}

func setTiming(metrics *IOCMetrics, offsets []int64) {
	if len(offsets) == 0 {
		return
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })

	var sum int64
	for _, offset := range offsets {
		sum += offset
	}

	metrics.SettleOffsetAvgMs = decmath.Div(float64(sum), float64(len(offsets)))
	metrics.SettleOffsetMedianMs = medianInt64(offsets)
	metrics.SettleOffsetMinMs = offsets[0]
	metrics.SettleOffsetMaxMs = offsets[len(offsets)-1]
}

func finalizeMetrics(cycles int, ioc *IOCMetrics, trap *TrapMetrics, acc *reportAccumulator) {
	ioc.FillRatePct = pct(acc.iocFilled, cycles)
	ioc.AvgSlippagePct = avgFloat(acc.slippages)
	ioc.AvgMFEPct = avgFloat(acc.iocMFE)
	ioc.AvgMAEPct = avgFloat(acc.iocMAE)
	trap.FillRatePct = pct(acc.trapFilled, trap.EnabledCycles)
	trap.AvgMFEPct = avgFloat(acc.trapMFE)
	trap.AvgMAEPct = avgFloat(acc.trapMAE)
}

func addFRBucketRecord(bucketAccs map[string]*frBucketAccumulator, rec *domain.CycleRecord) {
	def := frBucketFor(rec.Decision.FRAtScan)
	ba := bucketAccs[def.label]
	if ba == nil {
		ba = &frBucketAccumulator{
			bucket: FRBucket{
				Bucket:      def.label,
				MinAbsFRPct: def.min,
				MaxAbsFRPct: def.max,
				Outcomes:    make(map[string]int),
				Trap: TrapMetrics{
					Outcomes:    make(map[string]int),
					SkipReasons: make(map[string]int),
					BySource:    make(map[string]int),
				},
			},
		}
		bucketAccs[def.label] = ba
	}
	ba.bucket.Cycles++
	addOutcome(ba.bucket.Outcomes, rec)
	ba.acc.addIOC(rec)
	ba.acc.addTrap(rec, &ba.bucket.Trap)
}

func frBucketFor(fr float64) frBucketDef {
	frPct := math.Abs(decmath.Mul(fr, 100))
	for _, bucket := range frBuckets {
		if bucket.max == 0 {
			if frPct >= bucket.min {
				return bucket
			}
			continue
		}
		if frPct >= bucket.min && frPct < bucket.max {
			return bucket
		}
	}
	return frBuckets[0]
}

func buildFRBuckets(bucketAccs map[string]*frBucketAccumulator) []FRBucket {
	out := make([]FRBucket, 0, len(bucketAccs))
	for _, def := range frBuckets {
		ba := bucketAccs[def.label]
		if ba == nil {
			continue
		}
		finalizeMetrics(ba.bucket.Cycles, &ba.bucket.IOC, &ba.bucket.Trap, &ba.acc)
		setTiming(&ba.bucket.IOC, ba.acc.offsets)
		out = append(out, ba.bucket)
	}
	return out
}

func recommendations(r Report) []string {
	var out []string
	if r.Cycles == 0 {
		return []string{"No matching cycles found."}
	}
	if r.IOC.FillRatePct < 50 {
		out = append(out, "IOC fill rate below 50%; review slippage caps, timing, and symbol liquidity before tuning TP/SL.")
	}
	if r.IOC.SettleOffsetMedianMs < -150 {
		out = append(out, "Median IOC fire is earlier than -150ms; consider reducing buffer or firing later.")
	}
	if r.IOC.SettleOffsetMedianMs > 150 {
		out = append(out, "Median IOC fire is later than 150ms; consider increasing fire offset or latency estimate.")
	}
	if r.Trap.EnabledCycles > 0 && r.Trap.FillRatePct < 20 {
		out = append(out, "Trap fill rate below 20%; review trap depth or disable Trap for weak buckets.")
	}
	if len(r.UnitWarnings) > 0 {
		out = append(out, "Unit warnings present; resolve percent schema issues before changing risk size.")
	}
	return out
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return decmath.Mul(decmath.Div(float64(n), float64(d)), 100)
}

func avgFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum = decmath.Add(sum, v)
	}
	return decmath.Div(sum, float64(len(values)))
}

func medianInt64(values []int64) float64 {
	n := len(values)
	if n == 0 {
		return 0
	}
	mid := n / 2
	if n%2 == 1 {
		return float64(values[mid])
	}
	return decmath.Div(float64(values[mid-1]+values[mid]), 2)
}

package journal

import (
	"fmt"
	"log/slog"
	"sync"
)

// ──────────────────────────────────────────────────────────────────────
// PnLTracker — per-symbol realized PnL tracking
// ──────────────────────────────────────────────────────────────────────.

// PositionRecord represents an open entry that hasn't been closed yet.
type PositionRecord struct {
	Symbol     string
	Side       string // "LONG" or "SHORT"
	EntryPrice float64
	Volume     float64
	FeeRate    float64 // taker fee rate (e.g. 0.0006)
}

// PnLResult holds the realized PnL for a single closed position.
type PnLResult struct {
	Symbol     string
	Side       string
	EntryPrice float64
	ExitPrice  float64
	Volume     float64
	GrossPnL   float64
	Fees       float64
	NetPnL     float64
}

// SymbolSummary holds aggregated PnL for a single symbol.
type SymbolSummary struct {
	Symbol    string
	Trades    int
	GrossPnL  float64
	TotalFees float64
	NetPnL    float64
}

// PnLTracker tracks realized profit and loss per symbol per session.
// Thread-safe for concurrent access from multiple workers.
type PnLTracker struct {
	mu        sync.RWMutex
	positions map[string]*PositionRecord // symbol → open position
	results   []PnLResult                // all closed trades
}

// NewPnLTracker creates a new PnL tracker.
func NewPnLTracker() *PnLTracker {
	return &PnLTracker{
		positions: make(map[string]*PositionRecord),
	}
}

// RecordEntry logs an entry (open) position.
func (t *PnLTracker) RecordEntry(symbol, side string, price, volume, feeRate float64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.positions[symbol] = &PositionRecord{
		Symbol:     symbol,
		Side:       side,
		EntryPrice: price,
		Volume:     volume,
		FeeRate:    feeRate,
	}
}

// RecordExit calculates and logs realized PnL for a symbol's open position.
// Returns the PnLResult, or nil if no open position exists for this symbol.
func (t *PnLTracker) RecordExit(symbol string, exitPrice, exitFeeRate float64) *PnLResult {
	t.mu.Lock()
	defer t.mu.Unlock()

	pos, ok := t.positions[symbol]
	if !ok {
		return nil
	}

	// Calculate gross PnL.
	var grossPnL float64
	notional := pos.Volume * pos.EntryPrice

	if pos.Side == "LONG" {
		// For long positions, profit when exit price exceeds entry.
		grossPnL = (exitPrice - pos.EntryPrice) * pos.Volume
	} else {
		// For short positions, profit when entry price exceeds exit.
		grossPnL = (pos.EntryPrice - exitPrice) * pos.Volume
	}

	// Calculate fees (entry + exit).
	entryFee := notional * pos.FeeRate
	exitFee := pos.Volume * exitPrice * exitFeeRate
	totalFees := entryFee + exitFee

	result := PnLResult{
		Symbol:     symbol,
		Side:       pos.Side,
		EntryPrice: pos.EntryPrice,
		ExitPrice:  exitPrice,
		Volume:     pos.Volume,
		GrossPnL:   grossPnL,
		Fees:       totalFees,
		NetPnL:     grossPnL - totalFees,
	}

	t.results = append(t.results, result)
	delete(t.positions, symbol)

	return &result
}

// Summary returns a per-symbol summary and the overall totals.
func (t *PnLTracker) Summary() (bySymbol map[string]*SymbolSummary, totalNetPnL float64) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	bySymbol = make(map[string]*SymbolSummary)

	for i := range t.results {
		r := &t.results[i]
		s, ok := bySymbol[r.Symbol]
		if !ok {
			s = &SymbolSummary{Symbol: r.Symbol}
			bySymbol[r.Symbol] = s
		}
		s.Trades++
		s.GrossPnL += r.GrossPnL
		s.TotalFees += r.Fees
		s.NetPnL += r.NetPnL
		totalNetPnL += r.NetPnL
	}

	return bySymbol, totalNetPnL
}

// LogSummary prints the session PnL summary to slog.
func (t *PnLTracker) LogSummary(log *slog.Logger) {
	bySymbol, totalNet := t.Summary()

	if len(bySymbol) == 0 {
		log.Info("📊 PnL Summary: no closed trades this session")
		return
	}

	log.Info("📊 ═══ PnL Summary ═══", "total_net_pnl", fmt.Sprintf("%.4f", totalNet), "symbols", len(bySymbol))

	for _, s := range bySymbol {
		log.Info("📊 Symbol PnL",
			"symbol", s.Symbol,
			"trades", s.Trades,
			"gross", fmt.Sprintf("%.4f", s.GrossPnL),
			"fees", fmt.Sprintf("%.4f", s.TotalFees),
			"net", fmt.Sprintf("%.4f", s.NetPnL),
		)
	}
}

// Results returns all closed trade results.
func (t *PnLTracker) Results() []PnLResult {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]PnLResult, len(t.results))
	copy(out, t.results)
	return out
}

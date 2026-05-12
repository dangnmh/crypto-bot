// Package journal provides persistent logging of order executions and PnL tracking.
package journal

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ──────────────────────────────────────────────────────────────────────
// OrderEntry — a single order record
// ──────────────────────────────────────────────────────────────────────.

// OrderEntry represents a single order event for the journal.
type OrderEntry struct {
	Timestamp time.Time
	ReqID     string // Cycle correlation ID
	Symbol    string
	Side      string // "OPEN_LONG", "OPEN_SHORT", etc.
	OrderType string // "IOC", "LIMIT_TRAP", "TRAILING_STOP"
	Price     float64
	Volume    float64
	OrderID   string
	ExtOID    string
	Status    string // "SUBMITTED", "FILLED", "REJECTED", "CANCELLED"
	Error     string // Empty if no error
}

// ──────────────────────────────────────────────────────────────────────
// OrderJournal — interface for recording orders
// ──────────────────────────────────────────────────────────────────────.

// OrderJournal is the interface for recording order events.
type OrderJournal interface {
	Record(entry OrderEntry) error
	Close() error
}

// ──────────────────────────────────────────────────────────────────────
// CSVJournal — append-only CSV file journal
// ──────────────────────────────────────────────────────────────────────.

var csvHeader = []string{
	"timestamp", "req_id", "symbol", "side", "order_type",
	"price", "volume", "order_id", "ext_oid", "status", "error",
}

// CSVJournal writes order entries to a daily CSV file under the given directory.
// Files are named: orders-YYYY-MM-DD.csv.
// Thread-safe — multiple goroutines can write concurrently.
type CSVJournal struct {
	dir    string
	mu     sync.Mutex
	file   *os.File
	writer *csv.Writer
	date   string // current file date (YYYY-MM-DD)
}

// NewCSVJournal creates a new CSV journal that writes to the given directory.
// The directory is created if it doesn't exist.
func NewCSVJournal(dir string) (*CSVJournal, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create journal dir: %w", err)
	}
	j := &CSVJournal{dir: dir}
	if err := j.rotate(); err != nil {
		return nil, err
	}
	return j, nil
}

// Record appends an order entry to the CSV file.
// Rotates to a new file if the date has changed.
func (j *CSVJournal) Record(e OrderEntry) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	today := e.Timestamp.Format("2006-01-02")
	if today != j.date {
		if err := j.rotate(); err != nil {
			return err
		}
	}

	row := []string{
		e.Timestamp.Format(time.RFC3339Nano),
		e.ReqID,
		e.Symbol,
		e.Side,
		e.OrderType,
		fmt.Sprintf("%.8f", e.Price),
		fmt.Sprintf("%.8f", e.Volume),
		e.OrderID,
		e.ExtOID,
		e.Status,
		e.Error,
	}

	if err := j.writer.Write(row); err != nil {
		return fmt.Errorf("write journal row: %w", err)
	}
	j.writer.Flush()
	return j.writer.Error()
}

// Close flushes and closes the underlying file.
func (j *CSVJournal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.writer != nil {
		j.writer.Flush()
	}
	if j.file != nil {
		return j.file.Close()
	}
	return nil
}

// rotate opens a new daily CSV file, writing the header if it's a new file.
// Must be called with j.mu held.
func (j *CSVJournal) rotate() error {
	if j.writer != nil {
		j.writer.Flush()
	}
	if j.file != nil {
		_ = j.file.Close()
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(j.dir, fmt.Sprintf("orders-%s.csv", today))

	isNew := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		isNew = true
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open journal file: %w", err)
	}

	j.file = f
	j.writer = csv.NewWriter(f)
	j.date = today

	if isNew {
		if err := j.writer.Write(csvHeader); err != nil {
			return fmt.Errorf("write journal header: %w", err)
		}
		j.writer.Flush()
	}

	return nil
}

// ──────────────────────────────────────────────────────────────────────
// NoopJournal — no-op implementation for when journaling is disabled
// ──────────────────────────────────────────────────────────────────────.

// NoopJournal is a no-op journal that discards all entries.
type NoopJournal struct{}

// Record does nothing.
func (n *NoopJournal) Record(_ OrderEntry) error { return nil }

// Close does nothing.
func (n *NoopJournal) Close() error { return nil }

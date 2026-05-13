package journal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/domain"
)

// ──────────────────────────────────────────────────────────────────────
// JSONLCycleRecorder — append-only JSONL file recorder
// ──────────────────────────────────────────────────────────────────────.

// JSONLCycleRecorder writes cycle records as JSON Lines (one JSON object per line)
// to daily-rotated files. Thread-safe — multiple goroutines can write concurrently.
//
// File pattern: {dir}/cycles-YYYY-MM-DD.jsonl.
type JSONLCycleRecorder struct {
	dir  string
	mu   sync.Mutex
	file *os.File
	date string // current file date (YYYY-MM-DD)
}

// Compile-time interface compliance check.
var _ domain.CycleRecorder = (*JSONLCycleRecorder)(nil)

// NewJSONLCycleRecorder creates a new JSONL recorder that writes to the given directory.
// The directory is created if it doesn't exist.
func NewJSONLCycleRecorder(dir string) (*JSONLCycleRecorder, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create journal dir: %w", err)
	}
	r := &JSONLCycleRecorder{dir: dir}
	if err := r.rotate(); err != nil {
		return nil, err
	}
	return r, nil
}

// Record serializes the cycle record as a single JSON line and appends it to the file.
// Rotates to a new file if the date has changed.
func (r *JSONLCycleRecorder) Record(_ context.Context, rec domain.CycleRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal cycle record: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	today := rec.CreatedAt.Format("2006-01-02")
	if today != r.date {
		if err := r.rotate(); err != nil {
			return err
		}
	}

	// Write JSON + newline as a single atomic write.
	data = append(data, '\n')
	if _, err := r.file.Write(data); err != nil {
		return fmt.Errorf("write cycle record: %w", err)
	}

	return nil
}

// Close flushes and closes the underlying file.
func (r *JSONLCycleRecorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// rotate opens a new daily JSONL file. Must be called with r.mu held.
func (r *JSONLCycleRecorder) rotate() error {
	if r.file != nil {
		_ = r.file.Close()
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(r.dir, fmt.Sprintf("cycles-%s.jsonl", today))

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open journal file: %w", err)
	}

	r.file = f
	r.date = today
	return nil
}

// ──────────────────────────────────────────────────────────────────────
// NoopCycleRecorder — no-op implementation for when recording is disabled
// ──────────────────────────────────────────────────────────────────────.

// NoopCycleRecorder is a no-op recorder that discards all records.
type NoopCycleRecorder struct{}

// Compile-time interface compliance check.
var _ domain.CycleRecorder = (*NoopCycleRecorder)(nil)

// Record does nothing.
func (n *NoopCycleRecorder) Record(_ context.Context, _ domain.CycleRecord) error { return nil }

// Close does nothing.
func (n *NoopCycleRecorder) Close() error { return nil }

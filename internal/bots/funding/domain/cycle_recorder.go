package domain

import "context"

// CycleRecorder persists completed cycle records for post-analysis.
// Implementations may write to JSONL files, SQLite, or discard (noop).
//
// Interface defined in domain (consumer-defined), per coding conventions §2.2.
type CycleRecorder interface {
	Record(ctx context.Context, record CycleRecord) error
	Close() error
}

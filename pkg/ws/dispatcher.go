package ws

import (
	"errors"
	"sync"
)

var (
	// ErrDuplicateRequestID is returned when attempting to register an already active request ID.
	ErrDuplicateRequestID = errors.New("duplicate request id")

	// ErrDispatcherClosed is returned when attempting to register on a closed dispatcher.
	ErrDispatcherClosed = errors.New("dispatcher closed")
)

// RequestDispatcher coordinates thread-safe request-response correlation over duplex protocols like WebSockets.
type RequestDispatcher[T any] struct {
	mu      sync.RWMutex
	pending map[string]chan T
	closed  bool
}

// NewRequestDispatcher creates a new generic RequestDispatcher.
func NewRequestDispatcher[T any]() *RequestDispatcher[T] {
	return &RequestDispatcher[T]{
		pending: make(map[string]chan T),
	}
}

// Register registers a request ID and returns a receive channel, a cleanup function, and any error.
// The caller must call the returned cleanup function (or receive the response) to prevent memory leaks.
func (d *RequestDispatcher[T]) Register(reqID string) (<-chan T, func(), error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return nil, nil, ErrDispatcherClosed
	}

	if _, exists := d.pending[reqID]; exists {
		return nil, nil, ErrDuplicateRequestID
	}

	ch := make(chan T, 1)
	d.pending[reqID] = ch

	cancel := func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		delete(d.pending, reqID)
	}

	return ch, cancel, nil
}

// Dispatch routes a response to the matching registered channel.
// Returns true if a pending request was found and notified, false otherwise.
func (d *RequestDispatcher[T]) Dispatch(reqID string, resp T) bool {
	d.mu.Lock()
	ch, exists := d.pending[reqID]
	if exists {
		delete(d.pending, reqID)
	}
	d.mu.Unlock()

	if !exists {
		return false
	}

	ch <- resp
	return true
}

// AbortAll closes and drains all active pending channels without closing the dispatcher.
// In-flight requests will receive a channel close and fail fast instead of waiting for timeout.
func (d *RequestDispatcher[T]) AbortAll() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for id, ch := range d.pending {
		close(ch)
		delete(d.pending, id)
	}
}

// Close closes all pending channels, aborts in-flight requests, and marks the dispatcher closed.
func (d *RequestDispatcher[T]) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.closed = true
	for id, ch := range d.pending {
		close(ch)
		delete(d.pending, id)
	}
}

// Len returns the current number of active pending requests.
func (d *RequestDispatcher[T]) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.pending)
}

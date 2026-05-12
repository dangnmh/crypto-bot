package exchange

import (
	"errors"
	"fmt"
)

// ──────────────────────────────────────────────────────────────────────
// Structured error types for programmatic error handling
// ──────────────────────────────────────────────────────────────────────.

// APIError represents a non-200 HTTP response or a business-level error from the exchange API.
type APIError struct {
	StatusCode int    // HTTP status code (0 if not applicable)
	Code       int    // Exchange-specific error code
	Message    string // Human-readable error message
	Path       string // API endpoint path that caused the error
}

func (e *APIError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("exchange API error: HTTP %d, code=%d, message=%q, path=%s", e.StatusCode, e.Code, e.Message, e.Path)
	}
	return fmt.Sprintf("exchange API error: code=%d, message=%q, path=%s", e.Code, e.Message, e.Path)
}

// RateLimitError indicates the exchange rate limit has been exceeded.
type RateLimitError struct {
	Message string
	Path    string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded: %s (path=%s)", e.Message, e.Path)
}

// OrderRejectedError indicates an order was rejected by the exchange.
type OrderRejectedError struct {
	Code    int
	Reason  string
	Symbol  string
	OrderID string
}

func (e *OrderRejectedError) Error() string {
	return fmt.Sprintf("order rejected: code=%d, reason=%q, symbol=%s", e.Code, e.Reason, e.Symbol)
}

// IsAPIError checks if an error is an APIError and returns it.
// Uses errors.As to support wrapped errors.
func IsAPIError(err error) (*APIError, bool) {
	var e *APIError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

// IsRateLimitError checks if an error is a rate limit error.
// Uses errors.As to support wrapped errors.
func IsRateLimitError(err error) bool {
	var e *RateLimitError
	return errors.As(err, &e)
}

// IsOrderRejectedError checks if an error is an order rejected error.
// Uses errors.As to support wrapped errors.
func IsOrderRejectedError(err error) (*OrderRejectedError, bool) {
	var e *OrderRejectedError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}

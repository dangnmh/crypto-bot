package mexc

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crypto-bot/internal/infrastructure/exchange"
)

// APIResponse is the generic MEXC Futures REST response envelope.
type APIResponse[T any] struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Data    T      `json:"data"`
	Message string `json:"message,omitempty"`
}

// ParseResponse is a generic helper that unmarshals the MEXC response envelope,
// checks for API-level errors, and returns the typed Data payload.
// This replaces the repetitive unmarshal+check pattern across all API methods.
func ParseResponse[T any](body []byte, path string) (T, error) {
	var resp APIResponse[T]
	if err := json.Unmarshal(body, &resp); err != nil {
		var zero T
		return zero, fmt.Errorf("parse %s response: %w", path, err)
	}
	if !resp.Success {
		var zero T
		return zero, toAPIError(resp.Code, resp.Message, path)
	}
	return resp.Data, nil
}

// ParseResponseIgnoreData parses the envelope and checks for errors,
// but discards the data payload. Used for void-return operations (cancel, close).
func ParseResponseIgnoreData(body []byte, path string) error {
	var resp APIResponse[json.RawMessage]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse %s response: %w", path, err)
	}
	if !resp.Success {
		return toAPIError(resp.Code, resp.Message, path)
	}
	return nil
}

// toAPIError converts an MEXC error response into a structured exchange.APIError.
func toAPIError(code int, message, path string) *exchange.APIError {
	return &exchange.APIError{
		Code:    code,
		Message: message,
		Path:    path,
	}
}

// toHTTPError creates an APIError for non-200 HTTP status codes.
func toHTTPError(statusCode int, body []byte, path string) *exchange.APIError {
	return &exchange.APIError{
		StatusCode: statusCode,
		Message:    string(body),
		Path:       path,
	}
}

// isRateLimited checks if an HTTP response indicates rate limiting.
func isRateLimited(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests
}

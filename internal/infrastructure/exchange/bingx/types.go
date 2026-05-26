package bingx

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crypto-bot/internal/infrastructure/exchange"
)

// APIResponse is the generic BingX REST response envelope.
type APIResponse[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

// ParseResponse is a generic helper that unmarshals the BingX response envelope,
// checks for API-level errors (Code != 0), and returns the typed Data.
func ParseResponse[T any](body []byte, path string) (T, error) {
	var resp APIResponse[T]
	var zero T
	if err := json.Unmarshal(body, &resp); err != nil {
		return zero, fmt.Errorf("parse %s response: %w", path, err)
	}
	if resp.Code != 0 {
		return zero, toAPIError(resp.Code, resp.Msg, path)
	}
	return resp.Data, nil
}

// ParseResponseIgnoreData parses the envelope and checks for errors,
// but discards the data payload.
func ParseResponseIgnoreData(body []byte, path string) error {
	var resp APIResponse[json.RawMessage]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse %s response: %w", path, err)
	}
	if resp.Code != 0 {
		return toAPIError(resp.Code, resp.Msg, path)
	}
	return nil
}

// toAPIError converts a BingX error response into a structured exchange.APIError.
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

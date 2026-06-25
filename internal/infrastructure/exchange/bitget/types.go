package bitget

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crypto-bot/internal/infrastructure/exchange"

	"crypto-bot/pkg/xjson"
)

// APIResponse is the generic Bitget V2 REST response envelope.
type APIResponse[T any] struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

// ParseResponse is a generic helper that unmarshals the Bitget response envelope,
// checks for API-level errors (code != "00000"), and returns the typed Data.
func ParseResponse[T any](body []byte, path string) (T, error) {
	var resp APIResponse[T]
	var zero T
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return zero, fmt.Errorf("parse %s response: %w", path, err)
	}
	if resp.Code != "00000" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return zero, toAPIError(codeVal, resp.Msg, path)
	}
	return resp.Data, nil
}

// ParseResponseIgnoreData parses the envelope and checks for errors,
// but discards the data payload.
func ParseResponseIgnoreData(body []byte, path string) error {
	var resp APIResponse[json.RawMessage]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse %s response: %w", path, err)
	}
	if resp.Code != "00000" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return toAPIError(codeVal, resp.Msg, path)
	}
	return nil
}

// toAPIError converts a Bitget error response into a structured exchange.APIError.
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

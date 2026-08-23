package mexc

import (
	"encoding/json"
	"fmt"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
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
func ParseResponse[T any](body []byte, path string) (T, error) {
	var resp APIResponse[T]
	if err := xjson.Unmarshal(body, &resp); err != nil {
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
	if err := xjson.Unmarshal(body, &resp); err != nil {
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

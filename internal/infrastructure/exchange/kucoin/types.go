package kucoin

import (
	"encoding/json"
	"fmt"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

// APIResponse is the generic KuCoin REST response envelope.
type APIResponse[T any] struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

// ParseResponse is a generic helper that unmarshals the KuCoin response envelope,
// checks for API-level errors (Code != "200000"), and returns the typed Data.
func ParseResponse[T any](body []byte, path string) (T, error) {
	var resp APIResponse[T]
	var zero T
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return zero, fmt.Errorf("parse %s response: %w", path, err)
	}
	if resp.Code != "200000" {
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
	if resp.Code != "200000" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return toAPIError(codeVal, resp.Msg, path)
	}
	return nil
}

// toAPIError converts a KuCoin error response into a structured exchange.APIError.
func toAPIError(code int, message, path string) *exchange.APIError {
	return &exchange.APIError{
		Code:    code,
		Message: message,
		Path:    path,
	}
}

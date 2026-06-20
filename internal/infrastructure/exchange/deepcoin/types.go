package deepcoin

import (
	"encoding/json"
	"fmt"
)

// APIResponse represents the standard Deepcoin API response wrapper.
type APIResponse[T any] struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

// ParseResponse parses a JSON response where the 'data' field is a list.
func ParseResponse[T any](body []byte, desc string) ([]T, error) {
	var resp APIResponse[[]T]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse %s response: %w", desc, err)
	}
	if resp.Code != "0" {
		return nil, fmt.Errorf("%s failed (code %s): %s", desc, resp.Code, resp.Msg)
	}
	return resp.Data, nil
}

// ParseResponseFirst parses a JSON response where 'data' is a single object.
func ParseResponseFirst[T any](body []byte, desc string) (T, error) {
	var resp APIResponse[T]
	var zero T
	if err := json.Unmarshal(body, &resp); err != nil {
		return zero, fmt.Errorf("parse %s response: %w", desc, err)
	}
	if resp.Code != "0" {
		return zero, fmt.Errorf("%s failed (code %s): %s", desc, resp.Code, resp.Msg)
	}
	return resp.Data, nil
}

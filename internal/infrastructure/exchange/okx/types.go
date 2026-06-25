package okx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"

	"crypto-bot/pkg/xjson"
)

// APIResponse is the generic OKX V5 REST response envelope.
type APIResponse[T any] struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []T    `json:"data"`
}

// ParseResponse is a generic helper that unmarshals the OKX response envelope,
// checks for API-level errors (code != "0"), and returns the typed Data array.
func ParseResponse[T any](body []byte, path string) ([]T, error) {
	var resp APIResponse[T]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse %s response: %w", path, err)
	}
	if resp.Code != "0" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return nil, toAPIError(codeVal, resp.Msg, path)
	}
	return resp.Data, nil
}

// ParseResponseFirst is a helper that returns the first element of the Data array,
// or an error if the array is empty.
func ParseResponseFirst[T any](body []byte, path string) (T, error) {
	var zero T
	data, err := ParseResponse[T](body, path)
	if err != nil {
		return zero, err
	}
	if len(data) == 0 {
		return zero, fmt.Errorf("empty data array in %s response", path)
	}
	return data[0], nil
}

// ParseResponseIgnoreData parses the envelope and checks for errors,
// but discards the data payload.
func ParseResponseIgnoreData(body []byte, path string) error {
	var resp APIResponse[json.RawMessage]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse %s response: %w", path, err)
	}
	if resp.Code != "0" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return toAPIError(codeVal, resp.Msg, path)
	}
	return nil
}

// toAPIError converts an OKX error response into a structured exchange.APIError.
func toAPIError(code int, message, path string) *exchange.APIError {
	return &exchange.APIError{
		Code:    code,
		Message: message,
		Path:    path,
	}
}

// toHTTPError creates an APIError for non-200 HTTP status codes.
func toHTTPError(statusCode int, body []byte, path string) *exchange.APIError {
	apiErr := &exchange.APIError{
		StatusCode: statusCode,
		Message:    string(body),
		Path:       path,
	}

	var resp struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := xjson.Unmarshal(body, &resp); err == nil && resp.Code != "" {
		var codeVal int
		if _, err := fmt.Sscanf(resp.Code, "%d", &codeVal); err == nil {
			apiErr.Code = codeVal
		}
		if resp.Msg != "" {
			apiErr.Message = resp.Msg
		}
	}

	return apiErr
}

// isRateLimited checks if an HTTP response indicates rate limiting.
func isRateLimited(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests
}

// mapPositionType determines the exchange-agnostic position side type (1 = long, 2 = short).
func mapPositionType(posSide string, pos float64, instID, posCcy string) exchange.PositionType {
	if posSide == posSideShort {
		return exchange.PositionTypeShort // short
	}
	if posSide == posSideLong {
		return exchange.PositionTypeLong // long
	}
	if posSide == posSideNet {
		if posCcy != "" {
			// Margin: posCcy being base currency means long position, posCcy being quote currency means short position.
			parts := strings.Split(instID, "-")
			if len(parts) >= 2 && posCcy == parts[0] {
				return exchange.PositionTypeLong // long
			}
			return exchange.PositionTypeShort // short
		}
		// Futures/Swap/Option: positive pos means long position, negative pos means short position.
		if pos < 0 {
			return exchange.PositionTypeShort // short
		}
		return exchange.PositionTypeLong // long
	}
	return exchange.PositionTypeLong // default to long
}

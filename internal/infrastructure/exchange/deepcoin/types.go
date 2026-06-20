package deepcoin

import (
	"encoding/json"
	"fmt"
	"strconv"
)

const (
	posSideLong  = "long"
	posSideShort = "short"

	sideBuy  = "buy"
	sideSell = "sell"

	stateLive = "live"

	ordTypeMarket = "market"
	ordTypeLimit  = "limit"

	mgnModeCross    = "cross"
	mgnModeIsolated = "isolated"

	mrgPositionMerge = "merge"

	paramInstId      = "instId"
	paramOrdId       = "ordId"
	paramLever       = "lever"
	paramMgnMode     = "mgnMode"
	paramMrgPosition = "mrgPosition"

	wsAction   = "Action"
	wsSymbol   = "Symbol"
	wsTopic    = "Topic"
	wsLocalNo  = "LocalNo"
	wsResumeNo = "ResumeNo"

	wsTopicMarket = "market"

	wsTablePosition = "Position"
)

// flexString allows relaxed unmarshaling of numbers, strings, and booleans into string fields.
type flexString string

func (f *flexString) UnmarshalJSON(data []byte) error {
	var val any
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	switch v := val.(type) {
	case float64:
		*f = flexString(strconv.FormatFloat(v, 'f', -1, 64))
	case int64:
		*f = flexString(strconv.FormatInt(v, 10))
	case string:
		*f = flexString(v)
	case bool:
		*f = flexString(strconv.FormatBool(v))
	default:
		*f = ""
	}
	return nil
}

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

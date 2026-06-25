package xjson

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Number represents a flexible numeric type in JSON.
type Number string

// UnmarshalJSON unmarshals JSON number, string, boolean, or null into Number.
func (n *Number) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*n = ""
		return nil
	}
	var val any
	if err := json.Unmarshal(data, &val); err != nil {
		return err
	}
	switch v := val.(type) {
	case float64:
		*n = Number(strconv.FormatFloat(v, 'f', -1, 64))
	case string:
		*n = Number(v)
	case bool:
		*n = Number(strconv.FormatBool(v))
	case nil:
		*n = ""
	default:
		return fmt.Errorf("cannot unmarshal %T into xjson.Number", v)
	}
	return nil
}

// MarshalJSON marshals Number into JSON representation.
func (n Number) MarshalJSON() ([]byte, error) {
	if n == "" {
		return []byte("0"), nil
	}
	if _, err := strconv.ParseFloat(string(n), 64); err == nil {
		return []byte(n), nil
	}
	return json.Marshal(string(n))
}

// Float64 parses and returns the float64 representation of the Number.
func (n Number) Float64() (float64, error) {
	if n == "" {
		return 0, nil
	}
	return strconv.ParseFloat(string(n), 64)
}

// Int64 parses and returns the int64 representation of the Number.
func (n Number) Int64() (int64, error) {
	if n == "" {
		return 0, nil
	}
	return strconv.ParseInt(string(n), 10, 64)
}

// String returns the raw string representation.
func (n Number) String() string {
	return string(n)
}

// ToInt64 parses standard json.Number to int64, ignoring errors.
func ToInt64(n json.Number) int64 {
	i, _ := n.Int64()
	return i
}

// ToFloat64 parses standard json.Number to float64, ignoring errors.
func ToFloat64(n json.Number) float64 {
	f, _ := n.Float64()
	return f
}


package xjson

import "encoding/json"

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

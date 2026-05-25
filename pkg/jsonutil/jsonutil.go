package jsonutil

import (
	"encoding/json"
	"fmt"
)

// Unmarshal parses the JSON-encoded data and returns a value of type T.
func Unmarshal[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("unmarshal %T: %w", v, err)
	}
	return v, nil
}

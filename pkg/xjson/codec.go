package xjson

import "encoding/json"

// Marshal encodes v to JSON using standard encoding/json.
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal decodes JSON data to v using standard encoding/json.
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

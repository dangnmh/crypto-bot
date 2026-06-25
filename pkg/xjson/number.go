package xjson

import "encoding/json"

func ToInt64(n json.Number) int64 {
	i, _ := n.Int64()
	return i
}

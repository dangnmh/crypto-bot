package mexc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"crypto-bot/pkg/xjson"
)

// SignRequest generates the MEXC Futures HMAC-SHA256 signature.
//
// Signature algorithm (from Postman pre-request script):
//
//	GET/DELETE: HMAC-SHA256(apiKey + timestamp + sortedQueryString, apiSecret)
//	POST:      HMAC-SHA256(apiKey + timestamp + jsonBody, apiSecret)
func SignRequest(apiKey, apiSecret, timestamp, method string, params any) string {
	var paramStr string

	switch method {
	case "GET", "DELETE":
		if p, ok := params.(map[string]any); ok {
			paramStr = buildSortedQueryString(p)
		}
	case "POST":
		if params != nil {
			switch p := params.(type) {
			case string:
				paramStr = p
			case []byte:
				paramStr = string(p)
			default:
				data, err := xjson.Marshal(params)
				if err == nil {
					paramStr = string(data)
				}
			}
		}
	}

	message := apiKey + timestamp + paramStr
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// buildSortedQueryString creates a sorted key=value&key=value string from params.
// Keys are sorted alphabetically (matching MEXC Futures spec).
func buildSortedQueryString(params map[string]any) string {
	if len(params) == 0 {
		return ""
	}

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, params[k]))
	}
	return strings.Join(parts, "&")
}

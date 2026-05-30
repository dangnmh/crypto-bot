package bingx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// SignParams signs query parameters using HMAC-SHA256 with the API secret.
// Parameters are sorted alphabetically, URL-encoded/joined, signed, and hex-encoded.
func SignParams(secret string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var query strings.Builder
	for i, k := range keys {
		if i > 0 {
			query.WriteString("&")
		}
		// BingX requires standard query string formatting (e.g. key=value)
		_, _ = fmt.Fprintf(&query, "%s=%s", k, url.QueryEscape(params[k]))
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(query.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

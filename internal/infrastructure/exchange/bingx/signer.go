package bingx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// SignParams signs query parameters using HMAC-SHA256 with the API secret.
// Parameters are sorted alphabetically, formatted using formatQueryParams, and signed.
func SignParams(secret string, params map[string]string) string {
	queryString := formatQueryParams(params)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(queryString))
	return hex.EncodeToString(mac.Sum(nil))
}

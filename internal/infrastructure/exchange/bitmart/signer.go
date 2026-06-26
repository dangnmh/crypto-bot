package bitmart

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// GenerateSignature generates the HMAC-SHA256 signature for Bitmart requests.
// Pre-signed message string is generated using '#' as a delimiter:
// timestamp + "#" + memo + "#" + payload.
func GenerateSignature(timestamp, memo, apiSecret, payload string) string {
	message := timestamp + "#" + memo + "#" + payload
	h := hmac.New(sha256.New, []byte(apiSecret))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

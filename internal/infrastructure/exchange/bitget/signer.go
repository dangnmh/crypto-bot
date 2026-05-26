package bitget

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// SignRequest generates the Bitget API V2 HMAC-SHA256 signature.
// Message to sign is: timestamp + method + requestPath + body.
// The resulting bytes are then base64 encoded.
func SignRequest(apiSecret, timestamp, method, requestPath, body string) string {
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(timestamp + method + requestPath + body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

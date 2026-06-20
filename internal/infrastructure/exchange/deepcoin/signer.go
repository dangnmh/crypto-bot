package deepcoin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// SignRequest generates the Deepcoin V1 HMAC-SHA256 signature.
// Message to sign is: timestamp + method + requestPath + body.
func SignRequest(apiSecret, timestamp, method, requestPath, body string) string {
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(timestamp + method + requestPath + body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

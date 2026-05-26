package kucoin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// SignRequest generates the KuCoin API V2 request signature.
// Message to sign is: timestamp + method + requestPath + body.
// The resulting bytes are then base64 encoded.
func SignRequest(apiSecret, timestamp, method, requestPath, body string) string {
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(timestamp + method + requestPath + body))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// SignPassphrase signs the API passphrase using the API secret.
// The resulting bytes are then base64 encoded.
func SignPassphrase(apiSecret, passphrase string) string {
	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(passphrase))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

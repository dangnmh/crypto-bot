package futures

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"crypto-bot/pkg/xjson"
)

// BuildBybitAuthMessage constructs the HMAC SHA-256 authentication message for Bybit V5 WebSocket.
func BuildBybitAuthMessage(apiKey, apiSecret string, nowMs int64) map[string]any {
	expires := nowMs + 10000 // 10 seconds validity
	reqStr := fmt.Sprintf("GET/realtime%d", expires)

	h := hmac.New(sha256.New, []byte(apiSecret))
	h.Write([]byte(reqStr))
	signature := hex.EncodeToString(h.Sum(nil))

	return map[string]any{
		"op": wsOpAuth,
		"args": []any{
			apiKey,
			expires,
			signature,
		},
	}
}

// BybitWSAuthResponse represents the authentication acknowledgement frame from Bybit V5 WebSocket.
type BybitWSAuthResponse struct {
	Op      string `json:"op"`
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	ConnID  string `json:"connId"`
}

// ParseBybitAuthResponse inspects an inbound WebSocket frame for Bybit authentication results.
// Returns isAuth=true if the frame is an auth operation response, and success=true if retCode==0.
func ParseBybitAuthResponse(data []byte) (isAuth, success bool, retCode int, retMsg, connID string) {
	var resp BybitWSAuthResponse
	if err := xjson.Unmarshal(data, &resp); err == nil && resp.Op == wsOpAuth {
		return true, resp.RetCode == 0, resp.RetCode, resp.RetMsg, resp.ConnID
	}
	return false, false, 0, "", ""
}

// ExtractBybitReqID extracts the correlation request ID (reqId) from Bybit V5 JSON frames for RPC dispatch.
func ExtractBybitReqID(data []byte) (string, bool) {
	var header struct {
		ReqID string `json:"reqId"`
	}
	if err := xjson.Unmarshal(data, &header); err == nil && header.ReqID != "" {
		return header.ReqID, true
	}
	return "", false
}

// GetBybitPingConfig returns application ping payload and interval for Bybit V5 WebSockets.
func GetBybitPingConfig() (any, time.Duration) {
	return map[string]any{
		"op": wsOpPing,
	}, 20 * time.Second
}

// IsBybitPong determines if an incoming WebSocket payload is a Bybit V5 pong message.
func IsBybitPong(data []byte) bool {
	str := strings.ToLower(strings.TrimSpace(string(data)))
	if str == wsOpPong {
		return true
	}
	return strings.Contains(str, `"op":"pong"`) || strings.Contains(str, `"op": "pong"`)
}

// IsBybitPing determines if an incoming WebSocket payload is a Bybit V5 server ping message.
func IsBybitPing(data []byte) bool {
	str := strings.ToLower(strings.TrimSpace(string(data)))
	if str == wsOpPing {
		return true
	}
	return strings.Contains(str, `"op":"ping"`) || strings.Contains(str, `"op": "ping"`)
}

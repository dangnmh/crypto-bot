package binance

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/pkg/xjson"
)

const (
	wsDefaultTradeURL = "wss://ws-fapi.binance.com/ws-fapi/v1"
	wsTestnetTradeURL = "wss://testnet.binancefuture.com/ws-fapi/v1"

	wsMethodOrderPlace   = "order.place"
	wsMethodOrderCancel  = "order.cancel"
	wsMethodSessionLogon = "session.logon"
	wsMethodPing         = "ping"
)

// DefaultTradeURL returns the appropriate Binance WebSocket trade endpoint based on baseURL.
func DefaultTradeURL(baseURL string) string {
	if strings.Contains(strings.ToLower(baseURL), "testnet") {
		return wsTestnetTradeURL
	}
	return wsDefaultTradeURL
}

// ExtractBinanceReqID extracts the correlation request ID (id) from Binance WS JSON frames for RPC dispatch.
func ExtractBinanceReqID(data []byte) (string, bool) {
	var envelope struct {
		ID json.RawMessage `json:"id"`
	}
	if err := xjson.Unmarshal(data, &envelope); err != nil || len(envelope.ID) == 0 {
		return "", false
	}
	var idStr string
	if err := xjson.Unmarshal(envelope.ID, &idStr); err == nil && idStr != "" {
		return idStr, true
	}
	var idNum int64
	if err := xjson.Unmarshal(envelope.ID, &idNum); err == nil {
		return strconv.FormatInt(idNum, 10), true
	}
	raw := strings.Trim(string(envelope.ID), `"`)
	if raw != "" && raw != "null" {
		return raw, true
	}
	return "", false
}

// SignBinanceWSParams formats, populates apiKey, timestamp, and signs parameters using HMAC-SHA256.
func SignBinanceWSParams(params map[string]any, apiKey, apiSecret string, timestamp int64) map[string]any {
	signedParams := make(map[string]any, len(params)+3)
	for k, v := range params {
		if v != nil {
			signedParams[k] = v
		}
	}

	if apiKey != "" {
		signedParams["apiKey"] = apiKey
	}
	if timestamp > 0 {
		signedParams["timestamp"] = timestamp
	}

	if apiSecret == "" {
		return signedParams
	}

	values := url.Values{}
	for k, v := range signedParams {
		if v != nil {
			values.Set(k, fmt.Sprintf("%v", v))
		}
	}
	queryString := values.Encode()

	mac := hmac.New(sha256.New, []byte(apiSecret))
	mac.Write([]byte(queryString))
	signature := hex.EncodeToString(mac.Sum(nil))

	signedParams["signature"] = signature
	return signedParams
}

// GetBinancePingConfig returns application ping payload and interval for Binance WebSocket API.
func GetBinancePingConfig() (any, time.Duration) {
	return map[string]any{
		"id":     wsMethodPing,
		"method": wsMethodPing,
	}, 20 * time.Second
}

// IsBinancePong determines if an incoming WebSocket payload is a Binance WS pong response.
func IsBinancePong(data []byte) bool {
	var res struct {
		ID     string `json:"id"`
		Status int    `json:"status"`
	}
	if err := xjson.Unmarshal(data, &res); err == nil {
		if res.ID == wsMethodPing && res.Status == 200 {
			return true
		}
	}
	str := strings.ToLower(strings.TrimSpace(string(data)))
	return str == "pong" || (strings.Contains(str, `"id":"ping"`) && strings.Contains(str, `"status":200`))
}

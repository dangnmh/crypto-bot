package futures

import "strings"

const (
	exchangeName   = "bybit"
	categoryLinear = "linear"
	categoryKey    = "category"
	symbolKey      = "symbol"
	sideBuy        = "Buy"
	sideSell       = "Sell"
	tifIOC         = "IOC"
	orderIDKey     = "orderId"

	wsOpSubscribe   = "subscribe"
	wsOpUnsubscribe = "unsubscribe"
	wsOpAuth        = "auth"
	wsOpPing        = "ping"
	wsOpPong        = "pong"
	wsArgsKey       = "args"
	wsTopicOrder    = "order"
	wsTopicPosition = "position"

	orderTypeLimit = "Limit"
	limitKey       = "limit"

	accountTypeContract = "CONTRACT"
	accountTypeUnified  = "UNIFIED"
	paramAccountType    = "accountType"
	triggerByLastPrice  = "LastPrice"

	utaMarginIsolated  = "ISOLATED_MARGIN"
	utaMarginRegular   = "REGULAR_MARGIN"
	paramSetMarginMode = "setMarginMode"
	constantCross      = "CROSS"
)

// DefaultTradeURL returns the default Bybit V5 Trade WebSocket URL based on the REST base URL.
func DefaultTradeURL(baseURL string) string {
	lower := strings.ToLower(baseURL)
	switch {
	case strings.Contains(lower, "testnet"):
		return "wss://stream-testnet.bybit.com/v5/trade"
	case strings.Contains(lower, "bybit.tr"):
		return "wss://stream.bybit.tr/v5/trade"
	case strings.Contains(lower, "bybit.kz"):
		return "wss://stream.bybit.kz/v5/trade"
	default:
		return "wss://stream.bybit.com/v5/trade"
	}
}

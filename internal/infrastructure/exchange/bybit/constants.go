package bybit

const (
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

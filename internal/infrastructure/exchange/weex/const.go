package weex

const (
	// Standard API constants.
	sideBuy            = "BUY"
	sideSell           = "SELL"
	posSideLong        = "LONG"
	posSideShort       = "SHORT"
	marginTypeCross    = "CROSSED"
	marginTypeIsolated = "ISOLATED"
	separatedTypeSp    = "SEPARATED"
	separatedTypeCb    = "COMBINED"

	// Order types and TIF.
	typeLimit   = "LIMIT"
	typeMarket  = "MARKET"
	tifGtc      = "GTC"
	tifIoc      = "IOC"
	tifFok      = "FOK"
	tifPostOnly = "POST_ONLY"

	// Order states.
	stateNew             = "NEW"
	statePartialFill     = "PARTIAL_FILL"
	statePartiallyFilled = "PARTIALLY_FILLED"
	stateFilled          = "FILLED"
	stateCanceled        = "CANCELED"
	stateCancelled       = "CANCELLED"

	// Request/Response keys.
	keySymbol           = "symbol"
	keyOrderId          = "orderId"
	keyEvent            = "event"
	keyChannel          = "channel"
	keySubscribed       = "subscribed"
	keyOrderIdList      = "orderIdList"
	keyNewClientOrderId = "newClientOrderId"
	keyMarginType       = "marginType"

	// WS constants.
	wsMethodSubscribe   = "SUBSCRIBE"
	wsMethodUnsubscribe = "UNSUBSCRIBE"
	wsKeyParams         = "params"
	wsKeyMethod         = "method"
	wsChannelTicker     = "ticker"
	wsChannelDepth      = "depth"
	wsChannelPositions  = "positions"

	// Exchange identifier.
	exchangeName = "weex"
)

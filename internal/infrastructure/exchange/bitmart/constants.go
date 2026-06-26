package bitmart

const (
	exchangeName = "bitmart"
	// WS topics.
	topicTicker   = "futures/ticker"
	topicPosition = "futures/position"
	topicOrder    = "futures/order"

	// Actions for WebSocket frames.
	actionSubscribe   = "subscribe"
	actionUnsubscribe = "unsubscribe"
	actionAccess      = "access"
	actionPing        = "ping"

	// REST side mapping.
	sideOpenLong   = 1
	sideCloseShort = 2
	sideCloseLong  = 3
	sideOpenShort  = 4

	// Order types.
	orderTypeLimit  = "limit"
	orderTypeMarket = "market"

	// Time in force modes.
	modeGTC       = 1
	modeFOK       = 2
	modeIOC       = 3
	modeMakerOnly = 4

	// String constant values to resolve goconst lints.
	openTypeIsolated = "isolated"
	openTypeCross    = "cross"
	paramSymbol      = "symbol"
	paramOrderID     = "order_id"
	posSideShort     = "short"
	paramAction      = "action"
	paramArgs        = "args"
)

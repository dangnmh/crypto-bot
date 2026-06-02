package okx

const (
	// REST Endpoints.
	pathServerTime     = "/api/v5/public/time"
	pathInstruments    = "/api/v5/public/instruments"
	pathTickers        = "/api/v5/market/tickers"
	pathFundingRate    = "/api/v5/public/funding-rate"
	pathKlines         = "/api/v5/market/candles"
	pathBooks          = "/api/v5/market/books"
	pathPlaceOrder     = "/api/v5/trade/order"
	pathCancelOrder    = "/api/v5/trade/cancel-order"
	pathCancelBatch    = "/api/v5/trade/cancel-batch-orders"
	pathGetOrder       = "/api/v5/trade/order"
	pathPendingOrders  = "/api/v5/trade/orders-pending"
	pathSetLeverage    = "/api/v5/account/set-leverage"
	pathAccountBalance = "/api/v5/account/balance"
	pathOpenPositions  = "/api/v5/account/positions"

	// Websocket channels.
	channelTicker    = "tickers"
	channelKline     = "candle1m"
	channelDepth     = "books5"
	channelOrders    = "orders"
	channelPositions = "positions"

	// String literals used frequently.
	sideBuy       = "buy"
	sideSell      = "sell"
	posSideLong   = "long"
	posSideShort  = "short"
	posSideNet    = "net"
	stateLive     = "live"
	stateFilled   = "filled"
	stateCanceled = "canceled"
	statePartFill = "partially_filled"
	modeIsolated  = "isolated"
	modeCross     = "cross"

	// String literals flagged by goconst.
	instTypeSwap  = "SWAP"
	paramInstId   = "instId"
	paramLimit    = "limit"
	opSubscribe   = "subscribe"
	opUnsubscribe = "unsubscribe"
	fieldArgs     = "args"
	fieldChannel  = "channel"
	msgPong       = "pong"
	paramInstType = "instType"

	// Trigger price types.
	triggerPxTypeLast  = "last"
	triggerPxTypeIndex = "index"
	triggerPxTypeMark  = "mark"
)

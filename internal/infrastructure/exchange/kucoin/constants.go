package kucoin

const (
	// Base URL and Default Addresses.
	defaultRestURL = "https://api-futures.kucoin.com"

	// REST Endpoints.
	pathServerTime          = "/api/v1/timestamp"
	pathContracts           = "/api/v1/contracts/active"
	pathTickers             = "/api/v1/allTickers"
	pathTickerSingle        = "/api/v1/ticker"
	pathKlines              = "/api/v1/kline/query"
	pathDepth               = "/api/v1/level2/snapshot"
	pathPlaceOrder          = "/api/v1/orders"
	pathCancelOrder         = "/api/v1/orders"
	pathGetOrder            = "/api/v1/orders"
	pathPendingOrders       = "/api/v1/orders"
	pathAccountBalance      = "/api/v1/account-overview"
	pathOpenPositions       = "/api/v1/positions"
	pathBulletPublic        = "/api/v1/bullet-public"
	pathBulletPrivate       = "/api/v1/bullet-private"
	pathPositionsHistory    = "/api/v1/history-positions"
	pathFills               = "/api/v1/fills"
	pathGetOrderByClientOid = "/api/v1/orders/byClientOid"

	// Constants.
	headerKey        = "KC-API-KEY"
	headerSign       = "KC-API-SIGN"
	headerTimestamp  = "KC-API-TIMESTAMP"
	headerAuthPhrase = "KC-API-" + "PASSPHRASE"
	headerVersion    = "KC-API-KEY-VERSION"

	// String literals used frequently.
	sideBuy       = "buy"
	sideSell      = "sell"
	posSideLong   = "long"
	posSideShort  = "short"
	stateLive     = "active"
	stateFilled   = "done"
	stateCanceled = "canceled"

	// String literals flagged by goconst.
	paramSymbol              = "symbol"
	paramLimit               = "limit"
	constantUsdt             = "USDT"
	opSubscribe              = "subscribe"
	opUnsubscribe            = "unsubscribe"
	paramType                = "type"
	paramTopic               = "topic"
	paramPrivateChannel      = "privateChannel"
	paramResponse            = "response"
	paramPing                = "ping"
	paramKline               = "kline"
	constantSize             = "size"
	constantMarket           = "market"
	constantLong             = "LONG"
	constantShort            = "SHORT"
	constantBoth             = "BOTH"
	constantClientOid        = "clientOid"
	paramStatus              = "status"
	pathPositionAll          = "/contract/positionAll"
	constantPersonalPosition = "personal.position"
)

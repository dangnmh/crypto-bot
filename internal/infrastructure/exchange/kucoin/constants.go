package kucoin

const (
	// Base URL and Default Addresses.
	defaultRestURL    = "https://api-futures.kucoin.com"
	defaultUnifiedURL = "https://api.kucoin.com"

	// REST Endpoints.
	pathServerTime     = "/api/v1/timestamp"
	pathContracts      = "/api/v1/contracts/active"
	pathTickers        = "/api/v1/allTickers"
	pathTickerSingle   = "/api/v1/ticker"
	pathFundingRate    = "/api/ua/v1/market/funding-rate"
	pathKlines         = "/api/v1/kline/query"
	pathDepth          = "/api/v1/level2/snapshot"
	pathPlaceOrder     = "/api/v1/orders"
	pathCancelOrder    = "/api/v1/orders"
	pathGetOrder       = "/api/v1/orders"
	pathPendingOrders  = "/api/v1/orders"
	pathSetLeverage    = "/api/v1/position/leverage"
	pathAccountBalance = "/api/v1/account-overview"
	pathOpenPositions  = "/api/v1/positions"
	pathBulletPublic   = "/api/v1/bullet-public"

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
	paramSymbol         = "symbol"
	paramLimit          = "limit"
	constantUsdt        = "USDT"
	opSubscribe         = "subscribe"
	opUnsubscribe       = "unsubscribe"
	paramType           = "type"
	paramTopic          = "topic"
	paramPrivateChannel = "privateChannel"
	paramResponse       = "response"
	paramPing           = "ping"
	paramKline          = "kline"
)

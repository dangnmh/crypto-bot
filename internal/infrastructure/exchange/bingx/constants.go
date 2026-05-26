package bingx

const (
	// Base URL and Default Addresses.
	defaultRestURL = "https://open-api.bingx.com"
	defaultWsURL   = "wss://open-api-ws.bingx.com/market"

	// REST Endpoints.
	pathServerTime     = "/openApi/swap/v2/server/time"
	pathContracts      = "/openApi/swap/v2/quote/contracts"
	pathTickers        = "/openApi/swap/v2/quote/ticker"
	pathFundingRate    = "/openApi/swap/v2/quote/premiumIndex"
	pathKlines         = "/openApi/swap/v2/quote/klines"
	pathDepth          = "/openApi/swap/v2/quote/depth"
	pathPlaceOrder     = "/openApi/swap/v2/trade/order"
	pathCancelOrder    = "/openApi/swap/v2/trade/cancel"
	pathGetOrder       = "/openApi/swap/v2/trade/order"
	pathPendingOrders  = "/openApi/swap/v2/trade/openOrders"
	pathSetLeverage    = "/openApi/swap/v2/trade/leverage"
	pathAccountBalance = "/openApi/swap/v2/user/balance"
	pathOpenPositions  = "/openApi/swap/v2/user/positions"

	// WS Channels.
	channelTicker = "ticker"
	channelKline  = "kline"
	channelDepth  = "depth"

	// String literals used frequently.
	sideBuy       = "BUY"
	sideSell      = "SELL"
	posSideLong   = "LONG"
	posSideShort  = "SHORT"
	stateLive     = "NEW"
	stateFilled   = "FILLED"
	stateCanceled = "CANCELED"
	statePartFill = "PARTIALLY_FILLED"

	// Signature and Auth keys.
	headerKey = "X-BX-APIKEY"

	// String literals flagged by goconst.
	paramSymbol     = "symbol"
	paramLimit      = "limit"
	constantUsdt    = "USDT"
	opSubscribe     = "subscribe"
	opUnsubscribe   = "unsubscribe"
	constantSuccess = "success"

	paramReqType      = "reqType"
	paramDataType     = "dataType"
	posSideBoth       = "BOTH"
	paramPositionSide = "positionSide"
	paramOrderId      = "orderId"
	paramLeverage     = "leverage"
	msgPing           = "Ping"
	opUnsub           = "unsub"
	opSub             = "sub"
)

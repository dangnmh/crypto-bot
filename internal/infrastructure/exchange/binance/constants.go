package binance

const (
	posSideLong  = "LONG"
	posSideShort = "SHORT"
	sideBuy      = "BUY"
	sideSell     = "SELL"
	statusNew    = "NEW"
	statusPart   = "PARTIALLY_FILLED"
	statusFilled = "FILLED"
	statusCancel = "CANCELED"

	orderTypeLimit  = "LIMIT"
	orderTypeMarket = "MARKET"

	opSubscribe   = "SUBSCRIBE"
	opUnsubscribe = "UNSUBSCRIBE"

	paramParams   = "params"
	msgKline      = "kline"
	statusExpired = "EXPIRED"

	interval15m = "15m"
	interval30m = "30m"
	paramMethod = "method"

	chTicker   = "ticker"
	chDepth    = "depth"
	chKline    = "kline"
	chOrder    = "personal.order"
	chPosition = "personal.position"

	evt24hrTicker     = "24hrTicker"
	evt24hrMiniTicker = "24hrMiniTicker"
	evtBookTicker     = "bookTicker"

	defaultPublicURL = "wss://fstream.binance.com/public"
	defaultMarketURL = "wss://fstream.binance.com/market"
	binanceTrueStr   = "true"
)

package gate

const (
	gateInterval15m = "15m"
	gateInterval30m = "30m"

	gateTifIOC = "ioc"

	gateOrderStatusFinished = "finished"
	gateOrderStatusOpen     = "open"
	gateFinishAsFilled      = "filled"
	gateFinishAsCancelled   = "cancelled"

	gateJSONTime    = "time"
	gateJSONChannel = "channel"
	gateJSONEvent   = "event"
	gateJSONPayload = "payload"
	gateJSONAuth    = "auth"
	gateJSONKey     = "KEY"
	gateJSONSign    = "SIGN"

	gateEventSubscribe   = "subscribe"
	gateEventUnsubscribe = "unsubscribe"

	gateChannelCandlesticks = "futures.candlesticks"
	gateChannelOrderBook    = "futures.order_book"
	gateChannelOrders       = "futures.orders"
	gateChannelPositions    = "futures.positions"
	gateChannelTickers      = "futures.book_ticker"
	gateChannelPing         = "futures.ping"

	gateAuthMethodAPIKey = "api_key"
	gatePayloadAll       = "!all"
	gateJSONMethod       = "method"
	gateSettleUsdt       = "usdt"

	gatePriceTypeLast = "last"

	gateMarginModeCross    = "CROSS"
	gateMarginModeIsolated = "ISOLATED"
)

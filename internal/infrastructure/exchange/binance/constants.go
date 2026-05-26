package binance

const (
	binanceOrderStatusNew             = "NEW"
	binanceOrderStatusPartiallyFilled = "PARTIALLY_FILLED"
	binanceOrderStatusFilled          = "FILLED"
	binanceOrderStatusCanceled        = "CANCELED"
	binanceOrderStatusExpired         = "EXPIRED"

	binanceTifGTC = "GTC"
	binanceTifIOC = "IOC"
	binanceTifFOK = "FOK"
	binanceTifGTX = "GTX" // PostOnly

	// WS Channels / Stream names
	binanceStreamTicker     = "ticker"
	binanceStreamKline      = "kline_1m"
	binanceStreamDepth      = "depth"
	binanceStreamUserEvents = "user_data"
)

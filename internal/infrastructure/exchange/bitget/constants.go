package bitget

const (
	// Base URL and Default Addresses.
	defaultRestURL = "https://api.bitget.com"
	defaultWsURL   = "wss://ws.bitget.com/v2/ws/public"
	defaultWsPriv  = "wss://ws.bitget.com/v2/ws/private"

	// REST Endpoints.
	pathServerTime       = "/api/v2/public/time"
	pathContracts        = "/api/v2/mix/market/contracts"
	pathTickers          = "/api/v2/mix/market/tickers"
	pathFundingRate      = "/api/v2/mix/market/current-fund-rate"
	pathKlines           = "/api/v2/mix/market/candles"
	pathDepth            = "/api/v2/mix/market/merge-depth"
	pathPlaceOrder       = "/api/v2/mix/order/place-order"
	pathCancelOrder      = "/api/v2/mix/order/cancel-order"
	pathCancelBatch      = "/api/v2/mix/order/batch-cancel-orders"
	pathGetOrder         = "/api/v2/mix/order/detail"
	pathPendingOrders    = "/api/v2/mix/order/orders-pending"
	pathSetLeverage      = "/api/v2/mix/account/set-leverage"
	pathAccountBalance   = "/api/v2/mix/account/accounts"
	pathOpenPositions    = "/api/v2/mix/position/all-position"
	pathHistoryPositions = "/api/v2/mix/position/history-position"

	// WS Channels.
	channelTicker           = "ticker"
	channelKline            = "candle1m"
	channelDepth            = "books"
	channelOrders           = "orders"
	channelPositions        = "positions"
	channelPositionsHistory = "positions-history"

	// Constants.
	productTypeUsdtFutures = "USDT-FUTURES"

	// String literals used frequently.
	sideBuy       = "buy"
	sideSell      = "sell"
	posSideLong   = "long"
	posSideShort  = "short"
	posSideNet    = "net"
	stateLive     = "live"
	stateInit     = "init"
	stateFilled   = "filled"
	stateCanceled = "canceled"
	statePartFill = "partially_filled"
	modeIsolated  = "isolated"
	modeCross     = "cross"

	// Signature and Auth keys.
	headerKey        = "ACCESS-KEY"
	headerSign       = "ACCESS-SIGN"
	headerTimestamp  = "ACCESS-TIMESTAMP"
	headerPassphrase = "ACCESS-PASSPHRASE"

	// String literals flagged by goconst.
	chPersonalPosition = "personal.position"
	paramProductType   = "productType"
	paramSymbol        = "symbol"
	paramLimit         = "limit"
	paramMarginMode    = "marginMode"
	paramMarginCoin    = "marginCoin"
	paramLeverage      = "leverage"
	modeCrossed        = "crossed"
	sideOpen           = "open"
	sideClose          = "close"
	constantUsdt       = "USDT"
	opSubscribe        = "subscribe"
	opUnsubscribe      = "unsubscribe"
	opLogin            = "login"
	fieldArgs          = "args"
	fieldChannel       = "channel"
	constantDefault    = "default"
	fieldInstId        = "instId"
	fieldInstType      = "instType"
)

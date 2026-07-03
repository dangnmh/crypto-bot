package aster

const (
	sideBuy  = "BUY"
	sideSell = "SELL"

	posSideLong  = "LONG"
	posSideShort = "SHORT"
	posSideBoth  = "BOTH"

	typeLimit            = "LIMIT"
	typeMarket           = "MARKET"
	typeTakeProfitMarket = "TAKE_PROFIT_MARKET"
	typeStopMarket       = "STOP_MARKET"

	timeInForceIOC = "IOC"
	timeInForceGTC = "GTC"
	timeInForceGTX = "GTX"
	timeInForceFOK = "FOK"

	marginModeIsolated = "ISOLATED"
	marginModeCross    = "CROSSED"

	statusFilled          = "FILLED"
	statusPartiallyFilled = "PARTIALLY_FILLED"
	statusCanceled        = "CANCELED"

	paramSymbol        = "symbol"
	paramSide          = "side"
	paramPositionSide  = "positionSide"
	paramType          = "type"
	paramQuantity      = "quantity"
	paramOrderId       = "orderId"
	paramStopPrice     = "stopPrice"
	paramClosePosition = "closePosition"
	paramReduceOnly    = "reduceOnly"
	paramMethod        = "method"
	paramParams        = "params"

	eventBookTicker       = "bookTicker"
	event24hrTicker       = "24hrTicker"
	eventOrderTradeUpdate = "ORDER_TRADE_UPDATE"
	eventAccountUpdate    = "ACCOUNT_UPDATE"

	valTrue = "true"
)

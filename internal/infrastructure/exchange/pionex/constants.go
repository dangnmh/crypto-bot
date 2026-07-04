package pionex

type PositionSide string

const (
	PositionSideLong  PositionSide = "LONG"
	PositionSideShort PositionSide = "SHORT"
)

type OrderSide string

const (
	OrderSideBuy  OrderSide = "BUY"
	OrderSideSell OrderSide = "SELL"
)

type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

const (
	symbolKey     = "symbol"
	limitKey      = "limit"
	typePerp      = "PERP"
	depthTopic    = "DEPTH"
	typeKey       = "type"
	topicKey      = "topic"
	bothSide      = "BOTH"
	openCloseMode = "OPENCLOSE"
)

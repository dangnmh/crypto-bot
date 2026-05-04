package exchange

import (
	"fmt"
)

// ContractDetail holds contract specification for a symbol.
type ContractDetail struct {
	Symbol                    string   `json:"symbol"`
	DisplayName               string   `json:"displayName"`
	DisplayNameEn             string   `json:"displayNameEn"`
	PositionOpenType          int      `json:"positionOpenType"`
	BaseCoin                  string   `json:"baseCoin"`
	QuoteCoin                 string   `json:"quoteCoin"`
	SettleCoin                string   `json:"settleCoin"`
	ContractSize              float64  `json:"contractSize"`
	MinLeverage               int      `json:"minLeverage"`
	MaxLeverage               int      `json:"maxLeverage"`
	PriceScale                int      `json:"priceScale"`
	VolScale                  int      `json:"volScale"`
	AmountScale               int      `json:"amountScale"`
	PriceUnit                 float64  `json:"priceUnit"`
	VolUnit                   int      `json:"volUnit"`
	MinVol                    int      `json:"minVol"`
	MaxVol                    int      `json:"maxVol"`
	BidLimitPriceRate         float64  `json:"bidLimitPriceRate"`
	AskLimitPriceRate         float64  `json:"askLimitPriceRate"`
	TakerFeeRate              float64  `json:"takerFeeRate"`
	MakerFeeRate              float64  `json:"makerFeeRate"`
	MaintenanceMarginRate     float64  `json:"maintenanceMarginRate"`
	InitialMarginRate         float64  `json:"initialMarginRate"`
	RiskBaseVol               int      `json:"riskBaseVol"`
	RiskIncrVol               int      `json:"riskIncrVol"`
	RiskIncrMmr               float64  `json:"riskIncrMmr"`
	RiskIncrImr               float64  `json:"riskIncrImr"`
	RiskLevelLimit            int      `json:"riskLevelLimit"`
	PriceCoefficientVariation float64  `json:"priceCoefficientVariation"`
	IndexOrigin               []string `json:"indexOrigin"`
	State                     int      `json:"state"`
	IsNew                     bool     `json:"isNew"`
	IsHot                     bool     `json:"isHot"`
	IsHidden                  bool     `json:"isHidden"`
}

// Ticker holds real-time ticker data for a symbol.
type Ticker struct {
	Symbol         string  `json:"symbol"`
	LastPrice      float64 `json:"lastPrice"`
	Bid1           float64 `json:"bid1"`
	Ask1           float64 `json:"ask1"`
	Volume24       float64 `json:"volume24"`
	Amount24       float64 `json:"amount24"`
	HoldVol        float64 `json:"holdVol"`
	Lower24Price   float64 `json:"lower24Price"`
	High24Price    float64 `json:"high24Price"`
	Change24Price  float64 `json:"change24Price"`
	ChangeRate     float64 `json:"changeRate"`
	IndexPrice     float64 `json:"indexPrice"`
	FairPrice      float64 `json:"fairPrice"`
	FundingRate    float64 `json:"fundingRate"`
	NextSettleTime int64   `json:"nextSettleTime"` // Unix ms — next funding settlement
	MaxBidPrice    float64 `json:"maxBidPrice"`
	MinAskPrice    float64 `json:"minAskPrice"`
	Timestamp      int64   `json:"timestamp"`
	RiseFallRate   float64 `json:"riseFallRate"`
	RiseFallValue  float64 `json:"riseFallValue"`
}

// FundingRateHistory holds historical funding rate entry.
type FundingRateHistory struct {
	Symbol      string  `json:"symbol"`
	FundingRate float64 `json:"fundingRate"`
	SettleTime  int64   `json:"settleTime"`
}

// FundingRateDetail holds current funding rate information.
type FundingRateDetail struct {
	Symbol         string  `json:"symbol"`
	FundingRate    float64 `json:"fundingRate"`
	MaxFundingRate float64 `json:"maxFundingRate"`
	MinFundingRate float64 `json:"minFundingRate"`
	CollectCycle   int     `json:"collectCycle"`
	NextSettleTime int64   `json:"nextSettleTime"`
	Timestamp      int64   `json:"timestamp"`
}

// Kline holds a single candlestick.
type Kline struct {
	Timestamp int64   `json:"timestamp"` // or "t" in WS
	Open      float64 `json:"open"`      // or "o"
	Close     float64 `json:"close"`     // or "c"
	High      float64 `json:"high"`      // or "h"
	Low       float64 `json:"low"`       // or "l"
	Volume    float64 `json:"volume"`    // or "v"
	Amount    float64 `json:"amount"`    // or "a"
}

// OrderBookEntry represents a single price level in the order book.
type OrderBookEntry struct {
	Price  float64
	Volume float64
}

// OrderBook represents the full or partial depth of a market.
type OrderBook struct {
	Symbol  string
	Version int64
	Asks    []OrderBookEntry // Sorted by price ascending (lowest ask first)
	Bids    []OrderBookEntry // Sorted by price descending (highest bid first)
}

// AssetInfo holds account asset information.
type AssetInfo struct {
	Currency         string  `json:"currency"`
	PositionMargin   float64 `json:"positionMargin"`
	FrozenBalance    float64 `json:"frozenBalance"`
	AvailableBalance float64 `json:"availableBalance"`
	CashBalance      float64 `json:"cashBalance"`
	Equity           float64 `json:"equity"`
	Unrealized       float64 `json:"unrealized"`
	Bonus            float64 `json:"bonus"`
}

// Position holds position information.
type Position struct {
	PositionID     int64   `json:"positionId"`
	Symbol         string  `json:"symbol"`
	PositionType   int     `json:"positionType"`
	OpenType       int     `json:"openType"`
	State          int     `json:"state"`
	HoldVol        float64 `json:"holdVol"`
	FrozenVol      float64 `json:"frozenVol"`
	CloseVol       float64 `json:"closeVol"`
	HoldAvgPrice   float64 `json:"holdAvgPrice"`
	OpenAvgPrice   float64 `json:"openAvgPrice"`
	CloseAvgPrice  float64 `json:"closeAvgPrice"`
	LiquidatePrice float64 `json:"liquidatePrice"`
	OIM            float64 `json:"oim"`
	IM             float64 `json:"im"`
	HoldFee        float64 `json:"holdFee"`
	Realised       float64 `json:"realized"`
	Leverage       int     `json:"leverage"`
	CreateTime     int64   `json:"createTime"`
	UpdateTime     int64   `json:"updateTime"`
	AutoAddIM      bool    `json:"autoAddIm"`
}

// OrderInfo holds order information.
type OrderInfo struct {
	OrderID      string  `json:"orderId"`
	Symbol       string  `json:"symbol"`
	PositionID   int64   `json:"positionId"`
	Price        float64 `json:"price"`
	Vol          float64 `json:"vol"`
	Leverage     int     `json:"leverage"`
	Side         int     `json:"side"`
	Category     int     `json:"category"`
	OrderType    int     `json:"orderType"`
	DealAvgPrice float64 `json:"dealAvgPrice"`
	DealVol      float64 `json:"dealVol"`
	OrderMargin  float64 `json:"orderMargin"`
	TakerFee     float64 `json:"takerFee"`
	MakerFee     float64 `json:"makerFee"`
	Profit       float64 `json:"profit"`
	FeeCurrency  string  `json:"feeCurrency"`
	OpenType     int     `json:"openType"`
	State        int     `json:"state"`
	ExternalOID  string  `json:"externalOid"`
	ErrorCode    int     `json:"errorCode"`
	UsedMargin   float64 `json:"usedMargin"`
	CreateTime   int64   `json:"createTime"`
	UpdateTime   int64   `json:"updateTime"`
	PositionMode int     `json:"positionMode"`
}

// CreateOrderResponse is the response from the create order API (only orderId + ts).
type CreateOrderResponse struct {
	OrderID string `json:"orderId"`
	Ts      int64  `json:"ts"`
}

// SubmitOrderRequest is the request body for creating a new order.
type SubmitOrderRequest struct {
	Symbol          string  `json:"symbol"`
	Price           float64 `json:"price,omitempty"`
	Vol             float64 `json:"vol"`
	Leverage        int     `json:"leverage,omitempty"`
	Side            int     `json:"side"`     // 1=OpenLong, 2=CloseShort, 3=OpenShort, 4=CloseLong
	Type            int     `json:"type"`     // 1=Limit, 2=PostOnly, 3=IOC, 4=FOK, 5=Market
	OpenType        int     `json:"openType"` // 1=Isolated, 2=Cross
	ExternalOID     string  `json:"externalOid,omitempty"`
	PositionID      int64   `json:"positionId,omitempty"`
	PositionMode    int     `json:"positionMode,omitempty"`    // 1=Hedge, 2=OneWay
	ReduceOnly      bool    `json:"reduceOnly,omitempty"`      // Reduce-only, only applicable in one-way mode
	FlashClose      bool    `json:"flashClose,omitempty"`      // Flash close
	StopLossPrice   float64 `json:"stopLossPrice,omitempty"`   // Server-side stop loss trigger price
	TakeProfitPrice float64 `json:"takeProfitPrice,omitempty"` // Server-side take profit trigger price
}

// SubmitTrackOrderRequest is the request body for creating a track (trailing stop) order.
type SubmitTrackOrderRequest struct {
	Symbol       string  `json:"symbol"`
	Leverage     int     `json:"leverage"`
	Side         int     `json:"side"` // 1=OpenLong, 2=CloseShort, 3=OpenShort, 4=CloseLong
	Vol          float64 `json:"vol"`
	OpenType     int     `json:"openType"` // 1=Isolated, 2=Cross
	Trend        int     `json:"trend"`    // 1=Latest, 2=Fair, 3=Index
	ActivePrice  float64 `json:"activePrice,omitempty"`
	BackType     int     `json:"backType"` // 1=Percentage, 2=Absolute
	BackValue    float64 `json:"backValue"`
	PositionMode int     `json:"positionMode,omitempty"`
	ReduceOnly   bool    `json:"reduceOnly,omitempty"`
}

// ChangeLeverageRequest is the request body for changing leverage.
type ChangeLeverageRequest struct {
	Symbol       string `json:"symbol"`
	Leverage     int    `json:"leverage"`
	OpenType     int    `json:"openType"`
	PositionType int    `json:"positionType"`
}

// Order side constants.
const (
	SideOpenLong   = 1
	SideCloseShort = 2
	SideOpenShort  = 3
	SideCloseLong  = 4
)

// SideStr returns a human-readable string for the side constant.
func SideStr(side int) string {
	switch side {
	case SideOpenLong:
		return "LONG"
	case SideOpenShort:
		return "SHORT"
	case SideCloseShort:
		return "CLOSE_SHORT"
	case SideCloseLong:
		return "CLOSE_LONG"
	default:
		return "UNKNOWN"
	}
}

// CloseSideFor returns the close side for a given open side.
func CloseSideFor(openSide int) int {
	if openSide == SideOpenLong {
		return SideCloseLong
	}
	return SideCloseShort
}

// Order state constants.
const (
	OrderStateFilled   = 3
	OrderStateCanceled = 4
	OrderStatePartial  = 5
)

// IsTerminalOrderState returns true if the order state is a terminal state.
func IsTerminalOrderState(state int) bool {
	return state == OrderStateFilled || state == OrderStateCanceled || state == OrderStatePartial
}

// Order type constants.
const (
	OrderTypeLimit    = 1
	OrderTypePostOnly = 2
	OrderTypeIOC      = 3
	OrderTypeFOK      = 4
	OrderTypeMarket   = 5
)

// Open type constants.
const (
	OpenTypeIsolated = 1
	OpenTypeCross    = 2
)

const (
	IntervalMin1 = "Min1"
)

// WsOrderDeal represents the parsed data from push.personal.order.
type WsOrderDeal struct {
	Symbol       string      `json:"symbol"`
	OrderID      interface{} `json:"orderId"`
	Price        float64     `json:"price"`
	Vol          float64     `json:"vol"`
	Side         int         `json:"side"`
	DealAvgPrice float64     `json:"dealAvgPrice"`
	DealVol      float64     `json:"dealVol"`
	State        int         `json:"state"` // 2: filled partly, 3: filled, 4: canceled
	ExternalOID  string      `json:"externalOid"`
	TakerFee     float64     `json:"takerFee"`
	MakerFee     float64     `json:"makerFee"`
	Profit       float64     `json:"profit"`
}

// GetOrderID returns the order ID as a string, handling both string and numeric JSON formats.
func (w *WsOrderDeal) GetOrderID() string {
	if s, ok := w.OrderID.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", w.OrderID)
}

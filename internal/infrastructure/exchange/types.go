package exchange

import (
	"encoding/json"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
)

// ──────────────────────────────────────────────────────────────────────
// Type aliases — reference domain as single source of truth
// ──────────────────────────────────────────────────────────────────────.

// OrderBook is an alias for the domain OrderBook type.
type OrderBook = domain.OrderBook

// OrderBookEntry is an alias for the domain OrderBookEntry type.
type OrderBookEntry = domain.OrderBookEntry

// Kline is an alias for the domain Kline type.
type Kline = domain.Kline

// DepthCommit represents one incremental depth update (exchange-agnostic).
// Exchange adapters convert their raw commit format into this struct.
type DepthCommit struct {
	Version int64            `json:"version"`
	Asks    []OrderBookEntry `json:"asks"` // Updated ask levels
	Bids    []OrderBookEntry `json:"bids"` // Updated bid levels
}

// Side constants — delegate to domain for backward compat.
const (
	SideOpenLong   = int(domain.SideOpenLong)
	SideCloseShort = int(domain.SideCloseShort)
	SideOpenShort  = int(domain.SideOpenShort)
	SideCloseLong  = int(domain.SideCloseLong)
)

// SideStr returns a human-readable string for the side constant.
func SideStr(side int) string {
	return domain.Side(side).String()
}

// CloseSideFor returns the close side for a given open side.
func CloseSideFor(openSide int) int {
	return int(domain.CloseSideFor(domain.Side(openSide)))
}

// Order state — delegate to domain.
const (
	OrderStateFilled   = domain.OrderStateFilled
	OrderStateCanceled = domain.OrderStateCanceled
	OrderStatePartial  = domain.OrderStatePartial
)

// IsTerminalOrderState delegates to domain.
func IsTerminalOrderState(state int) bool {
	return domain.IsTerminalOrderState(state)
}

// Order type — delegate to domain.
const (
	OrderTypeLimit    = domain.OrderTypeLimit
	OrderTypePostOnly = domain.OrderTypePostOnly
	OrderTypeIOC      = domain.OrderTypeIOC
	OrderTypeFOK      = domain.OrderTypeFOK
	OrderTypeMarket   = domain.OrderTypeMarket
)

// Open type — delegate to domain.
const (
	OpenTypeIsolated = domain.OpenTypeIsolated
	OpenTypeCross    = domain.OpenTypeCross
)

// IntervalMin1 delegates to domain.
const IntervalMin1 = domain.IntervalMin1

// ──────────────────────────────────────────────────────────────────────
// Exchange-specific API types (NOT duplicated in domain)
// ──────────────────────────────────────────────────────────────────────.

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
	Side            int     `json:"side"`               // 1=OpenLong, 2=CloseShort, 3=OpenShort, 4=CloseLong
	Type            int     `json:"type"`               // 1=Limit, 2=PostOnly, 3=IOC, 4=FOK, 5=Market
	OpenType        int     `json:"openType,omitempty"` // 1=Isolated, 2=Cross
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

// WsOrderDeal represents the parsed data from push.personal.order.
// It is kept for backward compatibility with existing order lifecycle tests.
type WsOrderDeal struct {
	Symbol       string      `json:"symbol"`
	OrderID      interface{} `json:"orderId"`
	PositionID   int64       `json:"positionId,omitempty"`
	Price        float64     `json:"price"`
	Vol          float64     `json:"vol"`
	Leverage     int         `json:"leverage,omitempty"`
	Side         int         `json:"side"`
	Category     int         `json:"category,omitempty"`
	OrderType    int         `json:"orderType,omitempty"`
	DealAvgPrice float64     `json:"dealAvgPrice"`
	DealVol      float64     `json:"dealVol"`
	OrderMargin  float64     `json:"orderMargin,omitempty"`
	UsedMargin   float64     `json:"usedMargin,omitempty"`
	TakerFee     float64     `json:"takerFee"`
	MakerFee     float64     `json:"makerFee"`
	Profit       float64     `json:"profit"`
	FeeCurrency  string      `json:"feeCurrency,omitempty"`
	OpenType     int         `json:"openType,omitempty"`
	State        int         `json:"state"` // exchange lifecycle state; do not rely on it alone for fills
	ErrorCode    int         `json:"errorCode,omitempty"`
	ExternalOID  string      `json:"externalOid"`
	CreateTime   int64       `json:"createTime,omitempty"`
	UpdateTime   int64       `json:"updateTime,omitempty"`
	RemainVol    float64     `json:"remainVol,omitempty"`
	PositionMode int         `json:"positionMode,omitempty"`
	ReduceOnly   bool        `json:"reduceOnly,omitempty"`
}

// GetOrderID returns the order ID as a string, handling both string and numeric JSON formats.
func (w *WsOrderDeal) GetOrderID() string {
	return interfaceIDToString(w.OrderID)
}

// PersonalOrderDeal represents push.personal.order.deal execution data.
type PersonalOrderDeal struct {
	ID           interface{} `json:"id"`
	Symbol       string      `json:"symbol"`
	Side         int         `json:"side"`
	Vol          float64     `json:"vol"`
	Price        float64     `json:"price"`
	FeeCurrency  string      `json:"feeCurrency"`
	Fee          float64     `json:"fee"`
	Timestamp    int64       `json:"timestamp"`
	Profit       float64     `json:"profit"`
	IsTaker      bool        `json:"isTaker"`
	Category     int         `json:"category"`
	OrderID      interface{} `json:"orderId"`
	IsSelf       bool        `json:"isSelf"`
	ExternalOID  string      `json:"externalOid"`
	PositionMode int         `json:"positionMode"`
	ReduceOnly   bool        `json:"reduceOnly"`
	OpponentUID  int64       `json:"opponentUid"`
}

func (d *PersonalOrderDeal) GetID() string {
	return interfaceIDToString(d.ID)
}

func (d *PersonalOrderDeal) GetOrderID() string {
	return interfaceIDToString(d.OrderID)
}

// PersonalTrackOrderUpdate represents push.personal.track.order data.
type PersonalTrackOrderUpdate struct {
	ID           interface{} `json:"id"`
	Symbol       string      `json:"symbol"`
	Leverage     int         `json:"leverage"`
	Side         int         `json:"side"`
	Vol          float64     `json:"vol"`
	OpenType     int         `json:"openType"`
	Trend        int         `json:"trend"`
	ActivePrice  float64     `json:"activePrice"`
	MarkPrice    float64     `json:"markPrice"`
	BackType     int         `json:"backType"`
	BackValue    float64     `json:"backValue"`
	TriggerPrice float64     `json:"triggerPrice"`
	TriggerType  int         `json:"triggerType"`
	OrderID      interface{} `json:"orderId"`
	ErrorCode    int         `json:"errorCode"`
	State        int         `json:"state"`
	PositionMode int         `json:"positionMode"`
	ReduceOnly   bool        `json:"reduceOnly"`
	CreateTime   int64       `json:"createTime"`
	UpdateTime   int64       `json:"updateTime"`
}

func (t *PersonalTrackOrderUpdate) GetID() string {
	return interfaceIDToString(t.ID)
}

func (t *PersonalTrackOrderUpdate) GetOrderID() string {
	return interfaceIDToString(t.OrderID)
}

func interfaceIDToString(id interface{}) string {
	switch v := id.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
	case float32:
		v64 := float64(v)
		if v64 == float64(int64(v64)) {
			return strconv.FormatInt(int64(v64), 10)
		}
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	}
	return fmt.Sprintf("%v", id)
}

// PersonalPositionUpdate represents push.personal.position data.
type PersonalPositionUpdate struct {
	PositionID             int64           `json:"positionId"`
	Symbol                 string          `json:"symbol"`
	HoldVol                float64         `json:"holdVol"`
	PositionType           int             `json:"positionType"`
	OpenType               int             `json:"openType"`
	State                  int             `json:"state"`
	FrozenVol              float64         `json:"frozenVol"`
	CloseVol               float64         `json:"closeVol"`
	HoldAvgPrice           float64         `json:"holdAvgPrice"`
	HoldAvgPriceFullyScale string          `json:"holdAvgPriceFullyScale"`
	CloseAvgPrice          float64         `json:"closeAvgPrice"`
	OpenAvgPrice           float64         `json:"openAvgPrice"`
	OpenAvgPriceFullyScale string          `json:"openAvgPriceFullyScale"`
	LiquidatePrice         float64         `json:"liquidatePrice"`
	OIM                    float64         `json:"oim"`
	ADLLevel               int             `json:"adlLevel"`
	IM                     float64         `json:"im"`
	HoldFee                float64         `json:"holdFee"`
	Realized               float64         `json:"realised"` //nolint:misspell // MEXC uses this JSON spelling.
	Leverage               int             `json:"leverage"`
	AutoAddIM              bool            `json:"autoAddIm"`
	PNL                    float64         `json:"pnl"`
	MarginRatio            float64         `json:"marginRatio"`
	NewOpenAvgPrice        float64         `json:"newOpenAvgPrice"`
	NewCloseAvgPrice       float64         `json:"newCloseAvgPrice"`
	CloseProfitLoss        float64         `json:"closeProfitLoss"`
	Fee                    float64         `json:"fee"`
	DeductFeeList          json.RawMessage `json:"deductFeeList,omitempty"`
	MakerFeeRate           float64         `json:"makerFeeRate"`
	TakerFeeRate           float64         `json:"takerFeeRate"`
	CreateTime             int64           `json:"createTime"`
	UpdateTime             int64           `json:"updateTime"`
	Version                int64           `json:"version"`
}

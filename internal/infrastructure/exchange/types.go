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

// Exchange name constants.
const (
	ExchangeMexc        = "mexc"
	ExchangeGate        = "gate"
	ExchangeBybit       = "bybit"
	ExchangeBinance     = "binance"
	ExchangeOkx         = "okx"
	ExchangeHyperliquid = "hyperliquid"
	ExchangeBitget      = "bitget"
	ExchangeBingx       = "bingx"
	ExchangeKucoin      = "kucoin"
	ExchangeDeepcoin    = "deepcoin"
	ExchangeToobit      = "toobit"
	ExchangeWeex        = "weex"
)

// Side constants — delegate to domain.
const (
	SideOpenLong   = domain.SideOpenLong
	SideCloseShort = domain.SideCloseShort
	SideOpenShort  = domain.SideOpenShort
	SideCloseLong  = domain.SideCloseLong
)

// SideStr returns a human-readable string for the side constant.
func SideStr(side domain.Side) string {
	return side.String()
}

// CloseSideFor returns the close side for a given open side.
func CloseSideFor(openSide domain.Side) domain.Side {
	return domain.CloseSideFor(openSide)
}

// Order state — delegate to domain.
const (
	OrderStateNew             = domain.OrderStateNew
	OrderStatePartiallyFilled = domain.OrderStatePartiallyFilled
	OrderStateFilled          = domain.OrderStateFilled
	OrderStateCanceled        = domain.OrderStateCanceled
	OrderStatePartial         = domain.OrderStatePartial
	OrderStateUntriggered     = domain.OrderStateUntriggered
)

// IsTerminalOrderState delegates to domain.
func IsTerminalOrderState(state domain.OrderState) bool {
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
	Symbol       string  `json:"symbol"`
	LastPrice    float64 `json:"lastPrice"`
	Bid1         float64 `json:"bid1"`
	Ask1         float64 `json:"ask1"`
	Volume24     float64 `json:"volume24"`
	AmountUSDT24 float64 `json:"amountUSDT24"`
	Timestamp    int64   `json:"timestamp"`
}

// FundingRateHistory holds historical funding rate entry.
type FundingRateHistory struct {
	Symbol      string  `json:"symbol"`
	FundingRate float64 `json:"fundingRate"`
	SettleTime  int64   `json:"settleTime"`
}

// FundingRateResult holds the rate and settlement time for active scan query.
type FundingRateResult struct {
	Symbol     string  `json:"symbol"`
	Rate       float64 `json:"rate"`
	SettleTime int64   `json:"settleTime"`
}

// Position holds position information.
type Position struct {
	Symbol          string       `json:"symbol"`
	HoldVol         float64      `json:"holdVol"`
	PositionType    PositionType `json:"positionType"`
	OpenAvgPrice    float64      `json:"openAvgPrice"`
	HoldAvgPrice    float64      `json:"holdAvgPrice"`
	CloseAvgPrice   float64      `json:"closeAvgPrice"`
	CloseProfitLoss float64      `json:"closeProfitLoss"`
	Fee             float64      `json:"fee"`
	HoldFee         float64      `json:"holdFee"`
}

// OrderInfo holds order information.
type OrderInfo struct {
	OrderID      string              `json:"orderId"`
	Symbol       string              `json:"symbol"`
	Price        float64             `json:"price"`
	Vol          float64             `json:"vol"`
	DealAvgPrice float64             `json:"dealAvgPrice"`
	DealVol      float64             `json:"dealVol"`
	State        domain.OrderState   `json:"state"`
	ExternalOID  string              `json:"externalOid"`
	Side         domain.Side         `json:"side"`
	PositionMode domain.PositionMode `json:"positionMode"`
	CreateTime   int64               `json:"createTime,omitempty"`
	UpdateTime   int64               `json:"updateTime,omitempty"`
}

// CreateOrderResponse is the response from the create order API (only orderId + ts).
type CreateOrderResponse struct {
	OrderID string `json:"orderId"`
	Ts      int64  `json:"ts"`
}

// SubmitOrderRequest is the request body for creating a new order.
type SubmitOrderRequest struct {
	Symbol          string              `json:"symbol"`
	Price           float64             `json:"price,omitempty"`
	Vol             float64             `json:"vol"`
	Leverage        int                 `json:"leverage,omitempty"`
	Side            domain.Side         `json:"side"`               // 1=OpenLong, 2=CloseShort, 3=OpenShort, 4=CloseLong
	Type            domain.OrderType    `json:"type"`               // 1=Limit, 2=PostOnly, 3=IOC, 4=FOK, 5=Market
	OpenType        domain.OpenType     `json:"openType,omitempty"` // 1=Isolated, 2=Cross
	ExternalOID     string              `json:"externalOid,omitempty"`
	PositionID      int64               `json:"positionId,omitempty"`
	PositionMode    domain.PositionMode `json:"positionMode,omitempty"`    // 1=Hedge, 2=OneWay
	ReduceOnly      bool                `json:"reduceOnly,omitempty"`      // Reduce-only, only applicable in one-way mode
	FlashClose      bool                `json:"flashClose,omitempty"`      // Flash close
	StopLossPrice   float64             `json:"stopLossPrice,omitempty"`   // Server-side stop loss trigger price
	TakeProfitPrice float64             `json:"takeProfitPrice,omitempty"` // Server-side take profit trigger price
}

// PositionType represents a position side (1=Long, 2=Short, etc.).
type PositionType int

const (
	PositionTypeUnknown PositionType = 0
	PositionTypeLong    PositionType = 1
	PositionTypeShort   PositionType = 2
)

// ChangeLeverageRequest is the request body for changing leverage.
type ChangeLeverageRequest struct {
	Symbol       string          `json:"symbol"`
	Leverage     int             `json:"leverage"`
	OpenType     domain.OpenType `json:"openType"`
	PositionType PositionType    `json:"positionType"`
}

// WsOrderDeal represents the parsed data from push.personal.order.
// It is kept for backward compatibility with existing order lifecycle tests.
type WsOrderDeal struct {
	Symbol       string              `json:"symbol"`
	OrderID      any                 `json:"orderId"`
	PositionID   int64               `json:"positionId,omitempty"`
	Price        float64             `json:"price"`
	Vol          float64             `json:"vol"`
	Leverage     int                 `json:"leverage,omitempty"`
	Side         domain.Side         `json:"side"`
	Category     int                 `json:"category,omitempty"`
	OrderType    domain.OrderType    `json:"orderType,omitempty"`
	DealAvgPrice float64             `json:"dealAvgPrice"`
	DealVol      float64             `json:"dealVol"`
	OrderMargin  float64             `json:"orderMargin,omitempty"`
	UsedMargin   float64             `json:"usedMargin,omitempty"`
	TakerFee     float64             `json:"takerFee"`
	MakerFee     float64             `json:"makerFee"`
	Profit       float64             `json:"profit"`
	FeeCurrency  string              `json:"feeCurrency,omitempty"`
	OpenType     domain.OpenType     `json:"openType,omitempty"`
	State        domain.OrderState   `json:"state"` // exchange lifecycle state; do not rely on it alone for fills
	ErrorCode    int                 `json:"errorCode,omitempty"`
	ExternalOID  string              `json:"externalOid"`
	CreateTime   int64               `json:"createTime,omitempty"`
	UpdateTime   int64               `json:"updateTime,omitempty"`
	RemainVol    float64             `json:"remainVol,omitempty"`
	PositionMode domain.PositionMode `json:"positionMode,omitempty"`
	ReduceOnly   bool                `json:"reduceOnly,omitempty"`
}

// GetOrderID returns the order ID as a string, handling both string and numeric JSON formats.
func (w *WsOrderDeal) GetOrderID() string {
	return interfaceIDToString(w.OrderID)
}

// PersonalOrderDeal represents push.personal.order.deal execution data.
type PersonalOrderDeal struct {
	ID           any     `json:"id"`
	Symbol       string  `json:"symbol"`
	Side         int     `json:"side"`
	Vol          float64 `json:"vol"`
	Price        float64 `json:"price"`
	FeeCurrency  string  `json:"feeCurrency"`
	Fee          float64 `json:"fee"`
	Timestamp    int64   `json:"timestamp"`
	Profit       float64 `json:"profit"`
	IsTaker      bool    `json:"isTaker"`
	Category     int     `json:"category"`
	OrderID      any     `json:"orderId"`
	IsSelf       bool    `json:"isSelf"`
	ExternalOID  string  `json:"externalOid"`
	PositionMode int     `json:"positionMode"`
	ReduceOnly   bool    `json:"reduceOnly"`
	OpponentUID  int64   `json:"opponentUid"`
}

func (d *PersonalOrderDeal) GetID() string {
	return interfaceIDToString(d.ID)
}

func (d *PersonalOrderDeal) GetOrderID() string {
	return interfaceIDToString(d.OrderID)
}

// PersonalTrackOrderUpdate represents push.personal.track.order data.
type PersonalTrackOrderUpdate struct {
	ID           any     `json:"id"`
	Symbol       string  `json:"symbol"`
	Leverage     int     `json:"leverage"`
	Side         int     `json:"side"`
	Vol          float64 `json:"vol"`
	OpenType     int     `json:"openType"`
	Trend        int     `json:"trend"`
	ActivePrice  float64 `json:"activePrice"`
	MarkPrice    float64 `json:"markPrice"`
	BackType     int     `json:"backType"`
	BackValue    float64 `json:"backValue"`
	TriggerPrice float64 `json:"triggerPrice"`
	TriggerType  int     `json:"triggerType"`
	OrderID      any     `json:"orderId"`
	ErrorCode    int     `json:"errorCode"`
	State        int     `json:"state"`
	PositionMode int     `json:"positionMode"`
	ReduceOnly   bool    `json:"reduceOnly"`
	CreateTime   int64   `json:"createTime"`
	UpdateTime   int64   `json:"updateTime"`
}

func (t *PersonalTrackOrderUpdate) GetID() string {
	return interfaceIDToString(t.ID)
}

func (t *PersonalTrackOrderUpdate) GetOrderID() string {
	return interfaceIDToString(t.OrderID)
}

func interfaceIDToString(id any) string {
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
	Symbol          string       `json:"symbol"`
	HoldVol         float64      `json:"holdVol"`
	PositionType    PositionType `json:"positionType"`
	OpenAvgPrice    float64      `json:"openAvgPrice"`
	HoldAvgPrice    float64      `json:"holdAvgPrice"`
	CloseVol        float64      `json:"closeVol"`
	CloseAvgPrice   float64      `json:"closeAvgPrice"`
	CloseProfitLoss float64      `json:"closeProfitLoss"`
	Fee             float64      `json:"fee"`
	HoldFee         float64      `json:"holdFee"`
	Leverage        int          `json:"leverage,omitempty"`
	LiquidatePrice  float64      `json:"liquidatePrice,omitempty"`
	UpdateTime      int64        `json:"updateTime,omitempty"`
}

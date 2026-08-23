package futures

import (
	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

type mexcOrder struct {
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

type mexcPosition struct {
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

func mapMexcStatus(state int, dealVol float64) domain.OrderState {
	switch state {
	case 1, 2:
		if dealVol > 0 {
			return exchange.OrderStatePartiallyFilled
		}
		return exchange.OrderStateNew
	case 3:
		return exchange.OrderStateFilled
	case 4, 5:
		return exchange.OrderStateCanceled
	default:
		return exchange.OrderStateNew
	}
}

func (o *mexcOrder) toOrderInfo() *exchange.OrderInfo {
	if o == nil {
		return nil
	}
	return &exchange.OrderInfo{
		OrderID:      o.OrderID,
		Symbol:       o.Symbol,
		Price:        o.Price,
		Vol:          o.Vol,
		DealAvgPrice: o.DealAvgPrice,
		DealVol:      o.DealVol,
		State:        mapMexcStatus(o.State, o.DealVol),
		ExternalOID:  o.ExternalOID,
		Side:         domain.Side(o.Side),
		PositionMode: domain.PositionMode(o.PositionMode),
		CreateTime:   o.CreateTime,
		UpdateTime:   o.UpdateTime,
	}
}

func (p *mexcPosition) toPosition() exchange.Position {
	return exchange.Position{
		Symbol:          p.Symbol,
		HoldVolContract: p.HoldVol,
		HoldVolCoin:     p.HoldVol,
		RawHoldVol:      p.HoldVol,
		PositionType:    exchange.PositionType(p.PositionType),
		OpenAvgPrice:    p.OpenAvgPrice,
		HoldAvgPrice:    p.HoldAvgPrice,
		CloseAvgPrice:   p.CloseAvgPrice,
		HoldFee:         p.HoldFee,
		Leverage:        p.Leverage,
	}
}

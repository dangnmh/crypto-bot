package futures

import (
	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type kucoinContract struct {
	Symbol                  string  `json:"symbol"`
	BaseCurrency            string  `json:"baseCurrency"`
	QuoteCurrency           string  `json:"quoteCurrency"`
	SettleCurrency          string  `json:"settleCurrency"`
	LotSize                 int64   `json:"lotSize"`
	TickSize                float64 `json:"tickSize"`
	Multiplier              float64 `json:"multiplier"`
	Status                  string  `json:"status"`
	TurnoverOf24h           float64 `json:"turnoverOf24h"`
	VolumeOf24h             float64 `json:"volumeOf24h"`
	FundingFeeRate          float64 `json:"fundingFeeRate"`
	NextFundingRateDateTime int64   `json:"nextFundingRateDateTime"`
	MaxLeverage             float64 `json:"maxLeverage"`
	LastTradePrice          float64 `json:"lastTradePrice"`
	MarkPrice               float64 `json:"markPrice"`
	IndexPrice              float64 `json:"indexPrice"`
	PriceChgPct             float64 `json:"priceChgPct"`
	ChangeRate24h           float64 `json:"changeRate24h"`
	ChangePrice24h          float64 `json:"changePrice24h"`
}

type kucoinSingleTicker struct {
	Symbol       string `json:"symbol"`
	BestBidPrice string `json:"bestBidPrice"`
	BestAskPrice string `json:"bestAskPrice"`
	Price        string `json:"price"`
	Size         string `json:"size"`
	Ts           string `json:"ts"`
}

type kucoinTicker struct {
	Symbol             string `json:"symbol"`
	BestBidPrice       string `json:"bestBidPrice"`
	BestAskPrice       string `json:"bestAskPrice"`
	LastPrice          string `json:"lastPrice"`
	Price              string `json:"price"`
	Volume             string `json:"volume"`
	Vol                string `json:"vol"`
	PriceChangePercent string `json:"priceChangePercent"`
	PriceChgPct        string `json:"priceChgPct"`
	ChangeRate         string `json:"changeRate"`
	ChangePrice        string `json:"changePrice"`
	Ts                 int64  `json:"ts"`
}

type kucoinOrder struct {
	ID             string  `json:"id"`
	Symbol         string  `json:"symbol"`
	Type           string  `json:"type"`
	Side           string  `json:"side"`
	Price          string  `json:"price"`
	Size           float64 `json:"size"`
	Value          string  `json:"value"`
	DealValue      string  `json:"dealValue"`
	DealSize       float64 `json:"dealSize"`
	Stp            string  `json:"stp"`
	Stop           string  `json:"stop"`
	StopPriceType  string  `json:"stopPriceType"`
	StopPrice      string  `json:"stopPrice"`
	TimeInForce    string  `json:"timeInForce"`
	PostOnly       bool    `json:"postOnly"`
	Hidden         bool    `json:"hidden"`
	Iceberg        bool    `json:"iceberg"`
	Leverage       string  `json:"leverage"`
	ForceHold      bool    `json:"forceHold"`
	CloseOrder     bool    `json:"closeOrder"`
	VisibleSize    string  `json:"visibleSize"`
	ClientOid      string  `json:"clientOid"`
	Remark         string  `json:"remark"`
	Tags           string  `json:"tags"`
	IsActive       bool    `json:"isActive"`
	CancelExist    bool    `json:"cancelExist"`
	CreatedAt      int64   `json:"createdAt"`
	UpdatedAt      int64   `json:"updatedAt"`
	OrderTime      int64   `json:"orderTime"`
	SettleCurrency string  `json:"settleCurrency"`
	MarginMode     string  `json:"marginMode"`
	AvgDealPrice   string  `json:"avgDealPrice"`
	FilledSize     float64 `json:"filledSize"`
	FilledValue    string  `json:"filledValue"`
	Status         string  `json:"status"`
	ReduceOnly     bool    `json:"reduceOnly"`
}

type kucoinPosition struct {
	ID             string  `json:"id"`
	Symbol         string  `json:"symbol"`
	AutoDeposit    bool    `json:"autoDeposit"`
	MaintMarginReq float64 `json:"maintMarginReq"`
	RiskLimitLevel int     `json:"riskLimitLevel"`
	RealLeverage   float64 `json:"realLeverage"`
	CrossMode      bool    `json:"crossMode"`
	DelevPct       float64 `json:"delevPercentage"`
	OpeningTime    int64   `json:"openingTimestamp"`
	CurrentQty     float64 `json:"currentQty"`
	CurrentCost    float64 `json:"currentCost"`
	CurrentComm    float64 `json:"currentComm"`
	UnrealisedCost float64 `json:"unrealisedCost"`
	RealisedGross  float64 `json:"realisedGrossCost"`
	RealisedPnl    float64 `json:"realisedPnl"`
	RealisedCost   float64 `json:"realisedCost"`
	IsOpen         bool    `json:"isOpen"`
	MarkPrice      float64 `json:"markPrice"`
	MarkValue      float64 `json:"markValue"`
	PosCost        float64 `json:"posCost"`
	PosCross       float64 `json:"posCross"`
	PosInit        float64 `json:"posInit"`
	PosComm        float64 `json:"posComm"`
	PosLoss        float64 `json:"posLoss"`
	PosMargin      float64 `json:"posMargin"`
	PosMaint       float64 `json:"posMaint"`
	MaintMargin    float64 `json:"maintMargin"`
	RealisedGrossP float64 `json:"realisedGrossPnl"`
	UnrealisedPnl  float64 `json:"unrealisedPnl"`
	UnrealisedPnlP float64 `json:"unrealisedPnlPcnt"`
	UnrealisedRoeP float64 `json:"unrealisedRoePcnt"`
	AvgEntryPrice  float64 `json:"avgEntryPrice"`
	LiquidationP   float64 `json:"liquidationPrice"`
	BankruptPrice  float64 `json:"bankruptPrice"`
	SettleCurrency string  `json:"settleCurrency"`
}

func (o *kucoinOrder) toOrderInfo() *exchange.OrderInfo {
	if o == nil {
		return nil
	}

	price := decmath.ParseFloat(o.Price)
	dealAvgPrice := decmath.ParseFloat(o.AvgDealPrice)
	dealVol := o.DealSize
	if dealVol == 0 {
		dealVol = o.FilledSize
	}

	state := exchange.OrderStateNew
	if !o.IsActive {
		switch {
		case o.CancelExist:
			state = exchange.OrderStateCanceled
		case dealVol >= o.Size && o.Size > 0:
			state = exchange.OrderStateFilled
		default:
			state = exchange.OrderStateCanceled
		}
	} else if dealVol > 0 {
		state = exchange.OrderStatePartiallyFilled
	}

	side := domain.SideOpenLong
	if o.Side == sideSell {
		if o.ReduceOnly {
			side = domain.SideCloseLong
		} else {
			side = domain.SideOpenShort
		}
	}

	return &exchange.OrderInfo{
		OrderID:      o.ID,
		Symbol:       o.Symbol,
		Price:        price,
		Vol:          o.Size,
		DealAvgPrice: dealAvgPrice,
		DealVol:      dealVol,
		State:        state,
		ExternalOID:  o.ClientOid,
		Side:         side,
		CreateTime:   o.CreatedAt,
		UpdateTime:   o.UpdatedAt,
	}
}

func (p *kucoinPosition) toPosition() exchange.Position {
	posType := exchange.PositionTypeLong
	holdVol := p.CurrentQty
	if holdVol < 0 {
		posType = exchange.PositionTypeShort
		holdVol = -holdVol
	}

	return exchange.Position{
		Symbol:          p.Symbol,
		HoldVolContract: holdVol,
		RawHoldVol:      p.CurrentQty,
		PositionType:    posType,
		OpenAvgPrice:    p.AvgEntryPrice,
		HoldAvgPrice:    p.AvgEntryPrice,
		CloseAvgPrice:   p.MarkPrice,
		CloseProfitLoss: p.RealisedPnl,
		Fee:             p.CurrentComm,
		Leverage:        int(p.RealLeverage),
	}
}

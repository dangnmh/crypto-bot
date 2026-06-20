package binance

type checkServerTimeResponse struct {
	ServerTime int64 `json:"serverTime"`
}

type exchangeInformationResponse struct {
	Symbols []exchangeInfoSymbol `json:"symbols"`
}

type exchangeInfoSymbol struct {
	Symbol            string               `json:"symbol"`
	Status            string               `json:"status"`
	ContractType      string               `json:"contractType"`
	BaseAsset         string               `json:"baseAsset"`
	QuoteAsset        string               `json:"quoteAsset"`
	MarginAsset       string               `json:"marginAsset"`
	PricePrecision    int64                `json:"pricePrecision"`
	QuantityPrecision int64                `json:"quantityPrecision"`
	Filters           []exchangeInfoFilter `json:"filters"`
}

type exchangeInfoFilter struct {
	FilterType string `json:"filterType"`
	TickSize   string `json:"tickSize,omitempty"`
	MinQty     string `json:"minQty,omitempty"`
	StepSize   string `json:"stepSize,omitempty"`
}

type ticker24hStats struct {
	Symbol      string `json:"symbol"`
	Volume      string `json:"volume"`
	QuoteVolume string `json:"quoteVolume"`
	LastPrice   string `json:"lastPrice"`
}

type bookTicker struct {
	Symbol   string `json:"symbol"`
	BidPrice string `json:"bidPrice"`
	AskPrice string `json:"askPrice"`
}

type markPriceInfo struct {
	Symbol          string `json:"symbol"`
	LastFundingRate string `json:"lastFundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
}

type positionRiskItem struct {
	Symbol       string `json:"symbol"`
	PositionAmt  string `json:"positionAmt"`
	EntryPrice   string `json:"entryPrice"`
	PositionSide string `json:"positionSide"`
}

type userTradeItem struct {
	Symbol      string `json:"symbol"`
	ID          int64  `json:"id"`
	OrderId     int64  `json:"orderId"`
	Price       string `json:"price"`
	Qty         string `json:"qty"`
	RealizedPnl string `json:"realizedPnl"`
	Commission  string `json:"commission"`
	Buyer       bool   `json:"buyer"`
	Side        string `json:"side"`
	Time        int64  `json:"time"`
}

type incomeHistoryItem struct {
	Symbol     string  `json:"symbol"`
	IncomeType string  `json:"incomeType"`
	Income     *string `json:"income"`
	Time       int64   `json:"time"`
}

type binanceOrder struct {
	OrderId       int64  `json:"orderId"`
	Symbol        string `json:"symbol"`
	Status        string `json:"status"`
	ClientOrderId string `json:"clientOrderId"`
	Price         string `json:"price"`
	AvgPrice      string `json:"avgPrice"`
	OrigQty       string `json:"origQty"`
	ExecutedQty   string `json:"executedQty"`
	PositionSide  string `json:"positionSide"`
	Side          string `json:"side"`
	Time          *int64 `json:"time,omitempty"`
	UpdateTime    *int64 `json:"updateTime,omitempty"`
}

type changeLeverageResponse struct {
	Symbol   string `json:"symbol"`
	Leverage int    `json:"leverage"`
}

type listenKeyResponse struct {
	ListenKey string `json:"listenKey"`
}

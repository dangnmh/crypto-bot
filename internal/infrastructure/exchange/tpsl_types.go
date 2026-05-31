package exchange

// TPSLRequest is the request body for placing post-fill Take Profit and Stop Loss orders.
type TPSLRequest struct {
	Symbol          string  `json:"symbol"`
	PositionMode    int     `json:"positionMode"` // 1=Hedge, 2=OneWay
	Side            int     `json:"side"`         // Main order opening side (1=OpenLong, 3=OpenShort)
	TakeProfitPrice float64 `json:"takeProfitPrice,omitempty"`
	StopLossPrice   float64 `json:"stopLossPrice,omitempty"`
	Volume          float64 `json:"volume,omitempty"`
}

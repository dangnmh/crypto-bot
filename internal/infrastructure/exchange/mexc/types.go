package mexc

import (
	"encoding/json"
	"fmt"
	"net/http"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"

	"crypto-bot/pkg/xjson"
)

// APIResponse is the generic MEXC Futures REST response envelope.
type APIResponse[T any] struct {
	Success bool   `json:"success"`
	Code    int    `json:"code"`
	Data    T      `json:"data"`
	Message string `json:"message,omitempty"`
}

// ParseResponse is a generic helper that unmarshals the MEXC response envelope,
// checks for API-level errors, and returns the typed Data payload.
// This replaces the repetitive unmarshal+check pattern across all API methods.
func ParseResponse[T any](body []byte, path string) (T, error) {
	var resp APIResponse[T]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		var zero T
		return zero, fmt.Errorf("parse %s response: %w", path, err)
	}
	if !resp.Success {
		var zero T
		return zero, toAPIError(resp.Code, resp.Message, path)
	}
	return resp.Data, nil
}

// ParseResponseIgnoreData parses the envelope and checks for errors,
// but discards the data payload. Used for void-return operations (cancel, close).
func ParseResponseIgnoreData(body []byte, path string) error {
	var resp APIResponse[json.RawMessage]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse %s response: %w", path, err)
	}
	if !resp.Success {
		return toAPIError(resp.Code, resp.Message, path)
	}
	return nil
}

// toAPIError converts an MEXC error response into a structured exchange.APIError.
func toAPIError(code int, message, path string) *exchange.APIError {
	return &exchange.APIError{
		Code:    code,
		Message: message,
		Path:    path,
	}
}

// toHTTPError creates an APIError for non-200 HTTP status codes.
func toHTTPError(statusCode int, body []byte, path string) *exchange.APIError {
	return &exchange.APIError{
		StatusCode: statusCode,
		Message:    string(body),
		Path:       path,
	}
}

// isRateLimited checks if an HTTP response indicates rate limiting.
func isRateLimited(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests
}

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
		HoldVol:         p.HoldVol,
		PositionType:    exchange.PositionType(p.PositionType),
		OpenAvgPrice:    p.OpenAvgPrice,
		HoldAvgPrice:    p.HoldAvgPrice,
		CloseAvgPrice:   p.CloseAvgPrice,
		CloseProfitLoss: 0,
		Fee:             0,
		HoldFee:         p.HoldFee,
		Leverage:        p.Leverage,
	}
}

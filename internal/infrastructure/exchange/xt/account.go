package xt

import (
	"context"
	"crypto-bot/pkg/xjson"
	"net/http"
)

type xtBillItem struct {
	AfterAmount xjson.Number `json:"afterAmount"`
	Amount      xjson.Number `json:"amount"`
	Coin        string       `json:"coin"`
	CreatedTime int64        `json:"createdTime"`
	ID          xjson.Number `json:"id"`
	Side        string       `json:"side"`
	Symbol      string       `json:"symbol"`
	Type        string       `json:"type"`
}

type xtBillResult struct {
	HasNext bool         `json:"hasNext"`
	HasPrev bool         `json:"hasPrev"`
	Items   []xtBillItem `json:"items"`
}

type xtBalanceBillsResponse struct {
	ReturnCode int64        `json:"returnCode"`
	MsgInfo    string       `json:"msgInfo"`
	Result     xtBillResult `json:"result"`
}

// GetBalanceBillsRaw fetches raw account balance bills/flows.
func (c *Client) GetBalanceBillsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/future/user/v1/balance/bills", params, nil)
}

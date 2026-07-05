package bybit

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

const exchangeName = "bybit"

// Explicit request/response structs for account/position endpoints.

type bybitPositionsRequest struct {
	Category string `json:"category"`
	Symbol   string `json:"symbol,omitempty"`
}

type bybitClosedPnLRequest struct {
	Category  string `json:"category"`
	Symbol    string `json:"symbol"`
	Limit     int    `json:"limit,omitempty"`
	StartTime int64  `json:"startTime,omitempty"`
}

type bybitTransactionLogRequest struct {
	AccountType string `json:"accountType"`
	Category    string `json:"category"`
	Type        string `json:"type,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
	Limit       int    `json:"limit,omitempty"`
	StartTime   int64  `json:"startTime,omitempty"`
}

type bybitSwitchPositionModeRequest struct {
	Category string `json:"category"`
	Symbol   string `json:"symbol"`
	Mode     int    `json:"mode"`
}

type bybitPosition struct {
	Symbol          string `json:"symbol"`
	Side            string `json:"side"`
	Size            string `json:"size"`
	EntryPrice      string `json:"entryPrice"`
	AvgPrice        string `json:"avgPrice"`
	LiqPrice        string `json:"liqPrice"`
	Leverage        string `json:"leverage"`
	TradeMode       *int   `json:"tradeMode"`
	PositionIdx     int    `json:"positionIdx"`
	PositionValue   string `json:"positionValue"`
	PositionIM      string `json:"positionIM"`
	PositionBalance string `json:"positionBalance"`
	UnrealisedPnl   string `json:"unrealisedPnl"`
	CurRealisedPnl  string `json:"curRealisedPnl"`
	CumRealisedPnl  string `json:"cumRealisedPnl"`
	CreatedTime     string `json:"createdTime"`
	UpdatedTime     string `json:"updatedTime"`
	AutoAddMargin   int    `json:"autoAddMargin"`
}

type bybitClosedPnLRow struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Qty           string `json:"qty"`
	OrderPrice    string `json:"orderPrice"`
	OrderType     string `json:"orderType"`
	ClosedSize    string `json:"closedSize"`
	AvgEntryPrice string `json:"avgEntryPrice"`
	AvgExitPrice  string `json:"avgExitPrice"`
	ClosedPnl     string `json:"closedPnl"`
	OpenFee       string `json:"openFee"`
	CloseFee      string `json:"closeFee"`
	CreatedTime   string `json:"createdTime"`
	UpdatedTime   string `json:"updatedTime"`
}

type bybitClosedPnLResult struct {
	List []bybitClosedPnLRow `json:"list"`
}

type bybitTransactionLogChange struct {
	Symbol   string `json:"symbol"`
	Type     string `json:"type"`
	Funding  string `json:"funding"`
	Change   string `json:"change"`
	Fee      string `json:"fee"`
	CashFlow string `json:"cashFlow"`
}

// Private raw methods invoking the Bybit API.

func (c *Client) rawGetOpenPositions(ctx context.Context, req bybitPositionsRequest) ([]bybitPosition, error) {
	params := map[string]string{}
	if req.Category != "" {
		params["category"] = req.Category
	}
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}
	body, err := c.GetOpenPositionsRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	return decodeListResponse[bybitPosition](body, "bybit get position")
}

func (c *Client) rawGetClosedPnL(ctx context.Context, req bybitClosedPnLRequest) (*bybitClosedPnLResult, error) {
	params := map[string]string{}
	if req.Category != "" {
		params["category"] = req.Category
	}
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}
	if req.Limit > 0 {
		params["limit"] = fmt.Sprintf("%d", req.Limit)
	}
	if req.StartTime > 0 {
		params["startTime"] = fmt.Sprintf("%d", req.StartTime)
	}
	body, err := c.GetClosedPnLRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	res, err := parseResponse[bybitClosedPnLResult](body, "bybit get closed pnl")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) rawGetTransactionLog(ctx context.Context, req bybitTransactionLogRequest) ([]bybitTransactionLogChange, error) {
	params := map[string]string{}
	if req.AccountType != "" {
		params["accountType"] = req.AccountType
	}
	if req.Category != "" {
		params["category"] = req.Category
	}
	if req.Type != "" {
		params["type"] = req.Type
	}
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}
	if req.Limit > 0 {
		params["limit"] = fmt.Sprintf("%d", req.Limit)
	}
	if req.StartTime > 0 {
		params["startTime"] = fmt.Sprintf("%d", req.StartTime)
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/v5/account/transaction-log", params, nil)
	if err != nil {
		return nil, err
	}
	return decodeListResponse[bybitTransactionLogChange](body, "bybit query transaction log")
}

//nolint:dupl // Structurally similar to rawSwitchPositionMode and rawSetMarginMode.
func (c *Client) rawSwitchPositionMode(ctx context.Context, req bybitSwitchPositionModeRequest) error {
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return fmt.Errorf("bybit switch position mode marshal: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, "/v5/position/switch-mode", nil, bodyBytes)
	if err != nil {
		return fmt.Errorf("bybit switch position mode: %w", err)
	}

	var resp bybitResponse[any]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("bybit switch position mode json unmarshal: %w", err)
	}
	if resp.RetCode != 0 {
		if resp.RetCode == 110025 || strings.Contains(strings.ToLower(resp.RetMsg), "position mode not modified") {
			return nil
		}
		return fmt.Errorf("bybit switch position mode error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetOpenPositions returns all open positions, optionally filtered by symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	rawList, err := c.rawGetOpenPositions(ctx, bybitPositionsRequest{
		Category: categoryLinear,
		Symbol:   symbol,
	})
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(rawList))
	for i := range rawList {
		pos := &rawList[i]
		if decmath.ParseFloat(pos.Size) > 0 {
			positions = append(positions, mapPosition(*pos))
		}
	}

	return positions, nil
}

// ClosePosition closes a single position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         closeSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
		Leverage:     leverage,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "bybit"),
	}
	_, err := c.CreateOrder(ctx, req)
	return err
}

// CloseAllPositions closes all open positions.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := positions[i]
		if pos.HoldVol > 0 {
			side := domain.SideCloseShort
			if pos.PositionType == exchange.PositionTypeLong {
				side = domain.SideCloseLong
			}
			err = c.ClosePosition(ctx, symbol, side, pos.HoldVol, domain.PositionModeHedge, pos.Leverage)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// SwitchPositionMode switches the position mode (Hedge vs One-Way) for Bybit.
func (c *Client) SwitchPositionMode(ctx context.Context, symbol string, positionMode domain.PositionMode) error {
	mode := 0 // One-Way
	if positionMode == domain.PositionModeHedge {
		mode = 3 // Hedge
	}

	return c.rawSwitchPositionMode(ctx, bybitSwitchPositionModeRequest{
		Category: categoryLinear,
		Symbol:   symbol,
		Mode:     mode,
	})
}

// GetOrderPNL queries the most recent closed PnL ledger record from Bybit.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	rawOrder, err := c.rawGetOrder(ctx, bybitGetOrderRequest{
		Category: categoryLinear,
		OrderID:  orderID,
	})
	if err != nil {
		return nil, fmt.Errorf("bybit get order by ID %s failed: %w", orderID, err)
	}

	orderInfo := mapOrderInfo(*rawOrder)
	if orderInfo.State == exchange.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	entryCreatedTimeStr := rawOrder.CreatedTime

	req := bybitClosedPnLRequest{
		Category: categoryLinear,
		Symbol:   symbol,
		Limit:    10,
	}
	var startTimeVal time.Time
	if orderInfo.CreateTime > 0 {
		req.StartTime = orderInfo.CreateTime - 1000
		startTimeVal = time.UnixMilli(req.StartTime)
	}

	res, err := c.rawGetClosedPnL(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("query closed pnl failed: %w", err)
	}

	if len(res.List) == 0 {
		return nil, fmt.Errorf("query closed pnl failed: no closed pnl records found for symbol %s", symbol)
	}

	row := res.List[0]

	entryPrice := decmath.ParseFloat(row.AvgEntryPrice)
	exitPrice := decmath.ParseFloat(row.AvgExitPrice)
	closedSize := decmath.ParseFloat(row.ClosedSize)
	closedPnl := decmath.ParseFloat(row.ClosedPnl)
	openFee := decmath.ParseFloat(row.OpenFee)
	closeFee := decmath.ParseFloat(row.CloseFee)

	// Query Bybit's Transaction Log to get the settled funding fee (holdFee) for this symbol.
	fdFee, err := c.getHoldFee(ctx, symbol, startTimeVal)
	if err != nil {
		c.logger.Debug("Bybit failed to query transaction log for funding fee", slog.Any("error", err))
	}

	// Bybit's closedPnl is net realized PnL (already has openFee, closeFee, and fundingFee deducted).
	// We convert it to gross ClosedPnL (Gross Profit/Loss before any fees) by adding back execution fees and subtracting funding fee:
	grossPnL := closedPnl + openFee + closeFee - fdFee

	entryCreatedTime := decmath.ParseInt64(entryCreatedTimeStr)
	if entryCreatedTime == 0 {
		entryCreatedTime = decmath.ParseInt64(row.CreatedTime) // Fallback for unit tests where order createdTime might be empty.
	}
	closeTime := decmath.ParseInt64(row.UpdatedTime)
	duration := max(closeTime-entryCreatedTime, 0)

	return &exchange.ClosedPnLInfo{
		Exchange:   exchangeName,
		Symbol:     row.Symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   grossPnL,
		Fee:        openFee + closeFee,
		FundingFee: fdFee,
		DurationMs: duration,
		NetPnl:     closedPnl,
	}, nil
}

// Helper mapping functions.

// mapPosition maps a bybitPosition to exchange.Position.
func mapPosition(raw bybitPosition) exchange.Position {
	avgPrice := raw.EntryPrice
	if avgPrice == "" {
		avgPrice = raw.AvgPrice
	}

	lev, _ := strconv.Atoi(raw.Leverage)
	pos := exchange.Position{
		Symbol:       raw.Symbol,
		HoldVol:      decmath.ParseFloat(raw.Size),
		HoldAvgPrice: decmath.ParseFloat(avgPrice),
		OpenAvgPrice: decmath.ParseFloat(avgPrice),
		Leverage:     lev,
	}

	switch raw.PositionIdx {
	case 1:
		pos.PositionType = exchange.PositionTypeLong // Long
	case 2:
		pos.PositionType = exchange.PositionTypeShort // Short
	default:
		// OneWay mode fallback.
		if strings.EqualFold(raw.Side, "buy") {
			pos.PositionType = exchange.PositionTypeLong // Long
		} else if strings.EqualFold(raw.Side, "sell") {
			pos.PositionType = exchange.PositionTypeShort // Short
		}
	}

	return pos
}

// getHoldFee queries Bybit's Transaction Log to retrieve the settled funding fee for a symbol.
func (c *Client) getHoldFee(ctx context.Context, symbol string, startTime time.Time) (float64, error) {
	apiAccountType := accountTypeContract
	if strings.EqualFold(c.accountType, "unified") {
		apiAccountType = accountTypeUnified
	}
	req := bybitTransactionLogRequest{
		AccountType: apiAccountType,
		Category:    categoryLinear,
		Type:        "SETTLEMENT",
		Symbol:      symbol,
		Limit:       10,
	}

	if !startTime.IsZero() {
		req.StartTime = startTime.UnixMilli()
	}

	list, err := c.rawGetTransactionLog(ctx, req)
	if err != nil {
		return 0, err
	}

	if len(list) == 0 {
		return 0, nil
	}

	// Prefer the 'funding' field for funding fee, fall back to 'change' if empty.
	fundingVal := list[0].Funding
	if fundingVal == "" {
		fundingVal = list[0].Change
	}

	return decmath.ParseFloat(fundingVal), nil
}

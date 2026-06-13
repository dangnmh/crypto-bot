package bybit

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for account endpoints.

type bybitWalletBalanceRequest struct {
	AccountType string `json:"accountType"`
}

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

type bybitCoinBalance struct {
	Coin          string `json:"coin"`
	Equity        string `json:"equity"`
	WalletBalance string `json:"walletBalance"`
	UnrealisedPnl string `json:"unrealisedPnl"`
}

type bybitWalletBalance struct {
	TotalEquity        string             `json:"totalEquity"`
	TotalWalletBalance string             `json:"totalWalletBalance"`
	Coin               []bybitCoinBalance `json:"coin"`
}

type bybitWalletBalanceResult struct {
	List []bybitWalletBalance `json:"list"`
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

func (c *Client) getRawAssets(ctx context.Context, req bybitWalletBalanceRequest) (*bybitWalletBalanceResult, error) {
	body, err := c.sendRequest(ctx, http.MethodGet, "/v5/account/wallet-balance", req, true)
	if err != nil {
		return nil, fmt.Errorf("bybit list assets: %w", err)
	}
	res, err := parseResponse[bybitWalletBalanceResult](body, "bybit list assets")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawOpenPositions(ctx context.Context, req bybitPositionsRequest) ([]bybitPosition, error) {
	body, err := c.sendRequest(ctx, http.MethodGet, "/v5/position/list", req, true)
	if err != nil {
		return nil, err
	}
	return decodeListResponse[bybitPosition](body, "bybit get position")
}

func (c *Client) getRawClosedPnL(ctx context.Context, req bybitClosedPnLRequest) (*bybitClosedPnLResult, error) {
	body, err := c.sendRequest(ctx, http.MethodGet, "/v5/position/closed-pnl", req, true)
	if err != nil {
		return nil, err
	}
	res, err := parseResponse[bybitClosedPnLResult](body, "bybit get closed pnl")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawTransactionLog(ctx context.Context, req bybitTransactionLogRequest) ([]bybitTransactionLogChange, error) {
	body, err := c.sendRequest(ctx, http.MethodGet, "/v5/account/transaction-log", req, true)
	if err != nil {
		return nil, err
	}
	return decodeListResponse[bybitTransactionLogChange](body, "bybit query transaction log")
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetAssets returns the account assets.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	apiAccountType := accountTypeContract
	if strings.EqualFold(c.accountType, "unified") {
		apiAccountType = accountTypeUnified
	}

	res, err := c.getRawAssets(ctx, bybitWalletBalanceRequest{
		AccountType: apiAccountType,
	})
	if err != nil {
		return nil, err
	}

	assets := []exchange.AssetInfo{}
	for i := range res.List {
		wallet := &res.List[i]
		for j := range wallet.Coin {
			coin := &wallet.Coin[j]
			equity := decmath.ParseFloat(coin.Equity)
			unrealized := decmath.ParseFloat(coin.UnrealisedPnl)
			assets = append(assets, exchange.AssetInfo{
				Currency:         coin.Coin,
				AvailableBalance: decmath.ParseFloat(coin.WalletBalance) + unrealized,
				Equity:           equity,
				Unrealized:       unrealized,
				CashBalance:      equity - unrealized,
			})
		}
	}

	if len(assets) == 0 {
		// Provide default zero balance asset if empty.
		assets = append(assets, exchange.AssetInfo{
			Currency: "USDT",
		})
	}

	return assets, nil
}

// GetAssetByCurrency returns the account asset for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	assets, err := c.GetAssets(ctx)
	if err != nil {
		return nil, err
	}

	for _, asset := range assets {
		if strings.EqualFold(asset.Currency, currency) {
			return &asset, nil
		}
	}

	return &exchange.AssetInfo{
		Currency: currency,
	}, nil
}

// GetOpenPositions returns all open positions, optionally filtered by symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	rawList, err := c.getRawOpenPositions(ctx, bybitPositionsRequest{
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

// GetRecentClosedPnL queries the most recent closed PnL ledger record from Bybit.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	// Look up numeric orderID from client order ID (extOrderID / orderLinkId).
	rawOrder, err := c.getRawOrder(ctx, bybitGetOrderRequest{
		Category:    categoryLinear,
		OrderLinkID: extOrderID,
	})
	if err != nil {
		errStr := err.Error()
		errStr = strings.Replace(errStr, "bybit get order error", "bybit get order by external ID error", 1)
		return nil, fmt.Errorf("%s", errStr)
	}

	entryCreatedTimeStr := rawOrder.CreatedTime

	req := bybitClosedPnLRequest{
		Category: categoryLinear,
		Symbol:   symbol,
		Limit:    10,
	}
	if !startTime.IsZero() {
		req.StartTime = startTime.UnixMilli()
	}

	res, err := c.getRawClosedPnL(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("query closed pnl failed: %w", err)
	}

	if len(res.List) == 0 {
		return nil, fmt.Errorf("query closed pnl failed: no closed pnl records found for symbol %s", symbol)
	}

	row := res.List[0]
	updatedTime := decmath.ParseInt64(row.UpdatedTime)
	// Check if the record is fresh (updated within the last 15 seconds).
	if time.Now().UnixMilli()-updatedTime >= 15000 {
		return nil, fmt.Errorf("query closed pnl failed: stale closed pnl record found for symbol %s", symbol)
	}

	entryPrice := decmath.ParseFloat(row.AvgEntryPrice)
	exitPrice := decmath.ParseFloat(row.AvgExitPrice)
	closedSize := decmath.ParseFloat(row.ClosedSize)
	closedPnl := decmath.ParseFloat(row.ClosedPnl)
	openFee := decmath.ParseFloat(row.OpenFee)
	closeFee := decmath.ParseFloat(row.CloseFee)

	// Query Bybit's Transaction Log to get the settled funding fee (holdFee) for this symbol.
	fdFee, err := c.getHoldFee(ctx, symbol, startTime)
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
		Exchange:   "bybit",
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

	pos := exchange.Position{
		Symbol:       raw.Symbol,
		HoldVol:      decmath.ParseFloat(raw.Size),
		HoldAvgPrice: decmath.ParseFloat(avgPrice),
		OpenAvgPrice: decmath.ParseFloat(avgPrice),
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

	list, err := c.getRawTransactionLog(ctx, req)
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

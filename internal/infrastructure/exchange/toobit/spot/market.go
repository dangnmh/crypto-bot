package spot

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type toobitTicker struct {
	T   int64  `json:"t"`
	A   string `json:"a"`
	B   string `json:"b"`
	S   string `json:"s"`
	C   string `json:"c"`
	O   string `json:"o"`
	H   string `json:"h"`
	L   string `json:"l"`
	V   string `json:"v"`
	Qv  string `json:"qv"`
	Pc  string `json:"pc"`
	Pcp string `json:"pcp"`
}

type toobitExchangeInfo struct {
	Contracts []toobitContract `json:"contracts"`
	Symbols   []toobitContract `json:"symbols"`
}

type toobitContract struct {
	Symbol             string         `json:"symbol"`
	BaseAsset          string         `json:"baseAsset"`
	QuoteAsset         string         `json:"quoteAsset"`
	MarginAsset        string         `json:"marginAsset"`
	ContractMultiplier string         `json:"contractMultiplier"`
	Filters            []toobitFilter `json:"filters"`
}

type toobitFilter struct {
	FilterType string `json:"filterType"`
	TickSize   string `json:"tickSize,omitempty"`
	StepSize   string `json:"stepSize,omitempty"`
	MinQty     string `json:"minQty,omitempty"`
	MaxQty     string `json:"maxQty,omitempty"`
	MinPrice   string `json:"minPrice,omitempty"`
}

type toobitDepthResponse struct {
	Time int64            `json:"time"`
	Bids [][]xjson.Number `json:"bids"`
	Asks [][]xjson.Number `json:"asks"`
}

type serverTimeResponse struct {
	ServerTime int64 `json:"serverTime"`
}

func isToobitSpotSymbol(symbol string) bool {
	if strings.HasPrefix(symbol, "TBV_") || strings.HasPrefix(symbol, "TEST") {
		return false
	}
	return !strings.Contains(symbol, "-SWAP-")
}

func isToobitValidSpotTicker(symbol string, timestamp, nowMs int64) bool {
	if !isToobitSpotSymbol(symbol) {
		return false
	}
	if timestamp > 0 && nowMs > 0 {
		diff := nowMs - timestamp
		if diff < 0 {
			diff = -diff
		}
		if diff > int64(24*time.Hour/time.Millisecond) {
			return false
		}
	}
	return true
}

// GetContractDetails returns spot trading pairs specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/exchangeInfo", nil, false)
	if err != nil {
		return nil, err
	}
	var resp toobitExchangeInfo
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal exchange info: %w", err)
	}

	contracts := resp.Symbols
	if len(contracts) == 0 {
		contracts = resp.Contracts
	}

	details := make([]exchange.ContractDetail, 0, len(contracts))
	for i := range contracts {
		raw := &contracts[i]
		if !isToobitSpotSymbol(raw.Symbol) {
			continue
		}

		priceUnit := 0.0
		minVol := 0.0
		maxVol := 0.0
		stepSize := 0.0
		tickSizeStr := ""
		stepSizeStr := ""

		for _, f := range raw.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				priceUnit = decmath.ParseFloat(f.TickSize)
				tickSizeStr = f.TickSize
			case "LOT_SIZE":
				minVol = decmath.ParseFloat(f.MinQty)
				maxVol = decmath.ParseFloat(f.MaxQty)
				stepSize = decmath.ParseFloat(f.StepSize)
				stepSizeStr = f.StepSize
			}
		}

		priceScale := decmath.DecimalPlaces(tickSizeStr)
		volScale := decmath.DecimalPlaces(stepSizeStr)

		multiplier := 1.0
		if raw.ContractMultiplier != "" {
			multiplier = decmath.ParseFloat(raw.ContractMultiplier)
		}

		maxVolVal := int(maxVol)
		if maxVolVal <= 0 {
			maxVolVal = 1000000000
		}

		details = append(details, exchange.ContractDetail{
			Symbol:        raw.Symbol,
			DisplayName:   raw.Symbol,
			DisplayNameEn: raw.Symbol,
			BaseCoin:      raw.BaseAsset,
			QuoteCoin:     raw.QuoteAsset,
			ContractSize:  multiplier,
			MinLeverage:   1,
			MaxLeverage:   1,
			PriceUnit:     priceUnit,
			MinVol:        int(minVol),
			MaxVol:        maxVolVal,
			VolUnit:       int(stepSize),
			PriceScale:    priceScale,
			VolScale:      volScale,
			State:         1,
		})
	}

	return details, nil
}

// GetTopGainer returns spot tickers sorted by 24h price change percentage descending.
func (c *Client) GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/quote/v1/ticker/24hr", nil, false)
	if err != nil {
		return nil, fmt.Errorf("toobit get top gainer tickers: %w", err)
	}

	var rawList []toobitTicker
	if err := xjson.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal toobit spot top gainer tickers: %w", err)
	}

	nowMs := int64(0)
	if c.base.Clock() != nil {
		nowMs = c.base.Clock().Now().UnixMilli()
	}

	results := make([]exchange.TopGainerResult, 0, len(rawList))
	for i := range rawList {
		item := &rawList[i]
		if !isToobitValidSpotTicker(item.S, item.T, nowMs) {
			continue
		}
		last, _ := strconv.ParseFloat(item.C, 64)
		bid, _ := strconv.ParseFloat(item.B, 64)
		ask, _ := strconv.ParseFloat(item.A, 64)
		qv, _ := strconv.ParseFloat(item.Qv, 64)
		pcp, _ := strconv.ParseFloat(item.Pcp, 64)

		if last <= 0 {
			continue
		}

		spreadPct := 0.0
		if bid > 0 && ask > 0 {
			spreadPct = ((ask - bid) / bid) * 100.0
		}

		results = append(results, exchange.TopGainerResult{
			Symbol:        item.S,
			LastPrice:     last,
			Bid1:          bid,
			Ask1:          ask,
			Volume24hUSDT: qv,
			Gain24hPct:    pcp,
			SpreadPct:     spreadPct,
			Timestamp:     item.T,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Gain24hPct > results[j].Gain24hPct
	})

	if req.Limit > 0 && req.Limit < len(results) {
		results = results[:req.Limit]
	}

	return results, nil
}

func formatSpotSymbol(symbol string) string {
	s := strings.ToUpper(symbol)
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// GetDepth retrieves the current Level 2 orderbook for a spot symbol.
func (c *Client) GetDepth(ctx context.Context, symbol string) (*domain.OrderBook, error) {
	sym := formatSpotSymbol(symbol)
	params := map[string]string{
		symbolKey: sym,
		"limit":   "100",
	}
	body, err := c.base.Request(ctx, http.MethodGet, "/quote/v1/depth", params, false)
	if err != nil {
		return nil, fmt.Errorf("toobit get spot depth for %s: %w", symbol, err)
	}

	var data toobitDepthResponse
	if err := xjson.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("toobit parse spot depth for %s: %w", symbol, err)
	}

	bids := make([]domain.OrderBookEntry, 0, len(data.Bids))
	for _, b := range data.Bids {
		if len(b) >= 2 {
			p, v := xjson.ToFloat64(b[0]), xjson.ToFloat64(b[1])
			if p > 0 && v > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(data.Asks))
	for _, a := range data.Asks {
		if len(a) >= 2 {
			p, v := xjson.ToFloat64(a[0]), xjson.ToFloat64(a[1])
			if p > 0 && v > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return &domain.OrderBook{
		Symbol:  symbol,
		Version: data.Time,
		Bids:    bids,
		Asks:    asks,
	}, nil
}

// GetServerTime returns the server millisecond timestamp.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/time", nil, false)
	if err != nil {
		return 0, err
	}
	var resp serverTimeResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal server time: %w", err)
	}
	return resp.ServerTime, nil
}

// Ping sends a ping to the server.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.GetServerTime(ctx)
	return err
}

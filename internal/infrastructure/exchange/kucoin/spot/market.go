package spot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type kucoinSpotSymbol struct {
	Symbol         string `json:"symbol"`
	Name           string `json:"name"`
	BaseCurrency   string `json:"baseCurrency"`
	QuoteCurrency  string `json:"quoteCurrency"`
	PriceIncrement string `json:"priceIncrement"`
	BaseMinSize    string `json:"baseMinSize"`
	BaseIncrement  string `json:"baseIncrement"`
	EnableTrading  bool   `json:"enableTrading"`
}

type kucoinSpotAllTickersData struct {
	Time   int64              `json:"time"`
	Ticker []kucoinSpotTicker `json:"ticker"`
}

type kucoinSpotTicker struct {
	Symbol      string `json:"symbol"`
	SymbolName  string `json:"symbolName"`
	Buy         string `json:"buy"`
	Sell        string `json:"sell"`
	ChangeRate  string `json:"changeRate"`
	ChangePrice string `json:"changePrice"`
	High        string `json:"high"`
	Low         string `json:"low"`
	Vol         string `json:"vol"`
	VolValue    string `json:"volValue"`
	Last        string `json:"last"`
}

type kucoinSpotDepthData struct {
	Sequence string     `json:"sequence"`
	Time     int64      `json:"time"`
	Bids     [][]string `json:"bids"`
	Asks     [][]string `json:"asks"`
}

// GetContractDetails returns spot trading symbol specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v2/symbols", nil, nil, false)
	if err != nil {
		return nil, err
	}
	symbols, err := kucoin.ParseResponse[[]kucoinSpotSymbol](body, "spot_symbols")
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(symbols))
	for _, sym := range symbols {
		stateVal := 0
		if sym.EnableTrading {
			stateVal = 1
		}
		tickSize := decmath.ParseFloat(sym.PriceIncrement)
		if tickSize <= 0 {
			tickSize = 0.0001
		}
		priceScale := decmath.DecimalPlaces(sym.PriceIncrement)
		if priceScale <= 0 {
			priceScale = 2
		}

		details = append(details, exchange.ContractDetail{
			Symbol:           sym.Symbol,
			DisplayName:      sym.Name,
			DisplayNameEn:    sym.Name,
			PositionOpenType: 1,
			BaseCoin:         sym.BaseCurrency,
			QuoteCoin:        sym.QuoteCurrency,
			ContractSize:     1.0,
			MinLeverage:      1,
			MaxLeverage:      1,
			PriceScale:       priceScale,
			VolScale:         0,
			PriceUnit:        tickSize,
			State:            stateVal,
		})
	}
	return details, nil
}

// GetTopGainer returns spot tickers sorted by 24h price change percentage descending.
func (c *Client) GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/market/allTickers", nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("kucoin get spot allTickers: %w", err)
	}

	data, err := kucoin.ParseResponse[kucoinSpotAllTickersData](body, "spot_all_tickers")
	if err != nil {
		return nil, fmt.Errorf("parse kucoin spot allTickers: %w", err)
	}

	results := make([]exchange.TopGainerResult, 0, len(data.Ticker))
	for i := range data.Ticker {
		t := &data.Ticker[i]
		last := decmath.ParseFloat(t.Last)
		if last <= 0 {
			continue
		}
		volUSDT := decmath.ParseFloat(t.VolValue)
		bid := decmath.ParseFloat(t.Buy)
		ask := decmath.ParseFloat(t.Sell)
		spreadPct := 0.0
		if bid > 0 && ask > 0 {
			spreadPct = ((ask - bid) / bid) * 100.0
		}
		gainPct := decmath.ParseFloat(t.ChangeRate) * 100.0

		results = append(results, exchange.TopGainerResult{
			Symbol:        t.Symbol,
			LastPrice:     last,
			Bid1:          bid,
			Ask1:          ask,
			Volume24hUSDT: volUSDT,
			Gain24hPct:    gainPct,
			SpreadPct:     spreadPct,
			Timestamp:     data.Time,
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

// GetDepth retrieves the current Level 2 orderbook for a spot symbol.
func (c *Client) GetDepth(ctx context.Context, symbol string) (*domain.OrderBook, error) {
	params := map[string]string{
		"symbol": symbol,
	}
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/market/orderbook/level2_100", params, nil, false)
	if err != nil {
		return nil, fmt.Errorf("kucoin get spot depth for %s: %w", symbol, err)
	}

	data, err := kucoin.ParseResponse[kucoinSpotDepthData](body, "spot_depth_snapshot")
	if err != nil {
		return nil, fmt.Errorf("kucoin parse spot depth for %s: %w", symbol, err)
	}

	bids := make([]domain.OrderBookEntry, 0, len(data.Bids))
	for _, b := range data.Bids {
		if len(b) >= 2 {
			p := decmath.ParseFloat(b[0])
			v := decmath.ParseFloat(b[1])
			if p > 0 && v > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(data.Asks))
	for _, a := range data.Asks {
		if len(a) >= 2 {
			p := decmath.ParseFloat(a[0])
			v := decmath.ParseFloat(a[1])
			if p > 0 && v > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return &domain.OrderBook{
		Symbol:  symbol,
		Version: decmath.ParseInt64(data.Sequence),
		Bids:    bids,
		Asks:    asks,
	}, nil
}

// GetServerTime returns the server timestamp.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/timestamp", nil, nil, false)
	if err != nil {
		return 0, err
	}
	var numVal int64
	if err := xjson.Unmarshal(body, &numVal); err == nil {
		return numVal, nil
	}
	return kucoin.ParseResponse[int64](body, "server_time")
}

// Ping sends a lightweight ping to verify connection.
func (c *Client) Ping(ctx context.Context) error {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/timestamp", nil, nil, false)
	if err != nil {
		return err
	}
	var raw json.RawMessage
	return xjson.Unmarshal(body, &raw)
}

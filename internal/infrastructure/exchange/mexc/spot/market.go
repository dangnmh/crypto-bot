package spot

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type mexcSpotTicker struct {
	Symbol             string       `json:"symbol"`
	PriceChangePercent xjson.Number `json:"priceChangePercent"`
	LastPrice          xjson.Number `json:"lastPrice"`
	BidPrice           xjson.Number `json:"bidPrice"`
	AskPrice           xjson.Number `json:"askPrice"`
	QuoteVolume        xjson.Number `json:"quoteVolume"`
	Volume             xjson.Number `json:"volume"`
	CloseTime          int64        `json:"closeTime"`
}

type mexcSpotDepthRawData struct {
	LastUpdateId int64            `json:"lastUpdateId"`
	Bids         [][]xjson.Number `json:"bids"`
	Asks         [][]xjson.Number `json:"asks"`
}

type mexcSpotSymbolInfo struct {
	Symbol             string       `json:"symbol"`
	Status             string       `json:"status"`
	BaseAsset          string       `json:"baseAsset"`
	QuoteAsset         string       `json:"quoteAsset"`
	QuotePrecision     int          `json:"quotePrecision"`
	BaseAssetPrecision int          `json:"baseAssetPrecision"`
	Filters            []mexcFilter `json:"filters"`
}

type mexcFilter struct {
	FilterType string `json:"filterType"`
	TickSize   string `json:"tickSize,omitempty"`
	MinQty     string `json:"minQty,omitempty"`
	MaxQty     string `json:"maxQty,omitempty"`
	StepSize   string `json:"stepSize,omitempty"`
}

type mexcSpotExchangeInfo struct {
	Symbols []mexcSpotSymbolInfo `json:"symbols"`
}

// GetContractDetails returns spot exchange symbol specifications.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v3/exchangeInfo", nil, nil, false)
	if err != nil {
		return nil, err
	}
	var info mexcSpotExchangeInfo
	if err := xjson.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("parse mexc spot exchangeInfo: %w", err)
	}

	details := make([]exchange.ContractDetail, 0, len(info.Symbols))
	for _, sym := range info.Symbols {
		tickSize := 0.0001
		if sym.QuotePrecision > 0 {
			tickSize = 1.0
			for i := 0; i < sym.QuotePrecision; i++ {
				tickSize /= 10.0
			}
		}
		for _, f := range sym.Filters {
			if f.FilterType == "PRICE_FILTER" && f.TickSize != "" {
				if val, err := strconv.ParseFloat(f.TickSize, 64); err == nil && val > 0 {
					tickSize = val
				}
			}
		}
		details = append(details, exchange.ContractDetail{
			Symbol:       sym.Symbol,
			DisplayName:  sym.Symbol,
			BaseCoin:     sym.BaseAsset,
			QuoteCoin:    sym.QuoteAsset,
			ContractSize: 1.0,
			PriceUnit:    tickSize,
			MinLeverage:  1,
			MaxLeverage:  1,
			PriceScale:   sym.QuotePrecision,
			VolScale:     sym.BaseAssetPrecision,
		})
	}
	return details, nil
}

// GetDepth retrieves spot depth orderbook snapshot via /api/v3/depth.
func (c *Client) GetDepth(ctx context.Context, symbol string) (*domain.OrderBook, error) {
	params := map[string]any{
		"symbol": symbol,
		"limit":  100,
	}
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v3/depth", params, nil, false)
	if err != nil {
		return nil, fmt.Errorf("mexc get spot depth for %s: %w", symbol, err)
	}

	var data mexcSpotDepthRawData
	if err := xjson.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("mexc parse spot depth for %s: %w", symbol, err)
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
		Version: data.LastUpdateId,
		Bids:    bids,
		Asks:    asks,
	}, nil
}

// GetTopGainer returns spot top gaining symbols ranked by 24h rise rate via /api/v3/ticker/24hr.
func (c *Client) GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v3/ticker/24hr", nil, nil, false)
	if err != nil {
		return nil, fmt.Errorf("mexc get spot top gainer tickers: %w", err)
	}

	var tickers []mexcSpotTicker
	if err := xjson.Unmarshal(body, &tickers); err != nil {
		return nil, fmt.Errorf("mexc parse spot top gainer tickers: %w", err)
	}

	results := make([]exchange.TopGainerResult, 0, len(tickers))
	for _, t := range tickers {
		if !strings.HasSuffix(t.Symbol, "USDT") && !strings.HasSuffix(t.Symbol, "USDC") {
			continue
		}
		lastPrice := xjson.ToFloat64(t.LastPrice)
		if lastPrice <= 0 {
			continue
		}
		bidPrice := xjson.ToFloat64(t.BidPrice)
		askPrice := xjson.ToFloat64(t.AskPrice)
		gainPct := xjson.ToFloat64(t.PriceChangePercent)
		volumeUSDT := xjson.ToFloat64(t.QuoteVolume)

		spreadPct := 0.0
		if bidPrice > 0 && askPrice > 0 {
			spreadPct = ((askPrice - bidPrice) / bidPrice) * 100.0
		}

		results = append(results, exchange.TopGainerResult{
			Symbol:        t.Symbol,
			LastPrice:     lastPrice,
			Bid1:          bidPrice,
			Ask1:          askPrice,
			Volume24hUSDT: volumeUSDT,
			Gain24hPct:    gainPct,
			SpreadPct:     spreadPct,
			Timestamp:     t.CloseTime,
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

// Ping checks connectivity to the MEXC API server.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.base.Request(ctx, http.MethodGet, "/api/v3/ping", nil, nil, false)
	return err
}

// GetServerTime returns the MEXC spot server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v3/time", nil, nil, false)
	if err != nil {
		return 0, err
	}
	var res struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := xjson.Unmarshal(body, &res); err != nil {
		return 0, fmt.Errorf("parse spot server time: %w", err)
	}
	return res.ServerTime, nil
}

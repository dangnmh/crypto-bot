package hyperliquid

import (
	"context"
	"math"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	hl "github.com/sonirico/go-hyperliquid"
)

func (c *Client) getHyperliquidVolumes24h(ctx context.Context, symbol string) (vols map[string]float64, amts map[string]float64, lasts map[string]float64, err error) {
	data, err := c.info.MetaAndAssetCtxs(ctx, hl.MetaAndAssetCtxsParams{})
	if err != nil {
		return nil, nil, nil, err
	}

	vols = make(map[string]float64)
	amts = make(map[string]float64)
	lasts = make(map[string]float64)
	for i := range data.Universe {
		asset := &data.Universe[i]
		if symbol != "" && asset.Name != symbol {
			continue
		}
		if asset.IsDelisted {
			continue
		}

		ctxVal := &data.Ctxs[i]
		lastPx := 0.0
		if ctxVal.MidPx != "" {
			lastPx = decmath.ParseFloat(ctxVal.MidPx)
		} else if ctxVal.MarkPx != "" {
			lastPx = decmath.ParseFloat(ctxVal.MarkPx)
		}

		vol24h := decmath.ParseFloat(ctxVal.DayNtlVlm)

		vols[asset.Name] = vol24h / lastPx
		amts[asset.Name] = vol24h
		lasts[asset.Name] = lastPx
	}

	return vols, amts, lasts, nil
}

func (c *Client) getHyperliquidFundingRates(ctx context.Context, symbol string) ([]exchange.FundingRateResult, error) {
	data, err := c.info.MetaAndAssetCtxs(ctx, hl.MetaAndAssetCtxsParams{})
	if err != nil {
		return nil, err
	}

	rates := make([]exchange.FundingRateResult, 0, len(data.Universe))
	for i := range data.Universe {
		asset := &data.Universe[i]
		if symbol != "" && asset.Name != symbol {
			continue
		}
		if asset.IsDelisted {
			continue
		}

		ctxVal := &data.Ctxs[i]
		fundingRate := decmath.ParseFloat(ctxVal.Funding)
		vol24h := decmath.ParseFloat(ctxVal.DayNtlVlm)
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     asset.Name,
			Rate:       fundingRate,
			SettleTime: time.Now().Truncate(time.Hour).Add(time.Hour).UnixMilli(),
			Volume24h:  vol24h,
		})
	}

	return rates, nil
}

// GetTickers returns ticker data for all symbols or a single symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	data, err := c.info.MetaAndAssetCtxs(ctx, hl.MetaAndAssetCtxsParams{})
	if err != nil {
		return nil, err
	}

	tickers := make([]exchange.Ticker, 0, len(data.Universe))
	for i := range data.Universe {
		asset := &data.Universe[i]
		if symbol != "" && asset.Name != symbol {
			continue
		}
		if asset.IsDelisted {
			continue
		}

		ctxVal := &data.Ctxs[i]
		lastPx := 0.0
		if ctxVal.MidPx != "" {
			lastPx = decmath.ParseFloat(ctxVal.MidPx)
		} else if ctxVal.MarkPx != "" {
			lastPx = decmath.ParseFloat(ctxVal.MarkPx)
		}

		fundingRate := decmath.ParseFloat(ctxVal.Funding)
		vol24h := decmath.ParseFloat(ctxVal.DayNtlVlm)

		tickers = append(tickers, exchange.Ticker{
			Symbol:         asset.Name,
			LastPrice:      lastPx,
			Bid1:           lastPx,
			Ask1:           lastPx,
			Volume24:       vol24h / lastPx,
			Amount24:       vol24h,
			FundingRate:    fundingRate,
			NextSettleTime: time.Now().Truncate(time.Hour).Add(time.Hour).UnixMilli(),
			Timestamp:      time.Now().UnixMilli(),
		})
	}
	return tickers, nil
}

// GetContractDetails returns specifications for all perpetual contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	meta, err := c.info.Meta(ctx)
	if err != nil {
		return nil, err
	}

	details := make([]exchange.ContractDetail, 0, len(meta.Universe))
	for i := range meta.Universe {
		asset := &meta.Universe[i]
		if asset.IsDelisted {
			continue
		}

		minVol := 1.0 / math.Pow10(asset.SzDecimals)
		priceUnit := 0.00001
		priceScale := 5

		if asset.Name == assetBtc || asset.Name == "ETH" {
			priceUnit = 0.01
			priceScale = 2
		}

		details = append(details, exchange.ContractDetail{
			Symbol:           asset.Name,
			DisplayName:      asset.Name,
			DisplayNameEn:    asset.Name,
			PositionOpenType: 1, // Isolated by default
			BaseCoin:         asset.Name,
			QuoteCoin:        "USD",
			SettleCoin:       settleUsdc,
			ContractSize:     1.0,
			MinLeverage:      1,
			MaxLeverage:      asset.MaxLeverage,
			PriceScale:       priceScale,
			VolScale:         asset.SzDecimals,
			PriceUnit:        priceUnit,
			MinVol:           int(minVol),
			State:            1,
		})
	}
	return details, nil
}

// GetFundingRates returns current funding rate details for all active symbols.
func (c *Client) GetFundingRates(ctx context.Context) ([]exchange.FundingRateResult, error) {
	data, err := c.info.MetaAndAssetCtxs(ctx, hl.MetaAndAssetCtxsParams{})
	if err != nil {
		return nil, err
	}

	rates := make([]exchange.FundingRateResult, 0, len(data.Universe))
	nextHour := time.Now().Truncate(time.Hour).Add(time.Hour).UnixMilli()
	for i := range data.Universe {
		asset := &data.Universe[i]
		if asset.IsDelisted {
			continue
		}
		ctxVal := &data.Ctxs[i]
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     asset.Name,
			Rate:       decmath.ParseFloat(ctxVal.Funding),
			SettleTime: nextHour,
			Volume24h:  decmath.ParseFloat(ctxVal.DayNtlVlm),
		})
	}
	return rates, nil
}

// GetServerTime returns local synced timestamp.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return time.Now().UnixMilli(), nil
}

// GetKlines returns candlestick data.
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]exchange.Kline, error) {
	hlInterval := "1m"
	switch interval {
	case "Min1", "1m":
		hlInterval = "1m"
	case "Min5", "5m":
		hlInterval = "5m"
	case "Min15", interval15m:
		hlInterval = interval15m
	case "Min30", interval30m:
		hlInterval = interval30m
	case "Hour1", "1h":
		hlInterval = "1h"
	case "Hour4", "4h":
		hlInterval = "4h"
	case "Day1", "1d":
		hlInterval = "1d"
	}

	candles, err := c.info.CandlesSnapshot(ctx, symbol, hlInterval, start, end)
	if err != nil {
		return nil, err
	}

	klines := make([]exchange.Kline, 0, len(candles))
	for i := range candles {
		cand := &candles[i]
		open, _ := strconv.ParseFloat(cand.Open, 64)
		high, _ := strconv.ParseFloat(cand.High, 64)
		low, _ := strconv.ParseFloat(cand.Low, 64)
		closeVal, _ := strconv.ParseFloat(cand.Close, 64)
		vol, _ := strconv.ParseFloat(cand.Volume, 64)

		klines = append(klines, exchange.Kline{
			Timestamp: cand.TimeOpen,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeVal,
			Volume:    vol,
			Amount:    vol * closeVal,
		})
	}
	return klines, nil
}

// GetDepthSnapshot returns standard exchange orderbook.
func (c *Client) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	snap, err := c.info.L2Snapshot(ctx, symbol)
	if err != nil {
		return nil, err
	}

	bids := make([]exchange.OrderBookEntry, 0, len(snap.Levels[0]))
	for i := range snap.Levels[0] {
		level := &snap.Levels[0][i]
		bids = append(bids, exchange.OrderBookEntry{
			Price:  level.Px,
			Volume: level.Sz,
		})
	}

	asks := make([]exchange.OrderBookEntry, 0, len(snap.Levels[1]))
	for i := range snap.Levels[1] {
		level := &snap.Levels[1][i]
		asks = append(asks, exchange.OrderBookEntry{
			Price:  level.Px,
			Volume: level.Sz,
		})
	}

	return &exchange.OrderBook{
		Symbol: symbol,
		Bids:   bids,
		Asks:   asks,
	}, nil
}

// GetDepthCommits is a stub.
func (c *Client) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]exchange.DepthCommit, error) {
	return nil, nil
}

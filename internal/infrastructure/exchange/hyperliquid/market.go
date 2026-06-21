package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	hl "github.com/sonirico/go-hyperliquid"
)

const venueHlPerp = "HlPerp"

type RawVenueFunding struct {
	FundingRate     string `json:"fundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
}

type RawVenueItem struct {
	Venue string
	Info  RawVenueFunding
}

func (r *RawVenueItem) UnmarshalJSON(data []byte) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	if len(arr) < 2 {
		return fmt.Errorf("invalid venue item length")
	}
	if err := json.Unmarshal(arr[0], &r.Venue); err != nil {
		return err
	}
	if err := json.Unmarshal(arr[1], &r.Info); err != nil {
		return err
	}
	return nil
}

type RawAssetFunding struct {
	Asset  string
	Venues []RawVenueItem
}

func (r *RawAssetFunding) UnmarshalJSON(data []byte) error {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	if len(arr) < 2 {
		return fmt.Errorf("invalid asset funding length")
	}
	if err := json.Unmarshal(arr[0], &r.Asset); err != nil {
		return err
	}
	if err := json.Unmarshal(arr[1], &r.Venues); err != nil {
		return err
	}
	return nil
}

type hyperliquidMetaAndAssetCtxsRequest struct{}

type hyperliquidMetaRequest struct{}

type hyperliquidPredictedFundingsRequest struct{}

// Private raw methods invoking the Hyperliquid API or SDK.

func (c *Client) getRawMetaAndAssetCtxs(ctx context.Context, _ hyperliquidMetaAndAssetCtxsRequest) (*hl.MetaAndAssetCtxs, error) {
	return c.info.MetaAndAssetCtxs(ctx, hl.MetaAndAssetCtxsParams{})
}

func (c *Client) getRawMeta(ctx context.Context, _ hyperliquidMetaRequest) (*hl.Meta, error) {
	return c.info.Meta(ctx)
}

func (c *Client) getRawPredictedFundings(ctx context.Context, _ hyperliquidPredictedFundingsRequest) ([]RawAssetFunding, error) {
	payload := map[string]string{
		"type": "predictedFundings",
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/info", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("predictedFundings API error status=%d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var rawAssets []RawAssetFunding
	if err := json.Unmarshal(body, &rawAssets); err != nil {
		return nil, err
	}
	return rawAssets, nil
}

// Public mapper methods implementing the exchange.MarketDataProvider interface.

// GetTickers returns ticker data for all symbols or a single symbol.
func (c *Client) GetTickers(ctx context.Context, symbol string) ([]exchange.Ticker, error) {
	data, err := c.getRawMetaAndAssetCtxs(ctx, hyperliquidMetaAndAssetCtxsRequest{})
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

		vol24h := decmath.ParseFloat(ctxVal.DayNtlVlm)

		tickers = append(tickers, exchange.Ticker{
			Symbol:       asset.Name,
			LastPrice:    lastPx,
			Bid1:         lastPx,
			Ask1:         lastPx,
			Volume24:     vol24h / lastPx,
			AmountUSDT24: vol24h,
			Timestamp:    time.Now().UnixMilli(),
		})
	}
	return tickers, nil
}

// GetContractDetails returns specifications for all perpetual contracts.
func (c *Client) GetContractDetails(ctx context.Context) ([]exchange.ContractDetail, error) {
	meta, err := c.getRawMeta(ctx, hyperliquidMetaRequest{})
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
			PositionOpenType: 1, // Isolated by default.
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

// GetFundingRates returns current funding rate details for the specified symbols.
func (c *Client) GetFundingRates(ctx context.Context, symbols []string) ([]exchange.FundingRateResult, error) {
	if len(symbols) == 0 {
		return nil, nil
	}

	rawAssets, err := c.getRawPredictedFundings(ctx, hyperliquidPredictedFundingsRequest{})
	if err != nil {
		return nil, err
	}

	fundingMap := make(map[string]RawAssetFunding)
	for _, a := range rawAssets {
		fundingMap[a.Asset] = a
	}

	rates := make([]exchange.FundingRateResult, 0, len(symbols))
	for _, sym := range symbols {
		assetFunding, ok := fundingMap[sym]
		if !ok {
			c.logger.WarnContext(ctx, "Hyperliquid asset not found in predicted fundings", slog.String("symbol", sym))
			continue
		}

		var hlFunding *RawVenueFunding
		for i := range assetFunding.Venues {
			if assetFunding.Venues[i].Venue == venueHlPerp {
				hlFunding = &assetFunding.Venues[i].Info
				break
			}
		}

		if hlFunding == nil {
			c.logger.WarnContext(ctx, "HlPerp venue not found in predicted fundings for asset", slog.String("symbol", sym))
			continue
		}

		fr, _ := strconv.ParseFloat(hlFunding.FundingRate, 64)
		rates = append(rates, exchange.FundingRateResult{
			Symbol:     sym,
			Rate:       fr,
			SettleTime: hlFunding.NextFundingTime,
		})
	}

	return rates, nil
}

// GetServerTime returns local synced timestamp.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return time.Now().UnixMilli(), nil
}

func filterUniverse(data *hl.MetaAndAssetCtxs, minVol24h, maxVol24h float64, whitelistMap, blacklistMap map[string]bool) ([]string, map[string]float64) {
	var filteredSymbols []string
	volMap := make(map[string]float64)

	for i := range data.Universe {
		asset := &data.Universe[i]
		if asset.IsDelisted {
			continue
		}
		if blacklistMap[asset.Name] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[asset.Name] {
			continue
		}

		ctxVal := &data.Ctxs[i]
		vol := decmath.ParseFloat(ctxVal.DayNtlVlm)
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, asset.Name)
		volMap[asset.Name] = vol
	}
	return filteredSymbols, volMap
}

func buildFundingMap(rawAssets []RawAssetFunding) map[string]*RawVenueFunding {
	fundingMap := make(map[string]*RawVenueFunding)
	for _, a := range rawAssets {
		for i := range a.Venues {
			if a.Venues[i].Venue == venueHlPerp {
				fundingMap[a.Asset] = &a.Venues[i].Info
				break
			}
		}
	}
	return fundingMap
}

func (c *Client) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]exchange.PotentialFundingResult, error) {
	// 1. Fetch meta and asset ctxs
	data, err := c.getRawMetaAndAssetCtxs(ctx, hyperliquidMetaAndAssetCtxsRequest{})
	if err != nil {
		return nil, fmt.Errorf("hyperliquid get meta and asset ctxs: %w", err)
	}

	// 2. Build maps
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	// 3. Filter symbols by whitelist, blacklist, and 24h volume
	filteredSymbols, volMap := filterUniverse(data, minVol24h, maxVol24h, whitelistMap, blacklistMap)
	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	// 4. Fetch predicted fundings
	rawAssets, err := c.getRawPredictedFundings(ctx, hyperliquidPredictedFundingsRequest{})
	if err != nil {
		return nil, fmt.Errorf("hyperliquid get predicted fundings: %w", err)
	}

	fundingMap := buildFundingMap(rawAssets)

	// 5. Build results
	var results []exchange.PotentialFundingResult
	for _, sym := range filteredSymbols {
		hlFunding, exists := fundingMap[sym]
		if !exists {
			continue
		}

		fr, _ := strconv.ParseFloat(hlFunding.FundingRate, 64)
		results = append(results, exchange.PotentialFundingResult{
			Symbol:     sym,
			Rate:       fr,
			SettleTime: hlFunding.NextFundingTime,
			Volume24h:  volMap[sym],
		})
	}

	return results, nil
}

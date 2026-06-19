package service

import (
	"context"

	"crypto-bot/internal/infrastructure/exchange"
)

type PotentialFundingResult struct {
	Symbol     string  `json:"symbol"`
	Rate       float64 `json:"rate"`
	SettleTime int64   `json:"settleTime"`
	Volume24h  float64 `json:"volume24h"`
}

type FundingService struct {
	client exchange.Client
}

func NewFundingService(client exchange.Client) *FundingService {
	return &FundingService{client: client}
}

func (s *FundingService) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]PotentialFundingResult, error) {
	// 1. Fetch all tickers
	tickers, err := s.client.GetTickers(ctx, "")
	if err != nil {
		return nil, err
	}

	// 2. Build whitelist and blacklist lookup maps
	whitelistMap := make(map[string]bool)
	for _, sym := range whitelist {
		whitelistMap[sym] = true
	}

	blacklistMap := make(map[string]bool)
	for _, sym := range blacklist {
		blacklistMap[sym] = true
	}

	// 3. Filter symbols by whitelist, blacklist, and 24h volume
	var filteredSymbols []string
	volMap := make(map[string]float64)

	for _, t := range tickers {
		if blacklistMap[t.Symbol] {
			continue
		}
		if len(whitelistMap) > 0 && !whitelistMap[t.Symbol] {
			continue
		}
		vol := t.AmountUSDT24
		if vol < minVol24h {
			continue
		}
		if maxVol24h > 0 && vol > maxVol24h {
			continue
		}

		filteredSymbols = append(filteredSymbols, t.Symbol)
		volMap[t.Symbol] = vol
	}

	if len(filteredSymbols) == 0 {
		return nil, nil
	}

	// 4. Query GetFundingRates with the filtered list
	rates, err := s.client.GetFundingRates(ctx, filteredSymbols)
	if err != nil {
		return nil, err
	}

	// 5. Build final combined results
	results := make([]PotentialFundingResult, 0, len(rates))
	for _, r := range rates {
		results = append(results, PotentialFundingResult{
			Symbol:     r.Symbol,
			Rate:       r.Rate,
			SettleTime: r.SettleTime,
			Volume24h:  volMap[r.Symbol],
		})
	}

	return results, nil
}

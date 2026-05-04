package domain

import (
	"testing"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/infrastructure/exchange"
)

func TestScanFundingRates(t *testing.T) {
	configs := []config.SymbolConfig{
		{Symbol: "BTC_USDT", MinFundingRate: 0.005}, // 0.5%
		{Symbol: "ETH_USDT", MinFundingRate: 0.01},  // 1%
		{Symbol: "XRP_USDT", MinFundingRate: 0.001}, // 0.1%
	}

	tickers := []exchange.Ticker{
		{Symbol: "BTC_USDT", FundingRate: 0.006, LastPrice: 60000, Bid1: 59999, Ask1: 60001, Volume24: 100, Amount24: 6000000}, // Meets criteria (0.6% > 0.5%), FR > 0 (LONG)
		{Symbol: "ETH_USDT", FundingRate: -0.015, LastPrice: 3000, Bid1: 2999, Ask1: 3001, Volume24: 200, Amount24: 600000},    // Meets criteria (|-1.5%| > 1%), FR < 0 (SHORT)
		{Symbol: "XRP_USDT", FundingRate: 0.0005, LastPrice: 0.5, Bid1: 0.49, Ask1: 0.51, Volume24: 10000, Amount24: 5000},     // Fails criteria (0.05% < 0.1%)
		{Symbol: "DOGE_USDT", FundingRate: 0.02, LastPrice: 0.1, Bid1: 0.09, Ask1: 0.11, Volume24: 100000, Amount24: 10000},    // Not in config
	}

	candidates := ScanFundingRates(tickers, configs)

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// Verify BTC_USDT (FR > 0)
	btc := candidates[0]
	if btc.Symbol != "BTC_USDT" {
		t.Errorf("expected BTC_USDT, got %s", btc.Symbol)
	}
	if btc.Side != exchange.SideOpenLong {
		t.Errorf("expected BTC SideOpenLong for FR > 0, got %d", btc.Side)
	}
	if btc.CloseSide != exchange.SideCloseLong {
		t.Errorf("expected BTC CloseSideLong for FR > 0, got %d", btc.CloseSide)
	}
	if btc.RefPriceType != "bestAsk" {
		t.Errorf("expected bestAsk for FR > 0, got %s", btc.RefPriceType)
	}

	// Verify ETH_USDT (FR < 0)
	eth := candidates[1]
	if eth.Symbol != "ETH_USDT" {
		t.Errorf("expected ETH_USDT, got %s", eth.Symbol)
	}
	if eth.Side != exchange.SideOpenShort {
		t.Errorf("expected ETH SideOpenShort for FR < 0, got %d", eth.Side)
	}
	if eth.CloseSide != exchange.SideCloseShort {
		t.Errorf("expected ETH CloseSideShort for FR < 0, got %d", eth.CloseSide)
	}
	if eth.RefPriceType != "bestBid" {
		t.Errorf("expected bestBid for FR < 0, got %s", eth.RefPriceType)
	}
}

func TestEnrichWithContractDetails(t *testing.T) {
	candidates := []Candidate{
		{Symbol: "BTC_USDT"},
		{Symbol: "ETH_USDT"},
	}

	details := []exchange.ContractDetail{
		{Symbol: "BTC_USDT", PriceUnit: 0.5, VolUnit: 1, MinVol: 2, ContractSize: 0.001, TakerFeeRate: 0.0006},
	}

	EnrichWithContractDetails(candidates, details)

	if candidates[0].ContractSpec.PriceUnit != 0.5 {
		t.Errorf("expected BTC PriceUnit 0.5, got %f", candidates[0].ContractSpec.PriceUnit)
	}
	if candidates[0].ContractSpec.MinVol != 2 {
		t.Errorf("expected BTC MinVol 2, got %d", candidates[0].ContractSpec.MinVol)
	}
	if candidates[0].ContractSpec.TakerFeeRate != 0.0006 {
		t.Errorf("expected BTC TakerFeeRate 0.0006, got %f", candidates[0].ContractSpec.TakerFeeRate)
	}

	if candidates[1].ContractSpec.PriceUnit != 0 {
		t.Errorf("expected ETH untouched (0), got %f", candidates[1].ContractSpec.PriceUnit)
	}
}

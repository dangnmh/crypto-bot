package domain_test

import (
	"testing"

	domain "crypto-bot/internal/bots/funding/domain"

	shared "crypto-bot/internal/domain"
)

func TestScanFundingRates(t *testing.T) {
	t.Parallel()
	configs := []domain.ScanConfig{
		{Symbol: "BTC_USDT", MinFundingRate: 0.005}, // 0.5%
		{Symbol: "ETH_USDT", MinFundingRate: 0.01},  // 1%
		{Symbol: "XRP_USDT", MinFundingRate: 0.001}, // 0.1%
	}

	tickers := []domain.ScanResult{
		{Symbol: "BTC_USDT", FundingRate: 0.006, LastPrice: 60000, BestBid: 59999, BestAsk: 60001, Volume24: 100, AmountUSDT24: 6000000}, // Meets criteria (0.6% > 0.5%), FR > 0 (LONG)
		{Symbol: "ETH_USDT", FundingRate: -0.015, LastPrice: 3000, BestBid: 2999, BestAsk: 3001, Volume24: 200, AmountUSDT24: 600000},    // Meets criteria (|-1.5%| > 1%), FR < 0 (SHORT)
		{Symbol: "XRP_USDT", FundingRate: 0.0005, LastPrice: 0.5, BestBid: 0.49, BestAsk: 0.51, Volume24: 10000, AmountUSDT24: 5000},     // Fails criteria (0.05% < 0.1%)
		{Symbol: "DOGE_USDT", FundingRate: 0.02, LastPrice: 0.1, BestBid: 0.09, BestAsk: 0.11, Volume24: 100000, AmountUSDT24: 10000},    // Not in config
	}

	candidates := domain.ScanFundingRates(tickers, configs)

	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}

	// Verify BTC_USDT (FR > 0)
	btc := candidates[0]
	if btc.Symbol != "BTC_USDT" {
		t.Errorf("expected BTC_USDT, got %s", btc.Symbol)
	}
	if btc.Side != shared.SideOpenLong {
		t.Errorf("expected BTC SideOpenLong for FR > 0, got %d", btc.Side)
	}
	if btc.CloseSide != shared.SideCloseLong {
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
	if eth.Side != shared.SideOpenShort {
		t.Errorf("expected ETH SideOpenShort for FR < 0, got %d", eth.Side)
	}
	if eth.CloseSide != shared.SideCloseShort {
		t.Errorf("expected ETH CloseSideShort for FR < 0, got %d", eth.CloseSide)
	}
	if eth.RefPriceType != "bestBid" {
		t.Errorf("expected bestBid for FR < 0, got %s", eth.RefPriceType)
	}
}

func TestEnrichWithContractSpec(t *testing.T) {
	t.Parallel()
	candidates := []domain.Candidate{
		{TradeIntent: domain.TradeIntent{Symbol: "BTC_USDT"}},
		{TradeIntent: domain.TradeIntent{Symbol: "ETH_USDT"}},
	}

	specs := map[string]domain.ContractSpec{
		"BTC_USDT": {PriceUnit: 0.5, VolUnit: 1, MinVol: 2, ContractSize: 0.001, TakerFeeRate: 0.0006},
	}

	domain.EnrichWithContractSpec(candidates, specs)

	if candidates[0].PriceUnit != 0.5 {
		t.Errorf("expected BTC PriceUnit 0.5, got %f", candidates[0].PriceUnit)
	}
	if candidates[0].MinVol != 2 {
		t.Errorf("expected BTC MinVol 2, got %d", candidates[0].MinVol)
	}
	if candidates[0].TakerFeeRate != 0.0006 {
		t.Errorf("expected BTC TakerFeeRate 0.0006, got %f", candidates[0].TakerFeeRate)
	}

	if candidates[1].PriceUnit != 0 {
		t.Errorf("expected ETH untouched (0), got %f", candidates[1].PriceUnit)
	}
}

func TestScanFundingRates_EmptyInputs(t *testing.T) {
	t.Parallel()

	t.Run("no tickers", func(t *testing.T) {
		t.Parallel()
		candidates := domain.ScanFundingRates(nil, []domain.ScanConfig{{Symbol: "BTC_USDT", MinFundingRate: 0.001}})
		if len(candidates) != 0 {
			t.Errorf("expected 0 candidates, got %d", len(candidates))
		}
	})

	t.Run("no configs", func(t *testing.T) {
		t.Parallel()
		candidates := domain.ScanFundingRates([]domain.ScanResult{{Symbol: "BTC_USDT", FundingRate: 0.01}}, nil)
		if len(candidates) != 0 {
			t.Errorf("expected 0 candidates, got %d", len(candidates))
		}
	})
}

func TestScanFundingRates_FiltersMinVol24USD(t *testing.T) {
	t.Parallel()

	candidates := domain.ScanFundingRates(
		[]domain.ScanResult{
			{Symbol: "BTC_USDT", FundingRate: 0.006, AmountUSDT24: 999999},
			{Symbol: "ETH_USDT", FundingRate: 0.006, AmountUSDT24: 1000000},
		},
		[]domain.ScanConfig{
			{Symbol: "BTC_USDT", MinFundingRate: 0.001, MinVol24USD: 1000000},
			{Symbol: "ETH_USDT", MinFundingRate: 0.001, MinVol24USD: 1000000},
		},
	)

	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Symbol != "ETH_USDT" {
		t.Errorf("expected ETH_USDT, got %s", candidates[0].Symbol)
	}
}

package domain_test

import (
	"testing"

	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateDepthImbalance(t *testing.T) {
	t.Parallel()

	t.Run("Short candidate with heavy bids triggers SuggestSkip", func(t *testing.T) {
		t.Parallel()
		ob := &shared.OrderBook{
			Symbol: "LSK_USDT",
			Bids: []shared.OrderBookEntry{
				{Price: 100.0, Volume: 20}, // 2000 USDT (within 1%)
				{Price: 99.5, Volume: 10},  // 995 USDT (within 1%)
				{Price: 98.0, Volume: 50},  // outside 1% (100 * 0.99 = 99)
			},
			Asks: []shared.OrderBookEntry{
				{Price: 100.2, Volume: 5},  // 501 USDT (within 1%)
				{Price: 100.8, Volume: 5},  // 504 USDT (within 1%)
				{Price: 102.0, Volume: 50}, // outside 1% (100.2 * 1.01 = 101.2)
			},
		}

		res := fundingdomain.EvaluateDepthImbalance(ob, shared.SideOpenShort, 0.01, 1.5)
		// Bid notional = 2000 + 995 = 2995
		// Ask notional = 501 + 504 = 1005
		// Ratio = 2995 / 1005 = ~2.98 > 1.5
		assert.True(t, res.SuggestSkip, "Should suggest skipping short due to heavy bids")
		assert.InDelta(t, 2995.0, res.BidNotional, 0.1)
		assert.InDelta(t, 1005.0, res.AskNotional, 0.1)
		assert.GreaterOrEqual(t, res.Ratio, 1.5)
		assert.Contains(t, res.SkipReason, "bid depth")
	})

	t.Run("Short candidate with favorable depth does not trigger SuggestSkip", func(t *testing.T) {
		t.Parallel()
		ob := &shared.OrderBook{
			Symbol: "LSK_USDT",
			Bids: []shared.OrderBookEntry{
				{Price: 100.0, Volume: 5},
			},
			Asks: []shared.OrderBookEntry{
				{Price: 100.2, Volume: 20},
			},
		}

		res := fundingdomain.EvaluateDepthImbalance(ob, shared.SideOpenShort, 0.01, 1.5)
		assert.False(t, res.SuggestSkip)
		assert.Less(t, res.Ratio, 1.0)
	})

	t.Run("Long candidate with heavy asks triggers SuggestSkip", func(t *testing.T) {
		t.Parallel()
		ob := &shared.OrderBook{
			Symbol: "BTC_USDT",
			Bids: []shared.OrderBookEntry{
				{Price: 100.0, Volume: 5},
			},
			Asks: []shared.OrderBookEntry{
				{Price: 100.2, Volume: 20},
			},
		}

		res := fundingdomain.EvaluateDepthImbalance(ob, shared.SideOpenLong, 0.01, 1.5)
		assert.True(t, res.SuggestSkip, "Should suggest skipping long due to heavy asks")
		assert.Contains(t, res.SkipReason, "ask depth")
	})

	t.Run("Empty orderbook returns safe zero result", func(t *testing.T) {
		t.Parallel()
		res := fundingdomain.EvaluateDepthImbalance(nil, shared.SideOpenShort, 0.01, 1.5)
		assert.False(t, res.SuggestSkip)
		assert.Equal(t, 0.0, res.BidNotional)
		assert.Equal(t, 0.0, res.AskNotional)
		assert.Equal(t, 1.0, res.Ratio)
	})
}

package application

import (
	"context"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"

	"github.com/ThreeDotsLabs/watermill/message"
)

// ──────────────────────────────────────────────────────────────────────.
// CycleOrchestrator support — event consumption, market data helpers.
// ──────────────────────────────────────────────────────────────────────.

// consumeTopic abstracts the boilerplate of subscribing to an eventbus topic
// and consuming messages in a goroutine until the context is canceled.
func (o *CycleOrchestrator) consumeTopic(ctx context.Context, topic string, handler func(*message.Message)) {
	msgs, err := o.bus.Subscribe(ctx, topic)
	if err != nil {
		o.deps.Log.Error("Failed to subscribe to topic", "topic", topic, "error", err)
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				msg.Ack()
				handler(msg)
			}
		}
	}()
}

func (o *CycleOrchestrator) buildCandidate(td *store.TickerData) domain.Candidate {
	intent := domain.TradeIntent{
		Symbol:      td.Symbol,
		FundingRate: td.FundingRate,
	}
	if td.FundingRate > 0 {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenLong, shared.SideCloseLong, "bestAsk"
	} else {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenShort, shared.SideCloseShort, "bestBid"
	}

	return domain.Candidate{
		Config:      toTradeConfig(o.cfg),
		TradeIntent: intent,
		MarketData: domain.MarketData{
			LastPrice: td.LastPrice,
			BestBid:   td.BestBid,
			BestAsk:   td.BestAsk,
			Volume24:  td.Volume24,
			Amount24:  td.Amount24,
		},
		Phase: domain.PhaseScanning,
	}
}

func (o *CycleOrchestrator) enrich(ctx context.Context, c *domain.Candidate) bool {
	cd, err := o.deps.ContractStore.GetContract(ctx, c.Symbol)
	if err != nil {
		o.deps.Log.Warn("🟡 No contract data — skip")
		return false
	}
	c.ContractSpec = domain.ContractSpec{
		PriceUnit:    cd.PriceUnit,
		VolUnit:      cd.VolUnit,
		MinVol:       cd.MinVol,
		PriceScale:   cd.PriceScale,
		VolScale:     cd.VolScale,
		ContractSize: cd.ContractSize,
		TakerFeeRate: cd.TakerFeeRate,
		MakerFeeRate: cd.MakerFeeRate,
	}
	return true
}

func (o *CycleOrchestrator) refreshPrice(ctx context.Context, c *domain.Candidate) error {
	pd, err := o.deps.PriceStore.GetPrice(ctx, c.Symbol, 5*time.Second)
	if err == nil {
		c.BestBid, c.BestAsk, c.LastPrice = pd.BestBid, pd.BestAsk, pd.LastPrice
	}
	return err
}

// initKlines fetches initial 1-minute klines via REST if we don't have enough data.
func (o *CycleOrchestrator) initKlines(ctx context.Context) {
	klines := o.deps.KlineStore.GetKlines(ctx, o.cfg.Symbol)
	if len(klines) >= 14 {
		return
	}
	o.deps.Log.Info("📊 Fetching initial 1-minute klines via REST")
	apiKlines, err := o.deps.Client.GetKlines(ctx, o.cfg.Symbol, exchange.IntervalMin1, 0, 0)
	if err != nil {
		o.deps.Log.Warn("🟡 Failed to fetch initial klines", "error", err)
		return
	}
	if len(apiKlines) > 20 {
		apiKlines = apiKlines[len(apiKlines)-20:]
	}
	o.deps.KlineStore.InitKlines(o.cfg.Symbol, 20, apiKlines)
}

func (o *CycleOrchestrator) waitUntil(ctx context.Context, target time.Time) {
	if d := o.deps.Clock.Until(target); d > 0 {
		o.deps.Log.Debug("⏱️ wait", "target", target, "wait", d)
		_ = o.deps.Clock.Sleep(ctx, d)
	}
}

func (o *CycleOrchestrator) sleep(ctx context.Context, d time.Duration) bool {
	err := o.deps.Clock.Sleep(ctx, d)
	return err == nil
}

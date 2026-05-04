package application

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/ws"

	"golang.org/x/sync/errgroup"
)

// CloseResult holds the outcome of closing a single position.
type CloseResult struct {
	Symbol      string
	ExitOrderID string
	ExitPrice   float64
	ExitVol     float64
	Closed      bool
	TakerFee    float64
	MakerFee    float64
	Profit      float64
}

// ──────────────────────────────────────────────────────────────────────
// Sniper — top-level orchestrator
// ──────────────────────────────────────────────────────────────────────.

// Sniper spawns one independent worker goroutine per configured symbol.
type Sniper struct {
	cfg      *config.Config
	client   *exchange.Client
	ws       *ws.Client
	store    *store.GlobalStore
	timeSync *timesync.TimeSync
}

// NewSniper creates a new Sniper instance.
func NewSniper(cfg *config.Config, client *exchange.Client, wsClient *ws.Client, gs *store.GlobalStore, ts *timesync.TimeSync) *Sniper {
	return &Sniper{cfg: cfg, client: client, ws: wsClient, store: gs, timeSync: ts}
}

// Run starts all symbol workers. Blocks until all stop or context is cancelled.
func (s *Sniper) Run(ctx context.Context) error {
	slog.Info("🚀 Sniper — launching per-symbol workers", "symbols", len(s.cfg.Symbols))

	g, workerCtx := errgroup.WithContext(ctx)
	for _, sc := range s.cfg.Symbols {
		g.Go(s.spawnWorker(ctx, workerCtx, sc))
	}

	err := g.Wait()
	slog.Info("🛑 All workers stopped")
	return err
}

// Stop implements the app.Bot interface. It executes any explicit teardown.
// The primary graceful shutdown is handled by the context passed to Run().
func (s *Sniper) Stop(ctx context.Context) error {
	slog.Info("🛑 Sniper explicit stop invoked")
	// Currently, all teardown is handled via Run() context cancellation.
	// You can add explicit resource cleanup here if needed later.
	return nil
}

func (s *Sniper) spawnWorker(parentCtx, workerCtx context.Context, symCfg config.SymbolConfig) func() error {
	return func() error {
		log := slog.Default().With("w", "sniper", "sym", symCfg.Symbol)
		w := &symbolWorker{
			cfg:      symCfg,
			global:   s.cfg,
			client:   s.client,
			ws:       s.ws,
			store:    s.store,
			ts:       s.timeSync,
			log:      log,
			trailing: NewTrailingManager(parentCtx, s.client, s.ws, log),
			subs:     NewSubscriptionManager(s.ws, symCfg.Symbol, symCfg.DynamicPricing, log),
		}
		w.log.Info("🚀 Worker started")
		w.run(workerCtx)
		w.log.Info("🛑 Worker stopped")
		return nil
	}
}

// ──────────────────────────────────────────────────────────────────────
// symbolWorker — isolated per-symbol execution unit
// ──────────────────────────────────────────────────────────────────────.

type symbolWorker struct {
	cfg      config.SymbolConfig
	global   *config.Config
	client   *exchange.Client
	ws       *ws.Client
	store    *store.GlobalStore
	ts       *timesync.TimeSync
	log      *slog.Logger
	trailing *TrailingManager
	subs     *SubscriptionManager
}

func (w *symbolWorker) run(ctx context.Context) {
	settle, err := w.nextSettleTime()
	if err != nil {
		w.log.Error("🔴 No settle time; retry in 1m", "error", err)
		if !w.sleep(ctx, time.Minute) {
			return
		}
		return
	}

	// Wait until T - 5 minutes before actively entering the cycle
	if d := w.ts.Until(settle.Add(-5 * time.Minute)); d > 0 {
		w.log.Debug("😴 Waiting for cycle window", "settle", settle, "wait", d)
		if !w.sleep(ctx, d) {
			return
		}
	}

	// If we are somehow already past the firing deadline (T - 5s), skip
	if w.ts.Until(settle.Add(-5*time.Second)) <= 0 {
		w.log.Warn("🔴 Settle time passed or missed", "settle", settle)
		return
	}

	// Execute one funding cycle via FSM
	w.cycle(ctx, settle)
}

// ──────────────────────────────────────────────────────────────────────
// FSM Cycle — dispatch loop
// ──────────────────────────────────────────────────────────────────────.

// cycle orchestrates one complete funding reversion cycle via FSM.
// All phase logic lives inside FSM callbacks (see state.go).
func (w *symbolWorker) cycle(ctx context.Context, settle time.Time) {
	w.log.Info("━━━ Cycle start ━━━", "settle", settle)

	cs := &CycleState{Settle: settle, NextEvent: EvStart}
	machine := NewCycleFSM(w.log, w, cs)

	for cs.NextEvent != "" && ctx.Err() == nil {
		if err := machine.Event(ctx, cs.NextEvent); err != nil {
			w.log.Error("🔴 FSM error", "error", err)
			break
		}
	}
}

// ──────────────────────────────────────────────────────────────────────
// Phase handlers — each returns bool (success → next state, fail → abort)
// ──────────────────────────────────────────────────────────────────────.

// onScan checks the funding rate against the configured threshold.
func (w *symbolWorker) onScan(cs *CycleState) bool {
	td, err := w.store.GetTicker(w.cfg.Symbol)
	if err != nil {
		w.log.Warn("🟡 No ticker", "error", err)
		return false
	}

	if math.Abs(td.FundingRate) < w.cfg.MinFundingRate {
		w.log.Info("😴 FR below threshold", "fr", td.FundingRate*100)
		return false
	}

	cs.Candidate = w.buildCandidate(td)
	if !w.enrich(&cs.Candidate) {
		return false
	}

	w.log.Info("🔍 Qualified",
		"side", exchange.SideStr(cs.Candidate.Side),
		"fr", cs.Candidate.FundingRate*100,
	)
	return true
}

// onArm subscribes to WS, runs safety checks, and calculates IOC price + volume.
func (w *symbolWorker) onArm(ctx context.Context, cs *CycleState) bool {
	c := &cs.Candidate

	if c.Config.DynamicPricing.Enabled {
		w.initKlines(ctx)
	}

	w.subs.SubscribeAll()
	w.sleep(ctx, 2*time.Second)
	w.refreshPrice(c)

	if c.Config.DynamicPricing.Enabled {
		klines := w.store.GetKlines(w.cfg.Symbol)
		c.ATR = domain.CalculateATR(klines, 14)
		c.PrepareDynamicPricing()
		w.log.Info("📈 Dynamic Pricing", "ATR", c.ATR, "TP", c.Config.TakeProfitPct, "SL", c.Config.StopLossPct)
	}

	// c.CalculateIOCPrice(nil) evaluated here just to ensure parameters are valid and price is calculable.
	ioc, err := c.CalculateIOCPrice(nil)
	if err != nil {
		w.log.Warn("🟡 IOC calc failed", "error", err)
		w.subs.UnsubscribeAll()
		return false
	}
	c.Volume = c.CalculateVolume()

	// Record actual slippage % for safety evaluation and logging
	if ioc > 0 {
		var refPrice float64
		if c.Side == exchange.SideOpenLong {
			refPrice = c.BestAsk
		} else {
			refPrice = c.BestBid
		}
		if refPrice > 0 {
			c.Slippage = math.Abs(ioc-refPrice) / refPrice * 100.0
		}
	}

	c.SafetyResult = c.EvaluateSafety(w.global.System.Safety.MaxImpactRatio)
	if !c.SafetyResult.Passed {
		w.log.Warn("🔴 Safety FAIL", "reason", c.SafetyResult.RejectReason)
		w.subs.UnsubscribeAll()
		return false
	}

	c.SettleTime = cs.Settle
	w.log.Info("🎯 Ready",
		"side", exchange.SideStr(c.Side),
		"fr", c.FundingRate*100,
		"ioc", ioc,
		"vol", c.Volume,
	)
	return true
}

// initKlines fetches initial 1-minute klines via REST if we don't have enough data.
func (w *symbolWorker) initKlines(ctx context.Context) {
	klines := w.store.GetKlines(w.cfg.Symbol)
	if len(klines) >= 14 {
		return
	}

	w.log.Info("📊 Fetching initial 1-minute klines via REST")
	apiKlines, err := w.client.GetKlines(ctx, w.cfg.Symbol, exchange.IntervalMin1, 0, 0)
	if err != nil {
		w.log.Warn("🟡 Failed to fetch initial klines", "error", err)
		return
	}

	if len(apiKlines) > 20 {
		apiKlines = apiKlines[len(apiKlines)-20:]
	}
	w.store.InitKlines(w.cfg.Symbol, 20, apiKlines)
}

// onWait sleeps until T-2s (server-synced).
func (w *symbolWorker) onWait(ctx context.Context, cs *CycleState) {
	w.waitUntil(ctx, cs.Settle.Add(-2*time.Second))
}

// onRecheck verifies the funding rate hasn't flipped sign at T-2s.
func (w *symbolWorker) onRecheck(cs *CycleState) bool {
	c := &cs.Candidate
	td, err := w.store.GetTicker(c.Symbol)
	if err != nil {
		w.log.Warn("🟡 No ticker for recheck")
		return false
	}

	if (td.FundingRate > 0) != (c.FundingRate > 0) {
		w.log.Error("🔴 FR sign flip!",
			"old", c.FundingRate*100,
			"new", td.FundingRate*100,
		)
		return false
	}

	if math.Abs(td.FundingRate) < w.cfg.MinFundingRate {
		w.log.Warn("🟡 FR dropped below threshold",
			"fr", td.FundingRate*100,
			"min", w.cfg.MinFundingRate*100,
		)
		return false
	}

	w.log.Info("🟢 FR OK", "fr", td.FundingRate*100)
	return true
}

// onFireIOC snapshots the peak price and submits the Sniper IOC order.
func (w *symbolWorker) onFireIOC(ctx context.Context, cs *CycleState) {
	c := &cs.Candidate
	settle := cs.Settle

	latencyMs := w.ts.LatencyMs()
	oneWayMs := latencyMs / 2
	bufferTime := time.Duration(w.global.System.Safety.BufferTime)

	fireOffset := time.Duration(oneWayMs)*time.Millisecond + bufferTime

	w.log.Info("⏱️ Firing configuration", "latency_rtt", latencyMs, "one_way", oneWayMs, "buffer", bufferTime, "total_offset", fireOffset)

	// Snapshot price before chaos begins
	snapshotOffset := 50 * time.Millisecond
	if fireOffset > snapshotOffset {
		snapshotOffset = fireOffset
	}
	w.waitUntil(ctx, settle.Add(-snapshotOffset))

	w.refreshPrice(c)

	// Refresh volume with latest price before OB sweep uses it
	c.Volume = c.CalculateVolume()

	var ob *exchange.OrderBook
	if c.Config.DynamicPricing.Enabled && c.Config.DynamicPricing.SlippageMode == "OB_IMBALANCE" {
		ob, _ = w.store.GetDepth(w.cfg.Symbol)
	}

	// Wait for precise fire moment, then shoot
	w.waitUntil(ctx, settle.Add(-fireOffset))
	res := FireIOC(ctx, w.client, c, w.ts, w.log, ob)
	cs.Results = append(cs.Results, res)

	if res.IsSuccess() {
		resCopy := res
		w.trailing.SetupFillCallback(res.OrderID, &resCopy)
	}
}

// onFireTrap submits the Limit Trap order after settlement to catch the wick.
func (w *symbolWorker) onFireTrap(ctx context.Context, cs *CycleState) {
	c := &cs.Candidate
	settle := cs.Settle
	trapOffset := time.Duration(w.global.System.Safety.TrapAfterSettle)

	w.waitUntil(ctx, settle.Add(trapOffset))
	res := FireLimitTrap(ctx, w.client, c, w.ts, w.log)
	cs.Results = append(cs.Results, res)

	if res.IsSuccess() {
		resCopy := res
		w.trailing.SetupFillCallback(res.OrderID, &resCopy)
	}
}

// ──────────────────────────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────────────────────────.

func (w *symbolWorker) buildCandidate(td *store.TickerData) domain.Candidate {
	c := domain.Candidate{
		Config:      w.cfg,
		Symbol:      td.Symbol,
		FundingRate: td.FundingRate,
		MarketData: domain.MarketData{
			LastPrice: td.LastPrice,
			BestBid:   td.BestBid,
			BestAsk:   td.BestAsk,
			Volume24:  td.Volume24,
			Amount24:  td.Amount24,
		},
		Phase: "SCANNING",
	}
	if td.FundingRate > 0 {
		c.Side, c.CloseSide, c.RefPriceType = exchange.SideOpenLong, exchange.SideCloseLong, "bestAsk"
	} else {
		c.Side, c.CloseSide, c.RefPriceType = exchange.SideOpenShort, exchange.SideCloseShort, "bestBid"
	}
	return c
}

func (w *symbolWorker) enrich(c *domain.Candidate) bool {
	cd, err := w.store.GetContract(c.Symbol)
	if err != nil {
		w.log.Warn("🟡 No contract data — skip")
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

func (w *symbolWorker) refreshPrice(c *domain.Candidate) {
	if pd, err := w.store.GetPrice(c.Symbol, 5*time.Second); err == nil {
		c.BestBid, c.BestAsk, c.LastPrice = pd.BestBid, pd.BestAsk, pd.LastPrice
	}
}

func (w *symbolWorker) nextSettleTime() (time.Time, error) {
	if w.cfg.SimulateSettle != "" {
		sim, err := time.Parse(time.RFC3339, w.cfg.SimulateSettle)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid simulateSettle datetime %q: %w", w.cfg.SimulateSettle, err)
		}
		if sim.After(time.Now().Add(time.Minute)) {
			return sim, nil
		}
	}

	st, err := w.store.GetSettleTime(w.cfg.Symbol)
	if err != nil {
		return time.Time{}, fmt.Errorf("settle time: %w", err)
	}
	return st, nil
}

func (w *symbolWorker) waitUntil(ctx context.Context, target time.Time) {
	if d := w.ts.Until(target); d > 0 {
		w.log.Debug("⏱️ wait", "target", target, "wait", d)
		select {
		case <-ctx.Done():
		case <-time.After(d):
		}
	}
}

func (w *symbolWorker) sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

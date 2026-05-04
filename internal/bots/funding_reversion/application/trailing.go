package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/exchange"
)

// TrailingManager handles post-fill trailing stop placement.
// Serializes concurrent calls to prevent API race conditions
// when both IOC and Trap fill simultaneously.
type TrailingManager struct {
	client exchange.Client
	ws     watcher.OrderNotifier
	log    *slog.Logger
	ctx    context.Context
	mu     sync.Mutex
}

// NewTrailingManager creates a new trailing stop manager.
func NewTrailingManager(ctx context.Context, client exchange.Client, wsClient watcher.OrderNotifier, log *slog.Logger) *TrailingManager {
	return &TrailingManager{ctx: ctx, client: client, ws: wsClient, log: log}
}

// SetupFillCallback registers a WS callback that triggers trailing stop on fill.
func (tm *TrailingManager) SetupFillCallback(orderID string, res *OrderResult) {
	if orderID == "" || res.Error != nil {
		return
	}

	resCopy := *res
	tm.ws.OnOrderUpdate(orderID, 5*time.Second, func(deal exchange.WsOrderDeal) {
		tm.handleOrderFill(tm.ctx, deal, &resCopy)
	})
}

// handleOrderFill executes via callback when an order update is received.
func (tm *TrailingManager) handleOrderFill(ctx context.Context, deal exchange.WsOrderDeal, r *OrderResult) {
	if !exchange.IsTerminalOrderState(deal.State) {
		return // not fully filled or canceled
	}

	// Remove callback as we only care about terminal states
	tm.ws.RemoveOrderCallback(deal.GetOrderID())

	if deal.DealVol <= 0 {
		tm.log.Warn("🟡 No fill",
			"phase", r.Candidate.Phase,
			"orderID", deal.GetOrderID(),
		)
		return
	}

	r.Filled = true
	r.Order = &exchange.OrderInfo{
		DealVol:      deal.DealVol,
		DealAvgPrice: deal.DealAvgPrice,
		OpenType:     r.Candidate.Config.ParsedOpenType,
		TakerFee:     deal.TakerFee,
		MakerFee:     deal.MakerFee,
		Profit:       deal.Profit,
	}

	tm.log.Info("📊 Position opened",
		"phase", r.Candidate.Phase,
		"entry", r.Order.DealAvgPrice,
		"vol", r.Order.DealVol,
	)

	// Trigger trailing stop asynchronously
	go tm.PlaceTrailingStop(ctx, r)
}

// PlaceTrailingStop pushes TrackOrders to MEXC for filled positions.
func (tm *TrailingManager) PlaceTrailingStop(ctx context.Context, res *OrderResult) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	c := res.Candidate

	var trailCfg = c.Config.TrailingConfig
	if c.Phase == "FIRED_TRAP" {
		trailCfg = c.Config.TrapTrailingConfig
	}

	if !trailCfg.Enabled {
		tm.log.Info("⏭️ Trailing disabled, position requires manual close", "phase", c.Phase)
		return
	}

	var activePrice float64
	if trailCfg.ActivationPct > 0 {
		if c.CloseSide == shared.SideCloseLong {
			activePrice = res.Order.DealAvgPrice * (1 + trailCfg.ActivationPct)
		} else {
			activePrice = res.Order.DealAvgPrice * (1 - trailCfg.ActivationPct)
		}
	}

	req := exchange.SubmitTrackOrderRequest{
		Symbol:       c.Symbol,
		Leverage:     c.Config.Leverage,
		Side:         int(c.CloseSide),
		Vol:          res.Order.DealVol,
		OpenType:     c.Config.ParsedOpenType,
		PositionMode: c.Config.ParsedPositionMode,
		Trend:        1,
		ActivePrice:  activePrice,
		BackType:     1,
		BackValue:    trailCfg.CallbackPct,
		ReduceOnly:   true,
	}

	tm.log.Info("🏃 Placing TrackOrder (Trailing)",
		"phase", c.Phase,
		"side", req.Side,
		"vol", req.Vol,
		"activePrice", activePrice,
		"callbackPct", req.BackValue*100,
	)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	trackID, err := tm.client.CreateTrackOrder(reqCtx, req)
	if err != nil {
		tm.log.Error("🔴 TrackOrder failed - fallback close", "error", err, "phase", c.Phase)
		_ = tm.client.CloseAllPositions(reqCtx, c.Symbol)
	} else {
		tm.log.Info("✅ TrackOrder placed successfully", "trackID", trackID, "phase", c.Phase)
	}
}

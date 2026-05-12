package application

import (
	"context"
	"fmt"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding_reversion/application/events"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/ThreeDotsLabs/watermill/message"
)

// subscribeFireTrap waits until settle time + TrapAfterSettle offset, then places the Trap order.
// It is completely independent of whether the IOC order was successful or not.
func (o *CycleOrchestrator) subscribeFireTrap(ctx context.Context, settle time.Time) {
	o.consumeTopic(ctx, events.TopicConfirmed, func(msg *message.Message) {
		// TopicConfirmed means the cycle is fully armed and we should schedule our trap.
		if !o.cfg.IsHedgeTrapEnabled() {
			return // Trap is completely disabled
		}

		go o.handleFireTrap(ctx, settle)
	})
}

func (o *CycleOrchestrator) handleFireTrap(ctx context.Context, settle time.Time) {
	// 1. Wait until settlement time + TrapAfterSettle delay.
	delay := time.Duration(o.global.System.Safety.TrapAfterSettle)
	if delay <= 0 {
		delay = 3 * time.Second
	}
	trapTime := settle.Add(delay)

	o.deps.Log.Info("⏱️ Fire Trap waiting", "delay", delay, "target_time", trapTime)
	o.waitUntil(ctx, trapTime)
	if ctx.Err() != nil {
		return
	}

	ob, err := o.deps.DepthStore.GetDepth(ctx, o.cfg.Symbol)
	if err != nil || ob == nil {
		o.deps.Log.Warn("🟡 Fire Trap: failed to fetch depth, falling back to static trap", "error", err)
		o.fireStaticTrap(ctx)
		return
	}

	// 3. Find Wall (opposite to original IOC entry side).
	trapSide := o.candidate.Side.Opposite()
	wallPrice := o.candidate.FindTrapWallPrice(ob)

	if wallPrice <= 0 {
		o.deps.Log.Info("🤷 Fire Trap: no suitable wall found, falling back to static trap", "side", trapSide.String())
		o.fireStaticTrap(ctx)
		return
	}

	o.publishOrLog(events.TopicOBWallFound, events.OBWallFoundEvent{
		Symbol:    o.cfg.Symbol,
		WallPrice: wallPrice,
		WallVol:   0, // wallVol is not rigorously tracked here anymore
		Side:      int(trapSide),
	})

	// 4. Calculate Trap Price (1 tick before the wall).
	trapPrice := o.candidate.CalculateOBTrapPrice(wallPrice)

	o.deps.Log.Info("🧱 OB Monitor: wall found", "side", trapSide.String(), "wallPrice", wallPrice, "trapPrice", trapPrice)

	// Update candidate with Trap price for TP calculation.
	trapCandidate := o.candidate
	trapCandidate.Side = trapSide
	trapCandidate.CloseSide = shared.CloseSideFor(trapSide)

	// 5. Fire Trap limit order.
	o.fireOBTrap(ctx, trapCandidate, trapPrice)
}

func (o *CycleOrchestrator) fireOBTrap(ctx context.Context, c domain.Candidate, trapPrice float64) {
	tpPrice := c.CalculateTrapTPPrice(trapPrice)
	slPrice := c.CalculateTrapSLPrice(trapPrice)

	extOID := fmt.Sprintf("trp_ob_%s_%d", c.Symbol, time.Now().UnixMilli())
	req := exchange.SubmitOrderRequest{
		Symbol:          c.Symbol,
		Price:           trapPrice,
		Vol:             c.Volume,
		Side:            int(c.Side),
		Type:            exchange.OrderTypePostOnly, // POST ONLY for Trap.
		OpenType:        c.Config.ParsedOpenType,
		PositionMode:    c.Config.ParsedPositionMode,
		Leverage:        c.Config.Leverage,
		ExternalOID:     extOID,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
	}

	o.deps.Log.Info("🩤 FIRE OB TRAP",
		"symbol", c.Symbol,
		"trapPrice", trapPrice,
		"vol", c.Volume,
		"side", c.Side.String(),
		"takeProfitPrice", tpPrice,
		"stopLossPrice", slPrice,
	)

	orderID, err := o.deps.Client.CreateOrder(ctx, req)
	if err != nil {
		o.deps.Log.Error("🔴 TRAP order failed", "error", err)
		return
	}

	o.deps.Log.Info("📨 TRAP submitted", "orderID", orderID)

	o.publishOrLog(events.TopicTrapFired, events.TrapFiredEvent{
		Symbol:    c.Symbol,
		OrderID:   orderID,
		Side:      int(c.Side),
		CloseSide: int(c.CloseSide),
		Price:     trapPrice,
		Volume:    c.Volume,
		TPPrice:   tpPrice,
		SLPrice:   slPrice,
		Source:    "ob_monitor",
		Timestamp: time.Now(),
	})
}

// fireStaticTrap is the fallback when no wall is found.
// It relies on FireLimitTrap from opener.go to calculate the standard trap price.
func (o *CycleOrchestrator) fireStaticTrap(ctx context.Context) {
	res := FireLimitTrap(ctx, o.deps.Client, &o.candidate, o.deps.Clock, o.deps.Log)
	if res.IsSuccess() {
		o.publishOrLog(events.TopicTrapFired, events.TrapFiredEvent{
			Symbol:    res.Candidate.Symbol,
			OrderID:   res.OrderID,
			Side:      int(res.Candidate.Side),
			CloseSide: int(res.Candidate.CloseSide),
			Price:     res.Candidate.CalculateTrapPrice(),
			Volume:    res.Candidate.Volume,
			Source:    "static_limit",
			Timestamp: time.Now(),
		})
	}
}

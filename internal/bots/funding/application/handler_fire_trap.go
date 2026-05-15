package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"
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

	o.deps.Log.Info("⏱️ Fire Trap waiting", slog.Duration("delay", delay), slog.Time("target_time", trapTime))
	o.waitUntil(ctx, trapTime)
	if ctx.Err() != nil {
		return
	}

	ob, err := o.deps.DepthStore.GetDepth(ctx, o.cfg.Symbol)
	if err != nil || ob == nil {
		o.deps.Log.Warn("🟡 Fire Trap: failed to fetch depth, falling back to static trap", slog.Any("error", err))
		o.fireStaticTrap(ctx)
		return
	}

	var trapSide shared.Side
	var wallPrice float64
	var trapCandidate domain.Candidate
	o.withLock(func() {
		trapSide = o.candidate.Side.Opposite()
		wallPrice = o.candidate.FindTrapWallPrice(ob)
		trapCandidate = o.candidate
		trapCandidate.Side = trapSide
		trapCandidate.CloseSide = shared.CloseSideFor(trapSide)
	})

	if wallPrice <= 0 {
		o.deps.Log.Info("🤷 Fire Trap: no suitable wall found, falling back to static trap", slog.String("side", trapSide.String()))
		o.fireStaticTrap(ctx)
		return
	}

	o.publishOrLog(events.TopicOBWallFound, events.OBWallFoundEvent{
		Symbol:    o.cfg.Symbol,
		WallPrice: wallPrice,
		WallVol:   0, // wallVol is not rigorously tracked here anymore
		Side:      trapSide,
	})

	// 4. Calculate Trap Price (1 tick before the wall).
	trapPrice := trapCandidate.CalculateOBTrapPrice(wallPrice)

	o.deps.Log.Info("🧱 OB Monitor: wall found",
		slog.String("side", trapSide.String()),
		slog.Float64("wallPrice", wallPrice),
		slog.Float64("trapPrice", trapPrice),
	)

	// 5. Fire Trap limit order.
	o.fireOBTrap(ctx, trapCandidate, trapPrice)
}

const trapSourceOBMonitor = "ob_monitor"
const trapSourceStaticLimit = "static_limit"

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
		slog.String("symbol", c.Symbol),
		slog.Float64("trapPrice", trapPrice),
		slog.Float64("vol", c.Volume),
		slog.String("side", c.Side.String()),
		slog.Float64("takeProfitPrice", tpPrice),
		slog.Float64("stopLossPrice", slPrice),
	)

	orderID, err := o.deps.Client.CreateOrder(ctx, req)
	if err != nil {
		o.deps.Log.Error("🔴 TRAP order failed", slog.Any("error", err))
		return
	}

	o.deps.Log.Info("📨 TRAP submitted", slog.String("orderID", orderID))

	o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
		b.TrapSource = trapSourceOBMonitor
		b.TrapPrice = trapPrice
		b.TrapOrderID = orderID
	})

	o.publishOrLog(events.TopicTrapFired, events.TrapFiredEvent{
		Symbol:    c.Symbol,
		OrderID:   orderID,
		Side:      c.Side,
		CloseSide: c.CloseSide,
		Price:     trapPrice,
		Volume:    c.Volume,
		TPPrice:   tpPrice,
		SLPrice:   slPrice,
		Source:    trapSourceOBMonitor,
		Timestamp: time.Now(),
	})
}

// fireStaticTrap is the fallback when no wall is found.
// It relies on FireLimitTrap from opener.go to calculate the standard trap price.
func (o *CycleOrchestrator) fireStaticTrap(ctx context.Context) {
	var c domain.Candidate
	o.withLock(func() {
		c = o.candidate
	})

	res := FireLimitTrap(ctx, o.deps.Client, &c, o.deps.Clock, o.deps.Log)

	o.withLock(func() {
		o.results = append(o.results, res)
	})

	if res.IsSuccess() {
		// Capture trap data for cycle record.
		o.recorder.Mutate(func(b *domain.CycleRecordBuilder) {
			b.TrapSource = trapSourceStaticLimit
			b.TrapPrice = res.Price
			b.TrapOrderID = res.OrderID
		})

		o.publishOrLog(events.TopicTrapFired, events.TrapFiredEvent{
			Symbol:    res.Candidate.Symbol,
			OrderID:   res.OrderID,
			Side:      res.Candidate.Side,
			CloseSide: res.Candidate.CloseSide,
			Price:     res.Price,
			Volume:    res.Volume,
			TPPrice:   res.TakeProfitPrice,
			SLPrice:   res.StopLossPrice,
			Source:    "static_limit",
			Timestamp: time.Now(),
		})
	}
}

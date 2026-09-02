package application

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	pjstore "crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/eventbus"
)

// WallDetector isolates orderbook depth scanning and point-in-time wall event generation.
type WallDetector struct {
	exchange      string
	cfg           pjdomain.WallDetectorConfig
	depthStore    *pjstore.DepthStore
	contractStore store.ContractReader
	bus           *eventbus.Bus
	logger        *slog.Logger
}

// NewWallDetector initializes a WallDetector.
func NewWallDetector(
	exchangeName string,
	cfg pjdomain.WallDetectorConfig,
	depthStore *pjstore.DepthStore,
	contractStore store.ContractReader,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *WallDetector {
	return &WallDetector{
		exchange:      exchangeName,
		cfg:           cfg,
		depthStore:    depthStore,
		contractStore: contractStore,
		bus:           bus,
		logger:        logger.With(slog.String("component", "WallDetector"), slog.String("exchange", exchangeName)),
	}
}

// ProcessOrderBook evaluates the latest orderbook snapshot and emits micro-state events.
func (d *WallDetector) ProcessOrderBook(ctx context.Context, ob *shared.OrderBook, now time.Time) []*pjdomain.Wall {
	if ob == nil || len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return nil
	}

	bestBid := ob.Bids[0].Price
	bestAsk := ob.Asks[0].Price
	if bestBid <= 0 || bestAsk <= 0 || bestBid >= bestAsk {
		return nil
	}

	spreadPct := (bestAsk - bestBid) / bestBid * 100.0
	if d.cfg.MaxSpreadPct > 0 && spreadPct > d.cfg.MaxSpreadPct {
		d.logger.Debug("Skipping orderbook: spread exceeds limit",
			slog.String("symbol", ob.Symbol),
			slog.Float64("spreadPct", spreadPct),
			slog.Float64("maxSpreadPct", d.cfg.MaxSpreadPct),
		)
		return nil
	}

	isSpot := !strings.Contains(strings.ToLower(d.exchange), "futures")
	var detected []*pjdomain.Wall

	// 1. Scan Bid Side (Long wall)
	bidWall := d.scanSide(ctx, ob.Symbol, shared.SideOpenLong, ob.Bids, ob.Bids, ob.Asks, bestBid, bestAsk, true, spreadPct, now)
	if bidWall != nil {
		detected = append(detected, bidWall)
	}

	// 2. Scan Ask Side (Short wall - skipped for Spot)
	if !isSpot {
		askWall := d.scanSide(ctx, ob.Symbol, shared.SideOpenShort, ob.Asks, ob.Bids, ob.Asks, bestBid, bestAsk, false, spreadPct, now)
		if askWall != nil {
			detected = append(detected, askWall)
		}
	}

	return detected
}

func (d *WallDetector) scanSide(
	ctx context.Context,
	symbol string,
	side shared.Side,
	levels []shared.OrderBookEntry,
	bids, asks []shared.OrderBookEntry,
	bestBid, bestAsk float64,
	isBid bool,
	spreadPct float64,
	now time.Time,
) *pjdomain.Wall {
	existingWall, _ := d.depthStore.GetActiveWall(symbol, side)

	// Case 1: Existing wall is being tracked -> check if still present at the EXACT same price
	if existingWall != nil {
		candidate := d.findLevelByPrice(ctx, symbol, side, levels, bids, asks, existingWall, bestBid, bestAsk, isBid, now)
		if candidate != nil {
			return d.handleExistingCandidate(ctx, symbol, side, candidate, existingWall, spreadPct, now)
		}

		// Wall moved or disappeared -> finalize old wall immediately
		d.handleDisappearedWall(symbol, side, existingWall, spreadPct, now)
	}

	// Case 2: Scan for fresh candidate
	candidate := d.findBestCandidate(ctx, symbol, side, levels, bids, asks, bestBid, bestAsk, isBid, now)
	if candidate == nil {
		return nil
	}

	return d.handleFreshCandidate(candidate, spreadPct, now)
}

func (d *WallDetector) findLevelByPrice(
	ctx context.Context,
	symbol string,
	side shared.Side,
	levels []shared.OrderBookEntry,
	bids, asks []shared.OrderBookEntry,
	existingWall *pjdomain.Wall,
	bestBid, bestAsk float64,
	isBid bool,
	now time.Time,
) *pjdomain.Wall {
	priceUnit := d.getPriceUnit(ctx, symbol)
	contractSize := d.getContractSize(ctx, symbol)
	maxLevels := min(len(levels), 20)

	for i := range maxLevels {
		lvl := levels[i]
		if math.Abs(lvl.Price-existingWall.Price) <= (priceUnit * 0.5) {
			ev, ok := d.evalLevel(levels, bids, asks, lvl, bestBid, bestAsk, isBid, contractSize)
			if !ok {
				return nil
			}

			return &pjdomain.Wall{
				ID:              existingWall.ID,
				Exchange:        d.exchange,
				Symbol:          symbol,
				Side:            side,
				Price:           ev.price,
				Volume:          ev.volume,
				InitialVolume:   existingWall.InitialVolume,
				AvgNearbyVolume: ev.avgNearbyVol,
				RelativeRatio:   ev.relativeRatio,
				DistancePct:     ev.distPct,
				DepthImbalance:  ev.depthImbalance,
				BackingRatio:    ev.backingRatio,
				FirstDetectedAt: existingWall.FirstDetectedAt,
				LastUpdatedAt:   now,
				Status:          existingWall.Status,
				EventSeq:        existingWall.EventSeq,
				Matured:         existingWall.Matured,
			}
		}
	}

	return nil
}

func (d *WallDetector) findBestCandidate(
	ctx context.Context,
	symbol string,
	side shared.Side,
	levels []shared.OrderBookEntry,
	bids, asks []shared.OrderBookEntry,
	bestBid, bestAsk float64,
	isBid bool,
	now time.Time,
) *pjdomain.Wall {
	contractSize := d.getContractSize(ctx, symbol)
	maxLevels := min(len(levels), 20)

	var bestCandidate *pjdomain.Wall
	maxNotional := 0.0

	for i := range maxLevels {
		ev, ok := d.evalLevel(levels, bids, asks, levels[i], bestBid, bestAsk, isBid, contractSize)
		if !ok {
			continue
		}

		if ev.notional > maxNotional {
			maxNotional = ev.notional
			bestCandidate = &pjdomain.Wall{
				ID:              exchange.ExternalUniqueID(symbol, now, d.exchange),
				Exchange:        d.exchange,
				Symbol:          symbol,
				Side:            side,
				Price:           ev.price,
				Volume:          ev.volume,
				InitialVolume:   ev.volume,
				AvgNearbyVolume: ev.avgNearbyVol,
				RelativeRatio:   ev.relativeRatio,
				DistancePct:     ev.distPct,
				DepthImbalance:  ev.depthImbalance,
				BackingRatio:    ev.backingRatio,
				FirstDetectedAt: now,
				LastUpdatedAt:   now,
				Status:          pjdomain.WallStatusActive,
				EventSeq:        0,
				Matured:         false,
			}
		}
	}

	return bestCandidate
}

type levelEval struct {
	price          float64
	volume         float64
	distPct        float64
	notional       float64
	avgNearbyVol   float64
	relativeRatio  float64
	backingRatio   float64
	depthImbalance float64
}

func (d *WallDetector) getContractSize(ctx context.Context, symbol string) float64 {
	if d.contractStore == nil {
		return 1
	}
	cd, err := d.contractStore.GetContract(ctx, symbol)
	if err != nil || cd == nil {
		return 1
	}
	if cd.ContractSize <= 0 {
		return 1
	}
	return cd.ContractSize
}

func (d *WallDetector) getPriceUnit(ctx context.Context, symbol string) float64 {
	if d.contractStore == nil {
		return 0.01
	}
	cd, err := d.contractStore.GetContract(ctx, symbol)
	if err != nil || cd == nil || cd.PriceUnit <= 0 {
		return 0.01
	}
	return cd.PriceUnit
}

func (d *WallDetector) evalLevel(
	levels []shared.OrderBookEntry,
	bids, asks []shared.OrderBookEntry,
	lvl shared.OrderBookEntry,
	bestBid, bestAsk float64,
	isBid bool,
	contractSize float64,
) (*levelEval, bool) {
	distPct := d.calcDistPct(lvl.Price, bestBid, bestAsk, isBid)
	if distPct < 0 || (d.cfg.MaxWallDistancePct > 0 && distPct > d.cfg.MaxWallDistancePct) {
		return nil, false
	}

	notional := lvl.Price * lvl.Volume * contractSize
	if d.cfg.MinVolumeUSDT > 0 && notional < d.cfg.MinVolumeUSDT {
		return nil, false
	}

	avgNearbyVol, relRatio, backingRatio := d.calcRelativeAndBackingRatio(levels, lvl.Price, lvl.Volume)
	if d.cfg.MinRelativeRatio > 0 && relRatio < d.cfg.MinRelativeRatio {
		return nil, false
	}

	depthImbalance := d.calcDepthImbalance(bids, asks, bestBid, bestAsk, isBid)

	return &levelEval{
		price:          lvl.Price,
		volume:         lvl.Volume,
		distPct:        distPct,
		notional:       notional,
		avgNearbyVol:   avgNearbyVol,
		relativeRatio:  relRatio,
		backingRatio:   backingRatio,
		depthImbalance: depthImbalance,
	}, true
}

func (d *WallDetector) calcDistPct(price, bestBid, bestAsk float64, isBid bool) float64 {
	if isBid {
		if price > bestBid {
			return 0
		}
		return (bestBid - price) / bestBid * 100.0
	}
	if price < bestAsk {
		return 0
	}
	return (price - bestAsk) / bestAsk * 100.0
}

func (d *WallDetector) calcDepthImbalance(bids, asks []shared.OrderBookEntry, bestBid, bestAsk float64, isBid bool) float64 {
	if bestBid <= 0 || bestAsk <= 0 || len(bids) == 0 || len(asks) == 0 {
		return 1.0
	}
	bidThreshold := bestBid * 0.99
	askThreshold := bestAsk * 1.01

	bidVol := 0.0
	for _, b := range bids {
		if b.Price >= bidThreshold {
			bidVol += b.Volume * b.Price
		} else {
			break
		}
	}

	askVol := 0.0
	for _, a := range asks {
		if a.Price <= askThreshold {
			askVol += a.Volume * a.Price
		} else {
			break
		}
	}

	if isBid {
		if askVol <= 0 {
			return 10.0
		}
		return bidVol / askVol
	}

	if bidVol <= 0 {
		return 10.0
	}
	return askVol / bidVol
}

func (d *WallDetector) calcRelativeAndBackingRatio(
	levels []shared.OrderBookEntry,
	wallPrice, wallVolume float64,
) (float64, float64, float64) {
	if len(levels) == 0 || wallVolume <= 0 {
		return 0, 0, 1.0
	}

	wallIdx := d.findLevelIndex(levels, wallPrice)
	avg := d.calcTrimmedNearbyAverage(levels, wallIdx)
	ratio := wallVolume / avg
	backingRatio := d.calcBackingRatio(levels, wallIdx, avg)

	return avg, ratio, backingRatio
}

func (d *WallDetector) findLevelIndex(levels []shared.OrderBookEntry, wallPrice float64) int {
	for i := range levels {
		if decmath.Equal(levels[i].Price, wallPrice) {
			return i
		}
	}
	return -1
}

func (d *WallDetector) calcTrimmedNearbyAverage(levels []shared.OrderBookEntry, wallIdx int) float64 {
	windowSize := d.cfg.NeighborhoodLevels
	if windowSize <= 0 {
		windowSize = 5
	}

	startIdx := 0
	endIdx := len(levels)
	if wallIdx != -1 {
		startIdx = max(0, wallIdx-windowSize)
		endIdx = min(len(levels), wallIdx+windowSize+1)
	}

	var candidateVols []float64
	for i := startIdx; i < endIdx; i++ {
		if i == wallIdx {
			continue
		}
		candidateVols = append(candidateVols, levels[i].Volume)
	}

	if len(candidateVols) == 0 {
		return 1.0
	}

	sort.Float64s(candidateVols)
	trimmed := candidateVols
	if len(candidateVols) >= 5 {
		trimCount := int(math.Floor(float64(len(candidateVols)) * 0.10))
		trimmed = candidateVols[trimCount : len(candidateVols)-trimCount]
	}

	var sum float64
	for _, v := range trimmed {
		sum += v
	}
	avg := sum / float64(len(trimmed))
	if avg <= 0 {
		return 1.0
	}
	return avg
}

func (d *WallDetector) calcBackingRatio(levels []shared.OrderBookEntry, wallIdx int, avgNearbyVol float64) float64 {
	if wallIdx == -1 || wallIdx >= len(levels)-1 || avgNearbyVol <= 0 {
		return 1.0
	}

	count := 0
	backingSum := 0.0
	for k := 1; k <= 3 && (wallIdx+k) < len(levels); k++ {
		backingSum += levels[wallIdx+k].Volume
		count++
	}
	if count > 0 {
		avgBacking := backingSum / float64(count)
		return avgBacking / avgNearbyVol
	}
	return 1.0
}

func (d *WallDetector) handleExistingCandidate(
	_ context.Context,
	symbol string,
	side shared.Side,
	candidate *pjdomain.Wall,
	existingWall *pjdomain.Wall,
	spreadPct float64,
	now time.Time,
) *pjdomain.Wall {
	prevVol := existingWall.Volume
	deltaVol := candidate.Volume - prevVol

	// Update current snapshot state
	existingWall.Volume = candidate.Volume
	existingWall.LastUpdatedAt = now
	prevDistPct := existingWall.DistancePct
	existingWall.DistancePct = candidate.DistancePct
	existingWall.AvgNearbyVolume = candidate.AvgNearbyVolume
	existingWall.RelativeRatio = candidate.RelativeRatio
	existingWall.DepthImbalance = candidate.DepthImbalance
	existingWall.BackingRatio = candidate.BackingRatio
	existingWall.RelativeRatio = candidate.RelativeRatio

	// Maturation check (>= minLifespan)
	minLifespan := d.cfg.MinLifespan.Duration()
	if !existingWall.Matured && minLifespan > 0 && existingWall.GetAgeAt(now) >= minLifespan {
		existingWall.Matured = true
		d.emitWallEvent(existingWall, pjdomain.WallEventMatured, 0, spreadPct, now)
	}

	// Price approached check
	priceAppThreshold := d.cfg.PriceApproachedDistancePct
	if priceAppThreshold <= 0 {
		priceAppThreshold = 0.1
	}
	if existingWall.DistancePct <= priceAppThreshold && prevDistPct > priceAppThreshold {
		d.emitWallEvent(existingWall, pjdomain.WallEventPriceApproached, 0, spreadPct, now)
	}

	// Check volume changes (absorption vs resize)
	d.checkVolumeChange(symbol, side, existingWall, deltaVol, spreadPct, now)

	// Weakened check (< 50% initial volume)
	d.checkWeakened(existingWall, spreadPct, now)

	d.depthStore.SaveActiveWall(*existingWall)

	return existingWall
}

func (d *WallDetector) checkVolumeChange(
	symbol string,
	side shared.Side,
	existingWall *pjdomain.Wall,
	deltaVol, spreadPct float64,
	now time.Time,
) {
	if deltaVol < 0 {
		takerSide := shared.SideOpenShort
		if !side.IsLong() {
			takerSide = shared.SideOpenLong
		}
		tradedVol := d.depthStore.GetTradedVolume(symbol, existingWall.Price, takerSide)
		if tradedVol > 0 {
			d.emitWallEvent(existingWall, pjdomain.WallEventAbsorbed, deltaVol, spreadPct, now)
		} else {
			d.emitWallEvent(existingWall, pjdomain.WallEventResized, deltaVol, spreadPct, now)
		}
	} else if deltaVol > 0 {
		d.emitWallEvent(existingWall, pjdomain.WallEventResized, deltaVol, spreadPct, now)
	}
}

func (d *WallDetector) checkWeakened(
	existingWall *pjdomain.Wall,
	spreadPct float64,
	now time.Time,
) {
	weakenedPct := d.cfg.WeakenedThresholdPct
	if weakenedPct <= 0 {
		weakenedPct = 50.0
	}
	if existingWall.InitialVolume > 0 && (existingWall.Volume/existingWall.InitialVolume*100.0) < weakenedPct && existingWall.Status != pjdomain.WallStatusWeakened {
		existingWall.Status = pjdomain.WallStatusWeakened
		d.emitWallEvent(existingWall, pjdomain.WallEventWeakened, 0, spreadPct, now)
	}
}

func (d *WallDetector) handleFreshCandidate(
	candidate *pjdomain.Wall,
	spreadPct float64,
	now time.Time,
) *pjdomain.Wall {
	candidate.EventSeq = 0
	d.emitWallEvent(candidate, pjdomain.WallEventBorn, 0, spreadPct, now)
	d.depthStore.SaveActiveWall(*candidate)

	return candidate
}

func (d *WallDetector) handleDisappearedWall(
	symbol string,
	side shared.Side,
	existingWall *pjdomain.Wall,
	spreadPct float64,
	now time.Time,
) {
	takerSide := shared.SideOpenShort
	if !side.IsLong() {
		takerSide = shared.SideOpenLong
	}
	tradedVol := d.depthStore.GetTradedVolume(symbol, existingWall.Price, takerSide)

	if tradedVol >= existingWall.Volume && existingWall.Volume > 0 {
		existingWall.Status = pjdomain.WallStatusConsumed
		d.emitWallEvent(existingWall, pjdomain.WallEventConsumed, -existingWall.Volume, spreadPct, now)
	} else {
		existingWall.Status = pjdomain.WallStatusDisappeared
		d.emitWallEvent(existingWall, pjdomain.WallEventDisappeared, -existingWall.Volume, spreadPct, now)
	}

	d.depthStore.DeleteActiveWall(symbol, side)
}

func (d *WallDetector) emitWallEvent(
	wall *pjdomain.Wall,
	eventType pjdomain.WallEventType,
	deltaVol float64,
	spreadPct float64,
	now time.Time,
) {
	wall.EventSeq++

	evt := pjdomain.WallEvent{
		WallID:        wall.ID,
		Seq:           wall.EventSeq,
		Timestamp:     now,
		EventType:     eventType,
		Price:         wall.Price,
		Volume:        wall.Volume,
		DeltaVolume:   deltaVol,
		DistancePct:   wall.DistancePct,
		SpreadPct:     spreadPct,
		RelativeRatio: wall.RelativeRatio,
	}

	d.depthStore.AppendWallEvent(wall.ID, evt)

	payload := pjdomain.WallEventStreamPayload{
		Exchange:  d.exchange,
		Symbol:    wall.Symbol,
		Side:      wall.Side.String(),
		Event:     evt,
		Timestamp: now,
	}

	if err := d.bus.Publish(pjdomain.TopicWallEventStream, payload); err != nil {
		d.logger.Error("Failed to publish wall event",
			slog.String("wall_id", wall.ID),
			slog.String("event_type", string(eventType)),
			slog.Any("error", err),
		)
	}
}

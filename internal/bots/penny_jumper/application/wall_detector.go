package application

import (
	"context"
	"log/slog"
	"strings"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	pjstore "crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/eventbus"
)

// WallDetector detects and tracks orderbook walls from depth snapshots using DepthStore as an event generator.
type WallDetector struct {
	exchange      string
	cfg           pjdomain.WallDetectorConfig
	depthStore    *pjstore.DepthStore
	contractStore store.ContractReader
	bus           *eventbus.Bus
	logger        *slog.Logger
}

// NewWallDetector creates a new WallDetector.
func NewWallDetector(
	exch string,
	cfg pjdomain.WallDetectorConfig,
	depthStore *pjstore.DepthStore,
	contractStore store.ContractReader,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *WallDetector {
	return &WallDetector{
		exchange:      exch,
		cfg:           cfg,
		depthStore:    depthStore,
		contractStore: contractStore,
		bus:           bus,
		logger:        logger.With("component", "WallDetector", "exchange", exch),
	}
}

// ProcessOrderBook evaluates the orderbook depth for bid/ask walls and publishes events to TopicWallEventStream.
func (d *WallDetector) ProcessOrderBook(ctx context.Context, ob *shared.OrderBook, now time.Time) []*pjdomain.Wall {
	if ob == nil || len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return nil
	}

	if !ob.Timestamp.IsZero() {
		now = ob.Timestamp
	} else if now.IsZero() {
		now = time.Now().UTC()
	}

	d.depthStore.SaveDepthSnapshot(ob.Symbol, ob)

	bestBid := ob.Bids[0].Price
	bestAsk := ob.Asks[0].Price
	if bestBid <= 0 || bestAsk <= 0 || bestBid >= bestAsk {
		return nil
	}

	// Calculate and filter spread
	spreadPct := ((bestAsk - bestBid) / bestBid) * 100.0
	if d.cfg.MaxSpreadPct > 0 && spreadPct > d.cfg.MaxSpreadPct {
		return nil
	}

	var detected []*pjdomain.Wall

	// Scan Bid Side (for Long jump candidate)
	if bidWall := d.scanSide(ctx, ob.Symbol, shared.SideOpenLong, ob.Bids, bestBid, bestAsk, spreadPct, true, now); bidWall != nil {
		detected = append(detected, bidWall)
	}

	// Scan Ask Side (for Short jump candidate - skipped on spot markets)
	if !d.isSpot() {
		if askWall := d.scanSide(ctx, ob.Symbol, shared.SideOpenShort, ob.Asks, bestBid, bestAsk, spreadPct, false, now); askWall != nil {
			detected = append(detected, askWall)
		}
	}

	return detected
}

func (d *WallDetector) isSpot() bool {
	return strings.HasSuffix(d.exchange, "_spot") || strings.EqualFold(d.exchange, "spot")
}

func (d *WallDetector) scanSide(
	ctx context.Context,
	symbol string,
	side shared.Side,
	levels []shared.OrderBookEntry,
	bestBid, bestAsk, spreadPct float64,
	isBid bool,
	now time.Time,
) *pjdomain.Wall {
	existingWall, _ := d.depthStore.GetActiveWall(symbol, side)

	var candidate *pjdomain.Wall
	if existingWall != nil {
		candidate = d.findLevelByPrice(ctx, symbol, side, levels, existingWall, bestBid, bestAsk, isBid, now)
	}

	if candidate == nil && len(levels) > 0 {
		candidate = d.findBestCandidate(ctx, symbol, side, levels, bestBid, bestAsk, isBid, now)
	}

	if candidate != nil {
		return d.handleFoundCandidate(ctx, symbol, side, candidate, existingWall, spreadPct, now)
	}

	if existingWall != nil {
		d.handleDisappearedWall(symbol, side, existingWall, spreadPct, now)
	}

	return nil
}

func (d *WallDetector) findLevelByPrice(
	ctx context.Context,
	symbol string,
	side shared.Side,
	levels []shared.OrderBookEntry,
	existingWall *pjdomain.Wall,
	bestBid, bestAsk float64,
	isBid bool,
	now time.Time,
) *pjdomain.Wall {
	if existingWall == nil {
		return nil
	}
	contractSize := d.getContractSize(ctx, symbol)

	for i := 0; i < len(levels) && i < 20; i++ {
		if levels[i].Price != existingWall.Price {
			continue
		}

		ev, ok := d.evalLevel(levels[i], bestBid, bestAsk, isBid, contractSize)
		if !ok {
			return nil
		}

		avgNearby, relRatio := d.calcRelativeRatio(levels, ev.price, ev.volume)

		return &pjdomain.Wall{
			ID:              existingWall.ID,
			Exchange:        d.exchange,
			Symbol:          symbol,
			Side:            side,
			Price:           ev.price,
			Volume:          ev.volume,
			InitialVolume:   existingWall.InitialVolume,
			AvgNearbyVolume: avgNearby,
			RelativeRatio:   relRatio,
			DistancePct:     ev.distPct,
			FirstDetectedAt: existingWall.FirstDetectedAt,
			LastUpdatedAt:   now,
			Status:          existingWall.Status,
			EventSeq:        existingWall.EventSeq,
		}
	}
	return nil
}

type levelEval struct {
	price    float64
	volume   float64
	distPct  float64
	notional float64
}

func (d *WallDetector) getContractSize(ctx context.Context, symbol string) float64 {
	if d.contractStore == nil {
		return 1
	}
	cd, err := d.contractStore.GetContract(ctx, symbol)
	if err != nil || cd == nil {
		if err != nil {
			d.logger.Debug("GetContract not found, defaulting contractSize to 1", slog.String("symbol", symbol), slog.Any("error", err))
		}
		return 1
	}

	if cd.ContractSize <= 0 {
		return 1
	}
	return cd.ContractSize
}

func (d *WallDetector) evalLevel(
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

	return &levelEval{
		price:    lvl.Price,
		volume:   lvl.Volume,
		distPct:  distPct,
		notional: notional,
	}, true
}

func (d *WallDetector) findBestCandidate(
	ctx context.Context,
	symbol string,
	side shared.Side,
	levels []shared.OrderBookEntry,
	bestBid, bestAsk float64,
	isBid bool,
	now time.Time,
) *pjdomain.Wall {
	contractSize := d.getContractSize(ctx, symbol)

	var bestEval *levelEval
	maxNotional := 0.0

	for i := 0; i < len(levels) && i < 20; i++ {
		ev, ok := d.evalLevel(levels[i], bestBid, bestAsk, isBid, contractSize)
		if !ok {
			continue
		}

		if bestEval == nil || ev.notional > maxNotional {
			maxNotional = ev.notional
			bestEval = ev
		}
	}

	if bestEval == nil {
		return nil
	}

	avgNearby, relRatio := d.calcRelativeRatio(levels, bestEval.price, bestEval.volume)

	return &pjdomain.Wall{
		ID:              exchange.ExternalUniqueID(symbol, now, d.exchange),
		Exchange:        d.exchange,
		Symbol:          symbol,
		Side:            side,
		Price:           bestEval.price,
		Volume:          bestEval.volume,
		InitialVolume:   bestEval.volume,
		AvgNearbyVolume: avgNearby,
		RelativeRatio:   relRatio,
		DistancePct:     bestEval.distPct,
		FirstDetectedAt: now,
		LastUpdatedAt:   now,
		Status:          pjdomain.WallStatusActive,
	}
}

func (d *WallDetector) calcRelativeRatio(levels []shared.OrderBookEntry, wallPrice, wallVolume float64) (float64, float64) {
	sumOther := 0.0
	count := 0
	for i := 0; i < len(levels) && i < 20; i++ {
		if levels[i].Price == wallPrice {
			continue
		}
		sumOther += levels[i].Volume
		count++
	}
	if count == 0 || sumOther <= 0 {
		return 0, 1.0
	}
	avg := sumOther / float64(count)
	ratio := wallVolume / avg
	return avg, ratio
}

func (d *WallDetector) calcDistPct(price, bestBid, bestAsk float64, isBid bool) float64 {
	if isBid {
		if bestBid <= 0 {
			return -1
		}
		return ((bestBid - price) / bestBid) * 100.0
	}
	if bestAsk <= 0 {
		return -1
	}
	return ((price - bestAsk) / bestAsk) * 100.0
}

func (d *WallDetector) handleFoundCandidate(
	ctx context.Context,
	symbol string,
	side shared.Side,
	bestCandidate *pjdomain.Wall,
	existingWall *pjdomain.Wall,
	spreadPct float64,
	now time.Time,
) *pjdomain.Wall {
	if existingWall != nil && existingWall.Price == bestCandidate.Price {
		return d.handleExistingCandidate(ctx, symbol, side, bestCandidate, existingWall, spreadPct, now)
	}

	if existingWall != nil {
		d.handleDisappearedWall(symbol, side, existingWall, spreadPct, now)
	}

	return d.handleFreshCandidate(bestCandidate, spreadPct, now)
}

func (d *WallDetector) handleExistingCandidate(
	_ context.Context,
	_ string,
	_ shared.Side,
	bestCandidate *pjdomain.Wall,
	existingWall *pjdomain.Wall,
	spreadPct float64,
	now time.Time,
) *pjdomain.Wall {
	prevVol := existingWall.Volume
	deltaVol := bestCandidate.Volume - prevVol

	// Update current snapshot state
	existingWall.Volume = bestCandidate.Volume
	existingWall.LastUpdatedAt = now
	existingWall.DistancePct = bestCandidate.DistancePct
	existingWall.AvgNearbyVolume = bestCandidate.AvgNearbyVolume
	existingWall.RelativeRatio = bestCandidate.RelativeRatio

	// Maturation check (>= minLifespan)
	minLifespan := d.cfg.MinLifespan.Duration()
	if !existingWall.Matured && minLifespan > 0 && existingWall.GetAgeAt(now) >= minLifespan {
		existingWall.Matured = true
		d.emitWallEvent(existingWall, pjdomain.WallEventMatured, 0, spreadPct, now)
	}

	// Emit Resize discrete event on any volume change
	if deltaVol != 0 {
		d.emitWallEvent(existingWall, pjdomain.WallEventResized, deltaVol, spreadPct, now)
	}
	d.depthStore.SaveActiveWall(*existingWall)

	return existingWall
}

func (d *WallDetector) handleFreshCandidate(
	bestCandidate *pjdomain.Wall,
	spreadPct float64,
	now time.Time,
) *pjdomain.Wall {
	bestCandidate.EventSeq = 1
	d.emitWallEvent(bestCandidate, pjdomain.WallEventBorn, 0, spreadPct, now)
	d.depthStore.SaveActiveWall(*bestCandidate)

	return bestCandidate
}

func (d *WallDetector) handleDisappearedWall(
	symbol string,
	side shared.Side,
	existingWall *pjdomain.Wall,
	spreadPct float64,
	now time.Time,
) {
	d.depthStore.RecordWallPull(symbol, existingWall.Price, now)
	d.depthStore.DeleteActiveWall(symbol, side)

	d.emitWallEvent(existingWall, pjdomain.WallEventDisappeared, 0, spreadPct, now)
}

func (d *WallDetector) emitWallEvent(
	wall *pjdomain.Wall,
	eventType pjdomain.WallEventType,
	deltaVol, spreadPct float64,
	now time.Time,
) {
	if wall == nil {
		return
	}

	if eventType != pjdomain.WallEventBorn {
		wall.EventSeq++
	}

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

	_ = d.bus.Publish(pjdomain.TopicWallEventStream, pjdomain.WallEventStreamPayload{
		Exchange:  wall.Exchange,
		Symbol:    wall.Symbol,
		Side:      wall.Side.String(),
		Event:     evt,
		Timestamp: now,
	})
}

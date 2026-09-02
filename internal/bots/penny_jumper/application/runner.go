package application

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	"crypto-bot/internal/bots/penny_jumper/infrastructure/persistence"
	pjstore "crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/formatutil"
	"crypto-bot/pkg/xjson"

	"github.com/patrickmn/go-cache"
)

// PennyJumperRunner is the pure event-sourced execution engine for Penny Jumper.
type PennyJumperRunner struct {
	cfg            pjdomain.PennyJumperConfig
	depthStores    map[string]*pjstore.DepthStore
	wallDetectors  map[string]*WallDetector
	wallJudge      pjdomain.WallJudge
	wallRepo       persistence.WallRepository
	contractStores map[string]*store.ContractStore
	notifier       notifier.Notifier
	bus            *eventbus.Bus
	logger         *slog.Logger

	subOnce       sync.Once
	subWg         sync.WaitGroup
	notifiedCache *cache.Cache
	evalCache     *cache.Cache
}

// NewPennyJumperRunner creates a new PennyJumperRunner and strictly validates all required dependencies.
func NewPennyJumperRunner(
	cfg pjdomain.PennyJumperConfig,
	depthStores map[string]*pjstore.DepthStore,
	wallDetectors map[string]*WallDetector,
	wallJudge pjdomain.WallJudge,
	wallRepo persistence.WallRepository,
	contractStores map[string]*store.ContractStore,
	notif notifier.Notifier,
	bus *eventbus.Bus,
	logger *slog.Logger,
) (*PennyJumperRunner, error) {
	if wallRepo == nil {
		return nil, fmt.Errorf("wallRepo is required")
	}
	if bus == nil {
		return nil, fmt.Errorf("event bus is required")
	}
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	if contractStores == nil {
		return nil, fmt.Errorf("contractStores is required")
	}
	if wallJudge == nil {
		wallJudge = pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{})
	}
	for _, exch := range cfg.GetExchanges() {
		if depthStores[exch] == nil {
			return nil, fmt.Errorf("missing depthStore for exchange: %s", exch)
		}
		if wallDetectors[exch] == nil {
			return nil, fmt.Errorf("missing wallDetector for exchange: %s", exch)
		}
		if contractStores[exch] == nil {
			return nil, fmt.Errorf("missing contractStore for exchange: %s", exch)
		}
	}

	evalCooldown := cfg.WallJudge.EvalCooldown.Duration()

	return &PennyJumperRunner{
		cfg:            cfg,
		depthStores:    depthStores,
		wallDetectors:  wallDetectors,
		wallJudge:      wallJudge,
		wallRepo:       wallRepo,
		contractStores: contractStores,
		notifier:       notif,
		bus:            bus,
		logger:         logger.With("component", "PennyJumperRunner"),
		notifiedCache:  cache.New(15*time.Minute, 5*time.Minute),
		evalCache:      cache.New(evalCooldown, 1*time.Minute),
	}, nil
}

// InitGlobalSubscriptions registers all Penny Jumper topic subscriptions on the bus EXACTLY ONCE.
func InitGlobalSubscriptions(ctx context.Context, runner *PennyJumperRunner) {
	runner.subOnce.Do(func() {
		registerAllSubscriptions(ctx, runner)
	})
}

func registerAllSubscriptions(ctx context.Context, r *PennyJumperRunner) {
	// 1. Depth Stream -> Wall Detection & Event Generation
	subscribeTopic[pjdomain.DepthUpdatedEvent](ctx, r, pjdomain.TopicDepthUpdated, func(msgCtx context.Context, evt pjdomain.DepthUpdatedEvent) error {
		return r.HandleDepthUpdated(msgCtx, evt)
	})

	// 2. Pure Event Sourced Stream -> Model Evaluation, Qualification, Telegram Alert & Persistence
	subscribeTopic[pjdomain.WallEventStreamPayload](ctx, r, pjdomain.TopicWallEventStream, func(msgCtx context.Context, evt pjdomain.WallEventStreamPayload) error {
		return r.HandleWallEventStream(msgCtx, evt)
	})
}

func subscribeTopic[T any](ctx context.Context, r *PennyJumperRunner, topic string, handler func(context.Context, T) error) {
	ch, err := r.bus.Subscribe(ctx, topic)
	if err != nil {
		r.logger.ErrorContext(ctx, "Failed to subscribe to topic", slog.String("topic", topic), slog.Any("error", err))
		return
	}

	r.subWg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var evt T
				if err := xjson.Unmarshal(msg.Payload, &evt); err == nil {
					if err := handler(ctx, evt); err != nil {
						r.logger.ErrorContext(ctx, "Handler error on topic", slog.String("topic", topic), slog.Any("error", err))
					}
				}
				msg.Ack()
			}
		}
	})
}

// HandleDepthUpdated processes incoming depth stream.
func (r *PennyJumperRunner) HandleDepthUpdated(ctx context.Context, evt pjdomain.DepthUpdatedEvent) error {
	if evt.OrderBook == nil {
		return nil
	}
	if depthStore, ok := r.depthStores[evt.Exchange]; ok && depthStore != nil {
		depthStore.SaveDepthSnapshot(evt.Symbol, evt.OrderBook)
	}
	detector, ok := r.wallDetectors[evt.Exchange]
	if !ok || detector == nil {
		return nil
	}
	_ = detector.ProcessOrderBook(ctx, evt.OrderBook, evt.Timestamp)
	return nil
}

// HandleWallEventStream handles discrete event-sourced wall transitions.
func (r *PennyJumperRunner) HandleWallEventStream(ctx context.Context, payload pjdomain.WallEventStreamPayload) error {
	evt := payload.Event
	exchange := payload.Exchange
	if exchange == "" {
		return nil
	}
	depthStore := r.depthStores[exchange]
	if depthStore == nil {
		return nil
	}

	events := depthStore.GetWallEventStream(evt.WallID)
	side := parseWallSide(payload.Side)

	switch evt.EventType {
	case pjdomain.WallEventMatured, pjdomain.WallEventPriceApproached, pjdomain.WallEventAbsorbed:
		// In-memory model evaluation and qualification
		r.evaluateAndQualifyWall(ctx, exchange, payload.Symbol, side, evt, events)
		return nil

	case pjdomain.WallEventDisappeared, pjdomain.WallEventConsumed:
		// Save complete event stream to DB once wall lifecycle ends
		return r.handleWallLifecycleEnded(ctx, exchange, payload.Symbol, side, evt, depthStore, events)

	case pjdomain.WallEventBorn, pjdomain.WallEventResized, pjdomain.WallEventWeakened:
		return nil
	}

	return nil
}

func (r *PennyJumperRunner) handleWallLifecycleEnded(
	ctx context.Context,
	exchange, symbol string,
	side shared.Side,
	evt pjdomain.WallEvent,
	depthStore *pjstore.DepthStore,
	events []pjdomain.WallEvent,
) error {
	r.evalCache.Delete(evt.WallID)

	initialVol := evt.Volume
	createdAt := evt.Timestamp
	var wallPrice float64
	var firstDetectedAt time.Time
	if len(events) > 0 {
		initialVol = events[0].Volume
		wallPrice = events[0].Price
		if !events[0].Timestamp.IsZero() {
			firstDetectedAt = events[0].Timestamp
			createdAt = events[0].Timestamp
		}
	}
	if wallPrice == 0 {
		if wall, found := depthStore.GetActiveWall(symbol, side); found && wall != nil {
			wallPrice = wall.Price
			if firstDetectedAt.IsZero() {
				firstDetectedAt = wall.FirstDetectedAt
			}
		}
	}
	durationMs := max(evt.Timestamp.Sub(createdAt).Milliseconds(), 0)

	takerSide := shared.SideOpenShort
	if !side.IsLong() {
		takerSide = shared.SideOpenLong
	}
	trades := depthStore.GetTradesForWall(symbol, wallPrice, takerSide, firstDetectedAt, evt.Timestamp)
	metrics := pjdomain.ReconcileWallData(nil, events, trades)

	wallRec := &persistence.PennyJumperWallRecord{
		ID:             evt.WallID,
		Exchange:       exchange,
		Symbol:         symbol,
		Side:           side.String(),
		WallPrice:      wallPrice,
		InitialVolume:  initialVol,
		FinalVolume:    evt.Volume,
		AbsorbedVolume: metrics.AbsorbedVolume,
		PulledVolume:   metrics.PulledVolume,
		DistancePct:    evt.DistancePct,
		SpreadPct:      evt.SpreadPct,
		Outcome:        string(evt.EventType),
		Reason:         string(evt.EventType),
		DurationMs:     durationMs,
		CreatedAt:      createdAt,
		CompletedAt:    &evt.Timestamp,
	}
	_ = wallRec.SetEvents(events)
	_ = wallRec.SetTrades(trades)
	if err := r.wallRepo.Save(ctx, wallRec); err != nil {
		r.logger.ErrorContext(ctx, "Failed to save final wall record", slog.Any("error", err))
	}

	depthStore.ClearWallEvents(evt.WallID)
	return nil
}

func (r *PennyJumperRunner) evaluateAndQualifyWall(
	ctx context.Context,
	exchange, symbol string,
	side shared.Side,
	evt pjdomain.WallEvent,
	events []pjdomain.WallEvent,
) {
	depthStore := r.depthStores[exchange]
	contractStore := r.contractStores[exchange]
	if depthStore == nil || contractStore == nil {
		return
	}

	wall, found := depthStore.GetActiveWall(symbol, side)
	if !found || wall == nil {
		return
	}

	eligibility, ok := r.isEligibleWall(ctx, wall, contractStore, depthStore, evt.Timestamp)
	if !ok {
		return
	}

	takerSide := shared.SideOpenShort
	if !side.IsLong() {
		takerSide = shared.SideOpenLong
	}
	trades := depthStore.GetTradesForWall(wall.Symbol, wall.Price, takerSide, wall.FirstDetectedAt, evt.Timestamp)

	cooldown := time.Duration(r.cfg.WallJudge.EvalCooldown)
	if !r.canEvaluateWall(wall.ID, cooldown) {
		return
	}

	wall.Vol24h = eligibility.Vol24h
	wall.WallTo1mRatio = eligibility.Ratio1m
	history := depthStore.GetWallHistory(wall.Symbol, wall.Price, 1*time.Hour, evt.Timestamp)
	wall.PullCount1h = history.PullCountIn1h
	wall.FillCount1h = history.FillCountIn1h

	judgeRes, err := r.wallJudge.JudgeWall(ctx, wall, events, trades)
	if err != nil || !judgeRes.IsTrusted {
		return
	}

	bestBid, bestAsk := extractBBO(depthStore, symbol)
	targetEntryPrice := r.calculateFrontRunPrice(wall.Side, wall.Price, bestBid, bestAsk, eligibility.PriceUnit)

	_ = r.bus.Publish(pjdomain.TopicWallQualified, pjdomain.WallQualifiedEvent{
		Wall:             *wall,
		TrustScore:       judgeRes.TrustScore,
		TargetEntryPrice: targetEntryPrice,
		SpreadPct:        evt.SpreadPct,
		Timestamp:        evt.Timestamp,
	})

	r.logger.InfoContext(ctx, "🎯 Wall Qualified by Local Model",
		slog.String("wall_id", wall.ID),
		slog.String("exchange", wall.Exchange),
		slog.String("symbol", wall.Symbol),
		slog.Float64("trust_score", judgeRes.TrustScore),
		slog.String("reason", judgeRes.Reason),
		slog.Float64("target_entry", targetEntryPrice),
	)

	r.dispatchWallNotification(ctx, wall, evt.SpreadPct, eligibility.SizeUSD, eligibility.Vol24h, eligibility.Ratio1m, evt.Timestamp)
}

type wallEligibility struct {
	SizeUSD   float64
	PriceUnit float64
	Vol24h    float64
	Ratio1m   float64
}

func (r *PennyJumperRunner) isEligibleWall(
	ctx context.Context,
	wall *pjdomain.Wall,
	contractStore *store.ContractStore,
	depthStore *pjstore.DepthStore,
	now time.Time,
) (wallEligibility, bool) {
	minLifespan := r.cfg.WallDetector.MinLifespan.Duration()
	if minLifespan > 0 && !wall.Matured && wall.GetAgeAt(now) < minLifespan {
		return wallEligibility{}, false
	}

	contractSize, priceUnit := r.getContractMetrics(ctx, contractStore, wall.Symbol)
	sizeUSD := wall.Volume * wall.Price * contractSize

	// 1. Static check: minVolumeUSDT
	if r.cfg.WallDetector.MinVolumeUSDT > 0 && sizeUSD < r.cfg.WallDetector.MinVolumeUSDT {
		return wallEligibility{}, false
	}

	// 2. Dynamic check: minWallTo1mVolRatio (ratio of wall size vs 1-minute turnover estimated from 24h volume)
	vol24h := 0.0
	ratio1m := 0.0
	if v24h, found := depthStore.GetVolume24h(wall.Symbol); found && v24h > 0 {
		vol24h = v24h
		vol1m := v24h / 1440.0
		if vol1m > 0 {
			ratio1m = sizeUSD / vol1m
		}
	}
	if r.cfg.WallDetector.MinWallTo1mVolRatio > 0 && vol24h > 0 && ratio1m < r.cfg.WallDetector.MinWallTo1mVolRatio {
		return wallEligibility{}, false
	}

	return wallEligibility{
		SizeUSD:   sizeUSD,
		PriceUnit: priceUnit,
		Vol24h:    vol24h,
		Ratio1m:   ratio1m,
	}, true
}

func (r *PennyJumperRunner) canEvaluateWall(wallID string, cooldown time.Duration) bool {
	if _, found := r.evalCache.Get(wallID); found {
		return false
	}
	r.evalCache.Set(wallID, true, cooldown)
	return true
}

func extractBBO(depthStore *pjstore.DepthStore, symbol string) (float64, float64) {
	if ob, ok := depthStore.GetLatestDepth(symbol); ok && ob != nil {
		bestBid := 0.0
		bestAsk := 0.0
		if len(ob.Bids) > 0 {
			bestBid = ob.Bids[0].Price
		}
		if len(ob.Asks) > 0 {
			bestAsk = ob.Asks[0].Price
		}
		return bestBid, bestAsk
	}
	return 0.0, 0.0
}

func parseWallSide(sideStr string) shared.Side {
	if sideStr == "LONG" || sideStr == "open_long" || sideStr == "SideOpenLong" {
		return shared.SideOpenLong
	}
	return shared.SideOpenShort
}

func (r *PennyJumperRunner) getContractMetrics(
	ctx context.Context,
	cStore *store.ContractStore,
	symbol string,
) (float64, float64) {
	contractSize := 1.0
	priceUnit := 0.01
	if cd, err := cStore.GetContract(ctx, symbol); err == nil && cd != nil {
		if cd.ContractSize > 0 {
			contractSize = cd.ContractSize
		}
		if cd.PriceUnit > 0 {
			priceUnit = cd.PriceUnit
		}
	}
	return contractSize, priceUnit
}

func (r *PennyJumperRunner) calculateFrontRunPrice(
	side shared.Side,
	wallPrice, bestBid, bestAsk, tickSize float64,
) float64 {
	if tickSize <= 0 {
		tickSize = 0.01
	}

	if side == shared.SideOpenLong {
		// Jump 1 tick in front of bid wall: min(wallPrice + tickSize, bestAsk - tickSize)
		target := decmath.SnapToTickFloor(wallPrice+tickSize, tickSize)
		if bestAsk > 0 && target >= bestAsk {
			target = math.Max(bestBid, decmath.SnapToTickFloor(bestAsk-tickSize, tickSize))
		}
		return target
	}

	// Jump 1 tick in front of ask wall: max(wallPrice - tickSize, bestBid + tickSize)
	target := decmath.SnapToTickCeil(wallPrice-tickSize, tickSize)
	if bestBid > 0 && target <= bestBid {
		target = math.Min(bestAsk, decmath.SnapToTickCeil(bestBid+tickSize, tickSize))
	}
	return target
}

func (r *PennyJumperRunner) dispatchWallNotification(
	ctx context.Context,
	wall *pjdomain.Wall,
	spreadPct, sizeUSD, vol24h, ratio1m float64,
	now time.Time,
) {
	if r.notifier == nil {
		return
	}

	wallKey := fmt.Sprintf("%s:%s:%s:%.6f", wall.Exchange, wall.Symbol, wall.Side.String(), wall.Price)
	// Suppress duplicate notifications within 15 minutes for the same price level
	if _, found := r.notifiedCache.Get(wallKey); found {
		return
	}
	r.notifiedCache.Set(wallKey, true, cache.DefaultExpiration)

	msg := FormatWallDetectedNotification(wall, spreadPct, sizeUSD, vol24h, ratio1m, now)
	if err := r.notifier.Send(ctx, notifier.Event{
		Level:     notifier.LevelNormal,
		Message:   msg,
		Timestamp: now,
	}); err != nil {
		r.logger.ErrorContext(ctx, "Failed to send wall qualified notification",
			slog.String("wall_id", wall.ID),
			slog.String("symbol", wall.Symbol),
			slog.Any("error", err),
		)
	}
}

func formatSideString(side shared.Side) string {
	switch side {
	case shared.SideOpenLong:
		return "Long"
	case shared.SideOpenShort:
		return "Short"
	case shared.SideCloseLong:
		return "Close Long"
	case shared.SideCloseShort:
		return "Close Short"
	default:
		return "Unknown"
	}
}

// FormatWallDetectedNotification formats a human-readable notification for a qualified wall.
func FormatWallDetectedNotification(
	wall *pjdomain.Wall,
	spreadPct float64,
	sizeUSD, vol24h, ratio1m float64,
	now time.Time,
) string {
	sideStr := formatSideString(wall.Side)
	wallPriceStr := formatutil.FormatPriceWithCommas(wall.Price)
	sizeUSDStr := formatutil.FormatUSDWithCommas(sizeUSD)
	ageStr := formatutil.FormatDuration(wall.GetAgeAt(now).Milliseconds())
	if ageStr == "0s" {
		ageStr = "<1s"
	}

	volRatioStr := ""
	if vol24h > 0 && ratio1m > 0 {
		vol24hStr := formatutil.FormatUSDWithCommas(vol24h)
		volRatioStr = fmt.Sprintf(" (%.1fx 1m Vol | 24h Vol: %s USDT)", ratio1m, vol24hStr)
	}

	imbalanceStr := ""
	if wall.DepthImbalance > 0 {
		imbalanceStr = fmt.Sprintf("\n• Imbalance: %.1fx | Backing: %.1fx", wall.DepthImbalance, wall.BackingRatio)
	}

	historyStr := ""
	if wall.PullCount1h > 0 || wall.FillCount1h > 0 {
		historyStr = fmt.Sprintf("\n• 1h History: %d Pulls / %d Fills", wall.PullCount1h, wall.FillCount1h)
	}

	return fmt.Sprintf("🟢 [PENNY_JUMPER] [%s] [WALL_QUALIFIED]\n"+
		"• Symbol: %s | Side: %s\n"+
		"• Price: %s | Size: %s USDT%s\n"+
		"• Dist: %.2f%% | Spread: %.2f%%%s%s\n"+
		"• Wall Age: %s",
		wall.Exchange,
		wall.Symbol,
		sideStr,
		wallPriceStr,
		sizeUSDStr,
		volRatioStr,
		wall.DistancePct,
		spreadPct,
		imbalanceStr,
		historyStr,
		ageStr,
	)
}

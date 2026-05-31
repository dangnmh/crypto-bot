package application

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/orders"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/ticker"

	"github.com/ThreeDotsLabs/watermill"
)

// ScanOpportunity bundles a scanned candidate with its trigger target settlement time.
type ScanOpportunity struct {
	Candidate  domain.Candidate
	SettleTime time.Time
}

// Scanner defines the dynamic scanner interface.
type Scanner interface {
	Scan(ctx context.Context) ([]ScanOpportunity, error)
}

// symbolState tracks execution history per symbol to prevent double triggering.
type symbolState struct {
	lastTriggerSettle time.Time
}

// ScannerJob orchestrates the background scanning loop.
type ScannerJob struct {
	scanners []Scanner
	engine   *app.Engine
	log      *slog.Logger
	statesMu sync.Mutex
	states   map[string]*symbolState
}

// NewScannerJob creates a new ScannerJob.
func NewScannerJob(
	scanners []Scanner,
	engine *app.Engine,
	log *slog.Logger,
) *ScannerJob {
	return &ScannerJob{
		scanners: scanners,
		engine:   engine,
		log:      log.With("component", "scanner_job"),
		states:   make(map[string]*symbolState),
	}
}

// Run starts the background scanner tick loop using the application context.
func (j *ScannerJob) Run(ctx context.Context) error {
	j.log.InfoContext(ctx, "🚀 Starting background scanner job loop")
	defer j.log.InfoContext(context.WithoutCancel(ctx), "🛑 Background scanner job loop stopped")

	ticker.RunImmediate(ctx, time.Minute, func() bool {
		j.tick(ctx)
		return true
	})
	return nil
}

func (j *ScannerJob) tick(ctx context.Context) {
	for _, sc := range j.scanners {
		opportunities, err := sc.Scan(ctx)
		if err != nil {
			j.log.ErrorContext(ctx, "Scanner failed to scan", slog.Any("error", err))
			continue
		}

		for i := range opportunities {
			opp := &opportunities[i]
			if j.shouldTrigger(opp.Candidate.Config.Exchange, opp.Candidate.Symbol, opp.SettleTime) {
				j.trigger(ctx, opp.Candidate, opp.SettleTime)
			}
		}
	}
}

func (j *ScannerJob) shouldTrigger(exchange, symbol string, settle time.Time) bool {
	j.statesMu.Lock()
	defer j.statesMu.Unlock()

	key := exchange + ":" + symbol
	state, exists := j.states[key]
	if !exists {
		state = &symbolState{}
		j.states[key] = state
	}

	if !state.lastTriggerSettle.IsZero() && !settle.After(state.lastTriggerSettle) {
		return false
	}

	state.lastTriggerSettle = settle
	return true
}

func (j *ScannerJob) trigger(ctx context.Context, candidate domain.Candidate, settle time.Time) {
	reqID := observability.ReversionID(ctx)
	if reqID == "" {
		reqID = watermill.NewUUID()
	}
	runCtx := observability.WithReversionIDValue(ctx, reqID)

	candidate.ExternalID = orders.ExternalOrderID("ioc", candidate.Symbol)

	j.log.InfoContext(runCtx, "Opportunity found! Triggering reversion event flow",
		slog.String("symbol", candidate.Symbol),
		slog.String("exchange", candidate.Config.Exchange),
		slog.String("externalID", candidate.ExternalID),
		slog.Float64("fundingRate", candidate.FundingRate),
	)

	var eventTimestamp time.Time
	if prov, err := j.engine.GetProvider(candidate.Config.Exchange); err == nil {
		eventTimestamp = prov.TimeSync.Now()
	} else {
		eventTimestamp = time.Now()
	}

	startEvt := reversion.CandidateFoundEvent{
		BaseReversionEvent: reversion.BaseReversionEvent{
			Flow:       reversion.FlowReversion,
			ReqID:      reqID,
			Symbol:     candidate.Symbol,
			Exchange:   candidate.Config.Exchange,
			SendNotify: false,
			Timestamp:  eventTimestamp,
			EventID:    watermill.NewUUID(),
			Seq:        1,
			Topic:      reversion.TopicReversionCandidate,
			ExternalID: candidate.ExternalID,
			SettleTime: settle,
		},
		Candidate: candidate,
	}

	if err := j.engine.Bus.Publish(reversion.TopicReversionCandidate, startEvt); err != nil {
		j.log.ErrorContext(runCtx, "Failed to publish reversion candidate event", slog.Any("error", err))
	} else {
		j.log.InfoContext(runCtx, "Reversion candidate event successfully published", slog.Time("settle", settle))
	}
}

// ConfiguredScanner scans statically configured symbols.
type ConfiguredScanner struct {
	cfg            *config.Config
	engine         *app.Engine
	stores         map[string]strategy.FundingStoreSet
	log            *slog.Logger
	disabledReason func(string) (string, bool)
}

// NewConfiguredScanner creates a new ConfiguredScanner.
func NewConfiguredScanner(
	cfg *config.Config,
	engine *app.Engine,
	stores map[string]strategy.FundingStoreSet,
	log *slog.Logger,
	disabledReason func(string) (string, bool),
) *ConfiguredScanner {
	return &ConfiguredScanner{
		cfg:            cfg,
		engine:         engine,
		stores:         stores,
		log:            log.With("component", "configured_scanner"),
		disabledReason: disabledReason,
	}
}

// Scan loops over configured symbols and verifies them within the 60-second settlement window.
func (s *ConfiguredScanner) Scan(ctx context.Context) ([]ScanOpportunity, error) {
	var opportunities []ScanOpportunity

	for i := range s.cfg.Symbols {
		symCfg := s.cfg.Symbols[i]

		// Skip if disabled in-memory
		if reason, disabled := s.disabledReason(symCfg.Symbol); disabled {
			s.log.DebugContext(ctx, "Skipping disabled symbol", slog.String("symbol", symCfg.Symbol), slog.String("reason", reason))
			continue
		}

		storeSet, ok := s.stores[symCfg.Exchange]
		if !ok {
			s.log.WarnContext(ctx, "Store set not found for exchange", slog.String("exchange", symCfg.Exchange))
			continue
		}

		// Retrieve next settle time
		settle, err := GetNextSettleTime(ctx, symCfg.SimulateSettle, symCfg.Symbol, storeSet.Funding())
		if err != nil {
			s.log.DebugContext(ctx, "Failed to get next settle time", slog.String("symbol", symCfg.Symbol), slog.Any("error", err))
			continue
		}

		// Retrieve ticker data
		if storeSet.Ticker() == nil {
			s.log.WarnContext(ctx, "Ticker store not ready", slog.String("exchange", symCfg.Exchange))
			continue
		}

		td, err := storeSet.Ticker().GetTicker(ctx, symCfg.Symbol)
		if err != nil {
			s.log.DebugContext(ctx, "No ticker data available yet", slog.String("symbol", symCfg.Symbol), slog.Any("error", err))
			continue
		}

		// Build and enrich candidate opportunity
		candidate := s.buildCandidate(symCfg, td)
		if !s.enrich(ctx, storeSet.Contract(), &candidate) {
			s.log.DebugContext(ctx, "Contract enrichment failed for candidate", slog.String("symbol", symCfg.Symbol))
			continue
		}

		opportunities = append(opportunities, ScanOpportunity{
			Candidate:  candidate,
			SettleTime: settle,
		})
	}

	return opportunities, nil
}

func (s *ConfiguredScanner) buildCandidate(sc config.SymbolConfig, td *store.TickerData) domain.Candidate {
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
		Config:      ToTradeConfig(sc),
		TradeIntent: intent,
		MarketData: domain.MarketData{
			LastPrice: td.LastPrice,
			BestBid:   td.BestBid,
			BestAsk:   td.BestAsk,
			Volume24:  td.Volume24,
			Amount24:  td.Amount24,
		},
	}
}

func (s *ConfiguredScanner) enrich(ctx context.Context, contractStore store.ContractReader, c *domain.Candidate) bool {
	if contractStore == nil {
		return false
	}
	cd, err := contractStore.GetContract(ctx, c.Symbol)
	if err != nil {
		s.log.WarnContext(ctx, "🟡 No contract data — skip", slog.String("symbol", c.Symbol))
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

// ToTradeConfig converts config.SymbolConfig to domain.TradeConfig.
func ToTradeConfig(sc config.SymbolConfig) domain.TradeConfig {
	return domain.TradeConfig{
		Symbol:              sc.Symbol,
		Exchange:            sc.Exchange,
		SimulateSettle:      sc.SimulateSettle,
		MaxPriceDiffPercent: sc.MaxPriceDiffPercent,
		MarginUSDT:          sc.MarginUSDT,
		Leverage:            sc.Leverage,
		FundingReversion:    sc.FundingReversion,
		FundingTrap:         sc.FundingTrap,
		ParsedOpenType:      sc.ParsedOpenType,
		ParsedPositionMode:  sc.ParsedPositionMode,
	}
}

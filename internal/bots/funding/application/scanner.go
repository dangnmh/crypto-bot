package application

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application/orders"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"

	"github.com/ThreeDotsLabs/watermill"
	"github.com/robfig/cron/v3"
)

const (
	refPriceBestAsk = "bestAsk"
	refPriceBestBid = "bestBid"
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
	cfg      *config.Config
	log      *slog.Logger
	statesMu sync.Mutex
	states   map[string]*symbolState
}

// NewScannerJob creates a new ScannerJob.
func NewScannerJob(
	scanners []Scanner,
	engine *app.Engine,
	cfg *config.Config,
	log *slog.Logger,
) (*ScannerJob, error) {
	if cfg == nil || cfg.Reversion == nil {
		return nil, fmt.Errorf("scanner_job: config and reversion config cannot be nil")
	}
	if log == nil {
		return nil, fmt.Errorf("scanner_job: logger cannot be nil")
	}

	return &ScannerJob{
		scanners: scanners,
		engine:   engine,
		cfg:      cfg,
		log:      log.With("component", "scanner_job"),
		states:   make(map[string]*symbolState),
	}, nil
}

// Run starts the background scanner tick loop using the application context.
func (j *ScannerJob) Run(ctx context.Context) error {
	j.log.InfoContext(ctx, "🚀 Starting background scanner job loop")
	defer j.log.InfoContext(context.WithoutCancel(ctx), "🛑 Background scanner job loop stopped")

	// Execute an initial tick immediately on startup
	j.tick(ctx)

	c := cron.New(cron.WithLocation(time.Local))
	_, err := c.AddFunc("45 * * * *", func() {
		j.tick(ctx)
	})
	if err != nil {
		return fmt.Errorf("failed to schedule scanner cron job: %w", err)
	}

	c.Start()
	defer c.Stop()

	<-ctx.Done()
	return nil
}

func (j *ScannerJob) tick(ctx context.Context) {
	var wg sync.WaitGroup

	for _, sc := range j.scanners {
		wg.Add(1)
		go func(s Scanner) {
			defer wg.Done()

			opportunities, err := s.Scan(ctx)
			if err != nil {
				j.log.ErrorContext(ctx, "Scanner failed to scan", slog.Any("error", err))
				return
			}

			for i := range opportunities {
				opp := &opportunities[i]
				if j.shouldTrigger(opp.Candidate, opp.SettleTime) {
					j.trigger(opp.Candidate, opp.SettleTime)
				}
			}
		}(sc)
	}

	wg.Wait()
}

func (j *ScannerJob) getEngineProvider(exchangeName string) (*app.ExchangeProvider, error) {
	return j.engine.GetProvider(exchangeName)
}

func (j *ScannerJob) shouldTrigger(c domain.Candidate, settle time.Time) bool {
	j.statesMu.Lock()
	defer j.statesMu.Unlock()

	if c.SettleTime.IsZero() {
		c.SettleTime = settle
	}

	// Minimum funding rate check
	if math.Abs(c.FundingRate) < c.Config.MinFundingRate {
		j.log.Debug("Skipping trigger: funding rate below minimum",
			slog.String("exchange", c.Config.Exchange),
			slog.String("symbol", c.Symbol),
			slog.Float64("rate", c.FundingRate),
			slog.Float64("min", c.Config.MinFundingRate),
		)
		return false
	}

	// Safety limits check
	if !j.checkSafetyLimits(c) {
		return false
	}

	// Blacklist check
	if j.cfg != nil && j.cfg.Blacklist != nil {
		if j.cfg.Blacklist.IsBlacklisted(c.Config.Exchange, c.Symbol) {
			j.log.Debug("Skipping trigger: symbol is blacklisted",
				slog.String("symbol", c.Symbol),
				slog.String("exchange", c.Config.Exchange),
			)
			return false
		}
	}

	// Time window check: only trigger if we are within 15 minutes before the settlement time
	now := time.Now()
	if prov, err := j.getEngineProvider(c.Config.Exchange); err == nil {
		now = prov.TimeSync.Now()
	}
	if now.Add(15 * time.Minute).Before(settle) {
		j.log.Debug("Skipping trigger: too early for settlement",
			slog.String("exchange", c.Config.Exchange),
			slog.String("symbol", c.Symbol),
			slog.Time("now", now),
			slog.Time("settle", settle),
		)
		return false
	}
	if !now.Before(settle) {
		j.log.Debug("Skipping trigger: settlement time already passed",
			slog.String("exchange", c.Config.Exchange),
			slog.String("symbol", c.Symbol),
			slog.Time("now", now),
			slog.Time("settle", settle),
		)
		return false
	}

	key := c.Config.Exchange + ":" + c.Symbol
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

func (j *ScannerJob) trigger(candidate domain.Candidate, settle time.Time) {
	externalID := orders.ExternalOrderID(candidate.Symbol, settle, candidate.Config.Exchange)
	candidate.ExternalID = externalID

	j.log.Info("Opportunity found! Triggering reversion event flow",
		slog.String("symbol", candidate.Symbol),
		slog.String("exchange", candidate.Config.Exchange),
		slog.String("externalID", externalID),
		slog.Float64("fundingRate", candidate.FundingRate),
	)

	eventTimestamp := time.Now()
	if prov, err := j.getEngineProvider(candidate.Config.Exchange); err == nil {
		eventTimestamp = prov.TimeSync.Now()
	}

	startEvt := reversion.CandidateFoundEvent{
		BaseReversionEvent: reversion.BaseReversionEvent{
			Flow:         reversion.FlowReversion,
			ReqID:        orders.ExternalUniqueID(candidate.Symbol, settle, candidate.Config.Exchange) + strings.ToUpper(reversion.FlowReversion),
			Symbol:       candidate.Symbol,
			Exchange:     candidate.Config.Exchange,
			SendNotify:   false,
			Timestamp:    eventTimestamp,
			EventID:      watermill.NewUUID(),
			Seq:          1,
			Topic:        reversion.TopicReversionCandidate,
			ExternalID:   externalID,
			SettleTime:   settle,
			Side:         candidate.Side,
			FundingRate:  candidate.FundingRate,
			Vol24hUSDT:   candidate.Vol24USDT,
			ContractSize: candidate.ContractSize,
		},
		Candidate: candidate,
	}

	if err := j.engine.Bus.Publish(reversion.TopicReversionCandidate, startEvt); err != nil {
		j.log.Error("Failed to publish reversion candidate event",
			slog.String("exchange", candidate.Config.Exchange),
			slog.Any("error", err))
	} else {
		j.log.Info("Reversion candidate event successfully published",
			slog.String("exchange", candidate.Config.Exchange),
			slog.Time("settle", settle))
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
) (*ConfiguredScanner, error) {
	if cfg == nil || cfg.Reversion == nil {
		return nil, fmt.Errorf("configured_scanner: config and reversion config cannot be nil")
	}
	if log == nil {
		return nil, fmt.Errorf("configured_scanner: logger cannot be nil")
	}
	if disabledReason == nil {
		disabledReason = func(string) (string, bool) { return "", false }
	}

	return &ConfiguredScanner{
		cfg:            cfg,
		engine:         engine,
		stores:         stores,
		log:            log.With("component", "configured_scanner"),
		disabledReason: disabledReason,
	}, nil
}

func (s *ConfiguredScanner) getStoreSet(exchangeName string) (strategy.FundingStoreSet, bool) {
	set, ok := s.stores[exchangeName]
	return set, ok
}

// Scan loops over configured symbols and verifies them within the 60-second settlement window.
func (s *ConfiguredScanner) Scan(ctx context.Context) ([]ScanOpportunity, error) {
	var opportunities []ScanOpportunity

	for i := range s.cfg.Symbols {
		if storeSet, ok := s.resolveStoreSet(ctx, s.cfg.Symbols[i]); ok {
			if opp, ok := s.fetchCandidate(ctx, s.cfg.Symbols[i], storeSet); ok {
				opportunities = append(opportunities, opp)
			}
		}
	}

	return opportunities, nil
}

func (s *ConfiguredScanner) resolveStoreSet(ctx context.Context, symCfg config.SymbolConfig) (strategy.FundingStoreSet, bool) {
	if reason, disabled := s.disabledReason(symCfg.Symbol); disabled {
		s.log.DebugContext(ctx, "Skipping disabled symbol", slog.String("exchange", symCfg.Exchange), slog.String("symbol", symCfg.Symbol), slog.String("reason", reason))
		return nil, false
	}

	if s.cfg.Blacklist != nil && s.cfg.Blacklist.IsBlacklisted(symCfg.Exchange, symCfg.Symbol) {
		s.log.DebugContext(ctx, "Skipping blacklisted symbol", slog.String("symbol", symCfg.Symbol), slog.String("exchange", symCfg.Exchange))
		return nil, false
	}

	storeSet, ok := s.getStoreSet(symCfg.Exchange)
	if !ok {
		s.log.WarnContext(ctx, "Store set not found for exchange", slog.String("exchange", symCfg.Exchange))
		return nil, false
	}

	return storeSet, true
}

func (s *ConfiguredScanner) fetchCandidate(ctx context.Context, symCfg config.SymbolConfig, storeSet strategy.FundingStoreSet) (ScanOpportunity, bool) {
	settle, err := GetNextSettleTime(ctx, symCfg.SimulateSettle, symCfg.Symbol, storeSet.Funding())
	if err != nil {
		s.log.DebugContext(ctx, "Failed to get next settle time", slog.String("exchange", symCfg.Exchange), slog.String("symbol", symCfg.Symbol), slog.Any("error", err))
		return ScanOpportunity{}, false
	}

	if storeSet.Ticker() == nil {
		s.log.WarnContext(ctx, "Ticker store not ready", slog.String("exchange", symCfg.Exchange))
		return ScanOpportunity{}, false
	}

	td, err := storeSet.Ticker().GetTicker(ctx, symCfg.Symbol)
	if err != nil {
		s.log.DebugContext(ctx, "No ticker data available yet", slog.String("exchange", symCfg.Exchange), slog.String("symbol", symCfg.Symbol), slog.Any("error", err))
		return ScanOpportunity{}, false
	}

	fd, err := storeSet.Funding().GetFunding(ctx, symCfg.Symbol)
	if err != nil {
		s.log.DebugContext(ctx, "No funding data available yet", slog.String("exchange", symCfg.Exchange), slog.String("symbol", symCfg.Symbol), slog.Any("error", err))
		return ScanOpportunity{}, false
	}

	candidate := s.buildCandidate(symCfg, td, fd.FundingRate)

	if !matchTradeSide(s.cfg.Reversion.TradeSide, candidate.Side) {
		s.log.DebugContext(ctx, "Skipping candidate: side does not match tradeSide config",
			slog.String("exchange", candidate.Config.Exchange),
			slog.String("symbol", candidate.Symbol),
			slog.String("side", candidate.Side.String()),
			slog.String("configSide", s.cfg.Reversion.TradeSide),
		)
		return ScanOpportunity{}, false
	}

	candidate.SettleTime = settle
	if !s.enrich(ctx, storeSet.Contract(), &candidate) {
		s.log.DebugContext(ctx, "Contract enrichment failed for candidate", slog.String("exchange", symCfg.Exchange), slog.String("symbol", symCfg.Symbol))
		return ScanOpportunity{}, false
	}

	client := s.resolveClient(symCfg.Exchange)
	candidate.Config.Leverage = domain.DetermineCandidateLeverage(ctx, client, &candidate, s.log)
	candidate.Volume = candidate.CalculateVolume()

	return ScanOpportunity{
		Candidate:  candidate,
		SettleTime: settle,
	}, true
}

func (s *ConfiguredScanner) resolveClient(exchangeName string) exchange.Client {
	if s.engine == nil {
		return nil
	}
	prov, err := s.engine.GetProvider(exchangeName)
	if err != nil || prov == nil {
		return nil
	}
	return prov.Client
}

func (s *ConfiguredScanner) buildCandidate(sc config.SymbolConfig, td *store.TickerData, fundingRate float64) domain.Candidate {
	intent := domain.TradeIntent{
		Symbol:      td.Symbol,
		FundingRate: decmath.TakeDecimalPlaces(fundingRate, 3),
	}
	if fundingRate > 0 {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenLong, shared.SideCloseLong, refPriceBestAsk
	} else {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenShort, shared.SideCloseShort, refPriceBestBid
	}

	amtUSDT := td.AmountUSDT24
	if amtUSDT == 0 && td.Volume24 > 0 && td.LastPrice > 0 {
		amtUSDT = td.Volume24 * td.LastPrice
	}

	return domain.Candidate{
		Config:      ToTradeConfig(sc),
		TradeIntent: intent,
		MarketData: domain.MarketData{
			LastPrice: td.LastPrice,
			BestBid:   td.BestBid,
			BestAsk:   td.BestAsk,
			Volume24:  td.Volume24,
			Vol24USDT: amtUSDT,
		},
	}
}

func (s *ConfiguredScanner) enrich(ctx context.Context, contractStore store.ContractReader, c *domain.Candidate) bool {
	if contractStore == nil {
		return false
	}
	cd, err := contractStore.GetContract(ctx, c.Symbol)
	if err != nil {
		s.log.WarnContext(ctx, "🟡 No contract data — skip",
			slog.String("exchange", c.Config.Exchange),
			slog.String("symbol", c.Symbol))
		return false
	}
	c.ContractSpec = domain.ContractSpec{
		PriceUnit:    cd.PriceUnit,
		VolUnit:      cd.VolUnit,
		MinVol:       cd.MinVol,
		MaxVol:       cd.MaxVol,
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
		ParsedOpenType:      sc.ParsedOpenType,
		ParsedPositionMode:  sc.ParsedPositionMode,
		MinFundingRate:      sc.MinFundingRate,
		MinVol24USD:         sc.MinVol24USD,
	}
}

// TimeProvider abstracts provider time synchronization.
type TimeProvider interface {
	Now() time.Time
}

// ScheduleScanner scans for high-funding opportunities dynamically.
type ScheduleScanner struct {
	exchange       string
	cfg            *config.Config
	client         exchange.Client
	log            *slog.Logger
	disabledReason func(string) (string, bool)
	timeSync       TimeProvider
}

// SetTimeSync sets the time sync provider for the schedule scanner.
func (s *ScheduleScanner) SetTimeSync(ts TimeProvider) {
	s.timeSync = ts
}

func (s *ScheduleScanner) now() time.Time {
	if s.timeSync != nil {
		return s.timeSync.Now()
	}
	return time.Now()
}

// NewScheduleScanner creates a new ScheduleScanner.
func NewScheduleScanner(
	exchangeName string,
	cfg *config.Config,
	client exchange.Client,
	log *slog.Logger,
	disabledReason func(string) (string, bool),
) (*ScheduleScanner, error) {
	if strings.TrimSpace(exchangeName) == "" {
		return nil, fmt.Errorf("schedule_scanner: exchangeName cannot be empty")
	}
	if cfg == nil || cfg.Reversion == nil {
		return nil, fmt.Errorf("schedule_scanner: config and reversion config cannot be nil")
	}
	if client == nil {
		return nil, fmt.Errorf("schedule_scanner: exchange client cannot be nil")
	}
	if log == nil {
		return nil, fmt.Errorf("schedule_scanner: logger cannot be nil")
	}
	if disabledReason == nil {
		disabledReason = func(string) (string, bool) { return "", false }
	}

	return &ScheduleScanner{
		exchange:       exchangeName,
		cfg:            cfg,
		client:         client,
		log:            log.With("component", "schedule_scanner", "exchange", exchangeName),
		disabledReason: disabledReason,
	}, nil
}

func (s *ScheduleScanner) resolveExchangeReversionConfig(exchangeName string) (config.ExchangeReversionConfig, bool) {
	if s.cfg == nil || s.cfg.Reversion == nil || s.cfg.Reversion.Exchanges == nil {
		return config.ExchangeReversionConfig{}, false
	}
	cfg, ok := s.cfg.Reversion.Exchanges[exchangeName]
	return cfg, ok
}

// Scan queries tickers, filters by volume, fetches funding rates, and builds candidate opportunities.
func (s *ScheduleScanner) Scan(ctx context.Context) ([]ScanOpportunity, error) {
	var opportunities []ScanOpportunity

	// 1. Fetch blacklisted symbols to exclude them early
	var blacklist []string
	if s.cfg.Blacklist != nil {
		blacklist = append(blacklist, s.cfg.Blacklist.GetCommonBlacklist()...)
		blacklist = append(blacklist, s.cfg.Blacklist.GetExchangeBlacklist(s.exchange)...)
	}

	minVol := s.resolveMinVol24USD()

	// 2. Fetch potential funding symbols using native exchange client
	results, err := s.client.GetPotentialFundingSymbols(ctx, minVol, 0, nil, blacklist)
	if err != nil {
		return nil, fmt.Errorf("failed to get potential funding symbols: %w", err)
	}

	if len(results) == 0 {
		return nil, nil
	}

	// 3. Fetch tickers for enriched Candidate details
	tickers, err := s.client.GetTickers(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get tickers: %w", err)
	}
	tickerMap := make(map[string]exchange.Ticker)
	for _, t := range tickers {
		tickerMap[t.Symbol] = t
	}

	// 4. Fetch contract details for enrichment
	contractDetails, err := s.client.GetContractDetails(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get contract details: %w", err)
	}
	contracts := make(map[string]*exchange.ContractDetail)
	for i := range contractDetails {
		contracts[contractDetails[i].Symbol] = &contractDetails[i]
	}

	// 5. Build dynamic SymbolConfigs and build candidate opportunities
	for _, r := range results {
		opp, ok, err := s.processResult(ctx, r, tickerMap, contracts)
		if err != nil {
			return nil, err
		}
		if ok && s.isValidCandidate(&opp.Candidate) {
			opportunities = append(opportunities, opp)
		}
	}

	opportunities = s.selectBestOpportunities(ctx, opportunities)
	s.log.Info("Opportunies selected",
		slog.Int("results", len(results)),
		slog.Int("opportunities", len(opportunities)))

	return opportunities, nil
}

func (s *ScheduleScanner) resolveTotalMarginAndMaxCandidate() (float64, int, float64) {
	exchConfig := s.cfg.Reversion.Default
	if specific, exists := s.resolveExchangeReversionConfig(s.exchange); exists {
		config.MergeExchangeReversionConfig(&exchConfig, specific)
	}

	return exchConfig.MarginUSD, exchConfig.MaxCandidateTrade, exchConfig.MaxMarginUSDOfCandidate
}

func (s *ScheduleScanner) resolveMaxImpactRatio() float64 {
	return s.cfg.Reversion.Safety.MaxImpactRatio
}

func (s *ScheduleScanner) allocateCandidateMargins(
	ctx context.Context,
	selected []ScanOpportunity,
	totalMarginUSD float64,
	maxCandidateMargin float64,
) []ScanOpportunity {
	numToTrade := len(selected)
	if numToTrade == 0 {
		return nil
	}

	candidates := make([]domain.Candidate, numToTrade)
	for i := range selected {
		candidates[i] = selected[i].Candidate
	}

	maxImpactRatio := s.resolveMaxImpactRatio()

	allocator := domain.NewScoreMarginAllocator()
	allocated := allocator.AllocateMargins(
		ctx,
		candidates,
		totalMarginUSD,
		maxCandidateMargin,
		maxImpactRatio,
		s.client,
		s.log,
	)

	candMap := make(map[string]domain.Candidate, len(allocated))
	for i := range allocated {
		candMap[allocated[i].Symbol] = allocated[i]
	}

	var validOpportunities []ScanOpportunity
	for i := range selected {
		if updated, ok := candMap[selected[i].Candidate.Symbol]; ok {
			selected[i].Candidate = updated
			validOpportunities = append(validOpportunities, selected[i])
		}
	}
	return validOpportunities
}

func (s *ScheduleScanner) resolveScoringWeights() (float64, float64, float64) {
	exchConfig := s.cfg.Reversion.Default
	if specific, exists := s.resolveExchangeReversionConfig(s.exchange); exists {
		config.MergeExchangeReversionConfig(&exchConfig, specific)
	}
	return exchConfig.ScoringRateWeight, exchConfig.ScoringVolumeWeight, exchConfig.MaxVolumeScore
}

func (s *ScheduleScanner) selectBestOpportunities(ctx context.Context, opportunities []ScanOpportunity) []ScanOpportunity {
	if len(opportunities) == 0 {
		return nil
	}

	totalMarginUSD, maxCandidate, maxCandidateMargin := s.resolveTotalMarginAndMaxCandidate()
	rateWeight, volWeight, maxVolScore := s.resolveScoringWeights()

	// 2. Sort opportunities by opportunity score descending, tie-breaking by absolute funding rate descending
	sort.Slice(opportunities, func(i, j int) bool {
		scoreI := domain.CalculateOpportunityScore(
			opportunities[i].Candidate.FundingRate,
			opportunities[i].Candidate.Vol24USDT,
			rateWeight,
			volWeight,
			maxVolScore,
		)
		scoreJ := domain.CalculateOpportunityScore(
			opportunities[j].Candidate.FundingRate,
			opportunities[j].Candidate.Vol24USDT,
			rateWeight,
			volWeight,
			maxVolScore,
		)
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		rateI := math.Abs(opportunities[i].Candidate.FundingRate)
		rateJ := math.Abs(opportunities[j].Candidate.FundingRate)
		if rateI != rateJ {
			return rateI > rateJ
		}
		return opportunities[i].Candidate.Vol24USDT > opportunities[j].Candidate.Vol24USDT
	})

	// 3. Limit to maxCandidate config
	numToTrade := min(len(opportunities), maxCandidate)
	if numToTrade == 0 {
		return nil
	}

	selected := opportunities[:numToTrade]
	return s.allocateCandidateMargins(ctx, selected, totalMarginUSD, maxCandidateMargin)
}

func (s *ScheduleScanner) processResult(
	ctx context.Context,
	r exchange.PotentialFundingResult,
	tickerMap map[string]exchange.Ticker,
	contracts map[string]*exchange.ContractDetail,
) (ScanOpportunity, bool, error) {
	// Skip if disabled in-memory
	if reason, disabled := s.disabledReason(r.Symbol); disabled {
		s.log.DebugContext(ctx, "Skipping disabled symbol", slog.String("exchange", s.exchange), slog.String("symbol", r.Symbol), slog.String("reason", reason))
		return ScanOpportunity{}, false, nil
	}

	symCfg, err := s.cfg.NewSymbolConfig(s.exchange, r.Symbol)
	if err != nil {
		s.log.WarnContext(ctx, "Failed to resolve symbol config",
			slog.String("symbol", r.Symbol), slog.Any("error", err))
		return ScanOpportunity{}, false, nil
	}

	// Minimum funding rate check: read from configuration
	absRate := math.Abs(r.Rate)
	if absRate < symCfg.MinFundingRate {
		return ScanOpportunity{}, false, nil
	}

	td, ok := tickerMap[r.Symbol]
	if !ok {
		s.log.DebugContext(ctx, "No ticker data available for symbol", slog.String("exchange", s.exchange), slog.String("symbol", r.Symbol))
		return ScanOpportunity{}, false, nil
	}

	cd, ok := contracts[r.Symbol]
	if !ok {
		s.log.WarnContext(ctx, "No contract data available for symbol", slog.String("exchange", s.exchange), slog.String("symbol", r.Symbol))
		return ScanOpportunity{}, false, nil
	}

	candidate := s.buildCandidate(symCfg, td, r.Rate)

	if !matchTradeSide(s.cfg.Reversion.TradeSide, candidate.Side) {
		s.log.DebugContext(ctx, "Skipping candidate: side does not match tradeSide config",
			slog.String("symbol", candidate.Symbol),
			slog.String("side", candidate.Side.String()),
			slog.String("configSide", s.cfg.Reversion.TradeSide),
		)
		return ScanOpportunity{}, false, nil
	}

	settleTime := time.UnixMilli(r.SettleTime)
	candidate.SettleTime = settleTime
	candidate.ContractSpec = domain.ContractSpec{
		PriceUnit:    cd.PriceUnit,
		VolUnit:      cd.VolUnit,
		MinVol:       cd.MinVol,
		MaxVol:       cd.MaxVol,
		PriceScale:   cd.PriceScale,
		VolScale:     cd.VolScale,
		ContractSize: cd.ContractSize,
		TakerFeeRate: cd.TakerFeeRate,
		MakerFeeRate: cd.MakerFeeRate,
		MaxLeverage:  cd.MaxLeverage,
	}

	lev := domain.DetermineCandidateLeverage(ctx, s.client, &candidate, s.log)
	candidate.Config.Leverage = lev
	candidate.Volume = candidate.CalculateVolume()

	return ScanOpportunity{
		Candidate:  candidate,
		SettleTime: settleTime,
	}, true, nil
}

func (s *ScheduleScanner) buildCandidate(sc config.SymbolConfig, td exchange.Ticker, fundingRate float64) domain.Candidate {
	intent := domain.TradeIntent{
		Symbol:      td.Symbol,
		FundingRate: decmath.TakeDecimalPlaces(fundingRate, 3),
	}
	if fundingRate > 0 {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenLong, shared.SideCloseLong, refPriceBestAsk
	} else {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenShort, shared.SideCloseShort, refPriceBestBid
	}

	return domain.Candidate{
		Config:      ToTradeConfig(sc),
		TradeIntent: intent,
		MarketData: domain.MarketData{
			LastPrice: td.LastPrice,
			BestBid:   td.Bid1,
			BestAsk:   td.Ask1,
			Volume24:  td.Volume24,
			Vol24USDT: td.AmountUSDT24,
		},
	}
}

func (j *ScannerJob) checkSafetyLimits(c domain.Candidate) bool {
	maxPrice := 0.0
	if j.cfg != nil && j.cfg.Reversion != nil {
		maxPrice = j.cfg.Reversion.Safety.MaxSymbolUSDTPrice
	}
	now := time.Now()
	if prov, err := j.getEngineProvider(c.Config.Exchange); err == nil && prov.TimeSync != nil {
		now = prov.TimeSync.Now()
	}
	return checkCandidateSafetyLimits(c, maxPrice, now, j.log)
}

func (s *ScheduleScanner) resolveMinVol24USD() float64 {
	if s.cfg == nil || s.cfg.Reversion == nil {
		return 0
	}
	minVol := s.cfg.Reversion.Default.MinVol24USD
	if specific, exists := s.resolveExchangeReversionConfig(s.exchange); exists {
		if specific.MinVol24USD > 0 {
			minVol = specific.MinVol24USD
		}
	}
	return minVol
}

func (s *ScheduleScanner) isValidCandidate(c *domain.Candidate) bool {
	if c == nil {
		return false
	}
	minVol := s.resolveMinVol24USD()
	maxPrice := 0.0
	if s.cfg != nil && s.cfg.Reversion != nil {
		maxPrice = s.cfg.Reversion.Safety.MaxSymbolUSDTPrice
	}
	return checkCandidatePreAllocationSafetyLimits(*c, minVol, maxPrice, s.now(), s.log)
}

func checkCandidatePreAllocationSafetyLimits(c domain.Candidate, minVol, maxSymbolUSDTPrice float64, now time.Time, logger *slog.Logger) bool {
	if !validateCandidateSettleTime(c, now, logger) {
		return false
	}
	if !validateCandidate24hVolume(c, minVol, logger) {
		return false
	}
	return validateCandidateMaxSymbolPrice(c, maxSymbolUSDTPrice, logger)
}

func checkCandidateSafetyLimits(c domain.Candidate, maxSymbolUSDTPrice float64, now time.Time, logger *slog.Logger) bool {
	return checkCandidateSafetyLimitsWithMinVol(c, c.Config.MinVol24USD, maxSymbolUSDTPrice, now, logger)
}

func checkCandidateSafetyLimitsWithMinVol(c domain.Candidate, minVol, maxSymbolUSDTPrice float64, now time.Time, logger *slog.Logger) bool {
	if !checkCandidatePreAllocationSafetyLimits(c, minVol, maxSymbolUSDTPrice, now, logger) {
		return false
	}

	refPrice := c.ExecutionRefPrice()
	marginUSDT := c.Config.MarginUSDT
	leverage := float64(c.Config.Leverage)
	maxSpend := marginUSDT * leverage

	// Price over marginUSDT * leverage check
	if refPrice > 0 && maxSpend > 0 && refPrice > maxSpend {
		if logger != nil {
			logger.Debug("Skipping candidate: price exceeds marginUSDT * leverage limit",
				slog.String("exchange", c.Config.Exchange),
				slog.String("symbol", c.Symbol),
				slog.Float64("price", refPrice),
				slog.Float64("maxSpend", maxSpend),
			)
		}
		return false
	}

	// Minimum trade value check
	if refPrice > 0 && maxSpend > 0 {
		minTradeUSDT := float64(c.MinVol) * refPrice * c.ContractSize
		if maxSpend < minTradeUSDT {
			if logger != nil {
				logger.Debug("Skipping candidate: max spend below minimum symbol trade value",
					slog.String("exchange", c.Config.Exchange),
					slog.String("symbol", c.Symbol),
					slog.Float64("maxSpend", maxSpend),
					slog.Float64("minTradeUSDT", minTradeUSDT),
				)
			}
			return false
		}
	}

	return true
}

func validateCandidateSettleTime(c domain.Candidate, now time.Time, logger *slog.Logger) bool {
	if c.SettleTime.IsZero() {
		return true
	}
	if !now.Before(c.SettleTime) {
		if logger != nil {
			logger.Debug("Skipping candidate: settlement time already passed",
				slog.String("exchange", c.Config.Exchange),
				slog.String("symbol", c.Symbol),
				slog.Time("now", now),
				slog.Time("settle", c.SettleTime),
			)
		}
		return false
	}
	if now.Add(15 * time.Minute).Before(c.SettleTime) {
		if logger != nil {
			logger.Debug("Skipping candidate: too early for settlement (outside 15-minute window)",
				slog.String("exchange", c.Config.Exchange),
				slog.String("symbol", c.Symbol),
				slog.Time("now", now),
				slog.Time("settle", c.SettleTime),
			)
		}
		return false
	}
	return true
}

func validateCandidate24hVolume(c domain.Candidate, minVol float64, logger *slog.Logger) bool {
	if minVol > 0 && c.Vol24USDT < minVol {
		if logger != nil {
			logger.Debug("Skipping candidate: 24h volume below minimum safety limit",
				slog.String("exchange", c.Config.Exchange),
				slog.String("symbol", c.Symbol),
				slog.Float64("vol24h", c.Vol24USDT),
				slog.Float64("minVol", minVol),
			)
		}
		return false
	}
	return true
}

func validateCandidateMaxSymbolPrice(c domain.Candidate, maxSymbolUSDTPrice float64, logger *slog.Logger) bool {
	refPrice := c.ExecutionRefPrice()
	if refPrice > 0 && maxSymbolUSDTPrice > 0 && refPrice > maxSymbolUSDTPrice {
		if logger != nil {
			logger.Debug("Skipping candidate: price exceeds maxSymbolUSDTPrice safety limit",
				slog.String("exchange", c.Config.Exchange),
				slog.String("symbol", c.Symbol),
				slog.Float64("price", refPrice),
				slog.Float64("maxPrice", maxSymbolUSDTPrice),
			)
		}
		return false
	}
	return true
}

func matchTradeSide(cfgSide string, candidateSide shared.Side) bool {
	tradeSide := strings.ToLower(strings.TrimSpace(cfgSide))
	if tradeSide == "" || tradeSide == "both" {
		return true
	}
	cSide := strings.ToLower(candidateSide.String()) // "long" or "short"
	return cSide == tradeSide
}

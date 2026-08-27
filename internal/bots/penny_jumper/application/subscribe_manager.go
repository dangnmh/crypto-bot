package application

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store/orderbook"
)

const FlowIDPennyJumper = "penny_jumper"

// DepthSubscriber is the interface for WebSocket depth subscription management.
type DepthSubscriber interface {
	SubscribeDepth(ctx context.Context, flowID, symbol string) error
	UnsubscribeDepth(ctx context.Context, flowID, symbol string) error
}

// TradeSubscriber is the interface for WebSocket trade subscription management.
type TradeSubscriber interface {
	SubscribeTrade(ctx context.Context, flowID, symbol string) error
	UnsubscribeTrade(ctx context.Context, flowID, symbol string) error
}

// TopGainerFetcher is the interface for querying top gaining tickers.
type TopGainerFetcher interface {
	GetTopGainer(ctx context.Context, req exchange.TopGainerRequest) ([]exchange.TopGainerResult, error)
}

// ExchangeClient groups the necessary adapters and stores for a specific exchange.
type ExchangeClient struct {
	Exchange     string
	Fetcher      TopGainerFetcher
	Subscriber   DepthSubscriber
	Synchronizer orderbook.Synchronizer
}

// SubscribeManager manages dynamic symbol discovery (top 30 gainers) and depth subscriptions across multiple exchanges.
type SubscribeManager struct {
	cfg          pjdomain.PennyJumperConfig
	clients      []ExchangeClient
	blacklistMap map[string]bool
	logger       *slog.Logger

	mu              sync.RWMutex
	currentUniverse map[string]map[string]bool // exchange -> symbol -> bool
}

// NewSubscribeManager creates a SubscribeManager supporting configured exchange clients.
func NewSubscribeManager(
	cfg pjdomain.PennyJumperConfig,
	clients []ExchangeClient,
	blacklist []string,
	logger *slog.Logger,
) (*SubscribeManager, error) {
	if logger == nil {
		return nil, fmt.Errorf("logger is required")
	}
	for _, c := range clients {
		if c.Fetcher == nil {
			return nil, fmt.Errorf("fetcher is required for exchange: %s", c.Exchange)
		}
		if c.Subscriber == nil {
			return nil, fmt.Errorf("subscriber is required for exchange: %s", c.Exchange)
		}
		if c.Synchronizer == nil {
			return nil, fmt.Errorf("synchronizer is required for exchange: %s", c.Exchange)
		}
	}

	blMap := make(map[string]bool, len(blacklist))
	for _, b := range blacklist {
		blMap[b] = true
	}

	return &SubscribeManager{
		cfg:             cfg,
		clients:         clients,
		blacklistMap:    blMap,
		logger:          logger.With("component", "SubscribeManager"),
		currentUniverse: make(map[string]map[string]bool),
	}, nil
}

// RefreshUniverse fetches the top gainers across all configured exchanges, diffs universe, and adjusts subscriptions.
func (sm *SubscribeManager) RefreshUniverse(ctx context.Context) ([]string, error) {
	sm.logger.InfoContext(ctx, "🔍 Refreshing universe: querying top gainers across exchanges")

	var allQualified []string
	var allToAdd []string
	var allToRemove []string

	for _, client := range sm.clients {
		gainers, err := client.Fetcher.GetTopGainer(ctx, exchange.TopGainerRequest{})
		if err != nil {
			sm.logger.ErrorContext(ctx, "Failed to fetch top gainers for exchange", slog.String("exchange", client.Exchange), slog.Any("error", err))
			continue
		}

		qualified := sm.filterGainers(gainers)
		limit := sm.cfg.Universe.TopGainerLimit
		if limit > 0 && len(qualified) > limit {
			qualified = qualified[:limit]
		}

		toAdd, toRemove := sm.updateUniverseForExchange(client.Exchange, qualified)

		sm.subscribePairsForClient(ctx, client, toAdd)
		sm.unsubscribePairsForClient(ctx, client, toRemove)

		allQualified = append(allQualified, qualified...)
		allToAdd = append(allToAdd, toAdd...)
		allToRemove = append(allToRemove, toRemove...)
	}

	sm.logger.InfoContext(ctx, "✅ Universe refreshed",
		slog.Int("total_qualified", len(allQualified)),
		slog.Int("added", len(allToAdd)),
		slog.Int("removed", len(allToRemove)),
	)

	return allQualified, nil
}

func (sm *SubscribeManager) filterGainers(gainers []exchange.TopGainerResult) []string {
	minVol := sm.cfg.Universe.MinVolume24hUSDT
	maxPrice := sm.cfg.Universe.MaxCoinPrice
	var qualified []string

	for _, g := range gainers {
		sym := g.Symbol
		if sm.blacklistMap[sym] {
			continue
		}
		if minVol > 0 && g.Volume24hUSDT < minVol {
			continue
		}
		if maxPrice > 0 && (g.LastPrice > maxPrice || g.LastPrice <= 0) {
			continue
		}
		qualified = append(qualified, sym)
	}
	return qualified
}

func (sm *SubscribeManager) updateUniverseForExchange(exch string, qualified []string) (toAdd, toRemove []string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	exchUniverse, exists := sm.currentUniverse[exch]
	if !exists {
		exchUniverse = make(map[string]bool)
		sm.currentUniverse[exch] = exchUniverse
	}

	for _, sym := range qualified {
		if !exchUniverse[sym] {
			exchUniverse[sym] = true
			toAdd = append(toAdd, sym)
		}
	}

	for sym := range exchUniverse {
		isStillPresent := slices.Contains(qualified, sym)
		if !isStillPresent {
			delete(exchUniverse, sym)
			toRemove = append(toRemove, sym)
		}
	}

	return toAdd, toRemove
}

func (sm *SubscribeManager) subscribePairsForClient(ctx context.Context, client ExchangeClient, toAdd []string) {
	for _, sym := range toAdd {
		sm.logger.InfoContext(ctx, "➕ Subscribing to new top gainer depth and trades",
			slog.String("exchange", client.Exchange),
			slog.String("symbol", sym),
		)

		if err := client.Subscriber.SubscribeDepth(ctx, FlowIDPennyJumper, sym); err != nil {
			sm.logger.ErrorContext(ctx, "Failed to subscribe depth",
				slog.String("exchange", client.Exchange),
				slog.String("symbol", sym),
				slog.Any("error", err),
			)
		}
		if ts, ok := client.Subscriber.(TradeSubscriber); ok {
			if err := ts.SubscribeTrade(ctx, FlowIDPennyJumper, sym); err != nil {
				sm.logger.ErrorContext(ctx, "Failed to subscribe trade",
					slog.String("exchange", client.Exchange),
					slog.String("symbol", sym),
					slog.Any("error", err),
				)
			}
		}
	}
}

func (sm *SubscribeManager) unsubscribePairsForClient(ctx context.Context, client ExchangeClient, toRemove []string) {
	for _, sym := range toRemove {
		sm.logger.InfoContext(ctx, "➖ Unsubscribing removed symbol depth and trades",
			slog.String("exchange", client.Exchange),
			slog.String("symbol", sym),
		)
		client.Synchronizer.RemoveSymbol(sym)
		if err := client.Subscriber.UnsubscribeDepth(ctx, FlowIDPennyJumper, sym); err != nil {
			sm.logger.ErrorContext(ctx, "Failed to unsubscribe depth",
				slog.String("exchange", client.Exchange),
				slog.String("symbol", sym),
				slog.Any("error", err),
			)
		}
		if ts, ok := client.Subscriber.(TradeSubscriber); ok {
			if err := ts.UnsubscribeTrade(ctx, FlowIDPennyJumper, sym); err != nil {
				sm.logger.ErrorContext(ctx, "Failed to unsubscribe trade",
					slog.String("exchange", client.Exchange),
					slog.String("symbol", sym),
					slog.Any("error", err),
				)
			}
		}
	}
}

// UnsubscribeAll unregisters all active symbol depth streams across all exchanges upon shutdown.
func (sm *SubscribeManager) UnsubscribeAll(ctx context.Context) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, client := range sm.clients {
		exchUniverse, exists := sm.currentUniverse[client.Exchange]
		if !exists {
			continue
		}
		for sym := range exchUniverse {
			sm.logger.InfoContext(ctx, "➖ Unsubscribing symbol depth on shutdown",
				slog.String("exchange", client.Exchange),
				slog.String("symbol", sym),
			)
			client.Synchronizer.RemoveSymbol(sym)
			_ = client.Subscriber.UnsubscribeDepth(ctx, FlowIDPennyJumper, sym)
			if ts, ok := client.Subscriber.(TradeSubscriber); ok {
				_ = ts.UnsubscribeTrade(ctx, FlowIDPennyJumper, sym)
			}
		}
		delete(sm.currentUniverse, client.Exchange)
	}
}

// CurrentUniverse returns currently subscribed symbols across all exchanges.
func (sm *SubscribeManager) CurrentUniverse() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var out []string
	seen := make(map[string]bool)
	for _, exchMap := range sm.currentUniverse {
		for s := range exchMap {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

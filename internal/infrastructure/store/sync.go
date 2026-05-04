package store

import (
	"context"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/ticker"
)

// StartTickerSync periodically fetches all tickers and updates the store.
func (s *GlobalStore) StartTickerSync(ctx context.Context, client *exchange.Client, interval time.Duration) {
	s.logger.Debug("🔄 Starting ticker sync", "interval", interval)

	defer s.logger.Debug("🔄 Ticker sync stopped")
	ticker.RunImmediate(ctx, interval, func() bool {
		s.syncTickers(ctx, client)
		return true
	})
}

// StartContractSync periodically fetches contract details and updates the store.
func (s *GlobalStore) StartContractSync(ctx context.Context, client *exchange.Client, interval time.Duration) {
	s.logger.Debug("🔄 Starting contract sync", "interval", interval)

	defer s.logger.Debug("🔄 Contract sync stopped")
	ticker.RunImmediate(ctx, interval, func() bool {
		s.syncContracts(ctx, client)
		return true
	})
}

// StartFundingSync periodically fetches per-symbol funding rates and updates the store.
func (s *GlobalStore) StartFundingSync(ctx context.Context, client *exchange.Client, symbols []string, interval time.Duration) {
	s.logger.Debug("🔄 Starting funding sync", "interval", interval, "symbols", len(symbols))

	defer s.logger.Debug("🔄 Funding sync stopped")
	ticker.RunImmediate(ctx, interval, func() bool {
		s.syncFunding(ctx, client, symbols)
		return true
	})
}

// syncTickers fetches all tickers and updates the store.
func (s *GlobalStore) syncTickers(ctx context.Context, client *exchange.Client) {
	tickers, err := client.GetTickers(ctx, "")
	if err != nil {
		s.logger.Error("🔴 Ticker sync failed", "error", err)
		return
	}

	s.mu.Lock()
	for i := range tickers {
		s.tickers[tickers[i].Symbol] = TickerDataFromExchange(&tickers[i])
	}
	s.mu.Unlock()

	s.logger.Info("store.SyncTickers", "count", len(tickers))
	s.markTickerReady()
}

// syncContracts fetches all contract details and updates the store.
func (s *GlobalStore) syncContracts(ctx context.Context, client *exchange.Client) {
	details, err := client.GetContractDetails(ctx)
	if err != nil {
		s.logger.Error("🔴 Contract sync failed", "error", err)
		return
	}

	s.mu.Lock()
	for i := range details {
		s.contracts[details[i].Symbol] = ContractDataFromExchange(&details[i])
	}
	s.mu.Unlock()

	s.logger.Info("store.SyncContracts", "count", len(details))
	s.markContractReady()
}

// syncFunding fetches funding rate details for each configured symbol.
func (s *GlobalStore) syncFunding(ctx context.Context, client *exchange.Client, symbols []string) {
	for _, sym := range symbols {
		detail, err := client.GetFundingRate(ctx, sym)
		if err != nil {
			s.logger.Warn("🟡 Funding sync failed for symbol", "error", err, "symbol", sym)
			continue
		}

		fd := FundingDataFromExchange(detail)
		s.mu.Lock()
		s.funding[sym] = fd
		s.mu.Unlock()
	}

	s.logger.Debug("store.SyncFunding.done", "count", len(symbols))
	s.markFundingReady()
}

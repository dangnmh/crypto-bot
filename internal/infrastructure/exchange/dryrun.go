package exchange

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"crypto-bot/internal/domain"
)

// DryRunClient wraps a real Client and intercepts all write operations
// (order placement, cancellation, position management) with logged no-ops.
// Read operations (market data, account info) are delegated to the real client.
//
// This enables full pipeline execution without placing real orders.
type DryRunClient struct {
	inner    Client
	log      *slog.Logger
	orderSeq atomic.Int64
}

// NewDryRunClient wraps the given client with dry-run behavior.
func NewDryRunClient(inner Client) *DryRunClient {
	return &DryRunClient{
		inner: inner,
		log:   slog.Default().With("component", "dryrun"),
	}
}

// ── MarketDataProvider (delegated to real client) ────────────────────.

func (d *DryRunClient) GetTickers(ctx context.Context, symbol string) ([]Ticker, error) {
	return d.inner.GetTickers(ctx, symbol)
}

func (d *DryRunClient) GetContractDetails(ctx context.Context) ([]ContractDetail, error) {
	return d.inner.GetContractDetails(ctx)
}

func (d *DryRunClient) GetFundingRate(ctx context.Context, symbol string) (*FundingRateDetail, error) {
	return d.inner.GetFundingRate(ctx, symbol)
}

func (d *DryRunClient) GetServerTime(ctx context.Context) (int64, error) {
	return d.inner.GetServerTime(ctx)
}

func (d *DryRunClient) GetKlines(ctx context.Context, symbol, interval string, start, end int64) ([]Kline, error) {
	return d.inner.GetKlines(ctx, symbol, interval, start, end)
}

func (d *DryRunClient) GetDepthSnapshot(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return d.inner.GetDepthSnapshot(ctx, symbol, limit)
}

func (d *DryRunClient) GetDepthCommits(ctx context.Context, symbol string, limit int) ([]DepthCommit, error) {
	return d.inner.GetDepthCommits(ctx, symbol, limit)
}

// ── AccountProvider (delegated to real client) ───────────────────────.

func (d *DryRunClient) GetAssets(ctx context.Context) ([]AssetInfo, error) {
	return d.inner.GetAssets(ctx)
}

func (d *DryRunClient) GetAssetByCurrency(ctx context.Context, currency string) (*AssetInfo, error) {
	return d.inner.GetAssetByCurrency(ctx, currency)
}

func (d *DryRunClient) GetOpenPositions(ctx context.Context, symbol string) ([]Position, error) {
	return d.inner.GetOpenPositions(ctx, symbol)
}

// ── OrderExecutor (intercepted — no real orders) ─────────────────────.

func (d *DryRunClient) CreateOrder(_ context.Context, req SubmitOrderRequest) (string, error) {
	seq := d.orderSeq.Add(1)
	fakeID := fmt.Sprintf("dry_%d_%d", time.Now().UnixMilli(), seq)

	d.log.Warn("🧪 DRY-RUN CreateOrder",
		"orderID", fakeID,
		"symbol", req.Symbol,
		"side", req.Side,
		"type", req.Type,
		"price", req.Price,
		"vol", req.Vol,
		"leverage", req.Leverage,
		"extOID", req.ExternalOID,
	)

	return fakeID, nil
}

func (d *DryRunClient) CreateTrackOrder(_ context.Context, req SubmitTrackOrderRequest) (string, error) {
	seq := d.orderSeq.Add(1)
	fakeID := fmt.Sprintf("dry_trk_%d_%d", time.Now().UnixMilli(), seq)

	d.log.Warn("🧪 DRY-RUN CreateTrackOrder",
		"orderID", fakeID,
		"symbol", req.Symbol,
		"side", req.Side,
		"activePrice", req.ActivePrice,
	)

	return fakeID, nil
}

func (d *DryRunClient) CancelOrder(_ context.Context, symbol, orderID string) error {
	d.log.Warn("🧪 DRY-RUN CancelOrder", "symbol", symbol, "orderID", orderID)
	return nil
}

func (d *DryRunClient) CancelOrders(_ context.Context, orderIDs []string) error {
	d.log.Warn("🧪 DRY-RUN CancelOrders", "count", len(orderIDs))
	return nil
}

func (d *DryRunClient) CancelAllOpenOrders(_ context.Context, symbol string) error {
	d.log.Warn("🧪 DRY-RUN CancelAllOpenOrders", "symbol", symbol)
	return nil
}

func (d *DryRunClient) GetOrder(ctx context.Context, orderID string) (*OrderInfo, error) {
	return d.inner.GetOrder(ctx, orderID)
}

func (d *DryRunClient) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	return d.inner.GetOpenOrders(ctx, symbol)
}

func (d *DryRunClient) CloseAllPositions(_ context.Context, symbol string) error {
	d.log.Warn("🧪 DRY-RUN CloseAllPositions", "symbol", symbol)
	return nil
}

func (d *DryRunClient) ClosePosition(_ context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error {
	d.log.Warn("🧪 DRY-RUN ClosePosition",
		"symbol", symbol,
		"side", closeSide,
		"vol", volume,
		"positionMode", positionMode,
	)
	return nil
}

func (d *DryRunClient) ChangeLeverage(_ context.Context, req ChangeLeverageRequest) error {
	d.log.Warn("🧪 DRY-RUN ChangeLeverage", "symbol", req.Symbol, "leverage", req.Leverage)
	return nil
}

// ── Infrastructure (delegated) ───────────────────────────────────────.

func (d *DryRunClient) WarmUp(ctx context.Context, interval time.Duration) {
	d.inner.WarmUp(ctx, interval)
}

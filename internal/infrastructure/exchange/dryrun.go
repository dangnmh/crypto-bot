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

func (d *DryRunClient) GetFundingRates(ctx context.Context, symbols []string) ([]FundingRateResult, error) {
	return d.inner.GetFundingRates(ctx, symbols)
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

// GetRecentClosedPnL delegates to the inner client if it implements ClosedPnLProvider.
func (d *DryRunClient) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*ClosedPnLInfo, error) {
	if provider, ok := d.inner.(ClosedPnLProvider); ok {
		return provider.GetRecentClosedPnL(ctx, symbol, extOrderID, startTime)
	}
	return nil, ErrNotSupported
}

// ── OrderExecutor (intercepted — no real orders) ─────────────────────.

func (d *DryRunClient) CreateOrder(ctx context.Context, req SubmitOrderRequest) (CreateOrderResult, error) {
	seq := d.orderSeq.Add(1)
	fakeID := fmt.Sprintf("dry_%d_%d", time.Now().UnixMilli(), seq)

	d.log.WarnContext(ctx, "🧪 DRY-RUN CreateOrder",
		slog.String("orderID", fakeID),
		slog.String("symbol", req.Symbol),
		slog.Int("side", int(req.Side)),
		slog.Int("type", int(req.Type)),
		slog.Float64("price", req.Price),
		slog.Float64("vol", req.Vol),
		slog.Int("leverage", req.Leverage),
		slog.String("extOID", req.ExternalOID),
	)

	return CreateOrderResult{
		OrderID:       fakeID,
		TPSLSubmitted: false,
	}, nil
}

func (d *DryRunClient) CreateTrackOrder(ctx context.Context, req SubmitTrackOrderRequest) (string, error) {
	seq := d.orderSeq.Add(1)
	fakeID := fmt.Sprintf("dry_trk_%d_%d", time.Now().UnixMilli(), seq)

	d.log.WarnContext(ctx, "🧪 DRY-RUN CreateTrackOrder",
		slog.String("orderID", fakeID),
		slog.String("symbol", req.Symbol),
		slog.Int("side", int(req.Side)),
		slog.Float64("activePrice", req.ActivePrice),
	)

	return fakeID, nil
}

func (d *DryRunClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN CancelOrder", slog.String("symbol", symbol), slog.String("orderID", orderID))
	return nil
}

func (d *DryRunClient) CancelOrders(ctx context.Context, orderIDs []string) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN CancelOrders", slog.Int("count", len(orderIDs)))
	return nil
}

func (d *DryRunClient) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN CancelAllOpenOrders", slog.String("symbol", symbol))
	return nil
}

func (d *DryRunClient) GetOrder(ctx context.Context, symbol, orderID string) (*OrderInfo, error) {
	return d.inner.GetOrder(ctx, symbol, orderID)
}

func (d *DryRunClient) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*OrderInfo, error) {
	return d.inner.GetOrderByExternalID(ctx, symbol, externalOrderID)
}

func (d *DryRunClient) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	return d.inner.GetOpenOrders(ctx, symbol)
}

func (d *DryRunClient) CloseAllPositions(ctx context.Context, symbol string) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN CloseAllPositions", slog.String("symbol", symbol))
	return nil
}

func (d *DryRunClient) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN ClosePosition",
		slog.String("symbol", symbol),
		slog.String("side", closeSide.String()),
		slog.Float64("vol", volume),
		slog.Int("positionMode", int(positionMode)),
	)
	return nil
}

func (d *DryRunClient) ChangeLeverage(ctx context.Context, req ChangeLeverageRequest) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN ChangeLeverage", slog.String("symbol", req.Symbol), slog.Int("leverage", req.Leverage))
	return nil
}

func (d *DryRunClient) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN SwitchMarginMode", slog.String("symbol", symbol), slog.String("marginMode", marginMode), slog.Int("leverage", leverage), slog.String("side", side.String()))
	return nil
}

func (d *DryRunClient) SupportLeverageOnOrder() bool {
	return d.inner.SupportLeverageOnOrder()
}

// ── Infrastructure (delegated) ───────────────────────────────────────.

func (d *DryRunClient) WarmUp(ctx context.Context, interval time.Duration) {
	d.inner.WarmUp(ctx, interval)
}

// SetClock forwards the custom clock to the inner client if it supports ClockSetter.
func (d *DryRunClient) SetClock(clk Clock) {
	type clockSetter interface {
		SetClock(Clock)
	}
	if setter, ok := d.inner.(clockSetter); ok {
		setter.SetClock(clk)
	}
}

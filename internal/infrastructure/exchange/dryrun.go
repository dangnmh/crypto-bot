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

func (d *DryRunClient) GetPotentialFundingSymbols(
	ctx context.Context,
	minVol24h, maxVol24h float64,
	whitelist []string,
	blacklist []string,
) ([]PotentialFundingResult, error) {
	return d.inner.GetPotentialFundingSymbols(ctx, minVol24h, maxVol24h, whitelist, blacklist)
}

// ── AccountProvider (delegated to real client) ───────────────────────.

func (d *DryRunClient) GetOpenPositions(ctx context.Context, symbol string) ([]Position, error) {
	return d.inner.GetOpenPositions(ctx, symbol)
}

// GetOrderPNL delegates to the inner client if it implements ClosedPnLProvider.
func (d *DryRunClient) GetOrderPNL(ctx context.Context, symbol, orderID string) (*ClosedPnLInfo, error) {
	if provider, ok := d.inner.(ClosedPnLProvider); ok {
		return provider.GetOrderPNL(ctx, symbol, orderID)
	}
	return nil, ErrNotSupported
}

// RawRequest delegates to the inner client if it implements RawRequester.
func (d *DryRunClient) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	if provider, ok := d.inner.(RawRequester); ok {
		return provider.RawRequest(ctx, method, path, query, body)
	}
	return nil, ErrNotSupported
}

func (d *DryRunClient) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	if r, ok := d.inner.(RawRequest); ok {
		return r.GetFundingRateRaw(ctx, params)
	}
	return nil, ErrNotSupported
}

func (d *DryRunClient) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	if r, ok := d.inner.(RawRequest); ok {
		return r.GetTickersRaw(ctx, params)
	}
	return nil, ErrNotSupported
}

func (d *DryRunClient) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	if r, ok := d.inner.(RawRequest); ok {
		return r.GetOpenPositionsRaw(ctx, params)
	}
	return nil, ErrNotSupported
}

func (d *DryRunClient) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	if r, ok := d.inner.(RawRequest); ok {
		return r.GetHistoryPositionsRaw(ctx, params)
	}
	return nil, ErrNotSupported
}

func (d *DryRunClient) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	if r, ok := d.inner.(RawRequest); ok {
		return r.GetOrderDetailRaw(ctx, orderID, params)
	}
	return nil, ErrNotSupported
}

func (d *DryRunClient) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	if r, ok := d.inner.(RawRequest); ok {
		return r.GetHistoryOrdersRaw(ctx, params)
	}
	return nil, ErrNotSupported
}

func (d *DryRunClient) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	if r, ok := d.inner.(RawRequest); ok {
		return r.GetOrderPNLRaw(ctx, params)
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

func (d *DryRunClient) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN ClosePosition",
		slog.String("symbol", symbol),
		slog.String("side", closeSide.String()),
		slog.Float64("vol", volume),
		slog.Int("positionMode", int(positionMode)),
		slog.Int("leverage", leverage),
	)
	return nil
}

func (d *DryRunClient) ChangeLeverage(ctx context.Context, req ChangeLeverageRequest) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN ChangeLeverage", slog.String("symbol", req.Symbol), slog.Int("leverage", req.Leverage))
	return nil
}

func (d *DryRunClient) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	d.log.WarnContext(ctx, "🧪 DRY-RUN SwitchMarginMode", slog.String("symbol", symbol), slog.String("marginMode", string(marginMode)), slog.Int("leverage", leverage), slog.String("side", side.String()))
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

// FetchKlines delegates historical candlestick querying to the inner client if supported.
func (d *DryRunClient) FetchKlines(ctx context.Context, symbol string, interval Interval, start, end time.Time) ([]Kline, error) {
	if kp, ok := d.inner.(KlineProvider); ok {
		return kp.FetchKlines(ctx, symbol, interval, start, end)
	}
	return nil, fmt.Errorf("inner exchange client does not implement KlineProvider")
}

package orangex

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"
)

type WsAdapter struct {
	client        *Client
	pool          *pkgws.Pool
	authenticated chan struct{}
	authMu        sync.RWMutex
}

func NewWsAdapter(client *Client) *WsAdapter {
	a := &WsAdapter{
		client:        client,
		authenticated: make(chan struct{}),
	}
	close(a.authenticated) // Default to unblocked for tests/public conn
	return a
}

func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

func (a *WsAdapter) SubscribePublic(ctx context.Context, topic string, msg any) error {
	if a.pool == nil {
		return nil
	}
	return a.pool.SubscribePublic(ctx, topic, msg)
}

func (a *WsAdapter) UnsubscribePublic(ctx context.Context, topic string, msg any) error {
	if a.pool == nil {
		return nil
	}
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return pingMsg, 5 * time.Second
}

func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey == "" || apiSecret == "" {
		return nil
	}
	return func(c *pkgws.Client) {
		a.authMu.Lock()
		a.authenticated = make(chan struct{})
		a.authMu.Unlock()

		req := wsRequest{
			JsonRpc: rpcVersion,
			ID:      1000, // Fixed request ID for WS auth
			Method:  "/public/auth",
			Params: map[string]any{
				"grant_type":      grantClientCredentials,
				"client_id":       apiKey,
				paramClientSecret: apiSecret,
			},
		}
		if err := c.SendJSON(req); err != nil {
			slog.Default().Error("Failed to send OrangeX WS auth request", slog.Any("error", err))
		}
	}
}

func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(msg []byte) string {
		var wrapper struct {
			ID     xjson.Number `json:"id"`
			Method string       `json:"method"`
			Params struct {
				Channel string `json:"channel"`
			} `json:"params"`
			Error *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := xjson.Unmarshal(msg, &wrapper); err != nil {
			return ""
		}
		if wrapper.ID.String() == "1000" {
			if wrapper.Error == nil {
				a.authMu.Lock()
				select {
				case <-a.authenticated:
				default:
					close(a.authenticated)
				}
				a.authMu.Unlock()
			}
			return "auth"
		}
		if wrapper.Method == methodSubscription {
			ch := wrapper.Params.Channel
			if strings.HasPrefix(ch, "ticker.") {
				return "ticker"
			}
			if ch == userChangesChannel {
				return "personal.position"
			}
			return ch
		}
		return ""
	}
}

type wsRequest struct {
	JsonRpc string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	req := wsRequest{
		JsonRpc: rpcVersion,
		ID:      nextRequestID(),
		Method:  "/public/subscribe",
		Params: map[string]any{
			paramChannels: []string{fmt.Sprintf("ticker.%s.raw", symbol)},
		},
	}
	return a.pool.SubscribePublic(ctx, symbol, req)
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	req := wsRequest{
		JsonRpc: rpcVersion,
		ID:      nextRequestID(),
		Method:  "/public/unsubscribe",
		Params: map[string]any{
			paramChannels: []string{fmt.Sprintf("ticker.%s.raw", symbol)},
		},
	}
	return a.pool.UnsubscribePublic(ctx, symbol, req)
}

func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	a.authMu.RLock()
	authCh := a.authenticated
	a.authMu.RUnlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-authCh:
	}

	req := wsRequest{
		JsonRpc: rpcVersion,
		ID:      nextRequestID(),
		Method:  "/private/subscribe",
		Params: map[string]any{
			paramChannels: []string{userChangesChannel},
		},
	}
	return a.pool.SendPrivate(ctx, req)
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

type wsNotification[T any] struct {
	Method string `json:"method"`
	Params struct {
		Channel string `json:"channel"`
		Data    T      `json:"data"`
	} `json:"params"`
}

type wsTickerData struct {
	InstrumentName string       `json:"instrument_name"`
	BestBidPrice   xjson.Number `json:"best_bid_price"`
	BestAskPrice   xjson.Number `json:"best_ask_price"`
	LastPrice      xjson.Number `json:"last_price"`
	Stats          struct {
		Volume xjson.Number `json:"volume"`
	} `json:"stats"`
}

func (a *WsAdapter) ParseTicker(msg []byte) (string, *store.PriceData, error) {
	var payload wsNotification[wsTickerData]
	if err := xjson.Unmarshal(msg, &payload); err != nil {
		return "", nil, err
	}
	if payload.Method != methodSubscription {
		return "", nil, fmt.Errorf("unexpected method: %s", payload.Method)
	}

	d := payload.Params.Data
	return d.InstrumentName, &store.PriceData{
		BestBid:   xjson.ToFloat64(d.BestBidPrice),
		BestAsk:   xjson.ToFloat64(d.BestAskPrice),
		LastPrice: xjson.ToFloat64(d.LastPrice),
		Volume24:  xjson.ToFloat64(d.Stats.Volume),
	}, nil
}

type wsPositionData struct {
	InstrumentName string       `json:"instrument_name"`
	Side           string       `json:"side"`
	Size           xjson.Number `json:"size"`
	EntryPrice     xjson.Number `json:"entry_price"`
}

type wsChangesData struct {
	Positions []wsPositionData `json:"positions"`
}

func (a *WsAdapter) ParsePosition(msg []byte) (*exchange.PersonalPositionUpdate, error) {
	var payload wsNotification[wsChangesData]
	if err := xjson.Unmarshal(msg, &payload); err != nil {
		return nil, err
	}
	if payload.Method != methodSubscription || len(payload.Params.Data.Positions) == 0 {
		return nil, nil
	}
	p := payload.Params.Data.Positions[0]
	pType := exchange.PositionTypeLong
	if p.Side == dirSell || p.Side == "short" {
		pType = exchange.PositionTypeShort
	}
	holdVol := xjson.ToFloat64(p.Size)
	return &exchange.PersonalPositionUpdate{
		Symbol:          p.InstrumentName,
		HoldVolContract: holdVol,
		OpenAvgPrice:    xjson.ToFloat64(p.EntryPrice),
		HoldAvgPrice:    xjson.ToFloat64(p.EntryPrice),
		PositionType:    pType,
	}, nil
}

func (a *WsAdapter) ParseOrder(msg []byte) (*exchange.WsOrderDeal, error) {
	return nil, nil // No-op placeholder
}

// ParseDepth parses depth messages into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	return "", nil, nil
}

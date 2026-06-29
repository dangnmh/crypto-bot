package orangex

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"
)

type WsAdapter struct {
	client *Client
	pool   *pkgws.Pool
}

func NewWsAdapter(client *Client) *WsAdapter {
	return &WsAdapter{client: client}
}

func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return pingMsg, 5 * time.Second
}

func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(msg []byte) string {
		var wrapper struct {
			Method string `json:"method"`
			Params struct {
				Channel string `json:"channel"`
			} `json:"params"`
		}
		if err := xjson.Unmarshal(msg, &wrapper); err != nil {
			return ""
		}
		if wrapper.Method == methodSubscription {
			return wrapper.Params.Channel
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
		ID:      time.Now().UnixNano(),
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
		ID:      time.Now().UnixNano(),
		Method:  "/public/unsubscribe",
		Params: map[string]any{
			paramChannels: []string{fmt.Sprintf("ticker.%s.raw", symbol)},
		},
	}
	return a.pool.UnsubscribePublic(ctx, symbol, req)
}

func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	token, err := a.client.GetAccessToken(ctx)
	if err != nil {
		return err
	}
	req := wsRequest{
		JsonRpc: rpcVersion,
		ID:      time.Now().UnixNano(),
		Method:  "/private/subscribe",
		Params: map[string]any{
			paramAccessToken: token,
			paramChannels:    []string{"user.changes.perpetual.PERPETUAL.raw"},
		},
	}
	return a.pool.SendPrivate(ctx, req)
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
	if holdVol == 0 {
		pType = exchange.PositionTypeUnknown
	}
	return &exchange.PersonalPositionUpdate{
		Symbol:       p.InstrumentName,
		HoldVol:      holdVol,
		OpenAvgPrice: xjson.ToFloat64(p.EntryPrice),
		HoldAvgPrice: xjson.ToFloat64(p.EntryPrice),
		PositionType: pType,
	}, nil
}

func (a *WsAdapter) ParseOrder(msg []byte) (*exchange.WsOrderDeal, error) {
	return nil, nil // No-op placeholder
}

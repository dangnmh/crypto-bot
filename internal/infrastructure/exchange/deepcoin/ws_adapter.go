package deepcoin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"crypto-bot/pkg/xjson"
)

type WsAdapter struct {
	pool         *pkgws.Pool
	client       *Client
	privateURL   string
	cancelKeep   context.CancelFunc
	cancelKeepMu sync.Mutex
}

func NewWsAdapter(privateURL string) *WsAdapter {
	return &WsAdapter{
		privateURL: privateURL,
	}
}

func (a *WsAdapter) SetClient(client *Client) {
	a.client = client
}

func (a *WsAdapter) GetPrivateURLFunc(ctx context.Context) func() (string, error) {
	return func() (string, error) {
		if a.client == nil {
			return "", fmt.Errorf("client not injected in WsAdapter")
		}
		lk, err := a.client.CreateListenKey(ctx)
		if err != nil {
			return "", err
		}

		a.cancelKeepMu.Lock()
		if a.cancelKeep != nil {
			a.cancelKeep()
		}
		keepCtx, cancel := context.WithCancel(ctx)
		a.cancelKeep = cancel
		a.cancelKeepMu.Unlock()

		go a.keepAliveLoop(keepCtx, lk)

		base := a.privateURL
		if base == "" {
			base = "wss://stream.deepcoin.com/v1/private"
		}
		base = strings.TrimSuffix(base, "/")
		return base + "?listenKey=" + lk, nil
	}
}

func (a *WsAdapter) keepAliveLoop(ctx context.Context, listenKey string) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = a.client.KeepAliveListenKey(ctx, listenKey)
		}
	}
}

func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		wsAction:   "1",
		wsSymbol:   normalizeSymbol(symbol),
		wsTopic:    wsTopicMarket,
		wsLocalNo:  6,
		wsResumeNo: -1,
	}
	return a.pool.SubscribePublic(ctx, symbol+":ticker", msg)
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		wsAction:   "2",
		wsSymbol:   normalizeSymbol(symbol),
		wsTopic:    wsTopicMarket,
		wsLocalNo:  6,
		wsResumeNo: -1,
	}
	return a.pool.UnsubscribePublic(ctx, symbol+":ticker", msg)
}

func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	msg := map[string]any{
		"action": "subscribe",
		"tables": []string{wsTablePosition},
	}
	return a.pool.SendPrivate(ctx, msg)
}

func (a *WsAdapter) GetPingConfig() (payload any, interval time.Duration) {
	return "ping", 15 * time.Second
}

func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		trimmed := strings.TrimSpace(string(data))
		if strings.EqualFold(trimmed, "pong") {
			return "pong"
		}
		var topicCheck struct {
			Topic  string `json:"Topic"`
			A      string `json:"a"`
			Action string `json:"action"`
			Result []struct {
				Table string `json:"table"`
			} `json:"result"`
		}
		if err := xjson.Unmarshal(data, &topicCheck); err == nil {
			if topicCheck.Topic == wsTopicMarket || topicCheck.A == "PO" {
				return "ticker"
			}
			if len(topicCheck.Result) > 0 && strings.EqualFold(topicCheck.Result[0].Table, wsTablePosition) {
				return "personal.position"
			}
		}
		return ""
	}
}

func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	var msg struct {
		Topic string `json:"Topic"`
		Data  struct {
			Symbol    string       `json:"Symbol"`
			LastPrice xjson.Number `json:"LastPrice"`
			BidPrice  xjson.Number `json:"BidPrice"`
			AskPrice  xjson.Number `json:"AskPrice"`
			Volume24  xjson.Number `json:"Volume24"`
		} `json:"Data"`
		A string `json:"a"`
		D struct {
			I   string       `json:"I"`
			N   xjson.Number `json:"N"`
			BP1 xjson.Number `json:"BP1"`
			AP1 xjson.Number `json:"AP1"`
			V   xjson.Number `json:"V"`
		} `json:"d"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}

	if msg.A == "PO" {
		lastPx, _ := strconv.ParseFloat(string(msg.D.N), 64)
		bidPx, _ := strconv.ParseFloat(string(msg.D.BP1), 64)
		askPx, _ := strconv.ParseFloat(string(msg.D.AP1), 64)
		vol, _ := strconv.ParseFloat(string(msg.D.V), 64)

		pd = &store.PriceData{
			Symbol:    msg.D.I,
			LastPrice: lastPx,
			BestBid:   bidPx,
			BestAsk:   askPx,
			FairPrice: lastPx,
			Volume24:  vol,
			UpdatedAt: time.Now(),
		}
		return msg.D.I, pd, nil
	}

	lastPx, _ := strconv.ParseFloat(string(msg.Data.LastPrice), 64)
	bidPx, _ := strconv.ParseFloat(string(msg.Data.BidPrice), 64)
	askPx, _ := strconv.ParseFloat(string(msg.Data.AskPrice), 64)
	vol, _ := strconv.ParseFloat(string(msg.Data.Volume24), 64)

	pd = &store.PriceData{
		Symbol:    msg.Data.Symbol,
		LastPrice: lastPx,
		BestBid:   bidPx,
		BestAsk:   askPx,
		FairPrice: lastPx,
		Volume24:  vol,
		UpdatedAt: time.Now(),
	}
	return msg.Data.Symbol, pd, nil
}

func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Result []struct {
			Table string `json:"table"`
			Data  struct {
				I  string       `json:"I"`  // Instrument
				P  xjson.Number `json:"p"`  // Direction ("1" = Long, "2" = Short)
				Po xjson.Number `json:"Po"` // Position qty
				OP xjson.Number `json:"OP"` // Open Price
				U  xjson.Number `json:"u"`  // Used margin
			} `json:"data"`
		} `json:"result"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if len(msg.Result) == 0 {
		return nil, fmt.Errorf("no position payload")
	}
	raw := msg.Result[0].Data
	qty, _ := strconv.ParseFloat(string(raw.Po), 64)
	openPx, _ := strconv.ParseFloat(string(raw.OP), 64)

	posType := exchange.PositionTypeLong
	if raw.P == "2" {
		posType = exchange.PositionTypeShort
	}
	if qty == 0 {
		posType = exchange.PositionTypeUnknown
	}

	return &exchange.PersonalPositionUpdate{
		Symbol:       raw.I,
		HoldVol:      qty,
		HoldAvgPrice: openPx,
		PositionType: posType,
	}, nil
}

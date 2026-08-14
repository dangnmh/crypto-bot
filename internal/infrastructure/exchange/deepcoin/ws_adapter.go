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
	activeSubs   map[string]bool
	activeSubsMu sync.Mutex
}

func NewWsAdapter(privateURL string) *WsAdapter {
	return &WsAdapter{
		privateURL: privateURL,
		activeSubs: make(map[string]bool),
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

func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		wsAction:   "1",
		wsSymbol:   normalizeDeepcoinSymbol(symbol),
		wsTopic:    wsTopicMarket,
		wsLocalNo:  6,
		wsResumeNo: -1,
	}
	if err := a.pool.SubscribePublic(ctx, symbol+":ticker", msg); err != nil {
		return err
	}
	a.activeSubsMu.Lock()
	a.activeSubs[symbol] = true
	a.activeSubsMu.Unlock()
	return nil
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	a.activeSubsMu.Lock()
	delete(a.activeSubs, symbol)

	remaining := make([]string, 0, len(a.activeSubs))
	for sym := range a.activeSubs {
		remaining = append(remaining, sym)
	}
	a.activeSubsMu.Unlock()

	unsubMsg := map[string]any{
		wsAction:   "0",
		wsSymbol:   normalizeDeepcoinSymbol(symbol),
		wsTopic:    wsTopicMarket,
		wsLocalNo:  6,
		wsResumeNo: -1,
	}

	if len(remaining) > 0 {
		// 1. Send Action: "0" (unsubscribe all)
		if err := a.pool.UnsubscribePublic(ctx, symbol+":ticker", unsubMsg); err != nil {
			return err
		}

		// 2. Re-subscribe to other active symbols
		for _, activeSym := range remaining {
			subMsg := map[string]any{
				wsAction:   "1",
				wsSymbol:   normalizeDeepcoinSymbol(activeSym),
				wsTopic:    wsTopicMarket,
				wsLocalNo:  6,
				wsResumeNo: -1,
			}
			if err := a.pool.SubscribePublic(ctx, activeSym+":ticker", subMsg); err != nil {
				return err
			}
		}
		return nil
	}

	return a.pool.UnsubscribePublic(ctx, symbol+":ticker", unsubMsg)
}

func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	msg := map[string]any{
		"action": "subscribe",
		"tables": []string{wsTablePosition},
	}
	return a.pool.SendPrivate(ctx, msg)
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
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

func reconstructDeepcoinSymbol(symbol string) string {
	if strings.Contains(symbol, "-SWAP") || strings.Contains(symbol, "_SWAP") {
		return symbol
	}
	s := strings.ToUpper(symbol)
	if before, ok := strings.CutSuffix(s, "USDT"); ok {
		return before + "-USDT-SWAP"
	}
	if before, ok := strings.CutSuffix(s, "USDC"); ok {
		return before + "-USDC-SWAP"
	}
	if before, ok := strings.CutSuffix(s, "USD"); ok {
		return before + "-USD-SWAP"
	}
	return symbol
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
		D []struct {
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
		if len(msg.D) == 0 {
			return "", nil, fmt.Errorf("empty data array in PO message")
		}
		item := msg.D[0]
		lastPx, _ := strconv.ParseFloat(string(item.N), 64)
		bidPx, _ := strconv.ParseFloat(string(item.BP1), 64)
		askPx, _ := strconv.ParseFloat(string(item.AP1), 64)
		vol, _ := strconv.ParseFloat(string(item.V), 64)

		reconstructed := reconstructDeepcoinSymbol(item.I)
		pd = &store.PriceData{
			Symbol:    reconstructed,
			LastPrice: lastPx,
			BestBid:   bidPx,
			BestAsk:   askPx,
			FairPrice: lastPx,
			Volume24:  vol,
			UpdatedAt: time.Now(),
		}
		return reconstructed, pd, nil
	}

	lastPx, _ := strconv.ParseFloat(string(msg.Data.LastPrice), 64)
	bidPx, _ := strconv.ParseFloat(string(msg.Data.BidPrice), 64)
	askPx, _ := strconv.ParseFloat(string(msg.Data.AskPrice), 64)
	vol, _ := strconv.ParseFloat(string(msg.Data.Volume24), 64)

	reconstructed := reconstructDeepcoinSymbol(msg.Data.Symbol)
	pd = &store.PriceData{
		Symbol:    reconstructed,
		LastPrice: lastPx,
		BestBid:   bidPx,
		BestAsk:   askPx,
		FairPrice: lastPx,
		Volume24:  vol,
		UpdatedAt: time.Now(),
	}
	return reconstructed, pd, nil
}

func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Result []struct {
			Table string `json:"table"`
			Data  struct {
				I      string       `json:"I"`  // Instrument
				LowerI any          `json:"i"`  // Consumes lowercase "i" to avoid case-insensitive fallback to "I"
				P      xjson.Number `json:"p"`  // Direction ("1" = Long, "2" = Short)
				Po     xjson.Number `json:"Po"` // Position qty
				OP     xjson.Number `json:"OP"` // Open Price
				U      xjson.Number `json:"u"`  // Used margin (lowercase u)
				UpperU any          `json:"U"`  // Consumes uppercase "U" to avoid case-insensitive fallback to "u"
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

	reconstructed := reconstructDeepcoinSymbol(raw.I)
	return &exchange.PersonalPositionUpdate{
		Symbol:          reconstructed,
		HoldVolContract: qty,
		HoldAvgPrice:    openPx,
		PositionType:    posType,
	}, nil
}

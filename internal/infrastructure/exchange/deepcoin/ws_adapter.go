package deepcoin

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
)

type WsAdapter struct {
	pool *pkgws.Pool
}

func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	return fmt.Errorf("SubscribeTicker not supported on Deepcoin")
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	return fmt.Errorf("UnsubscribeTicker not supported on Deepcoin")
}

func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	return fmt.Errorf("SubscribeKline not supported on Deepcoin")
}

func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	return fmt.Errorf("UnsubscribeKline not supported on Deepcoin")
}

func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	return fmt.Errorf("SubscribeDepth not supported on Deepcoin")
}

func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	return fmt.Errorf("UnsubscribeDepth not supported on Deepcoin")
}

func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return fmt.Errorf("SubscribePersonal not supported on Deepcoin")
}

func (a *WsAdapter) GetPingConfig() (payload any, interval time.Duration) {
	return nil, 0
}

func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		return ""
	}
}

func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	return "", nil, fmt.Errorf("ParseTicker not supported on Deepcoin")
}

func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, fmt.Errorf("ParsePosition not supported on Deepcoin")
}

package koinbay_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/koinbay"

	"github.com/stretchr/testify/assert"
)

func TestClient_FetchKlines(t *testing.T) {
	t.Parallel()

	client := koinbay.NewClient(nil, "", config.LoggingConfig{})

	klines, err := client.FetchKlines(
		context.Background(),
		"BTC_USDT",
		exchange.Interval1h,
		time.Unix(1783504800, 0),
		time.Unix(1783508400, 0),
	)
	assert.NoError(t, err)
	assert.Nil(t, klines)
}

package okx_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/exchange/okx"

	"github.com/stretchr/testify/assert"
)

func TestSignRequest(t *testing.T) {
	t.Parallel()

	apiSecret := "secret"
	timestamp := "2020-12-08T09:08:09.123Z"
	method := "GET"
	requestPath := "/api/v5/account/balance?ccy=BTC"
	body := ""

	sig := okx.SignRequest(apiSecret, timestamp, method, requestPath, body)
	assert.NotEmpty(t, sig)

	// Test consistency
	sig2 := okx.SignRequest(apiSecret, timestamp, method, requestPath, body)
	assert.Equal(t, sig, sig2)
}

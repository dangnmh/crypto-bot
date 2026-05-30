package mexc_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/exchange/mexc"
)

func TestSignRequest_GET(t *testing.T) {
	t.Parallel()
	apiKey := "testApiKey"
	apiSecret := "testApiSecret"
	timestamp := "1609459200000"
	params := map[string]any{
		"symbol":   "BTC_USDT",
		"side":     "1",
		"page_num": 1,
	}

	sig := mexc.SignRequest(apiKey, apiSecret, timestamp, "GET", params)
	if sig == "" {
		t.Fatal("signature should not be empty")
	}

	// Verify determinism — same input should produce same output.
	sig2 := mexc.SignRequest(apiKey, apiSecret, timestamp, "GET", params)
	if sig != sig2 {
		t.Errorf("signatures should be deterministic: %s != %s", sig, sig2)
	}
}

func TestSignRequest_GET_NilParams(t *testing.T) {
	t.Parallel()
	sig := mexc.SignRequest("key", "secret", "12345", "GET", nil)
	if sig == "" {
		t.Fatal("signature should not be empty for nil params")
	}
}

func TestSignRequest_GET_WrongParamType(t *testing.T) {
	t.Parallel()
	// If params is not map[string]any, paramStr stays empty.
	sig := mexc.SignRequest("key", "secret", "12345", "GET", "not a map")
	if sig == "" {
		t.Fatal("signature should not be empty")
	}
}

func TestSignRequest_POST(t *testing.T) {
	t.Parallel()
	body := map[string]any{
		"symbol": "BTC_USDT",
		"vol":    100,
	}

	sig := mexc.SignRequest("key", "secret", "12345", "POST", body)
	if sig == "" {
		t.Fatal("signature should not be empty for POST")
	}
}

func TestSignRequest_POST_NilBody(t *testing.T) {
	t.Parallel()
	sig := mexc.SignRequest("key", "secret", "12345", "POST", nil)
	if sig == "" {
		t.Fatal("signature should not be empty for nil POST body")
	}
}

func TestSignRequest_DELETE(t *testing.T) {
	t.Parallel()
	params := map[string]any{"orderId": "123"}
	sig := mexc.SignRequest("key", "secret", "12345", "DELETE", params)
	if sig == "" {
		t.Fatal("signature should not be empty for DELETE")
	}
}

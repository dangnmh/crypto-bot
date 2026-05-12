package exchange_test

import (
	"errors"
	"fmt"
	"testing"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
)

func TestAPIError_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  *exchange.APIError
		want string
	}{
		{
			name: "with HTTP status",
			err:  &exchange.APIError{StatusCode: 429, Code: 10001, Message: "rate limited", Path: "/api/order"},
			want: `exchange API error: HTTP 429, code=10001, message="rate limited", path=/api/order`,
		},
		{
			name: "without HTTP status",
			err:  &exchange.APIError{Code: 500, Message: "internal error", Path: "/api/ping"},
			want: `exchange API error: code=500, message="internal error", path=/api/ping`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.err.Error())
		})
	}
}

func TestRateLimitError_Error(t *testing.T) {
	t.Parallel()
	err := &exchange.RateLimitError{Message: "too many requests", Path: "/api/order"}
	want := "rate limit exceeded: too many requests (path=/api/order)"
	assert.Equal(t, want, err.Error())
}

func TestOrderRejectedError_Error(t *testing.T) {
	t.Parallel()
	err := &exchange.OrderRejectedError{Code: 2001, Reason: "insufficient margin", Symbol: "BTC_USDT"}
	want := `order rejected: code=2001, reason="insufficient margin", symbol=BTC_USDT`
	assert.Equal(t, want, err.Error())
}

func TestIsAPIError(t *testing.T) {
	t.Parallel()
	apiErr := &exchange.APIError{Code: 1}
	got, ok := exchange.IsAPIError(apiErr)
	assert.True(t, ok)
	assert.Equal(t, 1, got.Code)

	_, ok = exchange.IsAPIError(errors.New("generic error"))
	assert.False(t, ok)
}

func TestIsRateLimitError(t *testing.T) {
	t.Parallel()
	assert.True(t, exchange.IsRateLimitError(&exchange.RateLimitError{}))
	assert.False(t, exchange.IsRateLimitError(errors.New("not rate limit")))
}

func TestIsOrderRejectedError(t *testing.T) {
	t.Parallel()
	orderErr := &exchange.OrderRejectedError{Code: 1, Reason: "test", Symbol: "BTC"}
	got, ok := exchange.IsOrderRejectedError(orderErr)
	assert.True(t, ok)
	assert.Equal(t, "BTC", got.Symbol)

	_, ok = exchange.IsOrderRejectedError(errors.New("generic"))
	assert.False(t, ok)
}

func TestIsAPIError_Wrapped(t *testing.T) {
	t.Parallel()
	apiErr := &exchange.APIError{Code: 42, Message: "wrapped"}
	wrapped := fmt.Errorf("outer: %w", apiErr)

	got, ok := exchange.IsAPIError(wrapped)
	assert.True(t, ok)
	assert.Equal(t, 42, got.Code)
}

func TestIsAPIError_Nil(t *testing.T) {
	t.Parallel()
	_, ok := exchange.IsAPIError(nil)
	assert.False(t, ok)
}

func TestIsRateLimitError_Wrapped(t *testing.T) {
	t.Parallel()
	rle := &exchange.RateLimitError{Message: "slow down", Path: "/api/test"}
	wrapped := fmt.Errorf("outer: %w", rle)
	assert.True(t, exchange.IsRateLimitError(wrapped))
}

func TestIsOrderRejectedError_Wrapped(t *testing.T) {
	t.Parallel()
	ore := &exchange.OrderRejectedError{Code: 99, Reason: "no funds", Symbol: "ETH"}
	wrapped := fmt.Errorf("outer: %w", ore)
	got, ok := exchange.IsOrderRejectedError(wrapped)
	assert.True(t, ok)
	assert.Equal(t, 99, got.Code)
}

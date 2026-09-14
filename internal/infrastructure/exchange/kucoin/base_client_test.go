package kucoin_test

import (
	"bytes"
	"log/slog"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/kucoin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // Mutates global slog default logger, cannot run in parallel
func TestNewBaseClient_ExchangeLogger(t *testing.T) {
	tests := []struct {
		name             string
		exchangeName     string
		expectedExchange string
	}{
		{
			name:             "futures exchange name",
			exchangeName:     kucoin.ExchangeFutures,
			expectedExchange: "kucoin_futures",
		},
		{
			name:             "spot exchange name",
			exchangeName:     kucoin.ExchangeSpot,
			expectedExchange: "kucoin_spot",
		},
		{
			name:             "fallback to kucoin when empty",
			exchangeName:     "",
			expectedExchange: "kucoin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := slog.NewJSONHandler(&buf, nil)
			oldDefault := slog.Default()
			slog.SetDefault(slog.New(h))
			t.Cleanup(func() {
				slog.SetDefault(oldDefault)
			})

			client := kucoin.NewBaseClient(nil, "https://example.com", "key", "secret", "pass", config.LoggingConfig{}, tt.exchangeName)
			require.NotNil(t, client)
			assert.Equal(t, tt.expectedExchange, client.ExchangeName())

			client.Logger().Info("test log message")
			logOutput := buf.String()
			assert.Contains(t, logOutput, `"exchange":"`+tt.expectedExchange+`"`)
			assert.Contains(t, logOutput, `"component":"exchange"`)
		})
	}
}

package mexc_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange/mexc"

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
			exchangeName:     mexc.ExchangeFutures,
			expectedExchange: "mexc_futures",
		},
		{
			name:             "spot exchange name",
			exchangeName:     mexc.ExchangeSpot,
			expectedExchange: "mexc_spot",
		},
		{
			name:             "fallback to mexc when empty",
			exchangeName:     "",
			expectedExchange: "mexc",
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

			client := mexc.NewBaseClient(nil, "https://example.com", "key", "secret", config.LoggingConfig{}, tt.exchangeName)
			require.NotNil(t, client)
			assert.Equal(t, tt.expectedExchange, client.ExchangeName())

			client.Logger().Info("test log message")
			logOutput := buf.String()
			assert.Contains(t, logOutput, `"exchange":"`+tt.expectedExchange+`"`)
			assert.Contains(t, logOutput, `"component":"exchange"`)
		})
	}
}

//nolint:paralleltest // Mutates global slog default logger
func TestBaseClient_HTTPLog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":200,"data":"ok"}`))
	}))
	defer server.Close()

	var buf bytes.Buffer
	h := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldDefault := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() {
		slog.SetDefault(oldDefault)
	})

	logCfg := config.LoggingConfig{HTTP: true}
	client := mexc.NewBaseClient(server.Client(), server.URL, "key", "secret", logCfg, mexc.ExchangeFutures)

	// 1. Without SetOrderHTTPClient
	_, err := client.Request(context.Background(), http.MethodPost, "/api/v1/private/order/create", nil, []byte(`{}`), false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "HTTP Request", "logs should contain HTTP Request before SetOrderHTTPClient")

	buf.Reset()

	// 2. With SetOrderHTTPClient
	orderClient := server.Client()
	client.SetOrderHTTPClient(orderClient)

	_, err = client.Request(context.Background(), http.MethodPost, "/api/v1/private/order/create", nil, []byte(`{}`), false)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "HTTP Request", "logs should contain HTTP Request even when using SetOrderHTTPClient")
}

package notifier

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewTelegramProviderValidatesInputs(t *testing.T) {
	t.Parallel()

	_, err := NewTelegramProvider("", "123", slog.Default())
	require.ErrorContains(t, err, "telegram bot token is required")
}

func TestTelegramProviderSendQueuesEvent(t *testing.T) {
	t.Parallel()

	p := &TelegramProvider{
		logger: slog.Default(),
		queue:  make(chan Event, 1),
	}

	evt := Event{Level: LevelTrading, Symbol: "BTC_USDT", Message: "entry"}
	require.NoError(t, p.Send(context.Background(), evt))
	require.Equal(t, evt, <-p.queue)
}

func TestTelegramProviderSendHandlesStoppedAndFullQueue(t *testing.T) {
	t.Parallel()

	t.Run("stopped", func(t *testing.T) {
		t.Parallel()

		p := &TelegramProvider{
			logger:  slog.Default(),
			queue:   make(chan Event, 1),
			stopped: true,
		}

		require.NoError(t, p.Send(context.Background(), Event{Message: "ignored"}))
		require.Empty(t, p.queue)
	})

	t.Run("full", func(t *testing.T) {
		t.Parallel()

		p := &TelegramProvider{
			logger: slog.Default(),
			queue:  make(chan Event, 1),
		}
		p.queue <- Event{Message: "occupy"}

		err := p.Send(context.Background(), Event{Message: "drop"})
		require.ErrorContains(t, err, "notifier queue full")
	})
}

func TestTelegramProviderLifecycleWithNilBot(t *testing.T) {
	t.Parallel()

	p := &TelegramProvider{
		logger: slog.Default(),
		queue:  make(chan Event, 10),
	}

	require.NoError(t, p.Start(context.Background()))
	require.NoError(t, p.Stop(context.Background()))
	require.True(t, p.stopped)
	require.NoError(t, p.Stop(context.Background()))
}

func TestTelegramProviderFormatMessage(t *testing.T) {
	t.Parallel()

	p := &TelegramProvider{}

	tests := []struct {
		name string
		evt  Event
		want string
	}{
		{
			name: "critical with symbol",
			evt:  Event{Level: LevelCritical, Symbol: "ETH_USDT", Message: "risk exceeded"},
			want: "🔴 [CRITICAL] [ETH_USDT]\nrisk exceeded",
		},
		{
			name: "trading",
			evt:  Event{Level: LevelTrading, Message: "order sent"},
			want: "🟡 [TRADING]\norder sent",
		},
		{
			name: "unknown defaults to info",
			evt:  Event{Level: Level("debug"), Message: "heartbeat"},
			want: telegramInfoPrefix + "\nheartbeat",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, p.formatMessage(tt.evt))
		})
	}
}

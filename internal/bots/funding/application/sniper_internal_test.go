package application

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"
	pkgws "crypto-bot/pkg/ws"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestSniperRunAsBackgroundWithReadyCoreServices(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	client.EXPECT().WarmUp(gomock.Any(), 4*time.Second).AnyTimes()
	client.EXPECT().GetServerTime(gomock.Any()).Return(time.Now().UnixMilli(), nil).AnyTimes()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &Sniper{
		engine: &infraapp.Engine{
			Client:   client,
			TimeSync: timesync.New(client, time.Hour),
			WS:       pkgws.NewPool("", 1, logger),
			Bus:      eventbus.New(logger),
		},
		stores: infraapp.NewCentralStore(),
		log:    logger,
	}

	done := make(chan error, 1)
	go func() {
		done <- s.RunAsBackground(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	require.NoError(t, <-done)
}

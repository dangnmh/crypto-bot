//nolint:testpackage // These tests exercise unexported cycle helpers.
package cycle

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestTrackStateToString(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "active", trackStateToString(0))
	assert.Equal(t, "triggered", trackStateToString(1))
	assert.Equal(t, "cancelled", trackStateToString(2))
	assert.Equal(t, "unknown", trackStateToString(99))
}

func TestSubscriptionManagerSuccessAndFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	ws := mocks.NewMockSubscriber(ctrl)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sm := NewSubscriptionManager(ws, "BTC_USDT", logger)

	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)
	require.NoError(t, sm.SubscribeAll(context.Background()))

	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil)
	sm.UnsubscribeAll(context.Background())

	ws.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(assert.AnError)
	require.Error(t, sm.SubscribeAll(context.Background()))

	ws.EXPECT().UnsubscribeTicker(gomock.Any(), "BTC_USDT").Return(assert.AnError)
	assert.NotPanics(t, func() { sm.UnsubscribeAll(context.Background()) })
}

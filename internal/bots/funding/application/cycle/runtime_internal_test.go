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

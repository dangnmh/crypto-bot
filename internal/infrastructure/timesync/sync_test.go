package timesync_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestTimeSync_StartAndAccessors(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetServerTime(gomock.Any()).DoAndReturn(func(context.Context) (int64, error) {
		return time.Now().Add(25 * time.Millisecond).UnixMilli(), nil
	}).AnyTimes()

	ts := timesync.New(client, 10*time.Millisecond)
	ctx := t.Context()

	go ts.Start(ctx)
	assert.NoError(t, ts.WaitReady(ctx))

	assert.NotZero(t, ts.GetServerTime())
	assert.NotZero(t, ts.Now())
	assert.LessOrEqual(t, ts.LatencyMs(), int64(100))
	assert.True(t, ts.IsHealthy())
	assert.NotZero(t, ts.Offset())
	assert.Less(t, ts.MsUntilTarget(time.Now().Add(time.Second).UnixMilli()), int64(time.Second*2/time.Millisecond))
	assert.Greater(t, ts.Until(time.Now().Add(50*time.Millisecond)), time.Duration(0))
}

func TestTimeSync_WaitReadyAndSleepCancel(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	ts := timesync.New(mocks.NewMockClient(ctrl), time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	assert.ErrorIs(t, ts.WaitReady(ctx), context.Canceled)
	assert.ErrorIs(t, ts.Sleep(ctx, time.Second), context.Canceled)
}

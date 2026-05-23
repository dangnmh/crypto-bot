//nolint:testpackage // These tests exercise unexported syncOnce branches.
package timesync

import (
	"context"
	"errors"
	"testing"
	"time"

	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"
)

func TestSyncOnceErrorAndHighLatencyBranches(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	client := mocks.NewMockClient(ctrl)
	ts := New(client, time.Second)

	client.EXPECT().GetServerTime(gomock.Any()).Return(int64(0), errors.New("server down"))
	ts.syncOnce(context.Background())
	assert.False(t, ts.IsHealthy())

	client.EXPECT().GetServerTime(gomock.Any()).DoAndReturn(func(context.Context) (int64, error) {
		time.Sleep(120 * time.Millisecond)
		return time.Now().UnixMilli(), nil
	})
	ts.syncOnce(context.Background())
	assert.GreaterOrEqual(t, ts.LatencyMs(), int64(100))
	assert.False(t, ts.IsHealthy())
}

func TestTimeSyncSleepSuccess(t *testing.T) {
	t.Parallel()

	ts := New(nil, time.Second)
	assert.NoError(t, ts.Sleep(context.Background(), time.Nanosecond))
}

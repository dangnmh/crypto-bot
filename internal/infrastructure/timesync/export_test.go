package timesync

import "context"

func (ts *TimeSync) SyncOnceForTest(ctx context.Context) {
	ts.syncOnce(ctx)
}

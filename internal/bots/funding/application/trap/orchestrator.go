package trap

import (
	"context"

	"crypto-bot/internal/bots/funding/application/cycle"
)

func Register(ctx context.Context, rt *cycle.Runtime) {
	subscribeFireTrap(ctx, rt)
	subscribeFillWatcher(ctx, rt)
	subscribeTrailing(ctx, rt)
	subscribeTrapOrderTimeoutGuard(ctx, rt)
	watchTrapBranchTerminal(ctx, rt)
}

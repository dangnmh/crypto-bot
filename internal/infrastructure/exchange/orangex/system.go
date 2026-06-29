package orangex

import (
	"context"
	"time"

	"crypto-bot/pkg/xjson"
)

type serverTimeResult xjson.Number

func (c *Client) rawGetServerTime(ctx context.Context) (*serverTimeResult, error) {
	respBytes, err := c.postRPC(ctx, "/public/time", "/public/time", nil, false)
	if err != nil {
		return nil, err
	}
	var envelope orangexRPCResponse[serverTimeResult]
	if err := xjson.Unmarshal(respBytes, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return &envelope.Result, nil
}

func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	res, err := c.rawGetServerTime(ctx)
	if err != nil {
		return 0, err
	}
	return xjson.ToInt64(xjson.Number(*res)) * 1000, nil
}

func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	_, _ = c.GetServerTime(ctx)
}

func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

func (c *Client) StartBackgroundTasks(ctx context.Context) {
	c.refresherOnce.Do(func() {
		_ = c.refreshToken(ctx)
		go func() {
			ticker := time.NewTicker(4 * time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					c.logger.DebugContext(ctx, "Stopped OrangeX token refresher loop")
					return
				case <-ticker.C:
					if err := c.refreshToken(ctx); err != nil {
						c.logger.ErrorContext(ctx, "Failed to refresh OrangeX token in background", "error", err)
					}
				}
			}
		}()
	})
}

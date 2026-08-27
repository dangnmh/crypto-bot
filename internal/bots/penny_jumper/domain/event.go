package domain

import (
	"time"

	shared "crypto-bot/internal/domain"
)

// Canonical topic names for Penny Jumper event bus.
const (
	TopicDepthUpdated     = "penny_jumper.depth.updated"
	TopicWallEventStream  = "penny_jumper.wall.event.stream"
	TopicWallQualified    = "penny_jumper.wall.qualified"
	TopicWallDisqualified = "penny_jumper.wall.disqualified"
)

// WallEventStreamPayload carries an event sourced micro-event for persistence and ML feature streams.
type WallEventStreamPayload struct {
	Exchange  string    `json:"exchange"`
	Symbol    string    `json:"symbol"`
	Side      string    `json:"side"`
	Event     WallEvent `json:"event"`
	Timestamp time.Time `json:"timestamp"`
}

// DepthUpdatedEvent is emitted when an orderbook depth update is applied.
type DepthUpdatedEvent struct {
	Exchange  string            `json:"exchange"`
	Symbol    string            `json:"symbol"`
	Version   int64             `json:"version"`
	OrderBook *shared.OrderBook `json:"order_book"`
	Timestamp time.Time         `json:"timestamp"`
}

// WallQualifiedEvent is emitted when a wall achieves an authentic wall trust score >= threshold.
type WallQualifiedEvent struct {
	Wall             Wall      `json:"wall"`
	TrustScore       float64   `json:"trust_score"`
	TargetEntryPrice float64   `json:"target_entry_price"`
	SpreadPct        float64   `json:"spread_pct"`
	Timestamp        time.Time `json:"timestamp"`
}

// WallDisqualifiedEvent is emitted when a previously trusted wall is invalidated or fails trust checks.
type WallDisqualifiedEvent struct {
	WallID    string    `json:"wall_id"`
	Exchange  string    `json:"exchange"`
	Symbol    string    `json:"symbol"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

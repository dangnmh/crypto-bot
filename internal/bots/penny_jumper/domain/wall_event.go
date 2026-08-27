package domain

import (
	"time"
)

// WallEventType defines the discrete micro-state transition types across a wall's lifecycle.
type WallEventType string

const (
	WallEventBorn            WallEventType = "WALL_BORN"
	WallEventMatured         WallEventType = "WALL_MATURED"
	WallEventResized         WallEventType = "WALL_RESIZED"
	WallEventFlapped         WallEventType = "WALL_FLAPPED"
	WallEventPriceApproached WallEventType = "WALL_PRICE_APPROACHED"
	WallEventWeakened        WallEventType = "WALL_WEAKENED"
	WallEventDisappeared     WallEventType = "WALL_DISAPPEARED"
	WallEventConsumed        WallEventType = "WALL_CONSUMED"
)

// WallEvent represents an immutable point-in-time micro-state transition of a wall.
type WallEvent struct {
	WallID        string        `json:"wall_id"`
	Seq           int64         `json:"seq"`
	Timestamp     time.Time     `json:"timestamp"`
	EventType     WallEventType `json:"event_type"`
	Price         float64       `json:"price,omitempty"`
	Volume        float64       `json:"volume"`
	DeltaVolume   float64       `json:"delta_volume"`
	DistancePct   float64       `json:"distance_pct"`
	SpreadPct     float64       `json:"spread_pct"`
	RelativeRatio float64       `json:"relative_ratio"`
}

package domain

import (
	"math"
	"time"

	shared "crypto-bot/internal/domain"
)

// WallStatus represents the lifecycle status of a detected wall.
type WallStatus string

const (
	WallStatusActive      WallStatus = "ACTIVE"
	WallStatusWeakened    WallStatus = "WEAKENED"
	WallStatusDisappeared WallStatus = "DISAPPEARED"
	WallStatusConsumed    WallStatus = "CONSUMED"
)

// Wall represents an orderbook wall detected by the WallDetector.
type Wall struct {
	ID              string      `json:"id"`
	Exchange        string      `json:"exchange"`
	Symbol          string      `json:"symbol"`
	Side            shared.Side `json:"side"` // SideOpenLong (Bid wall) or SideOpenShort (Ask wall)
	Price           float64     `json:"price"`
	Volume          float64     `json:"volume"`
	InitialVolume   float64     `json:"initial_volume"`
	AvgNearbyVolume float64     `json:"avg_nearby_volume"`
	RelativeRatio   float64     `json:"relative_ratio"` // volume / avg_nearby_volume
	DistancePct     float64     `json:"distance_pct"`   // Distance from best bid/ask
	FirstDetectedAt time.Time   `json:"first_detected_at"`
	LastUpdatedAt   time.Time   `json:"last_updated_at"`
	DisappearedAt   *time.Time  `json:"disappeared_at,omitempty"`
	Status          WallStatus  `json:"status"`
	EventSeq        int64       `json:"event_seq"`
	Matured         bool        `json:"matured"`
}

// GetAgeAt computes wall age relative to a specific timestamp.
func (w *Wall) GetAgeAt(at time.Time) time.Duration {
	if w.FirstDetectedAt.IsZero() {
		return 0
	}
	if w.DisappearedAt != nil && !w.DisappearedAt.IsZero() {
		return w.DisappearedAt.Sub(w.FirstDetectedAt)
	}
	if !at.IsZero() {
		return at.Sub(w.FirstDetectedAt)
	}
	return time.Since(w.FirstDetectedAt)
}

// GetAge computes wall age relative to current system time.
func (w *Wall) GetAge() time.Duration {
	return w.GetAgeAt(time.Now())
}

// ApplyEvent applies a discrete point-in-time WallEvent to update the Wall aggregate state.
func (w *Wall) ApplyEvent(evt WallEvent) {
	w.EventSeq = evt.Seq
	w.LastUpdatedAt = evt.Timestamp

	switch evt.EventType {
	case WallEventBorn:
		w.ID = evt.WallID
		w.Volume = evt.Volume
		w.InitialVolume = evt.Volume
		w.DistancePct = evt.DistancePct
		w.RelativeRatio = evt.RelativeRatio
		w.FirstDetectedAt = evt.Timestamp
		w.Status = WallStatusActive

	case WallEventMatured:
		w.Matured = true

	case WallEventResized:
		w.Volume = evt.Volume
		w.DistancePct = evt.DistancePct
		w.RelativeRatio = evt.RelativeRatio

	case WallEventFlapped:
		// Wall reappeared within grace window

	case WallEventPriceApproached:
		w.DistancePct = evt.DistancePct

	case WallEventWeakened:
		w.Status = WallStatusWeakened
		w.Volume = evt.Volume

	case WallEventDisappeared:
		w.Status = WallStatusDisappeared
		disappearedAt := evt.Timestamp
		w.DisappearedAt = &disappearedAt

	case WallEventConsumed:
		w.Status = WallStatusConsumed
		w.Volume = 0
		consumedAt := evt.Timestamp
		w.DisappearedAt = &consumedAt
	}
}

// ProjectWallFromEvents replays an event stream to reconstruct the current Wall aggregate.
func ProjectWallFromEvents(events []WallEvent) *Wall {
	if len(events) == 0 {
		return nil
	}
	wall := &Wall{}
	for i := range events {
		wall.ApplyEvent(events[i])
	}
	return wall
}

// WallMetrics contains reconciled analytical figures combining orderbook lifecycle events and trade executions.
type WallMetrics struct {
	InitialVolume     float64 `json:"initial_volume"`
	FinalVolume       float64 `json:"final_volume"`
	TotalDropVolume   float64 `json:"total_drop_volume"`
	TotalTradedVolume float64 `json:"total_traded_volume"`
	AbsorbedVolume    float64 `json:"absorbed_volume"`
	PulledVolume      float64 `json:"pulled_volume"`
	ResizeCount       int     `json:"resize_count"`
}

// ReconcileWallData combines orderbook wall lifecycle events with public trades to compute verified metrics.
func ReconcileWallData(wall *Wall, events []WallEvent, trades []shared.PublicTrade) WallMetrics {
	var initialVol, finalVol float64
	var totalDrop float64
	var resizeCount int

	if len(events) > 0 {
		initialVol = events[0].Volume
		finalVol = events[len(events)-1].Volume
	} else if wall != nil {
		initialVol = wall.InitialVolume
		finalVol = wall.Volume
	}

	for i := range events {
		if events[i].EventType == WallEventResized {
			resizeCount++
		}
		if events[i].DeltaVolume < 0 {
			totalDrop += -events[i].DeltaVolume
		}
	}

	var totalTraded float64
	for i := range trades {
		totalTraded += trades[i].Volume
	}

	absorbed := math.Min(totalDrop, totalTraded)
	pulled := math.Max(0, totalDrop-absorbed)

	return WallMetrics{
		InitialVolume:     initialVol,
		FinalVolume:       finalVol,
		TotalDropVolume:   totalDrop,
		TotalTradedVolume: totalTraded,
		AbsorbedVolume:    absorbed,
		PulledVolume:      pulled,
		ResizeCount:       resizeCount,
	}
}

// CalculateResizeCount returns the total number of resize events in the stream.
func CalculateResizeCount(events []WallEvent) int {
	var count int
	for i := range events {
		if events[i].EventType == WallEventResized {
			count++
		}
	}
	return count
}

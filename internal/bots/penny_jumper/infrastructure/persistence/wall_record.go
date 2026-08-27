package persistence

import (
	"encoding/json"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	shared "crypto-bot/internal/domain"
)

// PennyJumperWallRecord is the GORM database model representing a detected and tracked wall lifecycle and its event journal.
type PennyJumperWallRecord struct {
	ID             string     `gorm:"column:id;primaryKey;size:64"` // workflow_id / req_id
	Exchange       string     `gorm:"column:exchange;size:32;index:idx_pj_ex_sym;not null"`
	Symbol         string     `gorm:"column:symbol;size:32;index:idx_pj_ex_sym;not null"`
	Side           string     `gorm:"column:side;size:16;not null"`
	WallPrice      float64    `gorm:"column:wall_price;type:numeric(20,8);not null"`
	InitialVolume  float64    `gorm:"column:initial_volume;type:numeric(20,8);not null"`
	FinalVolume    float64    `gorm:"column:final_volume;type:numeric(20,8);not null"`
	AbsorbedVolume float64    `gorm:"column:absorbed_volume;type:numeric(20,8)"`
	PulledVolume   float64    `gorm:"column:pulled_volume;type:numeric(20,8)"`
	DistancePct    float64    `gorm:"column:distance_pct;type:numeric(10,4)"`
	SpreadPct      float64    `gorm:"column:spread_pct;type:numeric(10,4)"`
	Outcome        string     `gorm:"column:outcome;size:32"` // e.g. "ACTIVE", "DISAPPEARED", "CONSUMED"
	Reason         string     `gorm:"column:reason;size:64"`
	DurationMs     int64      `gorm:"column:duration_ms"`
	EventsJSON     string     `gorm:"column:events;type:text"` // Serialized JSON array of WallEvents
	TradesJSON     string     `gorm:"column:trades;type:text"` // Serialized JSON array of PublicTrades
	CreatedAt      time.Time  `gorm:"column:created_at;index;not null"`
	CompletedAt    *time.Time `gorm:"column:completed_at;index"`
}

// TableName overrides the default table name to penny_jumper_walls.
func (PennyJumperWallRecord) TableName() string {
	return "penny_jumper_walls"
}

// SetEvents serializes a slice of domain.WallEvent into EventsJSON.
func (r *PennyJumperWallRecord) SetEvents(events []pjdomain.WallEvent) error {
	if len(events) == 0 {
		r.EventsJSON = "[]"
		return nil
	}
	data, err := json.Marshal(events)
	if err != nil {
		return err
	}
	r.EventsJSON = string(data)
	return nil
}

// GetEvents deserializes EventsJSON back into a slice of domain.WallEvent.
func (r *PennyJumperWallRecord) GetEvents() ([]pjdomain.WallEvent, error) {
	if r.EventsJSON == "" || r.EventsJSON == "[]" {
		return nil, nil
	}
	var events []pjdomain.WallEvent
	if err := json.Unmarshal([]byte(r.EventsJSON), &events); err != nil {
		return nil, err
	}
	return events, nil
}

// SetTrades serializes a slice of domain.PublicTrade into TradesJSON.
func (r *PennyJumperWallRecord) SetTrades(trades []shared.PublicTrade) error {
	if len(trades) == 0 {
		r.TradesJSON = "[]"
		return nil
	}
	data, err := json.Marshal(trades)
	if err != nil {
		return err
	}
	r.TradesJSON = string(data)
	return nil
}

// GetTrades deserializes TradesJSON back into a slice of domain.PublicTrade.
func (r *PennyJumperWallRecord) GetTrades() ([]shared.PublicTrade, error) {
	if r.TradesJSON == "" || r.TradesJSON == "[]" {
		return nil, nil
	}
	var trades []shared.PublicTrade
	if err := json.Unmarshal([]byte(r.TradesJSON), &trades); err != nil {
		return nil, err
	}
	return trades, nil
}

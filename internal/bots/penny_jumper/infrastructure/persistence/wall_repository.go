package persistence

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WallRepository defines the database persistence port for Penny Jumper wall records.
type WallRepository interface {
	Save(ctx context.Context, record *PennyJumperWallRecord) error
	FindByID(ctx context.Context, id string) (*PennyJumperWallRecord, error)
	List(ctx context.Context, symbol string, limit int) ([]PennyJumperWallRecord, error)
}

// GormWallRepository implements WallRepository using GORM.
type GormWallRepository struct {
	db *gorm.DB
}

// NewGormWallRepository creates a new GormWallRepository.
func NewGormWallRepository(db *gorm.DB) *GormWallRepository {
	return &GormWallRepository{db: db}
}

// Save inserts or updates a wall record in PostgreSQL.
func (r *GormWallRepository) Save(ctx context.Context, record *PennyJumperWallRecord) error {
	if r.db == nil || record == nil {
		return nil
	}

	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"exchange",
			"symbol",
			"side",
			"wall_price",
			"initial_volume",
			"final_volume",
			"absorbed_volume",
			"pulled_volume",
			"distance_pct",
			"spread_pct",
			"outcome",
			"reason",
			"duration_ms",
			"events",
			"trades",
			"completed_at",
		}),
	}).Create(record).Error

	if err != nil {
		return fmt.Errorf("save wall record %s: %w", record.ID, err)
	}

	return nil
}

// FindByID retrieves a wall record by its ID.
func (r *GormWallRepository) FindByID(ctx context.Context, id string) (*PennyJumperWallRecord, error) {
	if r.db == nil {
		return nil, nil
	}

	var rec PennyJumperWallRecord
	if err := r.db.WithContext(ctx).First(&rec, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("find wall record %s: %w", id, err)
	}

	return &rec, nil
}

// List returns recent wall records filtered optionally by symbol.
func (r *GormWallRepository) List(ctx context.Context, symbol string, limit int) ([]PennyJumperWallRecord, error) {
	if r.db == nil {
		return nil, nil
	}

	if limit <= 0 {
		limit = 50
	}

	var records []PennyJumperWallRecord
	query := r.db.WithContext(ctx).Order("created_at DESC").Limit(limit)
	if symbol != "" {
		query = query.Where("symbol = ?", symbol)
	}

	if err := query.Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list wall records: %w", err)
	}

	return records, nil
}

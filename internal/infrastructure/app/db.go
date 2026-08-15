package app

import (
	"context"
	"fmt"
	"os"

	"github.com/uptrace/opentelemetry-go-extra/otelgorm"
	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// InitDatabase initializes the GORM DB connection pool and runs AutoMigrate for given models.
func InitDatabase(lc fx.Lifecycle, models ...any) (*gorm.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, nil // gracefully skip if DATABASE_URL is not set
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Use(otelgorm.NewPlugin()); err != nil {
		return nil, fmt.Errorf("failed to register otelgorm plugin: %w", err)
	}

	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			return nil, fmt.Errorf("failed to auto-migrate database: %w", err)
		}
	}

	if lc != nil {
		lc.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				sqlDB, err := db.DB()
				if err != nil {
					return fmt.Errorf("failed to get underlying sql.DB: %w", err)
				}
				if err := sqlDB.Close(); err != nil {
					return fmt.Errorf("failed to close sql.DB: %w", err)
				}
				return nil
			},
		})
	}

	return db, nil
}

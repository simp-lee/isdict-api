package postgresutil

import (
	"context"
	"database/sql"
	"fmt"

	"gorm.io/gorm"
)

const RequiredExtensionName = "pg_trgm"

func CheckRequiredExtensionPresent(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is nil")
	}

	var exists bool
	if err := db.QueryRowContext(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)`,
		RequiredExtensionName,
	).Scan(&exists); err != nil {
		return fmt.Errorf("check required extension %s: %w", RequiredExtensionName, err)
	}

	if !exists {
		return fmt.Errorf("required extension %s is not enabled", RequiredExtensionName)
	}

	return nil
}

func EnsureRequiredExtensionsEnabled(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is nil")
	}

	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`).Error; err != nil {
		return fmt.Errorf("enable required extension %s: %w", RequiredExtensionName, err)
	}

	return nil
}

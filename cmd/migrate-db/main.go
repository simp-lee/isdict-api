package main

import (
	"log"
	"os"

	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/migration"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// migrate-db is a standalone tool for database migration
// Usage: go run cmd/migrate-db/main.go [--drop] [--verify]
func main() {
	// Parse flags
	dropTables := false
	verifyOnly := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--drop":
			dropTables = true
		case "--verify":
			verifyOnly = true
		case "--help", "-h":
			printUsage()
			return
		}
	}

	// Load configuration
	cfg := config.Load()

	// Connect to database
	log.Printf("Connecting to database: %s@%s:%s/%s", cfg.DBUser, cfg.DBHost, cfg.DBPort, cfg.DBName)
	db, err := gorm.Open(postgres.Open(cfg.GetDSN()), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Create migrator
	migrator := migration.NewMigrator(db)

	// Verify only mode
	if verifyOnly {
		log.Println("========== Verifying Migration ==========")
		status, err := migrator.VerifyMigration(nil, nil)
		if err != nil {
			log.Fatalf("Verification failed: %v", err)
		}
		log.Println(status.Summary())

		if !status.IsComplete() {
			os.Exit(1)
		}
		if status.HasIssues() {
			log.Println()
			log.Println("⚠️  Migration has integrity issues")
			os.Exit(1)
		}
		return
	}

	// Run migration
	opts := &migration.MigrateOptions{
		DropTables: dropTables,
		Verbose:    true,
	}

	if err := migrator.Migrate(opts); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	log.Println()
	log.Println("✅ Database migration completed successfully!")
}

func printUsage() {
	log.Println("Database Migration Tool")
	log.Println()
	log.Println("Usage:")
	log.Println("  go run cmd/migrate-db/main.go [options]")
	log.Println()
	log.Println("Options:")
	log.Println("  --drop        Drop existing tables before migration")
	log.Println("  --verify      Only verify migration status without making changes")
	log.Println("  --help, -h    Show this help message")
	log.Println()
	log.Println("Examples:")
	log.Println("  # Fresh migration (drop and recreate)")
	log.Println("  go run cmd/migrate-db/main.go --drop")
	log.Println()
	log.Println("  # Incremental migration (create missing tables/indexes)")
	log.Println("  go run cmd/migrate-db/main.go")
	log.Println()
	log.Println("  # Verify migration status")
	log.Println("  go run cmd/migrate-db/main.go --verify")
	log.Println()
	log.Println("Note: For SQL schema export, use pg_dump:")
	log.Println("  pg_dump -h localhost -U postgres -d isdict --schema-only > schema.sql")
}

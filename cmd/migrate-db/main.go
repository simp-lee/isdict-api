package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/simp-lee/isdict-api/internal/applog"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/migration"
	"github.com/simp-lee/isdict-data/postgresutil"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

const verifyScopeDescription = "migration-managed tables and indexes tracked by isdict-commons"
const advisoryVerifyScopeDescription = "reference SQL snapshot"
const requiredExtensionName = postgresutil.RequiredExtensionName
const dropConfirmationExampleTarget = "postgres@db.example.com:5432/isdict"

var baseReferenceIndexes = []string{
	"idx_pronunciation_primary_unique",
	"idx_word_variant_unique",
	"idx_words_headword_normalized",
	"idx_words_cefr_level",
	"idx_words_cet_level",
	"idx_words_oxford_level",
	"idx_words_school_level",
	"idx_words_frequency_rank",
	"idx_words_collins_stars",
	"idx_pronunciations_word_id",
	"idx_senses_word_id",
	"idx_examples_sense_id",
	"idx_word_variants_variant_text",
	"idx_word_variants_headword_normalized",
	"idx_word_variants_word_id",
	"idx_word_variants_frequency_rank",
}

var trigramReferenceIndexes = []string{
	"idx_words_headword_trgm",
	"idx_words_phrase_lower_trgm",
	"idx_word_variants_headword_trgm",
	"idx_word_variants_phrase_lower_trgm",
}

// migrate-db is a standalone tool for database migration
// Usage: go run cmd/migrate-db/main.go [--drop] [--verify]
func main() {
	os.Exit(run())
}

func run() int {
	return runWithArgs(os.Args[1:], os.Stdout, os.Stderr)
}

type runOptions struct {
	dropTables    bool
	verifyOnly    bool
	forceDrop     bool
	confirmTarget string
}

func runWithArgs(args []string, stdout io.Writer, stderr io.Writer) int {
	return runWithDependencies(
		args,
		stdout,
		stderr,
		config.Load,
		applog.NewBootstrap,
		applog.NewConfigured,
		openDatabase,
		func(db *gorm.DB) migrationRunner { return migration.NewMigrator(db) },
		ensureRequiredExtensionsEnabled,
		verifyRequiredExtensionPresent,
		verifyRequiredReferenceIndexes,
	)
}

type migrationRunner interface {
	VerifyMigration(opts *migration.MigrateOptions, skippedIndexes []string) (*migration.MigrationStatus, error)
	Migrate(opts *migration.MigrateOptions) error
}

type databaseHandle struct {
	db    *gorm.DB
	close func() error
}

func runWithDependencies(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
	loadConfig func() (*config.Config, error),
	newBootstrapLogger func() (applog.ManagedLogger, error),
	newConfiguredLogger func(*config.Config) (applog.ManagedLogger, error),
	openDB func(*config.Config) (*databaseHandle, error),
	newMigrator func(*gorm.DB) migrationRunner,
	ensureRequiredExtensions func(*gorm.DB) error,
	verifyRequiredExtension func(*gorm.DB) error,
	verifyIndexes func(*gorm.DB) error,
) int {
	opts, exitCode, stop := parseArgs(args, stdout, stderr)
	if stop {
		return exitCode
	}

	_, restoreDefaultLogger, err := initializeBootstrapLogger(newBootstrapLogger, stderr)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "failed to initialize bootstrap logger: %v\n", err)
		return 1
	}
	defer restoreDefaultLogger()

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		return 1
	}

	if err := validateDropRequest(opts, cfg); err != nil {
		_, _ = fmt.Fprintf(stderr, "error: %v\n\n", err)
		printUsage(stderr)
		return 1
	}

	log, cleanupAppLogger, err := initializeAppLogger(cfg, newConfiguredLogger, stderr)
	if err != nil {
		slog.Error("failed to initialize logger", "error", err)
		return 1
	}
	defer cleanupAppLogger()

	log.Info("logger initialized", "log_level", applog.NormalizeLevel(cfg.LogLevel))

	// Connect to database
	log.Info("connecting to database",
		"db_user", cfg.DBUser,
		"db_host", cfg.DBHost,
		"db_port", cfg.DBPort,
		"db_name", cfg.DBName,
	)
	database, err := openDB(cfg)
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		return 1
	}
	defer closeDatabaseHandle(log, database)

	migrator := newMigrator(database.db)
	if opts.verifyOnly {
		return runVerificationMode(log, database.db, migrator, verifyRequiredExtension, verifyIndexes)
	}
	return runMigrationMode(log, database.db, migrator, opts, ensureRequiredExtensions, verifyIndexes)
}

func initializeBootstrapLogger(newBootstrapLogger func() (applog.ManagedLogger, error), stderr io.Writer) (applog.ManagedLogger, func(), error) {
	bootstrapLogger, err := newBootstrapLogger()
	if err != nil {
		return nil, nil, err
	}
	previousDefaultLogger := slog.Default()
	slog.SetDefault(bootstrapLogger.With("phase", "bootstrap"))
	cleanup := func() {
		slog.SetDefault(previousDefaultLogger)
		applog.Cleanup(bootstrapLogger, stderr)
	}
	return bootstrapLogger, cleanup, nil
}

func initializeAppLogger(cfg *config.Config, newConfiguredLogger func(*config.Config) (applog.ManagedLogger, error), stderr io.Writer) (*slog.Logger, func(), error) {
	appLogger, err := newConfiguredLogger(cfg)
	if err != nil {
		return nil, nil, err
	}
	log := appLogger.With(
		"service", "isdict-migrate-db",
		"gin_mode", cfg.GinMode,
		"log_output", applog.NormalizeOutput(cfg.LogOutput),
	)
	slog.SetDefault(log)
	return log, func() { applog.Cleanup(appLogger, stderr) }, nil
}

func closeDatabaseHandle(log *slog.Logger, database *databaseHandle) {
	if database == nil || database.close == nil {
		return
	}
	if err := database.close(); err != nil {
		log.Error("failed to close database instance", "error", err)
	}
}

func runVerificationMode(log *slog.Logger, db *gorm.DB, migrator migrationRunner, verifyRequiredExtension func(*gorm.DB) error, verifyIndexes func(*gorm.DB) error) int {
	if err := verifyRequiredExtension(db); err != nil {
		log.Error("required extension verification failed", "error", err, "scope", verifyScopeDescription)
		return 1
	}

	log.Info("verifying core migration objects", "scope", verifyScopeDescription)
	status, err := migrator.VerifyMigration(nil, nil)
	if err != nil {
		log.Error("core migration verification failed", "error", err, "scope", verifyScopeDescription)
		return 1
	}
	logVerificationResult(log, "core migration verification completed", status)
	if err := requireSuccessfulVerification(status); err != nil {
		log.Error("core migration verification did not satisfy success criteria", "error", err, "scope", verifyScopeDescription)
		return 1
	}
	reportVerificationAdvisories(log, status, db, verifyIndexes)
	return 0
}

func runMigrationMode(log *slog.Logger, db *gorm.DB, migrator migrationRunner, opts runOptions, ensureRequiredExtensions func(*gorm.DB) error, verifyIndexes func(*gorm.DB) error) int {
	if err := ensureRequiredExtensions(db); err != nil {
		log.Error("required extension setup failed", "error", err, "scope", verifyScopeDescription)
		return 1
	}

	migrateOpts := &migration.MigrateOptions{DropTables: opts.dropTables, Verbose: true}
	log.Info("running migration", "drop_tables", migrateOpts.DropTables, "verbose", migrateOpts.Verbose)
	if err := migrator.Migrate(migrateOpts); err != nil {
		log.Error("migration failed", "error", err)
		return 1
	}

	status, err := migrator.VerifyMigration(nil, nil)
	if err != nil {
		log.Error("post-migration verification failed", "error", err, "scope", verifyScopeDescription)
		return 1
	}
	logVerificationResult(log, "post-migration verification completed", status)
	if err := requireSuccessfulVerification(status); err != nil {
		log.Error("post-migration verification did not satisfy success criteria", "error", err, "scope", verifyScopeDescription)
		return 1
	}
	reportVerificationAdvisories(log, status, db, verifyIndexes)
	log.Info("database migration completed successfully")
	return 0
}

func logVerificationResult(log *slog.Logger, message string, status *migration.MigrationStatus) {
	log.Info(message,
		"summary", status.Summary(),
		"complete", status.IsComplete(),
		"has_issues", status.HasIssues(),
		"scope", verifyScopeDescription,
	)
}

func requireSuccessfulVerification(status *migration.MigrationStatus) error {
	if status == nil {
		return fmt.Errorf("verification returned nil status")
	}
	if !status.IsComplete() {
		return fmt.Errorf("verification is incomplete")
	}
	return nil
}

func reportVerificationAdvisories(log *slog.Logger, status *migration.MigrationStatus, db *gorm.DB, verifyIndexes func(*gorm.DB) error) {
	if status != nil && status.HasIssues() {
		log.Warn("migration verification reported advisory integrity issues",
			"issues", status.Issues,
			"impact", "commons verification remains authoritative; generic issues are advisory unless migration-managed objects are incomplete",
			"scope", verifyScopeDescription,
		)
	}

	if verifyIndexes == nil {
		return
	}

	if err := verifyIndexes(db); err != nil {
		log.Warn("reference SQL snapshot differs from database",
			"error", err,
			"impact", "db/*.sql remains reference-only; isdict-commons migration is the authoritative success criterion",
			"scope", advisoryVerifyScopeDescription,
		)
	}
}

func ensureRequiredExtensionsEnabled(db *gorm.DB) error {
	return postgresutil.EnsureRequiredExtensionsEnabled(db)
}

func verifyRequiredExtensionPresent(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is nil")
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("open sql database handle: %w", err)
	}

	return postgresutil.CheckRequiredExtensionPresent(context.Background(), sqlDB)
}

func verifyRequiredReferenceIndexes(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is nil")
	}

	indexNames, err := listPublicIndexNames(db)
	if err != nil {
		return fmt.Errorf("list public indexes: %w", err)
	}

	missing := missingRequiredIndexes(indexNames, requiredReferenceIndexes())
	if len(missing) > 0 {
		return fmt.Errorf("missing required reference indexes: %s", strings.Join(missing, ", "))
	}

	return nil
}

func requiredReferenceIndexes() []string {
	required := append([]string{}, baseReferenceIndexes...)
	return append(required, trigramReferenceIndexes...)
}

func listPublicIndexNames(db *gorm.DB) ([]string, error) {
	rows, err := db.Raw("SELECT indexname FROM pg_indexes WHERE schemaname = current_schema()").Rows()
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	indexNames := make([]string, 0, len(baseReferenceIndexes)+len(trigramReferenceIndexes))
	for rows.Next() {
		var indexName string
		if err := rows.Scan(&indexName); err != nil {
			return nil, err
		}
		indexNames = append(indexNames, indexName)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return indexNames, nil
}

func missingRequiredIndexes(existing []string, required []string) []string {
	existingSet := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		existingSet[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
	}

	missing := make([]string, 0)
	for _, name := range required {
		normalized := strings.ToLower(strings.TrimSpace(name))
		if _, ok := existingSet[normalized]; ok {
			continue
		}
		missing = append(missing, name)
	}

	return missing
}

func openDatabase(cfg *config.Config) (*databaseHandle, error) {
	db, err := gorm.Open(postgres.Open(cfg.GetDSN()), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	return &databaseHandle{
		db: db,
		close: func() error {
			return sqlDB.Close()
		},
	}, nil
}

func dropConfirmationTarget(cfg *config.Config) (string, error) {
	host := strings.TrimSpace(cfg.DBHost)
	port := strings.TrimSpace(cfg.DBPort)
	user := strings.TrimSpace(cfg.DBUser)
	name := strings.TrimSpace(cfg.DBName)

	if host == "" || port == "" || user == "" || name == "" {
		return "", fmt.Errorf("DB_HOST, DB_PORT, DB_USER, and DB_NAME must all be set for destructive confirmation")
	}

	return fmt.Sprintf("%s@%s:%s/%s", user, host, port, name), nil
}

func validateDropRequest(opts runOptions, cfg *config.Config) error {
	if !opts.dropTables {
		return nil
	}

	target, err := dropConfirmationTarget(cfg)
	if err != nil {
		return err
	}

	if !opts.forceDrop {
		return fmt.Errorf("--drop requires --force and --confirm-drop %q", target)
	}

	if strings.TrimSpace(opts.confirmTarget) != target {
		return fmt.Errorf("--confirm-drop must exactly match target %q", target)
	}

	return nil
}

func parseArgs(args []string, stdout io.Writer, stderr io.Writer) (runOptions, int, bool) {
	var opts runOptions
	flagSet := flag.NewFlagSet("migrate-db", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	flagSet.BoolVar(&opts.dropTables, "drop", false, "Drop existing tables before migration")
	flagSet.BoolVar(&opts.verifyOnly, "verify", false, "Only verify migration-managed objects owned by isdict-commons without making changes")
	flagSet.BoolVar(&opts.forceDrop, "force", false, "Acknowledge the destructive --drop operation (required with --drop)")
	flagSet.StringVar(&opts.confirmTarget, "confirm-drop", "", "Exact target fingerprint required with --drop (format: user@host:port/db)")

	if err := flagSet.Parse(args); err != nil {
		if err == flag.ErrHelp {
			printUsage(stdout)
			return runOptions{}, 0, true
		}
		_, _ = fmt.Fprintf(stderr, "error: %v\n\n", err)
		printUsage(stderr)
		return runOptions{}, 1, true
	}

	if flagSet.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "error: unexpected arguments: %v\n\n", flagSet.Args())
		printUsage(stderr)
		return runOptions{}, 1, true
	}

	if opts.dropTables && opts.verifyOnly {
		_, _ = fmt.Fprintln(stderr, "error: --drop and --verify cannot be used together")
		_, _ = fmt.Fprintln(stderr)
		printUsage(stderr)
		return runOptions{}, 1, true
	}

	if !opts.dropTables && opts.forceDrop {
		_, _ = fmt.Fprintln(stderr, "error: --force can only be used together with --drop")
		_, _ = fmt.Fprintln(stderr)
		printUsage(stderr)
		return runOptions{}, 1, true
	}

	if !opts.dropTables && strings.TrimSpace(opts.confirmTarget) != "" {
		_, _ = fmt.Fprintln(stderr, "error: --confirm-drop can only be used together with --drop")
		_, _ = fmt.Fprintln(stderr)
		printUsage(stderr)
		return runOptions{}, 1, true
	}

	return opts, 0, false
}

func printUsage(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Database Migration Tool")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  go run cmd/migrate-db/main.go [options]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Options:")
	_, _ = fmt.Fprintln(w, "  --drop                    Drop existing tables before migration")
	_, _ = fmt.Fprintln(w, "  --force                   Acknowledge the destructive --drop operation")
	_, _ = fmt.Fprintln(w, "  --confirm-drop <target>   Exact target fingerprint required with --drop (user@host:port/db)")
	_, _ = fmt.Fprintln(w, "  --verify                  Only verify core migration objects without making changes")
	_, _ = fmt.Fprintln(w, "  --help, -h                Show this help message")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Examples:")
	_, _ = fmt.Fprintln(w, "  # Fresh migration (drop and recreate, requires two confirmations)")
	_, _ = fmt.Fprintf(w, "  go run cmd/migrate-db/main.go --drop --force --confirm-drop %s\n", dropConfirmationExampleTarget)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Incremental migration (create missing tables/indexes)")
	_, _ = fmt.Fprintln(w, "  go run cmd/migrate-db/main.go")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "  # Verify migration-managed objects owned by isdict-commons")
	_, _ = fmt.Fprintln(w, "  go run cmd/migrate-db/main.go --verify")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Note: For SQL schema export, use pg_dump:")
	_, _ = fmt.Fprintln(w, "  pg_dump -h db.example.com -U postgres -d isdict --schema-only > schema.sql")
}

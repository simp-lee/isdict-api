package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simp-lee/isdict-api/internal/applog"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/migration"
	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const migrationTestDSNEnv = "TEST_POSTGRES_DSN"
const migrationTestAdminDSNEnv = "TEST_POSTGRES_ADMIN_DSN"
const defaultFixtureDropTarget = "postgres@localhost:5432/isdict"
const allowNonLocalPostgresTestsEnv = "ISDICT_ALLOW_NONLOCAL_TEST_POSTGRES"

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		wantOptions    runOptions
		wantExitCode   int
		wantStop       bool
		wantStdoutText string
		wantStderrText string
	}{
		{
			name:        "no flags",
			args:        nil,
			wantStop:    false,
			wantOptions: runOptions{},
		},
		{
			name:        "drop flag",
			args:        []string{"--drop"},
			wantStop:    false,
			wantOptions: runOptions{dropTables: true},
		},
		{
			name:        "drop confirmation flags",
			args:        []string{"--drop", "--force", "--confirm-drop", dropConfirmationExampleTarget},
			wantStop:    false,
			wantOptions: runOptions{dropTables: true, forceDrop: true, confirmTarget: dropConfirmationExampleTarget},
		},
		{
			name:        "verify flag",
			args:        []string{"--verify"},
			wantStop:    false,
			wantOptions: runOptions{verifyOnly: true},
		},
		{
			name:           "help exits cleanly",
			args:           []string{"--help"},
			wantExitCode:   0,
			wantStop:       true,
			wantStdoutText: "Database Migration Tool",
		},
		{
			name:           "unknown flag fails closed",
			args:           []string{"--verfiy"},
			wantExitCode:   1,
			wantStop:       true,
			wantStderrText: "flag provided but not defined: -verfiy",
		},
		{
			name:           "positional args rejected",
			args:           []string{"unexpected"},
			wantExitCode:   1,
			wantStop:       true,
			wantStderrText: "unexpected arguments",
		},
		{
			name:           "drop and verify conflict",
			args:           []string{"--drop", "--verify"},
			wantExitCode:   1,
			wantStop:       true,
			wantStderrText: "--drop and --verify cannot be used together",
		},
		{
			name:           "force without drop",
			args:           []string{"--force"},
			wantExitCode:   1,
			wantStop:       true,
			wantStderrText: "--force can only be used together with --drop",
		},
		{
			name:           "confirm without drop",
			args:           []string{"--confirm-drop", dropConfirmationExampleTarget},
			wantExitCode:   1,
			wantStop:       true,
			wantStderrText: "--confirm-drop can only be used together with --drop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			gotOptions, gotExitCode, gotStop := parseArgs(tt.args, &stdout, &stderr)

			if gotOptions != tt.wantOptions {
				t.Fatalf("parseArgs() options = %+v, want %+v", gotOptions, tt.wantOptions)
			}
			if gotExitCode != tt.wantExitCode {
				t.Fatalf("parseArgs() exitCode = %d, want %d", gotExitCode, tt.wantExitCode)
			}
			if gotStop != tt.wantStop {
				t.Fatalf("parseArgs() stop = %v, want %v", gotStop, tt.wantStop)
			}
			if tt.wantStdoutText != "" && !strings.Contains(stdout.String(), tt.wantStdoutText) {
				t.Fatalf("stdout = %q, want substring %q", stdout.String(), tt.wantStdoutText)
			}
			if tt.wantStderrText != "" && !strings.Contains(stderr.String(), tt.wantStderrText) {
				t.Fatalf("stderr = %q, want substring %q", stderr.String(), tt.wantStderrText)
			}
		})
	}
}

func TestValidateManagedPostgresDSNForDestructiveTests(t *testing.T) {
	tests := []struct {
		name          string
		dsn           string
		allowNonLocal bool
		wantErr       bool
	}{
		{
			name:    "keyword localhost host",
			dsn:     "host=localhost user=postgres password=postgres dbname=isdict sslmode=disable",
			wantErr: false,
		},
		{
			name:    "url loopback host",
			dsn:     "postgres://postgres:postgres@127.0.0.1:5432/isdict?sslmode=disable",
			wantErr: false,
		},
		{
			name:    "url unix socket host query",
			dsn:     "postgres:///isdict?host=/var/run/postgresql&user=postgres&sslmode=disable",
			wantErr: false,
		},
		{
			name:    "keyword remote host",
			dsn:     "host=103.197.25.147 user=postgres password=postgres dbname=isdict sslmode=disable",
			wantErr: true,
		},
		{
			name:    "url remote host",
			dsn:     "postgres://postgres:postgres@db.example.com:5432/isdict?sslmode=disable",
			wantErr: true,
		},
		{
			name:          "keyword remote host with explicit override",
			dsn:           "host=103.197.25.147 user=postgres password=postgres dbname=isdict sslmode=disable",
			allowNonLocal: true,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := validateManagedPostgresDSNForDestructiveTestsForHostPolicy(tt.dsn, tt.allowNonLocal)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateManagedPostgresDSNForDestructiveTests() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunWithArgs_InvalidFlagsExitBeforeBootstrap(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithArgs([]string{"--unknown"}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("runWithArgs() = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "flag provided but not defined: -unknown") {
		t.Fatalf("stderr = %q, want unknown-flag error", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
}

func TestRunWithDependencies_DropRequiresSecondConfirmation(t *testing.T) {
	fixtures := newRunFixture()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDependencies(
		[]string{"--drop"},
		&stdout,
		&stderr,
		fixtures.loadConfig,
		fixtures.newBootstrapLogger,
		fixtures.newConfiguredLogger,
		fixtures.openDB,
		fixtures.newMigrator,
		fixtures.ensureRequiredExtensions,
		fixtures.verifyRequiredExtension,
		fixtures.verifyIndexes,
	)

	if exitCode != 1 {
		t.Fatalf("runWithDependencies() = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), fmt.Sprintf("--drop requires --force and --confirm-drop %q", defaultFixtureDropTarget)) {
		t.Fatalf("stderr = %q, want drop confirmation error", stderr.String())
	}
	if fixtures.openDBCalls != 0 {
		t.Fatalf("openDBCalls = %d, want 0", fixtures.openDBCalls)
	}
	if fixtures.migrator.migrateCalls != 0 {
		t.Fatalf("migrateCalls = %d, want 0", fixtures.migrator.migrateCalls)
	}
	if fixtures.configuredLogger.syncCalls != 0 || fixtures.configuredLogger.closeCalls != 0 {
		t.Fatalf("configured logger should not be created on validation failure, got sync=%d close=%d", fixtures.configuredLogger.syncCalls, fixtures.configuredLogger.closeCalls)
	}
}

func TestRunWithDependencies_DropRunsMigrationWithConfirmedDestructiveOptions(t *testing.T) {
	fixtures := newRunFixture()
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDependencies(
		[]string{"--drop", "--force", "--confirm-drop", defaultFixtureDropTarget},
		&stdout,
		&stderr,
		fixtures.loadConfig,
		fixtures.newBootstrapLogger,
		fixtures.newConfiguredLogger,
		fixtures.openDB,
		fixtures.newMigrator,
		fixtures.ensureRequiredExtensions,
		fixtures.verifyRequiredExtension,
		fixtures.verifyIndexes,
	)

	if exitCode != 0 {
		t.Fatalf("runWithDependencies() = %d, want 0; stderr = %s", exitCode, stderr.String())
	}
	if fixtures.migrator.migrateCalls != 1 {
		t.Fatalf("migrateCalls = %d, want 1", fixtures.migrator.migrateCalls)
	}
	if fixtures.migrator.migrateOpts == nil || !fixtures.migrator.migrateOpts.DropTables {
		t.Fatalf("migrateOpts = %#v, want DropTables=true", fixtures.migrator.migrateOpts)
	}
	if fixtures.migrator.verifyCalls != 1 {
		t.Fatalf("verifyCalls = %d, want 1", fixtures.migrator.verifyCalls)
	}
	if fixtures.openDBCalls != 1 || fixtures.closeCalls != 1 {
		t.Fatalf("openDBCalls/closeCalls = %d/%d, want 1/1", fixtures.openDBCalls, fixtures.closeCalls)
	}
	assertLoggerCleanedUp(t, fixtures)
}

func TestRunWithDependencies_DropConfirmationBindsToConfiguredTargetInstance(t *testing.T) {
	fixtures := newRunFixture()
	fixtures.loadConfig = func() (*config.Config, error) {
		return &config.Config{
			DBHost:    "db.internal",
			DBPort:    "5432",
			DBUser:    "postgres",
			DBName:    "isdict",
			DBSSLMode: "disable",
			GinMode:   "test",
			LogLevel:  "info",
			LogOutput: "stdout",
		}, nil
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDependencies(
		[]string{"--drop", "--force", "--confirm-drop", defaultFixtureDropTarget},
		&stdout,
		&stderr,
		fixtures.loadConfig,
		fixtures.newBootstrapLogger,
		fixtures.newConfiguredLogger,
		fixtures.openDB,
		fixtures.newMigrator,
		fixtures.ensureRequiredExtensions,
		fixtures.verifyRequiredExtension,
		fixtures.verifyIndexes,
	)

	if exitCode != 1 {
		t.Fatalf("runWithDependencies() = %d, want 1", exitCode)
	}
	if !strings.Contains(stderr.String(), "--confirm-drop must exactly match target \"postgres@db.internal:5432/isdict\"") {
		t.Fatalf("stderr = %q, want target-instance mismatch error", stderr.String())
	}
	if fixtures.openDBCalls != 0 {
		t.Fatalf("openDBCalls = %d, want 0", fixtures.openDBCalls)
	}
	if fixtures.migrator.migrateCalls != 0 {
		t.Fatalf("migrateCalls = %d, want 0", fixtures.migrator.migrateCalls)
	}
	if fixtures.configuredLogger.syncCalls != 0 || fixtures.configuredLogger.closeCalls != 0 {
		t.Fatalf("configured logger should not be created on validation failure, got sync=%d close=%d", fixtures.configuredLogger.syncCalls, fixtures.configuredLogger.closeCalls)
	}
}

func TestRunWithDependencies_VerifyExitCodes(t *testing.T) {
	tests := []struct {
		name            string
		status          *migration.MigrationStatus
		verifyErr       error
		verifyExtErr    error
		wantExitCode    int
		wantVerifyCalls int
	}{
		{
			name: "complete verification succeeds",
			status: &migration.MigrationStatus{
				Tables:     []string{"words", "word_variants", "pronunciations", "senses", "examples"},
				Indexes:    []string{"idx_pronunciation_primary_unique", "idx_word_variant_unique"},
				Extensions: []string{"pg_trgm"},
			},
			wantExitCode:    0,
			wantVerifyCalls: 1,
		},
		{
			name: "incomplete verification fails",
			status: &migration.MigrationStatus{
				Tables:        []string{"words"},
				MissingTables: []string{"senses"},
			},
			wantExitCode:    1,
			wantVerifyCalls: 1,
		},
		{
			name: "integrity issues are advisory by default",
			status: &migration.MigrationStatus{
				Tables:     []string{"words", "word_variants", "pronunciations", "senses", "examples"},
				Indexes:    []string{"idx_pronunciation_primary_unique", "idx_word_variant_unique"},
				Extensions: []string{"pg_trgm"},
				Issues:     []string{"duplicate indexes"},
			},
			wantExitCode:    0,
			wantVerifyCalls: 1,
		},
		{
			name:            "required extension verification failure fails before verification",
			verifyExtErr:    errors.New("required extension pg_trgm is not enabled"),
			wantExitCode:    1,
			wantVerifyCalls: 0,
		},
		{
			name:            "verification error fails",
			verifyErr:       errors.New("verify failed"),
			wantExitCode:    1,
			wantVerifyCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixtures := newRunFixture()
			fixtures.migrator.verifyStatus = tt.status
			fixtures.migrator.verifyErr = tt.verifyErr
			fixtures.verifyRequiredExtensionErr = tt.verifyExtErr
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			exitCode := runWithDependencies(
				[]string{"--verify"},
				&stdout,
				&stderr,
				fixtures.loadConfig,
				fixtures.newBootstrapLogger,
				fixtures.newConfiguredLogger,
				fixtures.openDB,
				fixtures.newMigrator,
				fixtures.ensureRequiredExtensions,
				fixtures.verifyRequiredExtension,
				fixtures.verifyIndexes,
			)

			if exitCode != tt.wantExitCode {
				t.Fatalf("runWithDependencies() = %d, want %d; stderr = %s", exitCode, tt.wantExitCode, stderr.String())
			}
			if fixtures.migrator.verifyCalls != tt.wantVerifyCalls {
				t.Fatalf("verifyCalls = %d, want %d", fixtures.migrator.verifyCalls, tt.wantVerifyCalls)
			}
			if len(fixtures.migrator.verifySkippedIndexes) != 0 {
				t.Fatalf("verifySkippedIndexes = %v, want none", fixtures.migrator.verifySkippedIndexes)
			}
			if fixtures.migrator.migrateCalls != 0 {
				t.Fatalf("migrateCalls = %d, want 0", fixtures.migrator.migrateCalls)
			}
			if fixtures.ensureRequiredExtensionsCalls != 0 {
				t.Fatalf("ensureRequiredExtensionsCalls = %d, want 0", fixtures.ensureRequiredExtensionsCalls)
			}
			wantVerifyExtensionCalls := 1
			if tt.verifyExtErr != nil {
				wantVerifyExtensionCalls = 1
			}
			if fixtures.verifyRequiredExtensionCalls != wantVerifyExtensionCalls {
				t.Fatalf("verifyRequiredExtensionCalls = %d, want %d", fixtures.verifyRequiredExtensionCalls, wantVerifyExtensionCalls)
			}
			if fixtures.openDBCalls != 1 || fixtures.closeCalls != 1 {
				t.Fatalf("openDBCalls/closeCalls = %d/%d, want 1/1", fixtures.openDBCalls, fixtures.closeCalls)
			}
			assertLoggerCleanedUp(t, fixtures)
		})
	}
}

func TestRunWithDependencies_MigrationErrorReturnsNonZero(t *testing.T) {
	fixtures := newRunFixture()
	fixtures.migrator.migrateErr = errors.New("migration failed")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDependencies(
		nil,
		&stdout,
		&stderr,
		fixtures.loadConfig,
		fixtures.newBootstrapLogger,
		fixtures.newConfiguredLogger,
		fixtures.openDB,
		fixtures.newMigrator,
		fixtures.ensureRequiredExtensions,
		fixtures.verifyRequiredExtension,
		fixtures.verifyIndexes,
	)

	if exitCode != 1 {
		t.Fatalf("runWithDependencies() = %d, want 1", exitCode)
	}
	if fixtures.migrator.migrateCalls != 1 {
		t.Fatalf("migrateCalls = %d, want 1", fixtures.migrator.migrateCalls)
	}
	if fixtures.ensureRequiredExtensionsCalls != 1 {
		t.Fatalf("ensureRequiredExtensionsCalls = %d, want 1", fixtures.ensureRequiredExtensionsCalls)
	}
	if fixtures.verifyRequiredExtensionCalls != 0 {
		t.Fatalf("verifyRequiredExtensionCalls = %d, want 0", fixtures.verifyRequiredExtensionCalls)
	}
	if fixtures.openDBCalls != 1 || fixtures.closeCalls != 1 {
		t.Fatalf("openDBCalls/closeCalls = %d/%d, want 1/1", fixtures.openDBCalls, fixtures.closeCalls)
	}
	assertLoggerCleanedUp(t, fixtures)
}

func TestRunWithDependencies_MigrationFailsWhenRequiredExtensionSetupFails(t *testing.T) {
	fixtures := newRunFixture()
	fixtures.ensureRequiredExtensionsErr = errors.New("enable required extension pg_trgm: permission denied")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDependencies(
		nil,
		&stdout,
		&stderr,
		fixtures.loadConfig,
		fixtures.newBootstrapLogger,
		fixtures.newConfiguredLogger,
		fixtures.openDB,
		fixtures.newMigrator,
		fixtures.ensureRequiredExtensions,
		fixtures.verifyRequiredExtension,
		fixtures.verifyIndexes,
	)

	if exitCode != 1 {
		t.Fatalf("runWithDependencies() = %d, want 1", exitCode)
	}
	if fixtures.migrator.migrateCalls != 0 {
		t.Fatalf("migrateCalls = %d, want 0", fixtures.migrator.migrateCalls)
	}
	if fixtures.migrator.verifyCalls != 0 {
		t.Fatalf("verifyCalls = %d, want 0", fixtures.migrator.verifyCalls)
	}
	if fixtures.ensureRequiredExtensionsCalls != 1 {
		t.Fatalf("ensureRequiredExtensionsCalls = %d, want 1", fixtures.ensureRequiredExtensionsCalls)
	}
	if fixtures.verifyRequiredExtensionCalls != 0 {
		t.Fatalf("verifyRequiredExtensionCalls = %d, want 0", fixtures.verifyRequiredExtensionCalls)
	}
	if len(fixtures.migrator.verifySkippedIndexes) != 0 {
		t.Fatalf("verifySkippedIndexes = %v, want none", fixtures.migrator.verifySkippedIndexes)
	}
	if fixtures.openDBCalls != 1 || fixtures.closeCalls != 1 {
		t.Fatalf("openDBCalls/closeCalls = %d/%d, want 1/1", fixtures.openDBCalls, fixtures.closeCalls)
	}
	assertLoggerCleanedUp(t, fixtures)
}

func TestRunWithDependencies_VerifyIgnoresMissingReferenceIndexesByDefault(t *testing.T) {
	fixtures := newRunFixture()
	fixtures.verifyIndexesErr = errors.New("missing required reference indexes: idx_words_frequency_rank")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDependencies(
		[]string{"--verify"},
		&stdout,
		&stderr,
		fixtures.loadConfig,
		fixtures.newBootstrapLogger,
		fixtures.newConfiguredLogger,
		fixtures.openDB,
		fixtures.newMigrator,
		fixtures.ensureRequiredExtensions,
		fixtures.verifyRequiredExtension,
		fixtures.verifyIndexes,
	)

	if exitCode != 0 {
		t.Fatalf("runWithDependencies() = %d, want 0", exitCode)
	}
	if fixtures.migrator.verifyCalls != 1 {
		t.Fatalf("verifyCalls = %d, want 1", fixtures.migrator.verifyCalls)
	}
	if fixtures.ensureRequiredExtensionsCalls != 0 {
		t.Fatalf("ensureRequiredExtensionsCalls = %d, want 0", fixtures.ensureRequiredExtensionsCalls)
	}
	if fixtures.verifyRequiredExtensionCalls != 1 {
		t.Fatalf("verifyRequiredExtensionCalls = %d, want 1", fixtures.verifyRequiredExtensionCalls)
	}
	if fixtures.openDBCalls != 1 || fixtures.closeCalls != 1 {
		t.Fatalf("openDBCalls/closeCalls = %d/%d, want 1/1", fixtures.openDBCalls, fixtures.closeCalls)
	}
	assertLoggerCleanedUp(t, fixtures)
}

func TestRunWithDependencies_MigrationIgnoresMissingReferenceIndexesByDefault(t *testing.T) {
	fixtures := newRunFixture()
	fixtures.verifyIndexesErr = errors.New("missing required reference indexes: idx_words_frequency_rank")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithDependencies(
		nil,
		&stdout,
		&stderr,
		fixtures.loadConfig,
		fixtures.newBootstrapLogger,
		fixtures.newConfiguredLogger,
		fixtures.openDB,
		fixtures.newMigrator,
		fixtures.ensureRequiredExtensions,
		fixtures.verifyRequiredExtension,
		fixtures.verifyIndexes,
	)

	if exitCode != 0 {
		t.Fatalf("runWithDependencies() = %d, want 0", exitCode)
	}
	if fixtures.migrator.migrateCalls != 1 {
		t.Fatalf("migrateCalls = %d, want 1", fixtures.migrator.migrateCalls)
	}
	if fixtures.migrator.verifyCalls != 1 {
		t.Fatalf("verifyCalls = %d, want 1", fixtures.migrator.verifyCalls)
	}
	if fixtures.ensureRequiredExtensionsCalls != 1 {
		t.Fatalf("ensureRequiredExtensionsCalls = %d, want 1", fixtures.ensureRequiredExtensionsCalls)
	}
	if fixtures.verifyRequiredExtensionCalls != 0 {
		t.Fatalf("verifyRequiredExtensionCalls = %d, want 0", fixtures.verifyRequiredExtensionCalls)
	}
	if fixtures.openDBCalls != 1 || fixtures.closeCalls != 1 {
		t.Fatalf("openDBCalls/closeCalls = %d/%d, want 1/1", fixtures.openDBCalls, fixtures.closeCalls)
	}
	assertLoggerCleanedUp(t, fixtures)
}

func TestRunWithDependencies_RestoresDefaultLoggerAcrossRepeatedRuns(t *testing.T) {
	originalDefault := slog.New(slog.NewTextHandler(io.Discard, nil))
	previousDefault := slog.Default()
	slog.SetDefault(originalDefault)
	defer slog.SetDefault(previousDefault)

	for runIndex := 0; runIndex < 2; runIndex++ {
		fixtures := newRunFixture()
		fixtures.migrator.migrateErr = errors.New("migration failed")
		var stdout bytes.Buffer
		var stderr bytes.Buffer

		exitCode := runWithDependencies(
			nil,
			&stdout,
			&stderr,
			fixtures.loadConfig,
			fixtures.newBootstrapLogger,
			fixtures.newConfiguredLogger,
			fixtures.openDB,
			fixtures.newMigrator,
			fixtures.ensureRequiredExtensions,
			fixtures.verifyRequiredExtension,
			fixtures.verifyIndexes,
		)

		if exitCode != 1 {
			t.Fatalf("runWithDependencies() run %d = %d, want %d", runIndex+1, exitCode, 1)
		}
		if slog.Default() != originalDefault {
			t.Fatalf("default logger after run %d = %p, want %p", runIndex+1, slog.Default(), originalDefault)
		}
	}
}

func TestEnsureRequiredExtensionsEnabled(t *testing.T) {
	t.Run("pg_trgm enabled succeeds", func(t *testing.T) {
		db := setupExtensionListDB(t, []string{"pg_trgm"})
		if err := ensureRequiredExtensionsEnabled(db); err != nil {
			t.Fatalf("ensureRequiredExtensionsEnabled() error = %v", err)
		}
	})

	t.Run("nil database fails", func(t *testing.T) {
		err := ensureRequiredExtensionsEnabled(nil)
		if err == nil || !strings.Contains(err.Error(), "database handle is nil") {
			t.Fatalf("ensureRequiredExtensionsEnabled() error = %v, want database handle is nil", err)
		}
	})
}

func TestVerifyRequiredExtensionPresent(t *testing.T) {
	t.Run("pg_trgm enabled succeeds", func(t *testing.T) {
		db := setupExtensionListDB(t, []string{"pg_trgm"})
		if err := verifyRequiredExtensionPresent(db); err != nil {
			t.Fatalf("verifyRequiredExtensionPresent() error = %v", err)
		}
	})

	t.Run("nil database fails", func(t *testing.T) {
		err := verifyRequiredExtensionPresent(nil)
		if err == nil || !strings.Contains(err.Error(), "database handle is nil") {
			t.Fatalf("verifyRequiredExtensionPresent() error = %v, want database handle is nil", err)
		}
	})
}

func TestRunWithDependencies_FailsWithoutPgTrgmPrivileges(t *testing.T) {
	restrictedDSN := createRestrictedTestDatabase(t, "verify_user", "verify_fixture")

	loadRestrictedConfig := func() (*config.Config, error) {
		return &config.Config{
			DBHost:    "db.example.com",
			DBPort:    "5432",
			DBUser:    "verify_user",
			DBName:    "verify_fixture",
			DBSSLMode: "require",
			GinMode:   "test",
			LogLevel:  "info",
			LogOutput: "stdout",
		}, nil
	}

	newStubLogger := func() (applog.ManagedLogger, error) {
		return newStubManagedLogger(), nil
	}

	openRestrictedDB := func(*config.Config) (*databaseHandle, error) {
		return openDatabaseHandleForDSN(restrictedDSN)
	}

	exitCode := runWithDependencies(
		nil,
		io.Discard,
		io.Discard,
		loadRestrictedConfig,
		newStubLogger,
		func(*config.Config) (applog.ManagedLogger, error) { return newStubManagedLogger(), nil },
		openRestrictedDB,
		func(db *gorm.DB) migrationRunner { return migration.NewMigrator(db) },
		ensureRequiredExtensionsEnabled,
		verifyRequiredExtensionPresent,
		nil,
	)
	if exitCode != 1 {
		t.Fatalf("runWithDependencies(migrate) = %d, want 1", exitCode)
	}

	exitCode = runWithDependencies(
		[]string{"--verify"},
		io.Discard,
		io.Discard,
		loadRestrictedConfig,
		newStubLogger,
		func(*config.Config) (applog.ManagedLogger, error) { return newStubManagedLogger(), nil },
		openRestrictedDB,
		func(db *gorm.DB) migrationRunner { return migration.NewMigrator(db) },
		ensureRequiredExtensionsEnabled,
		verifyRequiredExtensionPresent,
		nil,
	)
	if exitCode != 1 {
		t.Fatalf("runWithDependencies(--verify) = %d, want 1", exitCode)
	}

	fixtureDB, err := gorm.Open(pgdriver.Open(restrictedDSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() fixture error = %v", err)
	}
	fixtureSQLDB, err := fixtureDB.DB()
	if err != nil {
		t.Fatalf("fixtureDB.DB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = fixtureSQLDB.Close()
	})

	assertExtensionNotEnabled(t, fixtureSQLDB, "pg_trgm")
	assertTableNotExists(t, fixtureSQLDB, "words")
}

func TestMissingRequiredIndexes(t *testing.T) {
	missing := missingRequiredIndexes(
		[]string{" IDX_WORDS_HEADWORD_NORMALIZED ", "idx_words_cefr_level", "idx_word_variants_word_id"},
		[]string{"idx_words_headword_normalized", "idx_words_frequency_rank", "idx_word_variants_word_id"},
	)

	if len(missing) != 1 {
		t.Fatalf("len(missing) = %d, want 1", len(missing))
	}
	if missing[0] != "idx_words_frequency_rank" {
		t.Fatalf("missing[0] = %q, want %q", missing[0], "idx_words_frequency_rank")
	}
}

func TestRequiredReferenceIndexes(t *testing.T) {
	required := requiredReferenceIndexes()
	for _, name := range []string{
		"idx_pronunciations_word_id",
		"idx_senses_word_id",
		"idx_examples_sense_id",
	} {
		if !containsString(required, name) {
			t.Fatalf("requiredReferenceIndexes() missing %s", name)
		}
	}
	for _, name := range trigramReferenceIndexes {
		if !containsString(required, name) {
			t.Fatalf("requiredReferenceIndexes() missing %s", name)
		}
	}
}

// AC-B2.5: No code should attempt to treat GIN as a PostgreSQL extension.
func TestRequiredExtensionNameIsPgTrgmOnly(t *testing.T) {
	if requiredExtensionName != "pg_trgm" {
		t.Fatalf("requiredExtensionName = %q, want %q", requiredExtensionName, "pg_trgm")
	}
	if strings.EqualFold(requiredExtensionName, "gin") {
		t.Fatalf("requiredExtensionName must not model gin as an extension")
	}
}

func TestReferenceSQLSnapshotsStayCompatible(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "db", "schema.sql")
	indexesPath := filepath.Join("..", "..", "db", "indexes.sql")
	sampleDataPath := filepath.Join("..", "..", "db", "sample_data.sql")

	schemaSQL := mustReadFile(t, schemaPath)
	indexesSQL := mustReadFile(t, indexesPath)
	sampleDataSQL := mustReadFile(t, sampleDataPath)
	assertReferenceSnapshotCore(t, schemaSQL, indexesSQL, sampleDataSQL)
	assertReferenceSnapshotSnippets(t, sampleDataSQL, indexesSQL, schemaSQL)
	assertReferenceSnapshotDropDocs(t, schemaSQL, indexesSQL)
}

// AC-B2.5: No code should attempt to treat GIN as a PostgreSQL extension.
func TestReferenceSQLSnapshotsDoNotMentionGinAsExtension(t *testing.T) {
	snapshots := strings.ToLower(strings.Join([]string{
		mustReadFile(t, filepath.Join("..", "..", "db", "schema.sql")),
		mustReadFile(t, filepath.Join("..", "..", "db", "indexes.sql")),
		mustReadFile(t, filepath.Join("..", "..", "db", "sample_data.sql")),
	}, "\n"))

	for _, forbidden := range []string{
		"create extension gin",
		"create extension if not exists gin",
		"extname = 'gin'",
		"extname='gin'",
		"required extension gin",
	} {
		if strings.Contains(snapshots, forbidden) {
			t.Fatalf("reference SQL snapshots must not model gin as an extension: found %q", forbidden)
		}
	}
}

func assertReferenceSnapshotCore(t *testing.T, schemaSQL, indexesSQL, sampleDataSQL string) {
	t.Helper()
	if strings.Contains(schemaSQL, "UNIQUE (word_id, variant_text, kind, COALESCE(form_type, 0))") {
		t.Fatalf("schema snapshot still contains invalid table-level expression unique constraint")
	}
	if !strings.Contains(indexesSQL, "CREATE UNIQUE INDEX IF NOT EXISTS idx_word_variant_unique") {
		t.Fatalf("indexes snapshot must remain the authoritative unique expression definition")
	}
	if strings.Contains(sampleDataSQL, "DO UPDATE") {
		t.Fatalf("sample data snapshot must fail closed instead of overwriting existing dictionary rows")
	}
	if !strings.Contains(sampleDataSQL, "WITH sample_senses (headword, pos, definition_en, definition_zh, sense_order, cefr_level, cefr_source, oxford_level) AS (") {
		t.Fatalf("sample data snapshot must define sense-level cefr_source fixture data")
	}
	if !strings.Contains(sampleDataSQL, "INSERT INTO senses (word_id, pos, definition_en, definition_zh, sense_order, cefr_level, cefr_source, oxford_level)") {
		t.Fatalf("sample data snapshot must insert sense-level cefr_source")
	}
	if !strings.Contains(sampleDataSQL, "Load this file only into an empty disposable database") {
		t.Fatalf("sample data snapshot must document the empty disposable database requirement")
	}
	if !strings.Contains(schemaSQL, "COMMENT ON COLUMN word_variants.form_type IS 'Form type: 1=past, 2=past_participle, 3=present_3rd, 4=gerund, 5=plural, etc.';") {
		t.Fatalf("schema snapshot must document authoritative form_type semantics")
	}
}

func assertReferenceSnapshotSnippets(t *testing.T, sampleDataSQL, indexesSQL, schemaSQL string) {
	t.Helper()
	assertSnapshotContainsAll(t, sampleDataSQL, []string{
		"('run', 'ran', 'ran', 1, 1, 1024, 38765)",
		"('run', 'runs', 'runs', 1, 3, 756, 52341)",
		"('program', 'programs', 'programs', 1, 5, 1567, 23456)",
	}, "sample data snapshot missing expected form_type mapping: %s")
	assertSnapshotContainsAll(t, indexesSQL, []string{
		"idx_words_headword_normalized",
		"idx_words_frequency_rank",
		"idx_pronunciations_word_id",
		"idx_senses_word_id",
		"idx_examples_sense_id",
		"idx_word_variants_variant_text",
		"idx_word_variants_word_id",
	}, "indexes snapshot missing major index %s")
	assertSnapshotContainsAll(t, schemaSQL+"\n"+indexesSQL, []string{
		"CREATE EXTENSION IF NOT EXISTS pg_trgm",
		"CREATE INDEX IF NOT EXISTS idx_words_headword_trgm ON words USING gin(headword_normalized gin_trgm_ops)",
		"CREATE INDEX IF NOT EXISTS idx_word_variants_headword_trgm ON word_variants USING gin(headword_normalized gin_trgm_ops)",
	}, "reference SQL snapshots missing required pg_trgm snippet %q")
	assertSnapshotContainsAll(t, indexesSQL, []string{
		"idx_pronunciations_word_id",
		"idx_senses_word_id",
		"idx_examples_sense_id",
	}, "indexes snapshot should include foreign-key index %s")
	assertSnapshotContainsAll(t, indexesSQL, []string{
		"• Unique indexes: 2 (data integrity)",
		"• Non-unique B-tree indexes: 14 (exact/prefix/range/filter/join queries)",
		"• GIN trigram indexes: 4 (fuzzy search optimization)",
		"• Total: 20 indexes when pg_trgm is enabled",
	}, "indexes snapshot missing expected summary line %q")
}

func assertReferenceSnapshotDropDocs(t *testing.T, schemaSQL, indexesSQL string) {
	t.Helper()
	confirmedDropCommand := fmt.Sprintf("go run cmd/migrate-db/main.go --drop --force --confirm-drop %s", dropConfirmationExampleTarget)
	if !strings.Contains(schemaSQL, confirmedDropCommand) {
		t.Fatalf("schema snapshot header must document confirmed drop syntax")
	}
	if !strings.Contains(indexesSQL, confirmedDropCommand) {
		t.Fatalf("indexes snapshot header must document confirmed drop syntax")
	}
	if strings.Contains(schemaSQL, "go run cmd/migrate-db/main.go --drop") && !strings.Contains(schemaSQL, confirmedDropCommand) {
		t.Fatalf("schema snapshot header still references obsolete drop syntax")
	}
	if strings.Contains(indexesSQL, "go run cmd/migrate-db/main.go --drop") && !strings.Contains(indexesSQL, confirmedDropCommand) {
		t.Fatalf("indexes snapshot header still references obsolete drop syntax")
	}
}

func assertSnapshotContainsAll(t *testing.T, content string, snippets []string, message string) {
	t.Helper()
	for _, snippet := range snippets {
		if !strings.Contains(content, snippet) {
			t.Fatalf(message, snippet)
		}
	}
}

func TestMakefileRequiresFixtureConfirmation(t *testing.T) {
	makefile := mustReadFile(t, filepath.Join("..", "..", "Makefile"))

	for _, snippet := range []string{
		"CONFIRM_FIXTURES",
		"error: set CONFIRM_FIXTURES to $$EXPECTED_CONFIRM_VALUE",
		"error: CONFIRM_FIXTURES must exactly match $$EXPECTED_CONFIRM_VALUE",
		"db-fixtures only runs against an empty dictionary dataset",
		"SELECT EXISTS (SELECT 1 FROM words LIMIT 1)",
	} {
		if !strings.Contains(makefile, snippet) {
			t.Fatalf("Makefile is missing fixture confirmation guard snippet %q", snippet)
		}
	}
}

func TestSampleDataSnapshotFailsOnNonEmptyDatabase(t *testing.T) {
	dsn := createAdminOwnedTestDatabase(t, "sample_data_fail_closed")

	gormDB, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("gormDB.DB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	for _, path := range []string{
		filepath.Join("..", "..", "db", "schema.sql"),
		filepath.Join("..", "..", "db", "indexes.sql"),
		filepath.Join("..", "..", "db", "sample_data.sql"),
	} {
		executeSQLFile(t, sqlDB, path)
	}

	err = executeSQLFileWithError(sqlDB, filepath.Join("..", "..", "db", "sample_data.sql"))
	if err == nil {
		t.Fatal("expected second sample_data import to fail on non-empty database")
	}
	if !strings.Contains(err.Error(), "duplicate key value") {
		t.Fatalf("second sample_data import error = %v, want duplicate key failure", err)
	}
}

func TestReferenceSQLSnapshotsExecuteAgainstPostgres(t *testing.T) {
	dsn := createAdminOwnedTestDatabase(t, "sql_snapshots")

	gormDB, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	sqlDB, err := gormDB.DB()
	if err != nil {
		t.Fatalf("gormDB.DB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	for _, path := range []string{
		filepath.Join("..", "..", "db", "schema.sql"),
		filepath.Join("..", "..", "db", "indexes.sql"),
		filepath.Join("..", "..", "db", "sample_data.sql"),
	} {
		executeSQLFile(t, sqlDB, path)
	}

	assertTableRowCount(t, sqlDB, "words", 10)
	assertTableRowCount(t, sqlDB, "pronunciations", 10)
	assertTableRowCount(t, sqlDB, "senses", 7)
	assertTableRowCount(t, sqlDB, "examples", 7)
	assertTableRowCount(t, sqlDB, "word_variants", 9)
	assertExtensionEnabled(t, sqlDB, "pg_trgm")
	assertIndexExists(t, sqlDB, "idx_word_variant_unique")
	assertIndexExists(t, sqlDB, "idx_words_headword_normalized")
	assertIndexExists(t, sqlDB, "idx_words_frequency_rank")
	assertIndexExists(t, sqlDB, "idx_words_headword_trgm")
	assertIndexExists(t, sqlDB, "idx_word_variants_variant_text")
	assertIndexExists(t, sqlDB, "idx_word_variants_word_id")
	assertIndexExists(t, sqlDB, "idx_pronunciations_word_id")
	assertIndexExists(t, sqlDB, "idx_senses_word_id")
	assertIndexExists(t, sqlDB, "idx_examples_sense_id")
	assertWordVariantFormType(t, sqlDB, "ran", 1)
	assertWordVariantFormType(t, sqlDB, "learned", 2)
	assertWordVariantFormType(t, sqlDB, "runs", 3)
	assertWordVariantFormType(t, sqlDB, "running", 4)
	assertWordVariantFormType(t, sqlDB, "programs", 5)
	assertSenseCEFRSource(t, sqlDB, "hello", 9, 1, "oxford")
	assertSenseCEFRSource(t, sqlDB, "program", 2, 2, "oxford")
}

func TestReferenceSQLSnapshotsFailWithoutPgTrgmPrivileges(t *testing.T) {
	fixtureDSN := createRestrictedTestDatabase(t, "fixture_user", "fixture_sql_setup")

	fixtureDB, err := gorm.Open(pgdriver.Open(fixtureDSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() fixture error = %v", err)
	}

	fixtureSQLDB, err := fixtureDB.DB()
	if err != nil {
		t.Fatalf("fixtureDB.DB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = fixtureSQLDB.Close()
	})

	schemaPath := filepath.Join("..", "..", "db", "schema.sql")
	if err := executeSQLFileWithError(fixtureSQLDB, schemaPath); err == nil {
		t.Fatalf("executeSQLFile(%s) unexpectedly succeeded without pg_trgm privileges", schemaPath)
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("executeSQLFile(%s) error = %v, want permission denied", schemaPath, err)
	}

	assertExtensionNotEnabled(t, fixtureSQLDB, "pg_trgm")
	assertTableNotExists(t, fixtureSQLDB, "words")
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func executeSQLFile(t *testing.T, db *sql.DB, path string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	statements := splitSQLStatements(string(content))
	var tx *sql.Tx

	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}

		switch {
		case strings.EqualFold(trimmed, "BEGIN"):
			if tx != nil {
				t.Fatalf("nested BEGIN in %s", path)
			}
			tx, err = db.Begin()
			if err != nil {
				t.Fatalf("begin transaction for %s: %v", path, err)
			}
		case strings.EqualFold(trimmed, "COMMIT"):
			if tx == nil {
				t.Fatalf("COMMIT without active transaction in %s", path)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("commit transaction for %s: %v", path, err)
			}
			tx = nil
		default:
			if tx != nil {
				if _, err := tx.Exec(statement); err != nil {
					_ = tx.Rollback()
					t.Fatalf("execute statement from %s: %v\nstatement:\n%s", path, err, trimmed)
				}
				continue
			}
			if _, err := db.Exec(statement); err != nil {
				t.Fatalf("execute statement from %s: %v\nstatement:\n%s", path, err, trimmed)
			}
		}
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit trailing transaction for %s: %v", path, err)
		}
	}
}

func executeSQLFileWithError(db *sql.DB, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	statements := splitSQLStatements(string(content))
	var tx *sql.Tx

	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}

		switch {
		case strings.EqualFold(trimmed, "BEGIN"):
			if tx != nil {
				return fmt.Errorf("nested BEGIN in %s", path)
			}
			tx, err = db.Begin()
			if err != nil {
				return fmt.Errorf("begin transaction for %s: %w", path, err)
			}
		case strings.EqualFold(trimmed, "COMMIT"):
			if tx == nil {
				return fmt.Errorf("COMMIT without active transaction in %s", path)
			}
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit transaction for %s: %w", path, err)
			}
			tx = nil
		default:
			executor := interface {
				Exec(query string, args ...any) (sql.Result, error)
			}(db)
			if tx != nil {
				executor = tx
			}
			if _, err := executor.Exec(trimmed); err != nil {
				if tx != nil {
					_ = tx.Rollback()
				}
				return fmt.Errorf("execute statement from %s: %w", path, err)
			}
		}
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit transaction for %s: %w", path, err)
		}
	}

	return nil
}

func splitSQLStatements(content string) []string {
	statements := make([]string, 0, 64)
	var current strings.Builder
	state := sqlSplitState{}

	for index := 0; index < len(content); index++ {
		char := content[index]
		var next byte
		if index+1 < len(content) {
			next = content[index+1]
		}
		if state.consumeComment(&current, char, next, &index) {
			continue
		}
		if state.consumeDollarQuote(&current, content, &index, char) {
			continue
		}
		if state.startComment(&current, char, next, &index) {
			continue
		}
		if state.toggleSingleQuote(&current, char, next, &index) {
			continue
		}
		if state.startDollarQuote(&current, content, &index, char) {
			continue
		}

		if char == ';' && !state.inSingleQuote {
			statements = append(statements, current.String())
			current.Reset()
			continue
		}

		current.WriteByte(char)
	}

	if strings.TrimSpace(current.String()) != "" {
		statements = append(statements, current.String())
	}

	return statements
}

type sqlSplitState struct {
	inSingleQuote  bool
	inLineComment  bool
	inBlockComment bool
	dollarQuoteTag string
}

func (state *sqlSplitState) consumeComment(current *strings.Builder, char, next byte, index *int) bool {
	if state.inLineComment {
		current.WriteByte(char)
		if char == '\n' {
			state.inLineComment = false
		}
		return true
	}
	if !state.inBlockComment {
		return false
	}
	current.WriteByte(char)
	if char == '*' && next == '/' {
		current.WriteByte(next)
		*index += 1
		state.inBlockComment = false
	}
	return true
}

func (state *sqlSplitState) consumeDollarQuote(current *strings.Builder, content string, index *int, char byte) bool {
	if state.dollarQuoteTag == "" {
		return false
	}
	if strings.HasPrefix(content[*index:], state.dollarQuoteTag) {
		current.WriteString(state.dollarQuoteTag)
		*index += len(state.dollarQuoteTag) - 1
		state.dollarQuoteTag = ""
		return true
	}
	current.WriteByte(char)
	return true
}

func (state *sqlSplitState) startComment(current *strings.Builder, char, next byte, index *int) bool {
	if state.inSingleQuote {
		return false
	}
	if char == '-' && next == '-' {
		current.WriteByte(char)
		current.WriteByte(next)
		*index += 1
		state.inLineComment = true
		return true
	}
	if char == '/' && next == '*' {
		current.WriteByte(char)
		current.WriteByte(next)
		*index += 1
		state.inBlockComment = true
		return true
	}
	return false
}

func (state *sqlSplitState) toggleSingleQuote(current *strings.Builder, char, next byte, index *int) bool {
	if char != '\'' {
		return false
	}
	current.WriteByte(char)
	if state.inSingleQuote && next == '\'' {
		current.WriteByte(next)
		*index += 1
		return true
	}
	state.inSingleQuote = !state.inSingleQuote
	return true
}

func (state *sqlSplitState) startDollarQuote(current *strings.Builder, content string, index *int, char byte) bool {
	if state.inSingleQuote || char != '$' {
		return false
	}
	tag, ok := readDollarQuoteTag(content[*index:])
	if !ok {
		return false
	}
	current.WriteString(tag)
	*index += len(tag) - 1
	state.dollarQuoteTag = tag
	return true
}

func readDollarQuoteTag(content string) (string, bool) {
	if content == "" || content[0] != '$' {
		return "", false
	}

	for index := 1; index < len(content); index++ {
		char := content[index]
		if char == '$' {
			return content[:index+1], true
		}
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return "", false
		}
	}

	return "", false
}

func assertTableRowCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()

	var got int
	query := "SELECT COUNT(*) FROM " + table
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s row count = %d, want %d", table, got, want)
	}
}

func assertExtensionEnabled(t *testing.T, db *sql.DB, extension string) {
	t.Helper()

	var exists bool
	if err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)", extension).Scan(&exists); err != nil {
		t.Fatalf("check extension %s: %v", extension, err)
	}
	if !exists {
		t.Fatalf("expected extension %s to be enabled", extension)
	}
}

func assertExtensionNotEnabled(t *testing.T, db *sql.DB, extension string) {
	t.Helper()

	var exists bool
	if err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = $1)", extension).Scan(&exists); err != nil {
		t.Fatalf("check extension %s: %v", extension, err)
	}
	if exists {
		t.Fatalf("expected extension %s to be disabled", extension)
	}
}

func assertIndexExists(t *testing.T, db *sql.DB, index string) {
	t.Helper()

	var exists bool
	if err := db.QueryRow("SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1)", index).Scan(&exists); err != nil {
		t.Fatalf("check index %s: %v", index, err)
	}
	if !exists {
		t.Fatalf("expected index %s to exist", index)
	}
}

func assertWordVariantFormType(t *testing.T, db *sql.DB, variantText string, want int) {
	t.Helper()

	var got int
	if err := db.QueryRow("SELECT form_type FROM word_variants WHERE variant_text = $1", variantText).Scan(&got); err != nil {
		t.Fatalf("query form_type for variant %s: %v", variantText, err)
	}
	if got != want {
		t.Fatalf("variant %s form_type = %d, want %d", variantText, got, want)
	}
}

func assertSenseCEFRSource(t *testing.T, db *sql.DB, headword string, pos int, senseOrder int, want string) {
	t.Helper()

	var got string
	if err := db.QueryRow(
		`SELECT s.cefr_source
		FROM senses s
		JOIN words w ON w.id = s.word_id
		WHERE w.headword = $1 AND s.pos = $2 AND s.sense_order = $3`,
		headword,
		pos,
		senseOrder,
	).Scan(&got); err != nil {
		t.Fatalf("query cefr_source for %s/%d/%d: %v", headword, pos, senseOrder, err)
	}
	if got != want {
		t.Fatalf("sense cefr_source for %s/%d/%d = %q, want %q", headword, pos, senseOrder, got, want)
	}
}

func assertTableNotExists(t *testing.T, db *sql.DB, table string) {
	t.Helper()

	var exists bool
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1)`, table).Scan(&exists); err != nil {
		t.Fatalf("query table %s existence: %v", table, err)
	}
	if exists {
		t.Fatalf("table %s unexpectedly exists", table)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type runFixture struct {
	bootstrapLogger               *stubManagedLogger
	configuredLogger              *stubManagedLogger
	migrator                      *fakeMigrator
	loadConfig                    func() (*config.Config, error)
	ensureRequiredExtensionsErr   error
	verifyRequiredExtensionErr    error
	verifyIndexesErr              error
	ensureRequiredExtensionsCalls int
	verifyRequiredExtensionCalls  int
	openDBCalls                   int
	closeCalls                    int
}

func newRunFixture() *runFixture {
	fixture := &runFixture{
		bootstrapLogger:  newStubManagedLogger(),
		configuredLogger: newStubManagedLogger(),
		migrator: &fakeMigrator{verifyStatus: &migration.MigrationStatus{
			Tables:     []string{"words", "word_variants", "pronunciations", "senses", "examples"},
			Indexes:    []string{"idx_pronunciation_primary_unique", "idx_word_variant_unique", "idx_words_headword_trgm", "idx_words_phrase_lower_trgm", "idx_word_variants_headword_trgm", "idx_word_variants_phrase_lower_trgm"},
			Extensions: []string{"pg_trgm"},
		}},
	}
	fixture.loadConfig = func() (*config.Config, error) {
		return &config.Config{
			DBHost:    "localhost",
			DBPort:    "5432",
			DBUser:    "postgres",
			DBName:    "isdict",
			DBSSLMode: "disable",
			GinMode:   "test",
			LogLevel:  "info",
			LogOutput: "stdout",
		}, nil
	}
	return fixture
}

func (f *runFixture) newBootstrapLogger() (applog.ManagedLogger, error) {
	return f.bootstrapLogger, nil
}

func (f *runFixture) newConfiguredLogger(*config.Config) (applog.ManagedLogger, error) {
	return f.configuredLogger, nil
}

func (f *runFixture) openDB(*config.Config) (*databaseHandle, error) {
	f.openDBCalls++
	return &databaseHandle{
		db: &gorm.DB{},
		close: func() error {
			f.closeCalls++
			return nil
		},
	}, nil
}

func (f *runFixture) newMigrator(*gorm.DB) migrationRunner {
	return f.migrator
}

func (f *runFixture) ensureRequiredExtensions(*gorm.DB) error {
	f.ensureRequiredExtensionsCalls++
	return f.ensureRequiredExtensionsErr
}

func (f *runFixture) verifyRequiredExtension(*gorm.DB) error {
	f.verifyRequiredExtensionCalls++
	return f.verifyRequiredExtensionErr
}

func (f *runFixture) verifyIndexes(*gorm.DB) error {
	return f.verifyIndexesErr
}

func assertLoggerCleanedUp(t *testing.T, fixtures *runFixture) {
	t.Helper()
	if fixtures.bootstrapLogger.syncCalls != 1 || fixtures.bootstrapLogger.closeCalls != 1 {
		t.Fatalf("bootstrap cleanup = sync %d close %d, want 1/1", fixtures.bootstrapLogger.syncCalls, fixtures.bootstrapLogger.closeCalls)
	}
	if fixtures.configuredLogger.syncCalls != 1 || fixtures.configuredLogger.closeCalls != 1 {
		t.Fatalf("configured cleanup = sync %d close %d, want 1/1", fixtures.configuredLogger.syncCalls, fixtures.configuredLogger.closeCalls)
	}
}

type fakeMigrator struct {
	verifyStatus         *migration.MigrationStatus
	verifyErr            error
	verifyCalls          int
	verifySkippedIndexes []string
	migrateErr           error
	migrateOpts          *migration.MigrateOptions
	migrateCalls         int
}

func (f *fakeMigrator) VerifyMigration(opts *migration.MigrateOptions, skippedIndexes []string) (*migration.MigrationStatus, error) {
	f.verifyCalls++
	f.verifySkippedIndexes = append([]string(nil), skippedIndexes...)
	if f.verifyStatus == nil || f.verifyErr != nil {
		return f.verifyStatus, f.verifyErr
	}

	statusCopy := *f.verifyStatus
	statusCopy.Tables = append([]string(nil), f.verifyStatus.Tables...)
	statusCopy.MissingTables = append([]string(nil), f.verifyStatus.MissingTables...)
	statusCopy.Indexes = append([]string(nil), f.verifyStatus.Indexes...)
	statusCopy.MissingIndexes = append([]string(nil), f.verifyStatus.MissingIndexes...)
	statusCopy.Extensions = append([]string(nil), f.verifyStatus.Extensions...)
	statusCopy.Issues = append([]string(nil), f.verifyStatus.Issues...)
	statusCopy.SkippedIndexes = append([]string(nil), f.verifyStatus.SkippedIndexes...)

	return &statusCopy, nil
}

func (f *fakeMigrator) Migrate(opts *migration.MigrateOptions) error {
	f.migrateCalls++
	f.migrateOpts = opts
	return f.migrateErr
}

type stubManagedLogger struct {
	base       *slog.Logger
	syncCalls  int
	closeCalls int
}

func newStubManagedLogger() *stubManagedLogger {
	return &stubManagedLogger{base: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func (l *stubManagedLogger) With(args ...any) *slog.Logger {
	return l.base.With(args...)
}

func (l *stubManagedLogger) Sync() error {
	l.syncCalls++
	return nil
}

func (l *stubManagedLogger) Close() error {
	l.closeCalls++
	return nil
}

type postgresDSNConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	SSLMode  string
	Params   map[string]string
}

func requireManagedPostgresDSN(t *testing.T, envName string, purpose string) string {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(envName))
	if dsn == "" {
		t.Skipf("set %s to a disposable PostgreSQL DSN for %s", envName, purpose)
	}
	if err := validateManagedPostgresDSNForDestructiveTests(dsn); err != nil {
		t.Fatalf("unsafe %s for destructive PostgreSQL test setup: %v", envName, err)
	}

	return dsn
}

func validateManagedPostgresDSNForDestructiveTests(dsn string) error {
	return validateManagedPostgresDSNForDestructiveTestsForHostPolicy(dsn, allowNonLocalPostgresTestHosts())
}

func validateManagedPostgresDSNForDestructiveTestsForHostPolicy(dsn string, allowNonLocal bool) error {
	info, err := parsePostgresDSN(dsn)
	if err != nil {
		return err
	}
	if !allowNonLocal && !isLocalPostgresTestHost(info.Host) {
		return fmt.Errorf("destructive PostgreSQL tests only run against localhost, loopback IPs, or unix sockets by default; got host %q (set %s=true to opt in to a disposable non-local PostgreSQL instance)", info.Host, allowNonLocalPostgresTestsEnv)
	}
	return nil
}

func allowNonLocalPostgresTestHosts() bool {
	value := strings.TrimSpace(os.Getenv(allowNonLocalPostgresTestsEnv))
	return strings.EqualFold(value, "1") || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func isLocalPostgresTestHost(host string) bool {
	if host == "" {
		return true
	}

	for _, candidate := range strings.Split(host, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if strings.HasPrefix(candidate, "/") {
			continue
		}
		if strings.EqualFold(candidate, "localhost") {
			continue
		}
		if ip := net.ParseIP(strings.Trim(candidate, "[]")); ip != nil && ip.IsLoopback() {
			continue
		}
		return false
	}

	return true
}

func createAdminOwnedTestDatabase(t *testing.T, prefix string) string {
	t.Helper()

	adminInfo, adminSQLDB := requireAdminPostgres(t)
	databaseName := uniquePostgresIdentifier(prefix)
	if _, err := adminSQLDB.Exec("CREATE DATABASE " + quoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	t.Cleanup(func() {
		terminateDatabaseConnections(t, adminSQLDB, databaseName)
		_, _ = adminSQLDB.Exec("DROP DATABASE IF EXISTS " + quoteIdentifier(databaseName))
	})

	return buildPostgresDSN(adminInfo, databaseName, "", "")
}

func createRestrictedTestDatabase(t *testing.T, rolePrefix string, databasePrefix string) string {
	t.Helper()

	adminInfo, adminSQLDB := requireAdminPostgres(t)
	roleName := uniquePostgresIdentifier(rolePrefix)
	databaseName := uniquePostgresIdentifier(databasePrefix)
	password := "postgres"

	if _, err := adminSQLDB.Exec("CREATE ROLE " + quoteIdentifier(roleName) + " LOGIN PASSWORD '" + password + "' NOSUPERUSER"); err != nil {
		t.Fatalf("create role %s: %v", roleName, err)
	}
	if _, err := adminSQLDB.Exec("CREATE DATABASE " + quoteIdentifier(databaseName) + " OWNER " + quoteIdentifier(roleName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	t.Cleanup(func() {
		terminateDatabaseConnections(t, adminSQLDB, databaseName)
		_, _ = adminSQLDB.Exec("DROP DATABASE IF EXISTS " + quoteIdentifier(databaseName))
		_, _ = adminSQLDB.Exec("DROP ROLE IF EXISTS " + quoteIdentifier(roleName))
	})

	return buildPostgresDSN(adminInfo, databaseName, roleName, password)
}

func requireAdminPostgres(t *testing.T) (postgresDSNConfig, *sql.DB) {
	t.Helper()

	adminDSN := requireManagedPostgresDSN(t, migrationTestAdminDSNEnv, "tests that create databases or roles")
	adminInfo := mustParsePostgresDSN(t, adminDSN)
	adminDB, err := gorm.Open(pgdriver.Open(adminDSN), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() admin error = %v", err)
	}

	adminSQLDB, err := adminDB.DB()
	if err != nil {
		t.Fatalf("adminDB.DB() error = %v", err)
	}
	t.Cleanup(func() {
		_ = adminSQLDB.Close()
	})

	return adminInfo, adminSQLDB
}

func mustParsePostgresDSN(t *testing.T, dsn string) postgresDSNConfig {
	t.Helper()

	config, err := parsePostgresDSN(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL DSN: %v", err)
	}
	return config
}

func parsePostgresDSN(dsn string) (postgresDSNConfig, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsedURL, err := url.Parse(dsn)
		if err != nil {
			return postgresDSNConfig{}, err
		}
		password, _ := parsedURL.User.Password()
		params := map[string]string{}
		for key, values := range parsedURL.Query() {
			if len(values) > 0 {
				params[key] = values[0]
			}
		}
		host := parsedURL.Query().Get("host")
		if host == "" {
			host = parsedURL.Hostname()
		}
		return postgresDSNConfig{
			Host:     host,
			Port:     parsedURL.Port(),
			User:     parsedURL.User.Username(),
			Password: password,
			Database: strings.TrimPrefix(parsedURL.Path, "/"),
			SSLMode:  params["sslmode"],
			Params:   params,
		}, nil
	}

	config := postgresDSNConfig{Params: map[string]string{}}
	for _, field := range strings.Fields(dsn) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
		switch key {
		case "host":
			config.Host = value
		case "port":
			config.Port = value
		case "user":
			config.User = value
		case "password":
			config.Password = value
		case "dbname":
			config.Database = value
		case "sslmode":
			config.SSLMode = value
		default:
			config.Params[key] = value
		}
	}

	if config.Host == "" || config.User == "" || config.Database == "" {
		return postgresDSNConfig{}, fmt.Errorf("unsupported PostgreSQL DSN for test helpers")
	}
	return config, nil
}

func buildPostgresDSN(info postgresDSNConfig, databaseName string, user string, password string) string {
	if databaseName == "" {
		databaseName = info.Database
	}
	if user == "" {
		user = info.User
	}
	if password == "" {
		password = info.Password
	}

	host := info.Host
	if info.Port != "" {
		host = net.JoinHostPort(info.Host, info.Port)
	}

	query := url.Values{}
	for key, value := range info.Params {
		if key == "sslmode" {
			continue
		}
		query.Set(key, value)
	}
	if info.SSLMode != "" {
		query.Set("sslmode", info.SSLMode)
	}

	parsedURL := &url.URL{
		Scheme: "postgres",
		Host:   host,
		Path:   "/" + databaseName,
		User:   url.UserPassword(user, password),
	}
	if len(query) > 0 {
		parsedURL.RawQuery = query.Encode()
	}

	return parsedURL.String()
}

func uniquePostgresIdentifier(prefix string) string {
	cleaned := strings.ToLower(prefix)
	cleaned = strings.NewReplacer("/", "_", "-", "_", " ", "_").Replace(cleaned)
	cleaned = strings.Trim(cleaned, "_")
	if cleaned == "" {
		cleaned = "isdict"
	}
	return fmt.Sprintf("%s_%d", cleaned, time.Now().UnixNano())
}

func quoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func terminateDatabaseConnections(t *testing.T, adminSQLDB *sql.DB, databaseName string) {
	t.Helper()

	if _, err := adminSQLDB.Exec(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
		databaseName,
	); err != nil {
		t.Logf("terminate connections for %s: %v", databaseName, err)
	}
}

func openDatabaseHandleForDSN(dsn string) (*databaseHandle, error) {
	db, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{})
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

func setupExtensionListDB(t *testing.T, extensions []string) *gorm.DB {
	t.Helper()
	dsn := requireManagedPostgresDSN(t, migrationTestDSNEnv, "extension verification tests")

	db, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}

	if len(extensions) == 0 {
		return db
	}

	for _, extension := range extensions {
		if err := db.Exec("CREATE EXTENSION IF NOT EXISTS " + extension).Error; err != nil {
			t.Fatalf("create extension %s: %v", extension, err)
		}
	}

	return db
}

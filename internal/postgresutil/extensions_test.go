package postgresutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCheckRequiredExtensionPresent_ReturnsNilWhenPresent(t *testing.T) {
	db, tracker := newExtensionTestDB(t, extensionTestBehavior{present: true})

	if err := CheckRequiredExtensionPresent(context.Background(), db); err != nil {
		t.Fatalf("CheckRequiredExtensionPresent() error = %v", err)
	}
	if tracker.queryCalls != 1 {
		t.Fatalf("queryCalls = %d, want 1", tracker.queryCalls)
	}
	if tracker.execCalls != 0 {
		t.Fatalf("execCalls = %d, want 0", tracker.execCalls)
	}
}

func TestCheckRequiredExtensionPresent_ReturnsErrorWhenMissing(t *testing.T) {
	db, _ := newExtensionTestDB(t, extensionTestBehavior{present: false})

	err := CheckRequiredExtensionPresent(context.Background(), db)
	if err == nil || err.Error() != "required extension pg_trgm is not enabled" {
		t.Fatalf("CheckRequiredExtensionPresent() error = %v, want missing extension error", err)
	}
}

func TestCheckRequiredExtensionPresent_ReturnsQueryError(t *testing.T) {
	db, _ := newExtensionTestDB(t, extensionTestBehavior{queryErr: errors.New("query failed")})

	err := CheckRequiredExtensionPresent(context.Background(), db)
	if err == nil || err.Error() != "check required extension pg_trgm: query failed" {
		t.Fatalf("CheckRequiredExtensionPresent() error = %v, want wrapped query error", err)
	}
}

func TestCheckRequiredExtensionPresent_RejectsNilDB(t *testing.T) {
	err := CheckRequiredExtensionPresent(context.Background(), nil)
	if err == nil || err.Error() != "database handle is nil" {
		t.Fatalf("CheckRequiredExtensionPresent(nil) error = %v, want database handle is nil", err)
	}
}

func TestEnsureRequiredExtensionsEnabled_RejectsNilDB(t *testing.T) {
	err := EnsureRequiredExtensionsEnabled(nil)
	if err == nil || err.Error() != "database handle is nil" {
		t.Fatalf("EnsureRequiredExtensionsEnabled(nil) error = %v, want database handle is nil", err)
	}
}

func TestEnsureRequiredExtensionsEnabled_ReturnsExecError(t *testing.T) {
	db, mock := newExtensionTestGORMDB(t)
	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS pg_trgm").
		WillReturnError(errors.New("permission denied"))

	err := EnsureRequiredExtensionsEnabled(db)
	if err == nil || err.Error() != "enable required extension pg_trgm: permission denied" {
		t.Fatalf("EnsureRequiredExtensionsEnabled() error = %v, want wrapped exec error", err)
	}
}

func TestEnsureRequiredExtensionsEnabled_DoesNotEnumerateExtensionsAfterCreate(t *testing.T) {
	db, mock := newExtensionTestGORMDB(t)
	mock.ExpectExec("CREATE EXTENSION IF NOT EXISTS pg_trgm").
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := EnsureRequiredExtensionsEnabled(db); err != nil {
		t.Fatalf("EnsureRequiredExtensionsEnabled() error = %v", err)
	}
}

type extensionTestBehavior struct {
	present  bool
	queryErr error
	execErr  error
}

type extensionTestTracker struct {
	queryCalls int
	execCalls  int
}

func newExtensionTestDB(t *testing.T, behavior extensionTestBehavior) (*sql.DB, *extensionTestTracker) {
	t.Helper()

	tracker := &extensionTestTracker{}
	driverName := registerExtensionTestDriver(extensionTestDriver{
		behavior: behavior,
		tracker:  tracker,
	})

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	return db, tracker
}

func newExtensionTestGORMDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open() error = %v", err)
	}

	t.Cleanup(func() {
		_ = sqlDB.Close()
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("sql expectations: %v", err)
		}
	})

	return gormDB, mock
}

var extensionTestDriverRegistry struct {
	mu      sync.Mutex
	counter int
}

func registerExtensionTestDriver(drv extensionTestDriver) string {
	extensionTestDriverRegistry.mu.Lock()
	defer extensionTestDriverRegistry.mu.Unlock()

	extensionTestDriverRegistry.counter++
	name := "extension-test-driver-" + string(rune('a'+extensionTestDriverRegistry.counter-1))
	sql.Register(name, drv)
	return name
}

type extensionTestDriver struct {
	behavior extensionTestBehavior
	tracker  *extensionTestTracker
}

func (d extensionTestDriver) Open(string) (driver.Conn, error) {
	return extensionTestConn(d), nil
}

type extensionTestConn struct {
	behavior extensionTestBehavior
	tracker  *extensionTestTracker
}

func (c extensionTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented")
}

func (c extensionTestConn) Close() error {
	return nil
}

func (c extensionTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented")
}

func (c extensionTestConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	c.tracker.queryCalls++
	if c.behavior.queryErr != nil {
		return nil, c.behavior.queryErr
	}
	return &extensionPresenceRows{value: c.behavior.present}, nil
}

func (c extensionTestConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	c.tracker.execCalls++
	if c.behavior.execErr != nil {
		return nil, c.behavior.execErr
	}
	return driver.RowsAffected(0), nil
}

type extensionPresenceRows struct {
	yielded bool
	value   bool
}

func (r *extensionPresenceRows) Columns() []string {
	return []string{"exists"}
}

func (r *extensionPresenceRows) Close() error {
	return nil
}

func (r *extensionPresenceRows) Next(dest []driver.Value) error {
	if r.yielded {
		return io.EOF
	}
	r.yielded = true
	dest[0] = r.value
	return nil
}

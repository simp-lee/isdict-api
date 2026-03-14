package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newMockRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}

	gormDB, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{
		DisableAutomaticPing: true,
		Logger:               logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open gorm db: %v", err)
	}

	t.Cleanup(func() {
		_ = sqlDB.Close()
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("sql expectations: %v", err)
		}
	})

	return &Repository{db: gormDB}, mock
}

func TestGetWordsByHeadwords_PropagatesDeadlineToDB(t *testing.T) {
	repo, mock := newMockRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	mock.ExpectQuery(`SELECT \* FROM "words" WHERE headword_normalized IN \(.*\)`).
		WithArgs("hello", "world").
		WillDelayFor(50 * time.Millisecond).
		WillReturnRows(sqlmock.NewRows([]string{"id", "headword", "headword_normalized"}))

	_, err := repo.GetWordsByHeadwords(ctx, []string{"Hello", "World"}, false, false, false)
	if !errors.Is(err, sqlmock.ErrCancelled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected canceled or deadline-exceeded query error, got %v", err)
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", ctx.Err())
	}
}

func TestSuggestWords_PropagatesDeadlineToRawQuery(t *testing.T) {
	repo, mock := newMockRepository(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	mock.ExpectQuery(`(?s)WITH combined AS .*SELECT id, frequency_rank.*FROM ranked.*LIMIT \$3`).
		WithArgs("air%", "air%", 5).
		WillDelayFor(50 * time.Millisecond).
		WillReturnRows(sqlmock.NewRows([]string{"id", "frequency_rank"}))

	_, err := repo.SuggestWords(ctx, "Air", nil, nil, nil, nil, nil, 5)
	if !errors.Is(err, sqlmock.ErrCancelled) {
		t.Fatalf("expected canceled raw query error, got %v", err)
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("expected context deadline exceeded, got %v", ctx.Err())
	}
}

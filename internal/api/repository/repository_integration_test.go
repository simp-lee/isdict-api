package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/simp-lee/isdict-commons/migration"
	commonmodel "github.com/simp-lee/isdict-commons/model"
	"github.com/simp-lee/isdict-commons/textutil"
	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	postgresIntegrationDSNEnv     = "TEST_POSTGRES_DSN"
	allowNonLocalPostgresTestsEnv = "ISDICT_ALLOW_NONLOCAL_TEST_POSTGRES"
	externalPostgresTestMu        sync.Mutex
	externalPostgresSchema        = map[string]struct {
		initialized bool
		withIndexes bool
	}{}
	postgresTestTablesTruncateSQL = `
		TRUNCATE TABLE
			examples,
			senses,
			pronunciations,
			word_variants,
			words
		RESTART IDENTITY CASCADE
	`
)

func TestIsTransientExternalPostgresResetError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "driver bad connection",
			err:  driver.ErrBadConn,
			want: true,
		},
		{
			name: "wrapped connection reset by peer",
			err: &net.OpError{
				Op:  "read",
				Net: "tcp",
				Err: &os.SyscallError{Syscall: "read", Err: syscall.ECONNRESET},
			},
			want: true,
		},
		{
			name: "broken pipe message",
			err:  errors.New("write tcp 127.0.0.1:5432->127.0.0.1:12345: write: broken pipe"),
			want: true,
		},
		{
			name: "non transient sql error",
			err:  errors.New("ERROR: relation \"missing_table\" does not exist (SQLSTATE 42P01)"),
			want: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := isTransientExternalPostgresResetError(tt.err)
			if got != tt.want {
				t.Fatalf("isTransientExternalPostgresResetError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestValidatePostgresIntegrationDSNForDestructiveSetup(t *testing.T) {
	tests := []struct {
		name          string
		dsn           string
		allowNonLocal bool
		wantErr       bool
	}{
		{
			name:    "keyword localhost host",
			dsn:     "host=localhost user=test password=test dbname=isdict sslmode=disable",
			wantErr: false,
		},
		{
			name:    "url loopback host",
			dsn:     "postgres://test:test@127.0.0.1:5432/isdict?sslmode=disable",
			wantErr: false,
		},
		{
			name:    "keyword unix socket host",
			dsn:     "host=/var/run/postgresql user=test password=test dbname=isdict sslmode=disable",
			wantErr: false,
		},
		{
			name:    "keyword remote host",
			dsn:     "host=103.197.25.147 user=test password=test dbname=isdict sslmode=disable",
			wantErr: true,
		},
		{
			name:    "url remote host",
			dsn:     "postgres://test:test@db.example.com:5432/isdict?sslmode=disable",
			wantErr: true,
		},
		{
			name:          "keyword remote host with explicit override",
			dsn:           "host=103.197.25.147 user=test password=test dbname=isdict sslmode=disable",
			allowNonLocal: true,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := validatePostgresIntegrationDSNForDestructiveSetupForHostPolicy(tt.dsn, tt.allowNonLocal)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validatePostgresIntegrationDSNForDestructiveSetup() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetWordByHeadword_PostgresVariantFallback(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "program",
			HeadwordNormalized: textutil.ToNormalized("program"),
			CEFRLevel:          2,
			OxfordLevel:        1,
			FrequencyRank:      140,
			CollinsStars:       4,
			TranslationZH:      "程序；节目",
		},
		variants: []WordVariant{
			{
				VariantText:        "programmed",
				HeadwordNormalized: textutil.ToNormalized("programmed"),
				Kind:               commonmodel.VariantForm,
				FormType:           intPtr(1),
				FrequencyRank:      240,
			},
		},
	})

	word, variant, err := repo.GetWordByHeadword(ctx, "programmed", false, false, false)
	if err != nil {
		t.Fatalf("GetWordByHeadword() error = %v", err)
	}
	if word == nil {
		t.Fatal("expected main word to be returned")
	}
	if variant == nil {
		t.Fatal("expected matched variant to be returned")
	}
	if word.Headword != "program" {
		t.Fatalf("word.Headword = %q, want %q", word.Headword, "program")
	}
	if variant.VariantText != "programmed" {
		t.Fatalf("variant.VariantText = %q, want %q", variant.VariantText, "programmed")
	}
	if variant.WordID != word.ID {
		t.Fatalf("variant.WordID = %d, want %d", variant.WordID, word.ID)
	}
}

func TestSearchWords_PostgresAppliesFiltersAndDeduplicatesVariantMatches(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "alpha",
			HeadwordNormalized: textutil.ToNormalized("alpha"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      90,
			CollinsStars:       5,
			TranslationZH:      "阿尔法；开始",
		},
		senses: []Sense{
			{POS: 1, DefinitionEN: "the first letter of the Greek alphabet", DefinitionZH: "希腊字母表的第一个字母", SenseOrder: 1},
			{POS: 2, DefinitionEN: "to begin something", DefinitionZH: "开始", SenseOrder: 2},
		},
		variants: []WordVariant{
			{
				VariantText:        "al-pha",
				HeadwordNormalized: textutil.ToNormalized("al-pha"),
				Kind:               commonmodel.VariantAlias,
				FrequencyRank:      90,
			},
		},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "alpine",
			HeadwordNormalized: textutil.ToNormalized("alpine"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      150,
			CollinsStars:       5,
			TranslationZH:      "高山的",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "related to high mountains", DefinitionZH: "与高山有关", SenseOrder: 1}},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "alpaca",
			HeadwordNormalized: textutil.ToNormalized("alpaca"),
			CEFRLevel:          4,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      80,
			CollinsStars:       5,
			TranslationZH:      "羊驼",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a South American animal", DefinitionZH: "一种南美洲动物", SenseOrder: 1}},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "albatross",
			HeadwordNormalized: textutil.ToNormalized("albatross"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        2,
			FrequencyRank:      80,
			CollinsStars:       5,
			TranslationZH:      "信天翁",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a large seabird", DefinitionZH: "大型海鸟", SenseOrder: 1}},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "alloy",
			HeadwordNormalized: textutil.ToNormalized("alloy"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      70,
			CollinsStars:       3,
			TranslationZH:      "合金",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a metal made by combining elements", DefinitionZH: "由多种元素组成的金属", SenseOrder: 1}},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "alter",
			HeadwordNormalized: textutil.ToNormalized("alter"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      60,
			CollinsStars:       5,
			TranslationZH:      "改变",
		},
		senses: []Sense{{POS: 2, DefinitionEN: "to make different", DefinitionZH: "使不同", SenseOrder: 1}},
	})

	pos := 1
	cefrLevel := 2
	oxfordLevel := 1
	cetLevel := 1
	maxFrequencyRank := 100
	minCollinsStars := 4

	words, total, err := repo.SearchWords(ctx, "al", &pos, &cefrLevel, &oxfordLevel, &cetLevel, &maxFrequencyRank, &minCollinsStars, 10, 0)
	if err != nil {
		t.Fatalf("SearchWords() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want %d", total, 1)
	}
	if len(words) != 1 {
		t.Fatalf("len(words) = %d, want %d", len(words), 1)
	}
	if words[0].Headword != "alpha" {
		t.Fatalf("words[0].Headword = %q, want %q", words[0].Headword, "alpha")
	}
	if len(words[0].Senses) != 1 || words[0].Senses[0].POS != pos {
		t.Fatalf("expected filtered POS to be preserved in result, got %#v", words[0].Senses)
	}
}

func TestSearchWords_PostgresAppliesFiltersToVariantOnlyMatches(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "anchor",
			HeadwordNormalized: textutil.ToNormalized("anchor"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      80,
			CollinsStars:       4,
			TranslationZH:      "锚",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a heavy object used to moor a vessel", DefinitionZH: "锚", SenseOrder: 1}},
		variants: []WordVariant{{
			VariantText:        "neo-anchor",
			HeadwordNormalized: textutil.ToNormalized("neo-anchor"),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      80,
		}},
	})

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "harbor",
			HeadwordNormalized: textutil.ToNormalized("harbor"),
			CEFRLevel:          3,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      80,
			CollinsStars:       4,
			TranslationZH:      "港口",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a place on the coast", DefinitionZH: "港口", SenseOrder: 1}},
		variants: []WordVariant{{
			VariantText:        "neo-harbor",
			HeadwordNormalized: textutil.ToNormalized("neo-harbor"),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      80,
		}},
	})

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "berth",
			HeadwordNormalized: textutil.ToNormalized("berth"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        2,
			FrequencyRank:      80,
			CollinsStars:       4,
			TranslationZH:      "停泊处",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a place where a ship lies", DefinitionZH: "泊位", SenseOrder: 1}},
		variants: []WordVariant{{
			VariantText:        "neo-berth",
			HeadwordNormalized: textutil.ToNormalized("neo-berth"),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      80,
		}},
	})

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "pier",
			HeadwordNormalized: textutil.ToNormalized("pier"),
			CEFRLevel:          2,
			CETLevel:           2,
			OxfordLevel:        1,
			FrequencyRank:      80,
			CollinsStars:       4,
			TranslationZH:      "码头",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a platform on piles", DefinitionZH: "码头", SenseOrder: 1}},
		variants: []WordVariant{{
			VariantText:        "neo-pier",
			HeadwordNormalized: textutil.ToNormalized("neo-pier"),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      80,
		}},
	})

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "wharf",
			HeadwordNormalized: textutil.ToNormalized("wharf"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      180,
			CollinsStars:       4,
			TranslationZH:      "货运码头",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a structure beside water", DefinitionZH: "码头", SenseOrder: 1}},
		variants: []WordVariant{{
			VariantText:        "neo-wharf",
			HeadwordNormalized: textutil.ToNormalized("neo-wharf"),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      180,
		}},
	})

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "jetty",
			HeadwordNormalized: textutil.ToNormalized("jetty"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      80,
			CollinsStars:       3,
			TranslationZH:      "防波堤",
		},
		senses: []Sense{{POS: 1, DefinitionEN: "a landing pier", DefinitionZH: "栈桥", SenseOrder: 1}},
		variants: []WordVariant{{
			VariantText:        "neo-jetty",
			HeadwordNormalized: textutil.ToNormalized("neo-jetty"),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      80,
		}},
	})

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "quay",
			HeadwordNormalized: textutil.ToNormalized("quay"),
			CEFRLevel:          2,
			CETLevel:           1,
			OxfordLevel:        1,
			FrequencyRank:      80,
			CollinsStars:       4,
			TranslationZH:      "码头岸壁",
		},
		senses: []Sense{{POS: 2, DefinitionEN: "to dock at a quay", DefinitionZH: "靠码头停泊", SenseOrder: 1}},
		variants: []WordVariant{{
			VariantText:        "neo-quay",
			HeadwordNormalized: textutil.ToNormalized("neo-quay"),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      80,
		}},
	})

	pos := 1
	cefrLevel := 2
	oxfordLevel := 1
	cetLevel := 1
	maxFrequencyRank := 100
	minCollinsStars := 4

	words, total, err := repo.SearchWords(ctx, "neo", &pos, &cefrLevel, &oxfordLevel, &cetLevel, &maxFrequencyRank, &minCollinsStars, 10, 0)
	if err != nil {
		t.Fatalf("SearchWords() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want %d", total, 1)
	}
	if len(words) != 1 {
		t.Fatalf("len(words) = %d, want %d", len(words), 1)
	}
	if words[0].Headword != "anchor" {
		t.Fatalf("words[0].Headword = %q, want %q", words[0].Headword, "anchor")
	}
}

func TestSearchWords_PostgresTreatsPercentAsLiteral(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "100% pure",
			HeadwordNormalized: textutil.ToNormalized("100% pure"),
			FrequencyRank:      30,
			CollinsStars:       4,
			TranslationZH:      "百分之百纯净",
		},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "1000 pure",
			HeadwordNormalized: textutil.ToNormalized("1000 pure"),
			FrequencyRank:      31,
			CollinsStars:       4,
			TranslationZH:      "一千纯净",
		},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "100 percent pure",
			HeadwordNormalized: textutil.ToNormalized("100 percent pure"),
			FrequencyRank:      32,
			CollinsStars:       4,
			TranslationZH:      "百分之百纯净",
		},
	})

	words, total, err := repo.SearchWords(ctx, "100%", nil, nil, nil, nil, nil, nil, 10, 0)
	if err != nil {
		t.Fatalf("SearchWords() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want %d", total, 1)
	}
	if len(words) != 1 {
		t.Fatalf("len(words) = %d, want %d", len(words), 1)
	}
	if words[0].Headword != "100% pure" {
		t.Fatalf("words[0].Headword = %q, want %q", words[0].Headword, "100% pure")
	}
}

func TestSearchWords_PostgresTreatsWildcardsAsLiteralInVariantMatches(t *testing.T) {
	testCases := []struct {
		name         string
		query        string
		matching     wordFixture
		decoy        wordFixture
		wantHeadword string
	}{
		{
			name:  "percent",
			query: "mix%",
			matching: wordFixture{
				word: Word{
					Headword:           "spectrum",
					HeadwordNormalized: textutil.ToNormalized("spectrum"),
					FrequencyRank:      40,
					CollinsStars:       4,
					TranslationZH:      "范围",
				},
				variants: []WordVariant{{
					VariantText:        "mix%case",
					HeadwordNormalized: textutil.ToNormalized("mix%case"),
					Kind:               commonmodel.VariantAlias,
					FrequencyRank:      40,
				}},
			},
			decoy: wordFixture{
				word: Word{
					Headword:           "channel",
					HeadwordNormalized: textutil.ToNormalized("channel"),
					FrequencyRank:      41,
					CollinsStars:       4,
					TranslationZH:      "渠道",
				},
				variants: []WordVariant{{
					VariantText:        "mixxcase",
					HeadwordNormalized: textutil.ToNormalized("mixxcase"),
					Kind:               commonmodel.VariantAlias,
					FrequencyRank:      41,
				}},
			},
			wantHeadword: "spectrum",
		},
		{
			name:  "underscore",
			query: "mix_",
			matching: wordFixture{
				word: Word{
					Headword:           "pattern",
					HeadwordNormalized: textutil.ToNormalized("pattern"),
					FrequencyRank:      50,
					CollinsStars:       4,
					TranslationZH:      "模式",
				},
				variants: []WordVariant{{
					VariantText:        "mix_case",
					HeadwordNormalized: textutil.ToNormalized("mix_case"),
					Kind:               commonmodel.VariantAlias,
					FrequencyRank:      50,
				}},
			},
			decoy: wordFixture{
				word: Word{
					Headword:           "texture",
					HeadwordNormalized: textutil.ToNormalized("texture"),
					FrequencyRank:      51,
					CollinsStars:       4,
					TranslationZH:      "纹理",
				},
				variants: []WordVariant{{
					VariantText:        "muzxcase",
					HeadwordNormalized: textutil.ToNormalized("muzxcase"),
					Kind:               commonmodel.VariantAlias,
					FrequencyRank:      51,
				}},
			},
			wantHeadword: "pattern",
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			repo, db := newPostgresIntegrationRepository(t)
			ctx := context.Background()

			seedWordFixture(t, db, tt.matching)
			seedWordFixture(t, db, tt.decoy)

			words, total, err := repo.SearchWords(ctx, tt.query, nil, nil, nil, nil, nil, nil, 10, 0)
			if err != nil {
				t.Fatalf("SearchWords() error = %v", err)
			}
			if total != 1 {
				t.Fatalf("total = %d, want %d", total, 1)
			}
			if len(words) != 1 {
				t.Fatalf("len(words) = %d, want %d", len(words), 1)
			}
			if words[0].Headword != tt.wantHeadword {
				t.Fatalf("words[0].Headword = %q, want %q", words[0].Headword, tt.wantHeadword)
			}
		})
	}
}

func newPostgresIntegrationRepository(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()

	dsn := requirePostgresIntegrationDSN(t)

	db, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	prepareExternalPostgresRepository(t, dsn, db, false)

	return &Repository{db: db}, db
}

type wordFixture struct {
	word           Word
	pronunciations []Pronunciation
	senses         []Sense
	variants       []WordVariant
}

func seedWordFixture(t *testing.T, db *gorm.DB, fixture wordFixture) Word {
	t.Helper()

	if err := db.Create(&fixture.word).Error; err != nil {
		t.Fatalf("create word %q: %v", fixture.word.Headword, err)
	}

	for i := range fixture.pronunciations {
		fixture.pronunciations[i].WordID = fixture.word.ID
		if err := db.Create(&fixture.pronunciations[i]).Error; err != nil {
			t.Fatalf("create pronunciation for %q: %v", fixture.word.Headword, err)
		}
	}

	for i := range fixture.senses {
		fixture.senses[i].WordID = fixture.word.ID
		if err := db.Create(&fixture.senses[i]).Error; err != nil {
			t.Fatalf("create sense for %q: %v", fixture.word.Headword, err)
		}
	}

	for i := range fixture.variants {
		fixture.variants[i].WordID = fixture.word.ID
		if err := db.Create(&fixture.variants[i]).Error; err != nil {
			t.Fatalf("create variant for %q: %v", fixture.word.Headword, err)
		}
	}

	return fixture.word
}

func intPtr(value int) *int {
	return &value
}

func mustCreateWordsInBatches(t *testing.T, db *gorm.DB, words []Word) []Word {
	t.Helper()

	if len(words) == 0 {
		return words
	}
	if err := db.CreateInBatches(&words, 200).Error; err != nil {
		t.Fatalf("batch create words: %v", err)
	}
	return words
}

func mustCreateVariantsInBatches(t *testing.T, db *gorm.DB, variants []WordVariant) {
	t.Helper()

	if len(variants) == 0 {
		return
	}
	if err := db.CreateInBatches(&variants, 200).Error; err != nil {
		t.Fatalf("batch create variants: %v", err)
	}
}

func mustExplainPlan(t *testing.T, db *gorm.DB, query string, args ...interface{}) string {
	t.Helper()

	rows, err := db.Raw(query, args...).Rows()
	if err != nil {
		t.Fatalf("EXPLAIN error = %v", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatalf("rows.Close() error = %v", closeErr)
		}
	}()

	plan := ""
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		plan += line + "\n"
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate EXPLAIN rows: %v", err)
	}
	return plan
}

func mustExplainPlanWithSeqScanDisabled(t *testing.T, db *gorm.DB, query string, args ...interface{}) string {
	t.Helper()

	var plan string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
			return err
		}
		plan = mustExplainPlan(t, tx, query, args...)
		return nil
	})
	if err != nil {
		t.Fatalf("EXPLAIN in transaction error = %v", err)
	}
	return plan
}

func maxDurationForPostgresTest(base time.Duration) time.Duration {
	if os.Getenv(postgresIntegrationDSNEnv) != "" {
		return base * 3
	}
	return base
}

// newPostgresIntegrationRepositoryWithIndexes creates a repository with indexes
// enabled, for use by index-effectiveness and performance tests.
func newPostgresIntegrationRepositoryWithIndexes(t *testing.T) (*Repository, *gorm.DB) {
	t.Helper()

	dsn := requirePostgresIntegrationDSN(t)

	db, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	prepareExternalPostgresRepository(t, dsn, db, true)

	return &Repository{db: db}, db
}

func requirePostgresIntegrationDSN(t *testing.T) string {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(postgresIntegrationDSNEnv))
	if dsn == "" {
		t.Skip("repository PostgreSQL integration tests require TEST_POSTGRES_DSN pointing to a disposable PostgreSQL database")
	}
	if err := validatePostgresIntegrationDSNForDestructiveSetup(dsn); err != nil {
		t.Fatalf("unsafe TEST_POSTGRES_DSN for destructive repository test setup: %v", err)
	}

	return dsn
}

func validatePostgresIntegrationDSNForDestructiveSetup(dsn string) error {
	return validatePostgresIntegrationDSNForDestructiveSetupForHostPolicy(dsn, allowNonLocalPostgresTestHosts())
}

func validatePostgresIntegrationDSNForDestructiveSetupForHostPolicy(dsn string, allowNonLocal bool) error {
	host, err := postgresDSNHost(dsn)
	if err != nil {
		return err
	}
	if !allowNonLocal && !isLocalPostgresTestHost(host) {
		return fmt.Errorf("destructive PostgreSQL tests only run against localhost, loopback IPs, or unix sockets by default; got host %q (set %s=true to opt in to a disposable non-local PostgreSQL instance)", host, allowNonLocalPostgresTestsEnv)
	}
	return nil
}

func allowNonLocalPostgresTestHosts() bool {
	value := strings.TrimSpace(os.Getenv(allowNonLocalPostgresTestsEnv))
	return strings.EqualFold(value, "1") || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func postgresDSNHost(dsn string) (string, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsedURL, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("parse PostgreSQL DSN: %w", err)
		}
		if socketHost := parsedURL.Query().Get("host"); socketHost != "" {
			return socketHost, nil
		}
		return parsedURL.Hostname(), nil
	}

	for field := range strings.FieldsSeq(dsn) {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(parts[0]), "host") {
			return strings.Trim(strings.TrimSpace(parts[1]), "'\""), nil
		}
	}

	return "", nil
}

func isLocalPostgresTestHost(host string) bool {
	if host == "" {
		return true
	}

	for candidate := range strings.SplitSeq(host, ",") {
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

func prepareExternalPostgresRepository(t *testing.T, dsn string, db *gorm.DB, withIndexes bool) {
	t.Helper()

	externalPostgresTestMu.Lock()
	t.Cleanup(externalPostgresTestMu.Unlock)

	state := externalPostgresSchema[dsn]
	if !state.initialized || (withIndexes && !state.withIndexes) {
		migrator := migration.NewMigrator(db)
		if err := migrator.Migrate(postgresIntegrationMigrateOptions(withIndexes)); err != nil {
			if withIndexes {
				t.Fatalf("migrate with indexes: %v", err)
			}
			t.Fatalf("migrate test schema: %v", err)
		}
		state.initialized = true
		state.withIndexes = state.withIndexes || withIndexes
		externalPostgresSchema[dsn] = state
		return
	}

	resetPostgresTestData(t, db)
}

func postgresIntegrationMigrateOptions(withIndexes bool) *migration.MigrateOptions {
	return &migration.MigrateOptions{
		DropTables:  true,
		SkipIndexes: !withIndexes,
	}
}

func resetPostgresTestData(t *testing.T, db *gorm.DB) {
	t.Helper()

	if err := truncatePostgresTestData(db); err != nil {
		if !isTransientExternalPostgresResetError(err) {
			t.Fatalf("truncate test tables: %v", err)
		}

		sqlDB, sqlDBErr := db.DB()
		if sqlDBErr != nil {
			t.Fatalf("truncate test tables after transient connection error: get sql DB: %v (initial error: %v)", sqlDBErr, err)
		}

		pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		if pingErr := sqlDB.PingContext(pingCtx); pingErr != nil && !isTransientExternalPostgresResetError(pingErr) {
			t.Fatalf("truncate test tables after transient connection error: ping failed: %v (initial error: %v)", pingErr, err)
		}

		if retryErr := truncatePostgresTestData(db); retryErr != nil {
			t.Fatalf("truncate test tables after transient connection error: %v (initial error: %v)", retryErr, err)
		}
		return
	}
}

func truncatePostgresTestData(db *gorm.DB) error {
	return db.Exec(postgresTestTablesTruncateSQL).Error
}

func isTransientExternalPostgresResetError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, driver.ErrBadConn) {
		return true
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) && isTransientExternalPostgresResetError(opErr.Err) {
		return true
	}

	var syscallErr *os.SyscallError
	if errors.As(err, &syscallErr) {
		switch syscallErr.Err {
		case syscall.ECONNRESET, syscall.EPIPE, syscall.ECONNABORTED:
			return true
		}
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection reset by peer") ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "unexpected eof") ||
		strings.Contains(message, "driver: bad connection")
}

// =============================================================================
// Functional coverage: SuggestWords
// =============================================================================

func TestSuggestWords_PostgresReturnsMatchesByFrequency(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "apple",
			HeadwordNormalized: textutil.ToNormalized("apple"),
			FrequencyRank:      50,
			TranslationZH:      "苹果",
		},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "application",
			HeadwordNormalized: textutil.ToNormalized("application"),
			FrequencyRank:      20,
			TranslationZH:      "应用",
		},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "apply",
			HeadwordNormalized: textutil.ToNormalized("apply"),
			FrequencyRank:      10,
			TranslationZH:      "申请",
		},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "banana",
			HeadwordNormalized: textutil.ToNormalized("banana"),
			FrequencyRank:      30,
			TranslationZH:      "香蕉",
		},
	})

	words, err := repo.SuggestWords(ctx, "app", nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("SuggestWords() error = %v", err)
	}
	if len(words) != 3 {
		t.Fatalf("len(words) = %d, want 3", len(words))
	}
	if words[0].Headword != "apply" {
		t.Fatalf("words[0].Headword = %q, want %q (best frequency)", words[0].Headword, "apply")
	}
	if words[1].Headword != "application" {
		t.Fatalf("words[1].Headword = %q, want %q", words[1].Headword, "application")
	}
	if words[2].Headword != "apple" {
		t.Fatalf("words[2].Headword = %q, want %q", words[2].Headword, "apple")
	}
}

func TestSuggestWords_PostgresIncludesVariantMatches(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "run",
			HeadwordNormalized: textutil.ToNormalized("run"),
			FrequencyRank:      5,
			TranslationZH:      "跑",
		},
		variants: []WordVariant{{
			VariantText:        "running",
			HeadwordNormalized: textutil.ToNormalized("running"),
			Kind:               commonmodel.VariantForm,
			FormType:           intPtr(1),
			FrequencyRank:      5,
		}},
	})

	words, err := repo.SuggestWords(ctx, "runn", nil, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("SuggestWords() error = %v", err)
	}
	if len(words) != 1 {
		t.Fatalf("len(words) = %d, want 1", len(words))
	}
	if words[0].Headword != "run" {
		t.Fatalf("words[0].Headword = %q, want %q", words[0].Headword, "run")
	}
}

func TestSuggestWords_PostgresAppliesFilters(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "test",
			HeadwordNormalized: textutil.ToNormalized("test"),
			CEFRLevel:          2,
			FrequencyRank:      10,
			CollinsStars:       5,
			TranslationZH:      "测试",
		},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "testify",
			HeadwordNormalized: textutil.ToNormalized("testify"),
			CEFRLevel:          4,
			FrequencyRank:      80,
			CollinsStars:       3,
			TranslationZH:      "作证",
		},
	})

	cefrLevel := 2
	words, err := repo.SuggestWords(ctx, "test", &cefrLevel, nil, nil, nil, nil, 10)
	if err != nil {
		t.Fatalf("SuggestWords() error = %v", err)
	}
	if len(words) != 1 {
		t.Fatalf("len(words) = %d, want 1", len(words))
	}
	if words[0].Headword != "test" {
		t.Fatalf("words[0].Headword = %q, want %q", words[0].Headword, "test")
	}
}

func TestSuggestWords_PostgresRespectsLimit(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		hw := fmt.Sprintf("term%d", i)
		seedWordFixture(t, db, wordFixture{
			word: Word{
				Headword:           hw,
				HeadwordNormalized: textutil.ToNormalized(hw),
				FrequencyRank:      i + 1,
				TranslationZH:      "术语",
			},
		})
	}

	words, err := repo.SuggestWords(ctx, "term", nil, nil, nil, nil, nil, 3)
	if err != nil {
		t.Fatalf("SuggestWords() error = %v", err)
	}
	if len(words) != 3 {
		t.Fatalf("len(words) = %d, want 3", len(words))
	}
}

// =============================================================================
// Functional coverage: GetWordsByHeadwords
// =============================================================================

func TestGetWordsByHeadwords_PostgresBatchLookup(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "cat",
			HeadwordNormalized: textutil.ToNormalized("cat"),
			FrequencyRank:      30,
			TranslationZH:      "猫",
		},
	})
	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "dog",
			HeadwordNormalized: textutil.ToNormalized("dog"),
			FrequencyRank:      20,
			TranslationZH:      "狗",
		},
	})

	words, err := repo.GetWordsByHeadwords(ctx, []string{"cat", "dog", "missing"}, false, false, false)
	if err != nil {
		t.Fatalf("GetWordsByHeadwords() error = %v", err)
	}
	if len(words) != 2 {
		t.Fatalf("len(words) = %d, want 2", len(words))
	}
}

func TestGetWordsByHeadwords_PostgresPreloads(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "hello",
			HeadwordNormalized: textutil.ToNormalized("hello"),
			FrequencyRank:      5,
			TranslationZH:      "你好",
		},
		pronunciations: []Pronunciation{{
			Accent:    1,
			IPA:       "həˈloʊ",
			IsPrimary: true,
		}},
		senses: []Sense{{
			POS:          4,
			DefinitionEN: "a greeting",
			DefinitionZH: "问候",
			SenseOrder:   1,
		}},
		variants: []WordVariant{{
			VariantText:        "hallo",
			HeadwordNormalized: textutil.ToNormalized("hallo"),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      5,
		}},
	})

	words, err := repo.GetWordsByHeadwords(ctx, []string{"hello"}, true, true, true)
	if err != nil {
		t.Fatalf("GetWordsByHeadwords() error = %v", err)
	}
	if len(words) != 1 {
		t.Fatalf("len(words) = %d, want 1", len(words))
	}
	if len(words[0].Pronunciations) != 1 {
		t.Fatalf("pronunciations not preloaded: got %d", len(words[0].Pronunciations))
	}
	if len(words[0].Senses) != 1 {
		t.Fatalf("senses not preloaded: got %d", len(words[0].Senses))
	}
	if len(words[0].WordVariants) != 1 {
		t.Fatalf("variants not preloaded: got %d", len(words[0].WordVariants))
	}
}

// =============================================================================
// Functional coverage: GetPronunciationsByWordID / GetSensesByWordID
// =============================================================================

func TestGetPronunciationsByWordID_PostgresFilterByAccent(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	w := seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "schedule",
			HeadwordNormalized: textutil.ToNormalized("schedule"),
			FrequencyRank:      100,
			TranslationZH:      "时间表",
		},
		pronunciations: []Pronunciation{
			{Accent: 1, IPA: "ˈʃɛdjuːl", IsPrimary: true},
			{Accent: 2, IPA: "ˈskɛdʒuːl", IsPrimary: true},
		},
	})

	// All pronunciations
	all, err := repo.GetPronunciationsByWordID(ctx, w.ID, nil)
	if err != nil {
		t.Fatalf("GetPronunciationsByWordID(nil) error = %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}

	// Filter by accent
	accent := 1
	filtered, err := repo.GetPronunciationsByWordID(ctx, w.ID, &accent)
	if err != nil {
		t.Fatalf("GetPronunciationsByWordID(1) error = %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].IPA != "ˈʃɛdjuːl" {
		t.Fatalf("filtered[0].IPA = %q, want UK IPA", filtered[0].IPA)
	}
}

func TestGetSensesByWordID_PostgresOrderAndFilter(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	w := seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "run",
			HeadwordNormalized: textutil.ToNormalized("run"),
			FrequencyRank:      3,
			TranslationZH:      "跑",
		},
		senses: []Sense{
			{POS: 2, DefinitionEN: "to move fast", DefinitionZH: "快速移动", SenseOrder: 1},
			{POS: 1, DefinitionEN: "a period of running", DefinitionZH: "跑步", SenseOrder: 2},
			{POS: 2, DefinitionEN: "to operate", DefinitionZH: "操作", SenseOrder: 3},
		},
	})

	// All senses ordered
	all, err := repo.GetSensesByWordID(ctx, w.ID, nil)
	if err != nil {
		t.Fatalf("GetSensesByWordID(nil) error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("len(all) = %d, want 3", len(all))
	}
	if all[0].SenseOrder != 1 || all[1].SenseOrder != 2 || all[2].SenseOrder != 3 {
		t.Fatal("senses not ordered by sense_order")
	}

	// Filter by POS
	pos := 2
	filtered, err := repo.GetSensesByWordID(ctx, w.ID, &pos)
	if err != nil {
		t.Fatalf("GetSensesByWordID(verb) error = %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
}

// =============================================================================
// Functional coverage: GetWordByHeadword preloads and edge cases
// =============================================================================

func TestGetWordByHeadword_PostgresPreloads(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "book",
			HeadwordNormalized: textutil.ToNormalized("book"),
			FrequencyRank:      15,
			TranslationZH:      "书",
		},
		pronunciations: []Pronunciation{
			{Accent: 1, IPA: "bʊk", IsPrimary: true},
		},
		senses: []Sense{
			{POS: 1, DefinitionEN: "written pages", DefinitionZH: "书籍", SenseOrder: 1},
			{POS: 2, DefinitionEN: "to reserve", DefinitionZH: "预订", SenseOrder: 2},
		},
		variants: []WordVariant{
			{VariantText: "booked", HeadwordNormalized: textutil.ToNormalized("booked"), Kind: commonmodel.VariantForm, FormType: intPtr(1), FrequencyRank: 15},
		},
	})

	word, _, err := repo.GetWordByHeadword(ctx, "book", true, true, true)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(word.Pronunciations) != 1 {
		t.Fatalf("pronunciations not preloaded: %d", len(word.Pronunciations))
	}
	if len(word.Senses) != 2 {
		t.Fatalf("senses not preloaded: %d", len(word.Senses))
	}
	if len(word.WordVariants) != 1 {
		t.Fatalf("variants not preloaded: %d", len(word.WordVariants))
	}
}

func TestGetWordByHeadword_PostgresNormalizedFallback(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	seedWordFixture(t, db, wordFixture{
		word: Word{
			Headword:           "café",
			HeadwordNormalized: textutil.ToNormalized("café"),
			FrequencyRank:      100,
			TranslationZH:      "咖啡馆",
		},
	})

	// Exact match should work
	word, variant, err := repo.GetWordByHeadword(ctx, "café", false, false, false)
	if err != nil {
		t.Fatalf("exact match error = %v", err)
	}
	if variant != nil {
		t.Fatal("expected nil variant for direct match")
	}
	if word.Headword != "café" {
		t.Fatalf("Headword = %q, want %q", word.Headword, "café")
	}
}

// =============================================================================
// Functional coverage: SearchWords pagination
// =============================================================================

func TestSearchWords_PostgresPagination(t *testing.T) {
	repo, db := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		hw := fmt.Sprintf("page%d", i)
		seedWordFixture(t, db, wordFixture{
			word: Word{
				Headword:           hw,
				HeadwordNormalized: textutil.ToNormalized(hw),
				FrequencyRank:      i + 1,
				TranslationZH:      "页面",
			},
		})
	}

	words, total, err := repo.SearchWords(ctx, "page", nil, nil, nil, nil, nil, nil, 3, 0)
	if err != nil {
		t.Fatalf("page 1 error = %v", err)
	}
	if total != 10 {
		t.Fatalf("total = %d, want 10", total)
	}
	if len(words) != 3 {
		t.Fatalf("page 1 len = %d, want 3", len(words))
	}

	words2, total2, err := repo.SearchWords(ctx, "page", nil, nil, nil, nil, nil, nil, 3, 3)
	if err != nil {
		t.Fatalf("page 2 error = %v", err)
	}
	if total2 != 10 {
		t.Fatalf("total page 2 = %d, want 10", total2)
	}
	if len(words2) != 3 {
		t.Fatalf("page 2 len = %d, want 3", len(words2))
	}
	// Ensure no overlap
	for _, w1 := range words {
		for _, w2 := range words2 {
			if w1.ID == w2.ID {
				t.Fatalf("page 1 and page 2 overlap on word ID %d", w1.ID)
			}
		}
	}
}

func TestSearchWords_PostgresEmptyResult(t *testing.T) {
	repo, _ := newPostgresIntegrationRepository(t)
	ctx := context.Background()

	words, total, err := repo.SearchWords(ctx, "nonexistent", nil, nil, nil, nil, nil, nil, 10, 0)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if total != 0 {
		t.Fatalf("total = %d, want 0", total)
	}
	if len(words) != 0 {
		t.Fatalf("len(words) = %d, want 0", len(words))
	}
}

// =============================================================================
// Index effectiveness: verify queries use indexes via EXPLAIN
// =============================================================================

func TestIndexEffectiveness_PostgresHeadwordNormalizedLookupUsesIndex(t *testing.T) {
	_, db := newPostgresIntegrationRepositoryWithIndexes(t)
	ctx := context.Background()

	// Seed enough data to make the planner consider indexes
	words := make([]Word, 0, 200)
	for i := 0; i < 200; i++ {
		hw := fmt.Sprintf("word%04d", i)
		words = append(words, Word{
			Headword:           hw,
			HeadwordNormalized: textutil.ToNormalized(hw),
			FrequencyRank:      i + 1,
			TranslationZH:      "词",
		})
	}
	mustCreateWordsInBatches(t, db.WithContext(ctx), words)

	// ANALYZE so the planner has current statistics
	db.WithContext(ctx).Exec("ANALYZE words")

	plan := mustExplainPlanWithSeqScanDisabled(t, db.WithContext(ctx),
		"EXPLAIN (FORMAT TEXT) SELECT id FROM words WHERE headword_normalized = ?",
		"word0050",
	)

	if !containsString(plan, "Index Scan") && !containsString(plan, "Bitmap Index Scan") {
		t.Fatalf("expected index-backed plan for words.headword_normalized but got:\n%s", plan)
	}
	t.Logf("query plan:\n%s", plan)
}

func TestIndexEffectiveness_PostgresVariantTextLookupUsesIndex(t *testing.T) {
	_, db := newPostgresIntegrationRepositoryWithIndexes(t)
	ctx := context.Background()

	// Seed words and variants
	words := make([]Word, 0, 200)
	for i := 0; i < 200; i++ {
		hw := fmt.Sprintf("base%04d", i)
		words = append(words, Word{
			Headword:           hw,
			HeadwordNormalized: textutil.ToNormalized(hw),
			FrequencyRank:      i + 1,
			TranslationZH:      "词",
		})
	}
	words = mustCreateWordsInBatches(t, db.WithContext(ctx), words)

	variants := make([]WordVariant, 0, 200)
	for i, w := range words {
		variants = append(variants, WordVariant{
			WordID:             w.ID,
			VariantText:        fmt.Sprintf("var%04d", i),
			HeadwordNormalized: textutil.ToNormalized(fmt.Sprintf("var%04d", i)),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      i + 1,
		})
	}
	mustCreateVariantsInBatches(t, db.WithContext(ctx), variants)

	db.WithContext(ctx).Exec("ANALYZE words")
	db.WithContext(ctx).Exec("ANALYZE word_variants")

	plan := mustExplainPlanWithSeqScanDisabled(t, db.WithContext(ctx),
		"EXPLAIN (FORMAT TEXT) SELECT word_variants.* FROM word_variants INNER JOIN words ON word_variants.word_id = words.id WHERE word_variants.variant_text = ?",
		"var0050",
	)

	if !containsString(plan, "Index Scan") && !containsString(plan, "Bitmap Index Scan") {
		t.Fatalf("expected index-backed plan for word_variants.variant_text but got:\n%s", plan)
	}
	t.Logf("query plan:\n%s", plan)
}

func TestIndexEffectiveness_PostgresVariantHeadwordNormalizedLookupUsesIndex(t *testing.T) {
	_, db := newPostgresIntegrationRepositoryWithIndexes(t)
	ctx := context.Background()

	words := make([]Word, 0, 200)
	for i := 0; i < 200; i++ {
		hw := fmt.Sprintf("root%04d", i)
		words = append(words, Word{
			Headword:           hw,
			HeadwordNormalized: textutil.ToNormalized(hw),
			FrequencyRank:      i + 1,
			TranslationZH:      "词",
		})
	}
	words = mustCreateWordsInBatches(t, db.WithContext(ctx), words)

	variants := make([]WordVariant, 0, 200)
	for i, w := range words {
		variants = append(variants, WordVariant{
			WordID:             w.ID,
			VariantText:        fmt.Sprintf("alt%04d", i),
			HeadwordNormalized: textutil.ToNormalized(fmt.Sprintf("alt%04d", i)),
			Kind:               commonmodel.VariantAlias,
			FrequencyRank:      i + 1,
		})
	}
	mustCreateVariantsInBatches(t, db.WithContext(ctx), variants)

	db.WithContext(ctx).Exec("ANALYZE word_variants")

	plan := mustExplainPlanWithSeqScanDisabled(t, db.WithContext(ctx),
		"EXPLAIN (FORMAT TEXT) SELECT * FROM word_variants WHERE headword_normalized = ?",
		textutil.ToNormalized("alt0050"),
	)

	if !containsString(plan, "Index Scan") && !containsString(plan, "Bitmap Index Scan") {
		t.Fatalf("expected index-backed plan for word_variants.headword_normalized but got:\n%s", plan)
	}
	t.Logf("query plan:\n%s", plan)
}

// =============================================================================
// Performance: verify queries complete within reasonable time at scale
// =============================================================================

func TestPerformance_PostgresSearchWordsAtScale(t *testing.T) {
	repo, db := newPostgresIntegrationRepositoryWithIndexes(t)
	ctx := context.Background()

	const wordCount = 1000
	words := make([]Word, 0, wordCount)
	for i := 0; i < wordCount; i++ {
		hw := fmt.Sprintf("perf%04d", i)
		words = append(words, Word{
			Headword:           hw,
			HeadwordNormalized: textutil.ToNormalized(hw),
			FrequencyRank:      i + 1,
			CEFRLevel:          (i % 6) + 1,
			OxfordLevel:        (i % 2) + 1,
			CollinsStars:       (i % 5) + 1,
			TranslationZH:      "性能测试词",
		})
	}
	words = mustCreateWordsInBatches(t, db.WithContext(ctx), words)
	variants := make([]WordVariant, 0, wordCount/3+1)
	for i, w := range words {
		if i%3 == 0 {
			variants = append(variants, WordVariant{
				WordID:             w.ID,
				VariantText:        fmt.Sprintf("perfvar%04d", i),
				HeadwordNormalized: textutil.ToNormalized(fmt.Sprintf("perfvar%04d", i)),
				Kind:               commonmodel.VariantAlias,
				FrequencyRank:      i + 1,
			})
		}
	}
	mustCreateVariantsInBatches(t, db.WithContext(ctx), variants)

	db.WithContext(ctx).Exec("ANALYZE words")
	db.WithContext(ctx).Exec("ANALYZE word_variants")

	// SearchWords should complete quickly even with 1000+ words
	start := time.Now()
	words, total, err := repo.SearchWords(ctx, "perf", nil, nil, nil, nil, nil, nil, 20, 0)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("SearchWords error = %v", err)
	}
	if total != wordCount {
		t.Fatalf("total = %d, want %d", total, wordCount)
	}
	if len(words) != 20 {
		t.Fatalf("len(words) = %d, want 20", len(words))
	}

	maxDuration := maxDurationForPostgresTest(5 * time.Second)
	if elapsed > maxDuration {
		t.Fatalf("SearchWords took %v, want < %v", elapsed, maxDuration)
	}
	t.Logf("SearchWords(%d words) completed in %v", wordCount, elapsed)
}

func TestPerformance_PostgresSuggestWordsAtScale(t *testing.T) {
	repo, db := newPostgresIntegrationRepositoryWithIndexes(t)
	ctx := context.Background()

	const wordCount = 1000
	words := make([]Word, 0, wordCount)
	for i := 0; i < wordCount; i++ {
		hw := fmt.Sprintf("suggest%04d", i)
		words = append(words, Word{
			Headword:           hw,
			HeadwordNormalized: textutil.ToNormalized(hw),
			FrequencyRank:      i + 1,
			TranslationZH:      "建议测试词",
		})
	}
	mustCreateWordsInBatches(t, db.WithContext(ctx), words)

	db.WithContext(ctx).Exec("ANALYZE words")

	start := time.Now()
	words, err := repo.SuggestWords(ctx, "sugge", nil, nil, nil, nil, nil, 20)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("SuggestWords error = %v", err)
	}
	if len(words) != 20 {
		t.Fatalf("len(words) = %d, want 20", len(words))
	}

	maxDuration := maxDurationForPostgresTest(3 * time.Second)
	if elapsed > maxDuration {
		t.Fatalf("SuggestWords took %v, want < %v", elapsed, maxDuration)
	}
	t.Logf("SuggestWords(%d words) completed in %v", wordCount, elapsed)
}

func TestPerformance_PostgresGetWordByHeadwordAtScale(t *testing.T) {
	repo, db := newPostgresIntegrationRepositoryWithIndexes(t)
	ctx := context.Background()

	const wordCount = 500
	words := make([]Word, 0, wordCount)
	for i := 0; i < wordCount; i++ {
		hw := fmt.Sprintf("lookup%04d", i)
		words = append(words, Word{
			Headword:           hw,
			HeadwordNormalized: textutil.ToNormalized(hw),
			FrequencyRank:      i + 1,
			TranslationZH:      "查找测试词",
		})
	}
	mustCreateWordsInBatches(t, db.WithContext(ctx), words)

	db.WithContext(ctx).Exec("ANALYZE words")

	// Single lookup should be fast
	start := time.Now()
	word, variant, err := repo.GetWordByHeadword(ctx, "lookup0250", false, false, false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("GetWordByHeadword error = %v", err)
	}
	if word == nil {
		t.Fatal("expected word not nil")
	}
	if variant != nil {
		t.Fatal("expected variant nil for direct match")
	}

	maxDuration := maxDurationForPostgresTest(500 * time.Millisecond)
	if elapsed > maxDuration {
		t.Fatalf("GetWordByHeadword took %v, want < %v", elapsed, maxDuration)
	}
	t.Logf("GetWordByHeadword (single, %d rows) completed in %v", wordCount, elapsed)
}

func TestPerformance_PostgresBatchLookupAtScale(t *testing.T) {
	repo, db := newPostgresIntegrationRepositoryWithIndexes(t)
	ctx := context.Background()

	const wordCount = 500
	headwords := make([]string, 50)
	words := make([]Word, 0, wordCount)
	for i := 0; i < wordCount; i++ {
		hw := fmt.Sprintf("batch%04d", i)
		words = append(words, Word{
			Headword:           hw,
			HeadwordNormalized: textutil.ToNormalized(hw),
			FrequencyRank:      i + 1,
			TranslationZH:      "批量测试词",
		})
		if i < 50 {
			headwords[i] = hw
		}
	}
	mustCreateWordsInBatches(t, db.WithContext(ctx), words)

	db.WithContext(ctx).Exec("ANALYZE words")

	start := time.Now()
	words, err := repo.GetWordsByHeadwords(ctx, headwords, false, false, false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("GetWordsByHeadwords error = %v", err)
	}
	if len(words) != 50 {
		t.Fatalf("len(words) = %d, want 50", len(words))
	}

	maxDuration := maxDurationForPostgresTest(2 * time.Second)
	if elapsed > maxDuration {
		t.Fatalf("GetWordsByHeadwords(50 of %d) took %v, want < %v", wordCount, elapsed, maxDuration)
	}
	t.Logf("GetWordsByHeadwords (50 of %d) completed in %v", wordCount, elapsed)
}

// =============================================================================
// Index protection: verify GORM-managed indexes exist
// =============================================================================

func TestIndexProtection_PostgresCriticalIndexesExist(t *testing.T) {
	_, db := newPostgresIntegrationRepositoryWithIndexes(t)

	var extensionEnabled bool
	if err := db.Raw(`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')`).Scan(&extensionEnabled).Error; err != nil {
		t.Fatalf("check pg_trgm extension: %v", err)
	}
	if !extensionEnabled {
		t.Fatal("required extension pg_trgm is not enabled")
	}

	requiredIndexes := []struct {
		table string
		index string
	}{
		{"words", "idx_words_headword_normalized"},
		{"words", "idx_words_cefr_level"},
		{"words", "idx_words_frequency_rank"},
		{"words", "idx_words_oxford_level"},
		{"words", "idx_words_collins_stars"},
		{"words", "idx_words_headword_trgm"},
		{"words", "idx_words_phrase_lower_trgm"},
		{"word_variants", "idx_word_variants_variant_text"},
		{"word_variants", "idx_word_variants_headword_normalized"},
		{"word_variants", "idx_word_variants_word_id"},
		{"word_variants", "idx_word_variants_frequency_rank"},
		{"word_variants", "idx_word_variants_headword_trgm"},
		{"word_variants", "idx_word_variants_phrase_lower_trgm"},
		{"pronunciations", "idx_pronunciations_word_id"},
		{"senses", "idx_senses_word_id"},
	}

	for _, ri := range requiredIndexes {
		var exists bool
		err := db.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE tablename = ? AND indexname = ?
			)`, ri.table, ri.index).Scan(&exists).Error
		if err != nil {
			t.Fatalf("check index %s: %v", ri.index, err)
		}
		if !exists {
			t.Errorf("required index %s on table %s does not exist", ri.index, ri.table)
		}
	}
}

func TestIndexProtection_PostgresUniqueConstraintsExist(t *testing.T) {
	_, db := newPostgresIntegrationRepositoryWithIndexes(t)

	uniqueConstraints := []struct {
		index string
		desc  string
	}{
		{"idx_word_variant_unique", "word_variants composite unique"},
		{"idx_pronunciation_primary_unique", "pronunciation primary unique"},
	}

	for _, uc := range uniqueConstraints {
		var exists bool
		err := db.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM pg_indexes
				WHERE indexname = ?
			)`, uc.index).Scan(&exists).Error
		if err != nil {
			t.Fatalf("check unique constraint %s: %v", uc.index, err)
		}
		if !exists {
			t.Errorf("unique constraint %s (%s) does not exist", uc.index, uc.desc)
		}
	}
}

func TestIndexProtection_PostgresUniqueConstraintEnforced(t *testing.T) {
	_, db := newPostgresIntegrationRepositoryWithIndexes(t)

	w := Word{
		Headword:           "unique-test",
		HeadwordNormalized: textutil.ToNormalized("unique-test"),
		FrequencyRank:      1,
		TranslationZH:      "唯一测试",
	}
	if err := db.Create(&w).Error; err != nil {
		t.Fatalf("create word: %v", err)
	}

	v1 := WordVariant{
		WordID:             w.ID,
		VariantText:        "unique-var",
		HeadwordNormalized: textutil.ToNormalized("unique-var"),
		Kind:               commonmodel.VariantAlias,
		FrequencyRank:      1,
	}
	if err := db.Create(&v1).Error; err != nil {
		t.Fatalf("create first variant: %v", err)
	}

	// Duplicate should be rejected by the unique constraint
	v2 := WordVariant{
		WordID:             w.ID,
		VariantText:        "unique-var",
		HeadwordNormalized: textutil.ToNormalized("unique-var"),
		Kind:               commonmodel.VariantAlias,
		FrequencyRank:      1,
	}
	err := db.Create(&v2).Error
	if err == nil {
		t.Fatal("expected unique constraint violation, got nil error")
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

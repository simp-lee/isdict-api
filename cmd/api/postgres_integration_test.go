package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/simp-lee/isdict-api/internal/api/handler"
	"github.com/simp-lee/isdict-api/internal/config"
	"github.com/simp-lee/isdict-commons/migration"
	commonmodel "github.com/simp-lee/isdict-commons/model"
	"github.com/simp-lee/isdict-commons/textutil"
	"github.com/simp-lee/isdict-data/postgresutil"
	"github.com/simp-lee/isdict-data/repository"
	"github.com/simp-lee/isdict-data/service"
	pgdriver "gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

const (
	apiTestDSNEnv                 = "TEST_POSTGRES_DSN"
	apiTestAdminDSNEnv            = "TEST_POSTGRES_ADMIN_DSN"
	allowNonLocalPostgresTestsEnv = "ISDICT_ALLOW_NONLOCAL_TEST_POSTGRES"
)

type apiPostgresHarness struct {
	db     *gorm.DB
	sqlDB  *sql.DB
	router *gin.Engine
	cfg    *config.Config
}

type apiSuccessResponse[T any] struct {
	Success bool                   `json:"success"`
	Data    T                      `json:"data"`
	Error   *commonmodel.ErrorInfo `json:"error"`
	Meta    *commonmodel.MetaInfo  `json:"meta,omitempty"`
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

func TestAPIGetWord_PostgresIntegration(t *testing.T) {
	h := newAPIPostgresHarness(t, nil)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/words/hello", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[commonmodel.WordResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if resp.Data.Headword != "hello" {
		t.Fatalf("headword = %q, want %q", resp.Data.Headword, "hello")
	}
	if len(resp.Data.Pronunciations) != 2 {
		t.Fatalf("len(pronunciations) = %d, want 2", len(resp.Data.Pronunciations))
	}
	if len(resp.Data.Senses) != 1 {
		t.Fatalf("len(senses) = %d, want 1", len(resp.Data.Senses))
	}
	if resp.Data.CEFRLevel != "A1" {
		t.Fatalf("cefr_level = %q, want %q", resp.Data.CEFRLevel, "A1")
	}
	if resp.Data.SchoolLevel != 1 {
		t.Fatalf("school_level = %d, want %d", resp.Data.SchoolLevel, 1)
	}
}

func TestAPISearchWords_PostgresFindsVariantFuzzyMatch(t *testing.T) {
	h := newAPIPostgresHarness(t, nil)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/search?q=runn&limit=10", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SearchResultResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if resp.Meta == nil || resp.Meta.Total == nil || *resp.Meta.Total != 1 {
		t.Fatalf("meta.total = %+v, want 1", resp.Meta)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].Headword != "run" {
		t.Fatalf("headword = %q, want %q", resp.Data[0].Headword, "run")
	}
}

func TestAPISearchWords_PostgresSupportsPagination(t *testing.T) {
	h := newAPIPostgresHarness(t, seedTriSearchFixtures)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/search?q=tri&limit=2&offset=1", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SearchResultResponse]
	decodeJSONResponse(t, recorder, &resp)

	if resp.Meta == nil || resp.Meta.Total == nil || *resp.Meta.Total != 4 {
		t.Fatalf("meta.total = %+v, want 4", resp.Meta)
	}
	if resp.Meta.Limit == nil || *resp.Meta.Limit != 2 {
		t.Fatalf("meta.limit = %+v, want 2", resp.Meta)
	}
	if resp.Meta.Offset == nil || *resp.Meta.Offset != 1 {
		t.Fatalf("meta.offset = %+v, want 1", resp.Meta)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(resp.Data))
	}
	if got := []string{resp.Data[0].Headword, resp.Data[1].Headword}; !reflectStringSlice(got, []string{"triage", "trimark"}) {
		t.Fatalf("page results = %v, want [triage trimark]", got)
	}
}

func TestAPISearchWords_PostgresAppliesFilters(t *testing.T) {
	h := newAPIPostgresHarness(t, seedTriSearchFixtures)

	path := "/api/v1/search?q=tri&pos=noun&cefr_level=A2&oxford_level=1&cet_level=4&max_frequency_rank=35&min_collins_stars=5&limit=10"
	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, path, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SearchResultResponse]
	decodeJSONResponse(t, recorder, &resp)

	if resp.Meta == nil || resp.Meta.Total == nil || *resp.Meta.Total != 1 {
		t.Fatalf("meta.total = %+v, want 1", resp.Meta)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].Headword != "trimark" {
		t.Fatalf("headword = %q, want %q", resp.Data[0].Headword, "trimark")
	}
	if resp.Data[0].CEFRLevel != "A2" {
		t.Fatalf("cefr_level = %q, want %q", resp.Data[0].CEFRLevel, "A2")
	}
	if resp.Data[0].SchoolLevel != 2 {
		t.Fatalf("school_level = %d, want %d", resp.Data[0].SchoolLevel, 2)
	}
	if !reflectStringSlice(resp.Data[0].POS, []string{"noun"}) {
		t.Fatalf("pos = %v, want [noun]", resp.Data[0].POS)
	}
}

func TestAPISuggestWords_PostgresIntegration(t *testing.T) {
	h := newAPIPostgresHarness(t, nil)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/suggest?prefix=prog&limit=10", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SuggestResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Data) < 2 {
		t.Fatalf("len(data) = %d, want at least 2", len(resp.Data))
	}
	if resp.Data[0].Headword != "program" {
		t.Fatalf("first suggestion = %q, want %q", resp.Data[0].Headword, "program")
	}
	if !containsSuggestHeadword(resp.Data, "programming") {
		t.Fatalf("expected suggestions to include programming, got %#v", resp.Data)
	}
}

func TestAPIGetWordByVariant_PostgresIntegration(t *testing.T) {
	h := newAPIPostgresHarness(t, nil)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/words/by-variant/ran?kind=form", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.VariantReverseResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].Headword != "run" {
		t.Fatalf("headword = %q, want %q", resp.Data[0].Headword, "run")
	}
	if len(resp.Data[0].Pronunciations) != 2 {
		t.Fatalf("len(pronunciations) = %d, want 2", len(resp.Data[0].Pronunciations))
	}
	if len(resp.Data[0].Senses) != 2 {
		t.Fatalf("len(senses) = %d, want 2", len(resp.Data[0].Senses))
	}
	if !containsVariantInfo(resp.Data[0].VariantInfo, "ran") {
		t.Fatalf("expected variant info to include ran, got %#v", resp.Data[0].VariantInfo)
	}
}

func TestAPIGetWordsBatch_PostgresIntegration(t *testing.T) {
	h := newAPIPostgresHarness(t, nil)

	body := `{"words":["hello","ran","missing"]}`
	recorder := performPostgresAPIRequest(t, h.router, http.MethodPost, "/api/v1/words/batch", body)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.WordResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(resp.Data))
	}
	if resp.Data[0].Headword != "hello" {
		t.Fatalf("first headword = %q, want %q", resp.Data[0].Headword, "hello")
	}
	if resp.Data[1].Headword != "run" {
		t.Fatalf("second headword = %q, want %q", resp.Data[1].Headword, "run")
	}
	if resp.Data[1].QueriedVariant == nil || resp.Data[1].QueriedVariant.Text != "ran" {
		t.Fatalf("queried_variant = %+v, want ran", resp.Data[1].QueriedVariant)
	}
	if resp.Meta == nil {
		t.Fatal("expected meta payload")
	}
	if resp.Meta.Requested == nil || *resp.Meta.Requested != 3 {
		t.Fatalf("meta.requested = %+v, want 3", resp.Meta)
	}
	if resp.Meta.Found == nil || *resp.Meta.Found != 2 {
		t.Fatalf("meta.found = %+v, want 2", resp.Meta)
	}
	if !reflectStringSlice(resp.Meta.NotFound, []string{"missing"}) {
		t.Fatalf("meta.not_found = %v, want [missing]", resp.Meta.NotFound)
	}
	if len(resp.Data[1].Pronunciations) != 2 {
		t.Fatalf("len(run pronunciations) = %d, want 2", len(resp.Data[1].Pronunciations))
	}
	if len(resp.Data[1].Senses) != 2 {
		t.Fatalf("len(run senses) = %d, want 2", len(resp.Data[1].Senses))
	}
	if !containsWordVariant(resp.Data[1].Variants, "ran") {
		t.Fatalf("expected returned variants to include ran, got %#v", resp.Data[1].Variants)
	}
}

func TestAPISearchPhrases_PostgresWordBoundaryAndOrdering(t *testing.T) {
	h := newAPIPostgresHarness(t, seedPhraseFixtures)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/phrases?q=carry&limit=10", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SuggestResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(resp.Data))
	}
	if got := []string{resp.Data[0].Headword, resp.Data[1].Headword}; !reflectStringSlice(got, []string{"carry on", "carry out"}) {
		t.Fatalf("phrase results = %v, want [carry on carry out]", got)
	}
	if resp.Data[0].CEFRLevel != "B1" {
		t.Fatalf("first CEFR level = %q, want %q", resp.Data[0].CEFRLevel, "B1")
	}
	if resp.Data[0].TranslationZH != "继续" {
		t.Fatalf("first translation = %q, want %q", resp.Data[0].TranslationZH, "继续")
	}
}

func TestAPISearchPhrases_PostgresSupportsMultiWordPrefix(t *testing.T) {
	h := newAPIPostgresHarness(t, seedPhraseFixtures)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/phrases?q=how%20a&limit=10", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SuggestResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("len(data) = %d, want 2", len(resp.Data))
	}
	if got := []string{resp.Data[0].Headword, resp.Data[1].Headword}; !reflectStringSlice(got, []string{"how about", "how are you"}) {
		t.Fatalf("multi-word prefix results = %v, want [how about how are you]", got)
	}
}

func TestAPISearchPhrases_PostgresUsesVariantPhraseFallback(t *testing.T) {
	h := newAPIPostgresHarness(t, seedPhraseFixtures)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/phrases?q=carried&limit=10", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SuggestResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].Headword != "carry on" {
		t.Fatalf("headword = %q, want %q", resp.Data[0].Headword, "carry on")
	}
}

func TestAPISearchPhrases_PostgresAvoidsSubstringFalsePositives(t *testing.T) {
	h := newAPIPostgresHarness(t, seedPhraseFixtures)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/phrases?q=star&limit=10", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SuggestResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].Headword != "star turn" {
		t.Fatalf("headword = %q, want %q", resp.Data[0].Headword, "star turn")
	}
	if containsSuggestHeadword(resp.Data, "start up") {
		t.Fatalf("unexpected substring false positive in results: %#v", resp.Data)
	}
	if containsSuggestHeadword(resp.Data, "starlight express") {
		t.Fatalf("unexpected intra-word false positive in results: %#v", resp.Data)
	}
	if resp.Data[0].TranslationZH != "精彩表演" {
		t.Fatalf("translation = %q, want %q", resp.Data[0].TranslationZH, "精彩表演")
	}
	if resp.Data[0].CEFRLevel != "B2" {
		t.Fatalf("CEFR level = %q, want %q", resp.Data[0].CEFRLevel, "B2")
	}
	if resp.Data[0].CETLevel != 6 {
		t.Fatalf("CET level = %d, want %d", resp.Data[0].CETLevel, 6)
	}
}

func TestAPISearchPhrases_PostgresRespectsLimit(t *testing.T) {
	h := newAPIPostgresHarness(t, seedPhraseFixtures)

	recorder := performPostgresAPIRequest(t, h.router, http.MethodGet, "/api/v1/phrases?q=carry&limit=1", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var resp apiSuccessResponse[[]commonmodel.SuggestResponse]
	decodeJSONResponse(t, recorder, &resp)

	if !resp.Success {
		t.Fatalf("expected success response, got %+v", resp)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("len(data) = %d, want 1", len(resp.Data))
	}
	if resp.Data[0].Headword != "carry on" {
		t.Fatalf("headword = %q, want %q", resp.Data[0].Headword, "carry on")
	}
}

func TestAPISearchPlan_PostgresHeadwordFuzzyUsesTrgmIndex(t *testing.T) {
	h := newAPIPostgresHarness(t, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkSearchFixtures(tb, sqlDB, 400)
	})

	h.db.Exec("ANALYZE words")
	plan := mustExplainPlanWithSeqScanDisabled(t, h.db,
		"EXPLAIN (FORMAT TEXT) SELECT id FROM words WHERE headword_normalized LIKE ?",
		"%benchterm0%",
	)

	if !strings.Contains(plan, "idx_words_headword_trgm") {
		t.Fatalf("expected trigram index in plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("expected index-backed plan, got:\n%s", plan)
	}
}

func TestAPISearchPlan_PostgresVariantFuzzyUsesTrgmIndex(t *testing.T) {
	h := newAPIPostgresHarness(t, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkSearchFixtures(tb, sqlDB, 400)
	})

	h.db.Exec("ANALYZE word_variants")
	plan := mustExplainPlanWithSeqScanDisabled(t, h.db,
		"EXPLAIN (FORMAT TEXT) SELECT word_id FROM word_variants WHERE headword_normalized LIKE ?",
		"%benchvar0%",
	)

	if !strings.Contains(plan, "idx_word_variants_headword_trgm") {
		t.Fatalf("expected trigram index in plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("expected index-backed plan, got:\n%s", plan)
	}
}

func TestAPISearchPlan_PostgresActualSearchQueryUsesSearchIndexes(t *testing.T) {
	h := newAPIPostgresHarness(t, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkSearchFixtures(tb, sqlDB, 400)
	})

	h.db.Exec("ANALYZE words")
	h.db.Exec("ANALYZE word_variants")

	keyword := textutil.ToNormalized("bench")
	prefix := escapeLikePattern(keyword) + "%"
	fuzzy := "%" + escapeLikePattern(keyword) + "%"
	query := buildActualSearchWordsPageSQLForExplain()
	args := []any{prefix, fuzzy, prefix, fuzzy, 20, 0}

	plan := mustExplainPlanWithSeqScanDisabled(t, h.db, query, args...)

	if !strings.Contains(plan, "idx_words_headword_trgm") {
		t.Fatalf("expected words trigram index in actual search query plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "WindowAgg") {
		t.Fatalf("expected row_number/window step in actual search query plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "Append") {
		t.Fatalf("expected union-all append step in actual search query plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "word_variants") {
		t.Fatalf("expected variant branch participation in actual search query plan, got:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on words") || strings.Contains(plan, "Seq Scan on word_variants") {
		t.Fatalf("expected actual search query to avoid sequential scans on search tables, got:\n%s", plan)
	}
}

func TestAPIPhrasePlan_PostgresHeadwordPhraseSearchUsesTrgmIndex(t *testing.T) {
	h := newAPIPostgresHarness(t, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkPhraseFixtures(tb, sqlDB, 400)
	})

	h.db.Exec("ANALYZE words")
	plan := mustExplainPlanWithSeqScanDisabled(t, h.db,
		"EXPLAIN (FORMAT TEXT) SELECT id FROM words WHERE headword LIKE '% %' AND lower(headword) LIKE ?",
		"bench %",
	)

	if !strings.Contains(plan, "idx_words_phrase_lower_trgm") {
		t.Fatalf("expected phrase trigram index in plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("expected index-backed phrase plan, got:\n%s", plan)
	}
}

func TestAPIPhrasePlan_PostgresVariantPhraseSearchUsesIndexBackedPlan(t *testing.T) {
	h := newAPIPostgresHarness(t, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkPhraseFixtures(tb, sqlDB, 400)
	})

	h.db.Exec("ANALYZE word_variants")
	plan := mustExplainPlanWithSeqScanDisabled(t, h.db,
		"EXPLAIN (FORMAT TEXT) SELECT word_id FROM word_variants WHERE variant_text LIKE '% %' AND lower(variant_text) LIKE ?",
		"% alias 01%",
	)

	if !strings.Contains(plan, "Index Scan") && !strings.Contains(plan, "Bitmap Index Scan") {
		t.Fatalf("expected index-backed variant phrase plan, got:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on word_variants") {
		t.Fatalf("expected variant phrase plan to avoid sequential scans, got:\n%s", plan)
	}
}

func TestAPIPhrasePlan_PostgresActualPhraseQueryUsesPhraseSearchIndexes(t *testing.T) {
	h := newAPIPostgresHarness(t, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkPhraseFixtures(tb, sqlDB, 400)
	})

	h.db.Exec("ANALYZE words")
	h.db.Exec("ANALYZE word_variants")

	patternStart, patternPrefix, patternMiddle, patternEnd := buildPhrasePatterns("bench")
	query := buildActualSearchPhrasesSQLForExplain()
	args := []any{
		patternStart, patternPrefix, patternMiddle, patternEnd,
		patternStart, patternPrefix, patternMiddle, patternEnd,
		patternStart, patternPrefix, patternMiddle, patternEnd,
		patternStart, patternPrefix, patternMiddle, patternEnd,
		20,
	}

	plan := mustExplainPlanWithSeqScanDisabled(t, h.db, query, args...)

	if !strings.Contains(plan, "idx_words_phrase_lower_trgm") {
		t.Fatalf("expected words phrase trigram index in actual phrase query plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "WindowAgg") {
		t.Fatalf("expected row_number/window step in actual phrase query plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "Append") {
		t.Fatalf("expected union-all append step in actual phrase query plan, got:\n%s", plan)
	}
	if !strings.Contains(plan, "word_variants") {
		t.Fatalf("expected variant branch participation in actual phrase query plan, got:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on words") || strings.Contains(plan, "Seq Scan on word_variants") {
		t.Fatalf("expected actual phrase query to avoid sequential scans on phrase tables, got:\n%s", plan)
	}
}

func BenchmarkAPISearchWords_PostgresPrefix(b *testing.B) {
	h := newAPIPostgresHarness(b, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkSearchFixtures(tb, sqlDB, 1000)
	})

	benchmarkAPISearchPath(b, h, "/api/v1/search?q=benchterm00&limit=20")
}

func BenchmarkAPISearchWords_PostgresFuzzy(b *testing.B) {
	h := newAPIPostgresHarness(b, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkSearchFixtures(tb, sqlDB, 1000)
	})

	benchmarkAPISearchPath(b, h, "/api/v1/search?q=term00&limit=20")
}

func BenchmarkAPISearchPhrases_PostgresWordBoundary(b *testing.B) {
	h := newAPIPostgresHarness(b, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkPhraseFixtures(tb, sqlDB, 1000)
	})

	benchmarkAPISearchPath(b, h, "/api/v1/phrases?q=bench&limit=20")
}

func BenchmarkAPISearchPhrases_PostgresMultiWordPrefix(b *testing.B) {
	h := newAPIPostgresHarness(b, func(tb testing.TB, sqlDB *sql.DB) {
		seedBenchmarkPhraseFixtures(tb, sqlDB, 1000)
	})

	benchmarkAPISearchPath(b, h, "/api/v1/phrases?q=bench%20ph&limit=20")
}

func benchmarkAPISearchPath(b *testing.B, h *apiPostgresHarness, path string) {
	b.Helper()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		h.router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			b.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
	}
}

func newAPIPostgresHarness(tb testing.TB, seed func(testing.TB, *sql.DB)) *apiPostgresHarness {
	tb.Helper()

	dsn := createAdminOwnedAPITestDatabase(tb, "api_query")
	db, err := gorm.Open(pgdriver.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		tb.Fatalf("gorm.Open() error = %v", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		tb.Fatalf("db.DB() error = %v", err)
	}
	tb.Cleanup(func() { _ = sqlDB.Close() })

	migrateAPITestSchema(tb, db)
	executeSQLFileTB(tb, sqlDB, filepath.Join("..", "..", "db", "sample_data.sql"))

	if seed != nil {
		seed(tb, sqlDB)
	}

	cfg := &config.Config{
		GinMode:            gin.TestMode,
		APIBatchMaxSize:    100,
		APISearchMaxLimit:  100,
		APISuggestMaxLimit: 50,
		EnableRateLimit:    false,
		EnableCORS:         false,
		EnableTimeout:      false,
		EnableRequestID:    false,
		EnableCache:        false,
		TimeoutSeconds:     1,
	}

	repo := repository.NewRepository(db)
	wordService := service.NewWordService(repo, service.ServiceConfig{
		BatchMaxSize:    cfg.APIBatchMaxSize,
		SearchMaxLimit:  cfg.APISearchMaxLimit,
		SuggestMaxLimit: cfg.APISuggestMaxLimit,
	})
	wordHandler := handler.NewWordHandler(wordService, cfg)

	return &apiPostgresHarness{
		db:     db,
		sqlDB:  sqlDB,
		router: setupRouter(wordHandler, cfg, sqlDB),
		cfg:    cfg,
	}
}

func seedTriSearchFixtures(tb testing.TB, sqlDB *sql.DB) {
	tb.Helper()

	fixtures := []struct {
		headword      string
		cefrLevel     int
		cetLevel      int
		oxfordLevel   int
		schoolLevel   int
		frequencyRank int
		collinsStars  int
		translationZH string
		pos           int
		definitionEN  string
		definitionZH  string
	}{
		{"trimark", 2, 1, 1, 2, 30, 5, "三重标记", 1, "a triple mark", "三重标记"},
		{"trident", 2, 1, 1, 1, 40, 4, "三叉戟", 1, "a three-pronged spear", "三叉戟"},
		{"triage", 4, 1, 2, 3, 20, 5, "分诊", 1, "medical sorting", "分诊"},
		{"trigger", 2, 2, 1, 2, 10, 5, "触发", 2, "to cause something", "触发"},
	}

	for _, fixture := range fixtures {
		wordID := insertWordWithSchoolLevel(tb, sqlDB, fixture.headword, fixture.cefrLevel, fixture.cetLevel, fixture.oxfordLevel, fixture.schoolLevel, fixture.frequencyRank, fixture.collinsStars, fixture.translationZH)
		insertSense(tb, sqlDB, wordID, fixture.pos, fixture.definitionEN, fixture.definitionZH, 1, fixture.cefrLevel, "oxford", fixture.oxfordLevel)
	}
}

func seedPhraseFixtures(tb testing.TB, sqlDB *sql.DB) {
	tb.Helper()

	carryOnID := insertWord(tb, sqlDB, "carry on", 3, 1, 1, 40, 5, "继续")
	insertSense(tb, sqlDB, carryOnID, 2, "to continue doing something", "继续做某事", 1, 3, "oxford", 1)
	insertVariant(tb, sqlDB, carryOnID, "carried on", 1, intPtrValue(1), 55)

	carryOutID := insertWord(tb, sqlDB, "carry out", 4, 2, 2, 70, 4, "执行")
	insertSense(tb, sqlDB, carryOutID, 2, "to perform or complete something", "执行或完成某事", 1, 4, "oxford", 2)

	howAboutID := insertWord(tb, sqlDB, "how about", 2, 0, 1, 50, 4, "怎么样")
	insertSense(tb, sqlDB, howAboutID, 9, "used to make a suggestion", "用于提出建议", 1, 2, "oxford", 1)

	howAreYouID := insertWord(tb, sqlDB, "how are you", 2, 0, 1, 65, 4, "你好吗")
	insertSense(tb, sqlDB, howAreYouID, 9, "used as a greeting", "用于问候", 1, 2, "oxford", 1)

	starTurnID := insertWord(tb, sqlDB, "star turn", 4, 2, 2, 30, 4, "精彩表演")
	insertSense(tb, sqlDB, starTurnID, 1, "an outstanding performance", "精彩的表演", 1, 4, "oxford", 2)

	startUpID := insertWord(tb, sqlDB, "start up", 3, 1, 1, 45, 5, "启动")
	insertSense(tb, sqlDB, startUpID, 2, "to begin operating", "开始运转", 1, 3, "oxford", 1)

	starlightExpressID := insertWord(tb, sqlDB, "starlight express", 5, 0, 0, 90, 3, "星光快车")
	insertSense(tb, sqlDB, starlightExpressID, 1, "a named entertainment work", "作品名称", 1, 5, "cefrj", 0)
}

func seedBenchmarkSearchFixtures(tb testing.TB, sqlDB *sql.DB, count int) {
	tb.Helper()

	tx, err := sqlDB.Begin()
	if err != nil {
		tb.Fatalf("sqlDB.Begin() error = %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i := 0; i < count; i++ {
		headword := fmt.Sprintf("benchterm%04d", i)
		var wordID int64
		err := tx.QueryRow(
			`INSERT INTO words (headword, headword_normalized, cefr_level, cefr_source, cet_level, oxford_level, frequency_rank, frequency_count, collins_stars, translation_zh)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			headword,
			textutil.ToNormalized(headword),
			(i%6)+1,
			"oxford",
			(i%2)+1,
			(i%2)+1,
			i+1,
			10000-i,
			(i%5)+1,
			"benchmark word",
		).Scan(&wordID)
		if err != nil {
			tb.Fatalf("insert bench word %s: %v", headword, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO senses (word_id, pos, definition_en, definition_zh, sense_order, cefr_level, cefr_source, oxford_level)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			wordID,
			1,
			"benchmark noun",
			"基准词",
			1,
			(i%6)+1,
			"oxford",
			(i%2)+1,
		); err != nil {
			tb.Fatalf("insert bench sense %s: %v", headword, err)
		}

		if i%3 == 0 {
			variant := fmt.Sprintf("benchvar%04d", i)
			if _, err := tx.Exec(
				`INSERT INTO word_variants (word_id, variant_text, headword_normalized, kind, form_type, frequency_rank, frequency_count)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				wordID,
				variant,
				textutil.ToNormalized(variant),
				2,
				nil,
				i+1,
				10000-i,
			); err != nil {
				tb.Fatalf("insert bench variant %s: %v", variant, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		tb.Fatalf("tx.Commit() error = %v", err)
	}
}

func seedBenchmarkPhraseFixtures(tb testing.TB, sqlDB *sql.DB, count int) {
	tb.Helper()

	tx, err := sqlDB.Begin()
	if err != nil {
		tb.Fatalf("sqlDB.Begin() error = %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i := 0; i < count; i++ {
		headword := fmt.Sprintf("bench phrase %04d", i)
		var wordID int64
		err := tx.QueryRow(
			`INSERT INTO words (headword, headword_normalized, cefr_level, cefr_source, cet_level, oxford_level, frequency_rank, frequency_count, collins_stars, translation_zh)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
			headword,
			textutil.ToNormalized(headword),
			(i%6)+1,
			"oxford",
			(i%2)+1,
			(i%2)+1,
			i+1,
			10000-i,
			(i%5)+1,
			"benchmark phrase",
		).Scan(&wordID)
		if err != nil {
			tb.Fatalf("insert bench phrase %s: %v", headword, err)
		}

		if _, err := tx.Exec(
			`INSERT INTO senses (word_id, pos, definition_en, definition_zh, sense_order, cefr_level, cefr_source, oxford_level)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			wordID,
			1,
			"benchmark phrase entry",
			"基准短语",
			1,
			(i%6)+1,
			"oxford",
			(i%2)+1,
		); err != nil {
			tb.Fatalf("insert bench phrase sense %s: %v", headword, err)
		}

		if i%3 == 0 {
			variant := fmt.Sprintf("bench alias %04d", i)
			if _, err := tx.Exec(
				`INSERT INTO word_variants (word_id, variant_text, headword_normalized, kind, form_type, frequency_rank, frequency_count)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				wordID,
				variant,
				textutil.ToNormalized(variant),
				2,
				nil,
				i+1,
				10000-i,
			); err != nil {
				tb.Fatalf("insert bench phrase variant %s: %v", variant, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		tb.Fatalf("tx.Commit() error = %v", err)
	}
}

func insertWord(tb testing.TB, sqlDB *sql.DB, headword string, cefrLevel, cetLevel, oxfordLevel, frequencyRank, collinsStars int, translationZH string) int64 {
	return insertWordWithSchoolLevel(tb, sqlDB, headword, cefrLevel, cetLevel, oxfordLevel, 0, frequencyRank, collinsStars, translationZH)
}

func insertWordWithSchoolLevel(tb testing.TB, sqlDB *sql.DB, headword string, cefrLevel, cetLevel, oxfordLevel, schoolLevel, frequencyRank, collinsStars int, translationZH string) int64 {
	tb.Helper()

	var wordID int64
	err := sqlDB.QueryRow(
		`INSERT INTO words (headword, headword_normalized, cefr_level, cefr_source, cet_level, oxford_level, school_level, frequency_rank, frequency_count, collins_stars, translation_zh)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
		headword,
		textutil.ToNormalized(headword),
		cefrLevel,
		"oxford",
		cetLevel,
		oxfordLevel,
		schoolLevel,
		frequencyRank,
		1000-frequencyRank,
		collinsStars,
		translationZH,
	).Scan(&wordID)
	if err != nil {
		tb.Fatalf("insert word %s: %v", headword, err)
	}

	return wordID
}

func insertSense(tb testing.TB, sqlDB *sql.DB, wordID int64, pos int, definitionEN, definitionZH string, senseOrder, cefrLevel int, cefrSource string, oxfordLevel int) {
	tb.Helper()

	if _, err := sqlDB.Exec(
		`INSERT INTO senses (word_id, pos, definition_en, definition_zh, sense_order, cefr_level, cefr_source, oxford_level)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		wordID,
		pos,
		definitionEN,
		definitionZH,
		senseOrder,
		cefrLevel,
		cefrSource,
		oxfordLevel,
	); err != nil {
		tb.Fatalf("insert sense for word_id %d: %v", wordID, err)
	}
}

func insertVariant(tb testing.TB, sqlDB *sql.DB, wordID int64, variantText string, kind int, formType *int, frequencyRank int) {
	tb.Helper()

	if _, err := sqlDB.Exec(
		`INSERT INTO word_variants (word_id, variant_text, headword_normalized, kind, form_type, frequency_rank, frequency_count)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		wordID,
		variantText,
		textutil.ToNormalized(variantText),
		kind,
		formType,
		frequencyRank,
		1000-frequencyRank,
	); err != nil {
		tb.Fatalf("insert variant for word_id %d: %v", wordID, err)
	}
}

func intPtrValue(value int) *int {
	return &value
}

func performPostgresAPIRequest(tb testing.TB, router http.Handler, method, target, body string) *httptest.ResponseRecorder {
	tb.Helper()

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	router.ServeHTTP(recorder, req)
	return recorder
}

func decodeJSONResponse(tb testing.TB, recorder *httptest.ResponseRecorder, target any) {
	tb.Helper()
	if err := json.NewDecoder(strings.NewReader(recorder.Body.String())).Decode(target); err != nil {
		tb.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
	}
}

func reflectStringSlice(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func containsSuggestHeadword(items []commonmodel.SuggestResponse, headword string) bool {
	for _, item := range items {
		if item.Headword == headword {
			return true
		}
	}
	return false
}

func containsVariantInfo(items []commonmodel.VariantResponse, variantText string) bool {
	for _, item := range items {
		if item.VariantText == variantText {
			return true
		}
	}
	return false
}

func containsWordVariant(items []commonmodel.VariantResponse, variantText string) bool {
	for _, item := range items {
		if item.VariantText == variantText {
			return true
		}
	}
	return false
}

func buildActualSearchWordsPageSQLForExplain() string {
	return `
		EXPLAIN (FORMAT TEXT)
		WITH combined AS (
			SELECT 
				w.id,
				1 AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM words w
			WHERE w.headword_normalized LIKE ?
			UNION ALL
			SELECT 
				w.id,
				3 AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM words w
			WHERE w.headword_normalized LIKE ?
			UNION ALL
			SELECT 
				v.word_id AS id,
				2 AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM word_variants v
			INNER JOIN words w ON v.word_id = w.id
			WHERE v.headword_normalized LIKE ?
			UNION ALL
			SELECT 
				v.word_id AS id,
				4 AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM word_variants v
			INNER JOIN words w ON v.word_id = w.id
			WHERE v.headword_normalized LIKE ?
		), ranked AS (
			SELECT 
				id,
				priority,
				freq_rank,
				headword,
				ROW_NUMBER() OVER (
					PARTITION BY id
					ORDER BY freq_rank ASC, priority ASC, headword ASC
				) AS rn
			FROM combined
		)
		SELECT id, priority, freq_rank
		FROM ranked
		WHERE rn = 1
		ORDER BY freq_rank ASC, priority ASC, headword ASC
		LIMIT ? OFFSET ?
	`
}

func buildActualSearchPhrasesSQLForExplain() string {
	return `
		EXPLAIN (FORMAT TEXT)
		WITH combined AS (
			SELECT
				w.id,
				w.id AS id_ord,
				CASE
					WHEN LOWER(w.headword) LIKE ? THEN 1
					WHEN LOWER(w.headword) LIKE ? THEN 1
					WHEN LOWER(w.headword) LIKE ? THEN 3
					WHEN LOWER(w.headword) LIKE ? THEN 5
					ELSE 7
				END AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM words w
			WHERE w.headword LIKE '% %'
				AND (
					LOWER(w.headword) LIKE ?
					OR LOWER(w.headword) LIKE ?
					OR LOWER(w.headword) LIKE ?
					OR LOWER(w.headword) LIKE ?
				)

			UNION ALL

			SELECT
				v.word_id AS id,
				w.id AS id_ord,
				CASE
					WHEN LOWER(v.variant_text) LIKE ? THEN 2
					WHEN LOWER(v.variant_text) LIKE ? THEN 2
					WHEN LOWER(v.variant_text) LIKE ? THEN 4
					WHEN LOWER(v.variant_text) LIKE ? THEN 6
					ELSE 8
				END AS priority,
				CASE WHEN w.frequency_rank = 0 THEN 999999 ELSE w.frequency_rank END AS freq_rank,
				w.headword
			FROM word_variants v
			JOIN words w ON w.id = v.word_id
			WHERE w.headword LIKE '% %'
				AND v.variant_text LIKE '% %'
				AND (
					LOWER(v.variant_text) LIKE ?
					OR LOWER(v.variant_text) LIKE ?
					OR LOWER(v.variant_text) LIKE ?
					OR LOWER(v.variant_text) LIKE ?
				)
		), ranked AS (
			SELECT
				id,
				priority,
				freq_rank,
				id_ord,
				headword,
				ROW_NUMBER() OVER (
					PARTITION BY id
					ORDER BY freq_rank, priority, id_ord, headword
				) AS rn
			FROM combined
		)
		SELECT id, priority, freq_rank, id_ord, headword
		FROM ranked
		WHERE rn = 1
		ORDER BY freq_rank, priority, id_ord, headword
		LIMIT ?
	`
}

func buildPhrasePatterns(keyword string) (string, string, string, string) {
	lowerKeyword := strings.ToLower(strings.TrimSpace(keyword))
	escaped := escapeLikePattern(lowerKeyword)
	patternStart := escaped + " %"
	patternMiddle := "% " + escaped + " %"
	patternEnd := "% " + escaped
	patternPrefix := escaped + " %"
	if strings.Contains(lowerKeyword, " ") {
		patternPrefix = escaped + "%"
	}
	return patternStart, patternPrefix, patternMiddle, patternEnd
}

func escapeLikePattern(input string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(input)
}

func mustExplainPlanWithSeqScanDisabled(tb testing.TB, db *gorm.DB, query string, args ...any) string {
	tb.Helper()

	var plan string
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
			return err
		}
		rows, err := tx.Raw(query, args...).Rows()
		if err != nil {
			return err
		}
		defer rows.Close()

		var builder strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return err
			}
			builder.WriteString(line)
			builder.WriteByte('\n')
		}
		if err := rows.Err(); err != nil {
			return err
		}
		plan = builder.String()
		return nil
	})
	if err != nil {
		tb.Fatalf("EXPLAIN error = %v", err)
	}

	return plan
}

func createAdminOwnedAPITestDatabase(tb testing.TB, prefix string) string {
	tb.Helper()

	adminDSN := requireAPIPostgresAdminDSN(tb)
	adminInfo := mustParsePostgresDSN(tb, adminDSN)
	adminDB, err := gorm.Open(pgdriver.Open(adminDSN), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		tb.Fatalf("gorm.Open() admin error = %v", err)
	}

	adminSQLDB, err := adminDB.DB()
	if err != nil {
		tb.Fatalf("adminDB.DB() error = %v", err)
	}
	tb.Cleanup(func() { _ = adminSQLDB.Close() })

	databaseName := uniquePostgresIdentifier(prefix)
	if _, err := adminSQLDB.Exec("CREATE DATABASE " + quoteIdentifier(databaseName)); err != nil {
		if strings.Contains(err.Error(), "SQLSTATE 42501") {
			tb.Skipf("admin PostgreSQL test DSN lacks CREATEDB needed for API integration fixtures: %v", err)
		}
		tb.Fatalf("create database %s: %v", databaseName, err)
	}
	tb.Cleanup(func() {
		_, _ = adminSQLDB.Exec("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", databaseName)
		_, _ = adminSQLDB.Exec("DROP DATABASE IF EXISTS " + quoteIdentifier(databaseName))
	})

	return buildPostgresDSN(adminInfo, databaseName, "", "")
}

func migrateAPITestSchema(tb testing.TB, db *gorm.DB) {
	tb.Helper()

	if err := postgresutil.EnsureRequiredExtensionsEnabled(db); err != nil {
		tb.Fatalf("EnsureRequiredExtensionsEnabled() error = %v", err)
	}

	migrator := migration.NewMigrator(db)
	if err := migrator.Migrate(&migration.MigrateOptions{}); err != nil {
		tb.Fatalf("migrator.Migrate() error = %v", err)
	}

	status, err := migrator.VerifyMigration(nil, nil)
	if err != nil {
		tb.Fatalf("migrator.VerifyMigration() error = %v", err)
	}
	if status == nil || !status.IsComplete() {
		tb.Fatalf("migration verification incomplete: %+v", status)
	}
}

func requireAPIPostgresAdminDSN(tb testing.TB) string {
	tb.Helper()

	adminDSN := strings.TrimSpace(os.Getenv(apiTestAdminDSNEnv))
	if adminDSN == "" {
		adminDSN = strings.TrimSpace(os.Getenv(apiTestDSNEnv))
	}
	if adminDSN == "" {
		tb.Skipf("set %s or %s to a disposable PostgreSQL DSN for API integration tests", apiTestAdminDSNEnv, apiTestDSNEnv)
	}
	if err := validateManagedPostgresDSNForHostPolicy(adminDSN); err != nil {
		tb.Fatalf("unsafe PostgreSQL DSN for API integration tests: %v", err)
	}
	return adminDSN
}

func validateManagedPostgresDSNForHostPolicy(dsn string) error {
	info, err := parsePostgresDSN(dsn)
	if err != nil {
		return err
	}
	if allowNonLocalPostgresTestHosts() {
		return nil
	}
	if !isLocalPostgresTestHost(info.Host) {
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

func mustParsePostgresDSN(tb testing.TB, dsn string) postgresDSNConfig {
	tb.Helper()

	config, err := parsePostgresDSN(dsn)
	if err != nil {
		tb.Fatalf("parse PostgreSQL DSN: %v", err)
	}
	return config
}

func parsePostgresDSN(dsn string) (postgresDSNConfig, error) {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsedURL, err := url.Parse(dsn)
		if err != nil {
			return postgresDSNConfig{}, err
		}

		params := map[string]string{}
		for key, values := range parsedURL.Query() {
			if len(values) > 0 {
				params[key] = values[len(values)-1]
			}
		}

		host := parsedURL.Hostname()
		if socketHost := parsedURL.Query().Get("host"); socketHost != "" {
			host = socketHost
		}

		port := parsedURL.Port()
		if port == "" {
			port = "5432"
		}

		password, _ := parsedURL.User.Password()
		return postgresDSNConfig{
			Host:     host,
			Port:     port,
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
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
		switch strings.ToLower(key) {
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

	if config.Port == "" {
		config.Port = "5432"
	}
	if config.SSLMode == "" {
		config.SSLMode = "disable"
	}

	return config, nil
}

func buildPostgresDSN(info postgresDSNConfig, databaseName string, user string, password string) string {
	parts := []string{}
	if info.Host != "" {
		parts = append(parts, "host="+info.Host)
	}
	if info.Port != "" {
		parts = append(parts, "port="+info.Port)
	}
	if user == "" {
		user = info.User
	}
	if password == "" {
		password = info.Password
	}
	if user != "" {
		parts = append(parts, "user="+user)
	}
	if password != "" {
		parts = append(parts, "password="+password)
	}
	if databaseName == "" {
		databaseName = info.Database
	}
	if databaseName != "" {
		parts = append(parts, "dbname="+databaseName)
	}
	if info.SSLMode != "" {
		parts = append(parts, "sslmode="+info.SSLMode)
	}
	for key, value := range info.Params {
		if strings.EqualFold(key, "sslmode") {
			continue
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, " ")
}

func quoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func uniquePostgresIdentifier(prefix string) string {
	prefix = strings.ToLower(prefix)
	prefix = strings.ReplaceAll(prefix, "-", "_")
	prefix = strings.ReplaceAll(prefix, " ", "_")
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

func executeSQLFileTB(tb testing.TB, db *sql.DB, path string) {
	tb.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("read %s: %v", path, err)
	}

	statements := splitSQLStatements(string(content))
	var tx *sql.Tx

	for _, statement := range statements {
		trimmed := strings.TrimSpace(statement)
		if trimmed == "" {
			continue
		}
		control := normalizeSQLControlStatement(trimmed)
		if control == "" {
			continue
		}

		switch {
		case strings.EqualFold(control, "BEGIN"):
			if tx != nil {
				tb.Fatalf("nested BEGIN in %s", path)
			}
			tx, err = db.Begin()
			if err != nil {
				tb.Fatalf("begin transaction for %s: %v", path, err)
			}
		case strings.EqualFold(control, "COMMIT"):
			if tx == nil {
				tb.Fatalf("COMMIT without active transaction in %s", path)
			}
			if err := tx.Commit(); err != nil {
				tb.Fatalf("commit transaction for %s: %v", path, err)
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
				tb.Fatalf("execute statement from %s: %v\nstatement:\n%s", path, err, trimmed)
			}
		}
	}

	if tx != nil {
		if err := tx.Commit(); err != nil {
			tb.Fatalf("commit trailing transaction for %s: %v", path, err)
		}
	}
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

func normalizeSQLControlStatement(statement string) string {
	trimmed := strings.TrimSpace(statement)
	for trimmed != "" {
		switch {
		case strings.HasPrefix(trimmed, "--"):
			newline := strings.IndexByte(trimmed, '\n')
			if newline == -1 {
				return ""
			}
			trimmed = strings.TrimSpace(trimmed[newline+1:])
		case strings.HasPrefix(trimmed, "/*"):
			end := strings.Index(trimmed[2:], "*/")
			if end == -1 {
				return trimmed
			}
			trimmed = strings.TrimSpace(trimmed[end+4:])
		default:
			return trimmed
		}
	}

	return ""
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
	if state.inBlockComment {
		current.WriteByte(char)
		if char == '*' && next == '/' {
			current.WriteByte(next)
			*index++
			state.inBlockComment = false
		}
		return true
	}
	return false
}

func (state *sqlSplitState) consumeDollarQuote(current *strings.Builder, content string, index *int, char byte) bool {
	if state.dollarQuoteTag == "" {
		return false
	}
	remaining := content[*index:]
	if strings.HasPrefix(remaining, state.dollarQuoteTag) {
		current.WriteString(state.dollarQuoteTag)
		*index += len(state.dollarQuoteTag) - 1
		state.dollarQuoteTag = ""
		return true
	}
	current.WriteByte(char)
	return true
}

func (state *sqlSplitState) startComment(current *strings.Builder, char, next byte, index *int) bool {
	if state.inSingleQuote || state.dollarQuoteTag != "" {
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
	if state.dollarQuoteTag != "" {
		return false
	}
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
	remaining := content[*index:]
	end := strings.IndexByte(remaining[1:], '$')
	if end == -1 {
		return false
	}
	tag := remaining[:end+2]
	if len(tag) > 1 {
		for i := 1; i < len(tag)-1; i++ {
			if !(tag[i] == '_' || (tag[i] >= 'a' && tag[i] <= 'z') || (tag[i] >= 'A' && tag[i] <= 'Z') || (tag[i] >= '0' && tag[i] <= '9')) {
				return false
			}
		}
	}
	current.WriteString(tag)
	*index += len(tag) - 1
	state.dollarQuoteTag = tag
	return true
}

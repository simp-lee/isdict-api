package main

import (
	"errors"
	"net/http"
	"testing"
)

func TestValidateSimpleCORS_AllowsSimpleGetWithoutAllowMethods(t *testing.T) {
	cfg := settings{
		expectCORS:         true,
		expectedCORSOrigin: "https://web.isdict.test",
	}

	err := validateSimpleCORS(cfg, "https://web.isdict.test", "")
	if err != nil {
		t.Fatalf("validateSimpleCORS() error = %v, want nil", err)
	}
}

func TestValidateSimpleCORS_RejectsMissingOrigin(t *testing.T) {
	cfg := settings{expectCORS: true}

	err := validateSimpleCORS(cfg, "", "GET, POST")
	if err == nil {
		t.Fatal("validateSimpleCORS() error = nil, want non-nil")
	}
}

func TestValidateCORSPreflight_RequiresAllowMethodsForRequestedMethod(t *testing.T) {
	cfg := settings{expectedCORSOrigin: "https://web.isdict.test"}

	err := validateCORSPreflight(cfg, "https://web.isdict.test", "GET, POST, OPTIONS", "GET")
	if err != nil {
		t.Fatalf("validateCORSPreflight() error = %v, want nil", err)
	}
}

func TestValidateCORSPreflight_FailsWithoutAllowMethods(t *testing.T) {
	cfg := settings{}

	err := validateCORSPreflight(cfg, "*", "", "GET")
	if err == nil {
		t.Fatal("validateCORSPreflight() error = nil, want non-nil")
	}
}

func TestLoadSettingsDefaultsReadinessPath(t *testing.T) {
	t.Setenv("ISDICT_READINESS_PATH", "")

	cfg := loadSettings()
	if got, want := cfg.readinessPath, "/api/v1/health"; got != want {
		t.Fatalf("loadSettings() readinessPath = %q, want %q", got, want)
	}
}

func TestLoadSettingsAllowsReadinessOverride(t *testing.T) {
	t.Setenv("ISDICT_READINESS_PATH", "/readyz")

	cfg := loadSettings()
	if got, want := cfg.readinessPath, "/readyz"; got != want {
		t.Fatalf("loadSettings() readinessPath = %q, want %q", got, want)
	}
}

func TestLoadSettingsDefaultsRateLimitPath(t *testing.T) {
	t.Setenv("ISDICT_RATE_LIMIT_PATH", "")

	cfg := loadSettings()
	if got, want := cfg.rateLimitPath, defaultNonExemptProbePath; got != want {
		t.Fatalf("loadSettings() rateLimitPath = %q, want %q", got, want)
	}
}

func TestSummarizeBurstResultsTreatsNotFoundAsBaseline(t *testing.T) {
	t.Parallel()

	results := make(chan burstResult, 4)
	results <- burstResult{requestNumber: 1, statusCode: http.StatusNotFound}
	results <- burstResult{requestNumber: 2, statusCode: http.StatusTooManyRequests}
	results <- burstResult{requestNumber: 3, statusCode: http.StatusServiceUnavailable}
	results <- burstResult{requestNumber: 4, err: errors.New("timeout")}
	close(results)

	summary := summarizeBurstResults(map[int]int{http.StatusNotFound: 1}, results)
	if got, want := summary.baselineCount, 1; got != want {
		t.Fatalf("baselineCount = %d, want %d", got, want)
	}
	if got, want := summary.rateLimitedCount, 1; got != want {
		t.Fatalf("rateLimitedCount = %d, want %d", got, want)
	}
	if got, want := summary.unexpectedCount, 1; got != want {
		t.Fatalf("unexpectedCount = %d, want %d", got, want)
	}
	if got, want := summary.requestErrors, 1; got != want {
		t.Fatalf("requestErrors = %d, want %d", got, want)
	}
}

func TestSelectBurstBaselineStatusesRejectsAll429(t *testing.T) {
	t.Parallel()

	_, _, err := selectBurstBaselineStatuses([]int{http.StatusTooManyRequests, http.StatusTooManyRequests})
	if err == nil {
		t.Fatal("selectBurstBaselineStatuses() error = nil, want non-nil")
	}
}

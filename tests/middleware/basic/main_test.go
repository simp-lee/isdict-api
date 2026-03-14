package main

import (
	"errors"
	"net/http"
	"testing"
)

func TestValidateRequestIDCORS_AllowsSimpleGetWithoutAllowMethods(t *testing.T) {
	cfg := settings{
		expectRequestID:    true,
		expectCORS:         true,
		expectedCORSOrigin: "https://web.isdict.test",
	}

	err := validateRequestIDCORS(cfg, 200, "req-1", "https://web.isdict.test", "")
	if err != nil {
		t.Fatalf("validateRequestIDCORS() error = %v, want nil", err)
	}
}

func TestValidateCORSPreflight_RequiresAllowMethodsForRequestedMethod(t *testing.T) {
	cfg := settings{expectedCORSOrigin: "https://web.isdict.test"}

	err := validateCORSPreflight(cfg, 204, "https://web.isdict.test", "GET, POST, OPTIONS", "GET")
	if err != nil {
		t.Fatalf("validateCORSPreflight() error = %v, want nil", err)
	}
}

func TestValidateCORSPreflight_FailsWithoutAllowMethods(t *testing.T) {
	cfg := settings{}

	err := validateCORSPreflight(cfg, 204, "*", "", "GET")
	if err == nil {
		t.Fatal("validateCORSPreflight() error = nil, want non-nil")
	}
}

func TestLoadSettingsReplayStabilityDisabledByDefault(t *testing.T) {
	t.Setenv("ISDICT_ENABLE_REPLAY_STABILITY_CHECK", "")

	cfg := loadSettings()
	if cfg.enableReplayStabilityCheck {
		t.Fatal("loadSettings() enabled replay stability by default, want disabled")
	}
}

func TestLoadSettingsReplayStabilityOptIn(t *testing.T) {
	t.Setenv("ISDICT_ENABLE_REPLAY_STABILITY_CHECK", "1")

	cfg := loadSettings()
	if !cfg.enableReplayStabilityCheck {
		t.Fatal("loadSettings() did not enable replay stability after opt-in env var")
	}
}

func TestLoadSettingsDefaultsReadinessPath(t *testing.T) {
	t.Setenv("ISDICT_READINESS_PATH", "")

	cfg := loadSettings()
	if got, want := cfg.readinessPath, "/api/v1/health"; got != want {
		t.Fatalf("loadSettings() readinessPath = %q, want %q", got, want)
	}
}

func TestLoadSettingsDefaultsRateLimitPathAndExpectedMin429(t *testing.T) {
	t.Setenv("ISDICT_RATE_LIMIT_PATH", "")
	t.Setenv("ISDICT_EXPECT_MIN_429", "")

	cfg := loadSettings()
	if got, want := cfg.rateLimitPath, defaultNonExemptProbePath; got != want {
		t.Fatalf("loadSettings() rateLimitPath = %q, want %q", got, want)
	}
	if got, want := cfg.expectedMin429, 1; got != want {
		t.Fatalf("loadSettings() expectedMin429 = %d, want %d", got, want)
	}
}

func TestDefaultNonExemptProbePathIsAPINotFoundProbe(t *testing.T) {
	t.Parallel()

	if got, want := defaultNonExemptProbePath, "/api/__middleware_probe__/rate-limit"; got != want {
		t.Fatalf("defaultNonExemptProbePath = %q, want %q", got, want)
	}
	if got, want := corsPreflightProbePath, defaultNonExemptProbePath; got != want {
		t.Fatalf("corsPreflightProbePath = %q, want %q", got, want)
	}
}

func TestValidateBurstBaselineStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		wantError bool
	}{
		{name: "ok", status: http.StatusOK},
		{name: "not found baseline is allowed", status: http.StatusNotFound},
		{name: "too many requests is rejected", status: http.StatusTooManyRequests, wantError: true},
		{name: "server error is rejected", status: http.StatusServiceUnavailable, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBurstBaselineStatus(tt.status)
			if (err != nil) != tt.wantError {
				t.Fatalf("validateBurstBaselineStatus(%d) error = %v, wantError %t", tt.status, err, tt.wantError)
			}
		})
	}
}

func TestSummarizeBurstResultsTreatsBaselineAsNormal(t *testing.T) {
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
	if got, want := summary.firstRateLimitedRequest, 2; got != want {
		t.Fatalf("firstRateLimitedRequest = %d, want %d", got, want)
	}
	if got := summary.statusCounts[http.StatusNotFound]; got != 1 {
		t.Fatalf("statusCounts[404] = %d, want 1", got)
	}
	if got := summary.unexpectedStatusCounts[http.StatusServiceUnavailable]; got != 1 {
		t.Fatalf("unexpectedStatusCounts[503] = %d, want 1", got)
	}
}

func TestSelectBurstBaselineStatuses(t *testing.T) {
	t.Parallel()

	t.Run("chooses dominant non 429 status", func(t *testing.T) {
		statuses, got, err := selectBurstBaselineStatuses([]int{
			http.StatusOK,
			http.StatusNotFound,
			http.StatusNotFound,
			http.StatusTooManyRequests,
			http.StatusNotFound,
		})
		if err != nil {
			t.Fatalf("selectBurstBaselineStatuses() error = %v, want nil", err)
		}
		if want := http.StatusNotFound; got != want {
			t.Fatalf("selectBurstBaselineStatuses() primary = %d, want %d", got, want)
		}
		if statuses[http.StatusOK] != 1 || statuses[http.StatusNotFound] != 3 {
			t.Fatalf("selectBurstBaselineStatuses() counts = %#v, want 200=>1 and 404=>3", statuses)
		}
	})

	t.Run("fails when every probe is 429", func(t *testing.T) {
		_, _, err := selectBurstBaselineStatuses([]int{
			http.StatusTooManyRequests,
			http.StatusTooManyRequests,
		})
		if err == nil {
			t.Fatal("selectBurstBaselineStatuses() error = nil, want non-nil")
		}
	})
}

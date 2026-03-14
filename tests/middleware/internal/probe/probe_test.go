package probe

import (
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestStableReplayResult(t *testing.T) {
	first := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	second := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}

	ok, message := StableReplayResult(first, []byte(`{"status":"ok"}`), second, []byte(`{"status":"ok"}`))
	if !ok {
		t.Fatalf("StableReplayResult() ok = false, message = %q", message)
	}
	if message == "" {
		t.Fatal("StableReplayResult() returned empty success message")
	}
}

func TestStableReplayResultRejectsChangedBodies(t *testing.T) {
	first := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	second := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}

	ok, message := StableReplayResult(first, []byte(`{"status":"ok"}`), second, []byte(`{"status":"changed"}`))
	if ok {
		t.Fatal("StableReplayResult() ok = true, want false")
	}
	if message == "" {
		t.Fatal("StableReplayResult() returned empty failure message")
	}
}

func TestCacheReplayResultRequiresExplicitSignal(t *testing.T) {
	first := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	second := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}

	ok, message := CacheReplayResult(first, []byte(`{"status":"ok"}`), second, []byte(`{"status":"ok"}`), "", "")
	if ok {
		t.Fatal("CacheReplayResult() ok = true, want false")
	}
	if message == "" {
		t.Fatal("CacheReplayResult() returned empty failure message")
	}
	if got, want := message, "cache probe requires explicit ISDICT_CACHE_HIT_HEADER and ISDICT_CACHE_HIT_VALUE settings"; got != want {
		t.Fatalf("CacheReplayResult() message = %q, want %q", got, want)
	}
}

func TestCacheReplayResultRequiresMissToHitTransition(t *testing.T) {
	first := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	first.Header.Set("X-Cache", "MISS")
	second := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	second.Header.Set("X-Cache", "HIT")

	ok, message := CacheReplayResult(first, []byte(`{"status":"ok"}`), second, []byte(`{"status":"ok"}`), "X-Cache", "HIT")
	if !ok {
		t.Fatalf("CacheReplayResult() ok = false, message = %q", message)
	}
	if message == "" {
		t.Fatal("CacheReplayResult() returned empty success message")
	}
}

func TestCacheReplayResultRejectsWarmCacheStartingState(t *testing.T) {
	first := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	first.Header.Set("X-Cache", "HIT")
	second := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	second.Header.Set("X-Cache", "HIT")

	ok, message := CacheReplayResult(first, []byte(`{"status":"ok"}`), second, []byte(`{"status":"ok"}`), "X-Cache", "HIT")
	if ok {
		t.Fatal("CacheReplayResult() ok = true, want false")
	}
	if message == "" {
		t.Fatal("CacheReplayResult() returned empty failure message")
	}
}

func TestValidateRateLimitHeadersAcceptsDailyOnlyConfiguration(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	resp.Header.Set("X-RateLimit-Limit-Day", "5000")
	resp.Header.Set("X-RateLimit-Remaining-Day", "4999")
	resp.Header.Set("X-RateLimit-Reset-Day", "1700000000")

	ok, message := ValidateRateLimitHeaders(resp, "", "", "5000")
	if !ok {
		t.Fatalf("ValidateRateLimitHeaders() ok = false, message = %q", message)
	}
	if message == "" {
		t.Fatal("ValidateRateLimitHeaders() returned empty success message")
	}
}

func TestValidateRateLimitHeadersAcceptsNotFoundBaselineWhenHeadersPresent(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusNotFound, Header: make(http.Header)}
	resp.Header.Set("X-RateLimit-Limit", "100")
	resp.Header.Set("X-RateLimit-Remaining", "99")
	resp.Header.Set("X-RateLimit-Reset", "1700000000")

	ok, message := ValidateRateLimitHeaders(resp, "100", "", "")
	if !ok {
		t.Fatalf("ValidateRateLimitHeaders() ok = false, message = %q", message)
	}
	if message == "" {
		t.Fatal("ValidateRateLimitHeaders() returned empty success message")
	}
}

func TestValidateRateLimitHeadersRejectsMissingDailyHeadersWhenExpected(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	resp.Header.Set("X-RateLimit-Limit", "100")
	resp.Header.Set("X-RateLimit-Remaining", "99")
	resp.Header.Set("X-RateLimit-Reset", "1700000000")

	ok, message := ValidateRateLimitHeaders(resp, "100", "", "5000")
	if ok {
		t.Fatal("ValidateRateLimitHeaders() ok = true, want false")
	}
	if message == "" {
		t.Fatal("ValidateRateLimitHeaders() returned empty failure message")
	}
}

func TestValidateRateLimitHeadersRejectsIncompleteObservedBucket(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	resp.Header.Set("X-RateLimit-Limit-Hour", "1000")
	resp.Header.Set("X-RateLimit-Remaining-Hour", "999")

	ok, message := ValidateRateLimitHeaders(resp, "", "", "")
	if ok {
		t.Fatal("ValidateRateLimitHeaders() ok = true, want false")
	}
	if message == "" {
		t.Fatal("ValidateRateLimitHeaders() returned empty failure message")
	}
}

func TestValidateRateLimitHeadersRejectsPartiallyExposedBucketWithoutLimit(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}
	resp.Header.Set("X-RateLimit-Remaining-Hour", "999")
	resp.Header.Set("X-RateLimit-Reset-Hour", "1700000000")

	ok, message := ValidateRateLimitHeaders(resp, "", "", "")
	if ok {
		t.Fatal("ValidateRateLimitHeaders() ok = true, want false")
	}
	if message == "" {
		t.Fatal("ValidateRateLimitHeaders() returned empty failure message")
	}
	if got, want := message, "missing hourly rate-limit limit header while that bucket is partially exposed"; got != want {
		t.Fatalf("ValidateRateLimitHeaders() message = %q, want %q", got, want)
	}
}

func TestValidateRateLimitHeadersRejectsMissingAllBuckets(t *testing.T) {
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header)}

	ok, message := ValidateRateLimitHeaders(resp, "", "", "")
	if ok {
		t.Fatal("ValidateRateLimitHeaders() ok = true, want false")
	}
	if got, want := message, "response did not expose any rate-limit headers"; got != want {
		t.Fatalf("ValidateRateLimitHeaders() message = %q, want %q", got, want)
	}
}

func TestAddQueryParam(t *testing.T) {
	if got := AddQueryParam("/api/v1/search?q=test", "limit", "10"); got != "/api/v1/search?q=test&limit=10" {
		t.Fatalf("AddQueryParam() = %q", got)
	}
	if got := AddQueryParam("/api/v1/search", "q", "test"); got != "/api/v1/search?q=test" {
		t.Fatalf("AddQueryParam() = %q", got)
	}
}

func TestFormatStatusDistributionSortsAndLabelsStatuses(t *testing.T) {
	t.Parallel()

	got := FormatStatusDistribution(map[int]int{
		http.StatusTooManyRequests: 4,
		http.StatusNotFound:        9,
		http.StatusOK:              2,
	})
	want := "200 OK=2, 404 Not Found=9, 429 Too Many Requests=4"
	if got != want {
		t.Fatalf("FormatStatusDistribution() = %q, want %q", got, want)
	}
}

func TestFormatStatusDistributionEmpty(t *testing.T) {
	t.Parallel()

	if got := FormatStatusDistribution(nil); got != "none" {
		t.Fatalf("FormatStatusDistribution(nil) = %q, want %q", got, "none")
	}
}

func TestEnvBool(t *testing.T) {
	t.Setenv("ISDICT_TEST_BOOL_TRUE", "true")
	t.Setenv("ISDICT_TEST_BOOL_FALSE", "0")

	if !EnvBool("ISDICT_TEST_BOOL_TRUE", false) {
		t.Fatal("EnvBool() = false, want true")
	}
	if EnvBool("ISDICT_TEST_BOOL_FALSE", true) {
		t.Fatal("EnvBool() = true, want false")
	}

	if err := os.Unsetenv("ISDICT_TEST_BOOL_MISSING"); err != nil {
		t.Fatalf("Unsetenv(ISDICT_TEST_BOOL_MISSING) error = %v", err)
	}
	if !EnvBool("ISDICT_TEST_BOOL_MISSING", true) {
		t.Fatal("EnvBool() did not return fallback for missing env")
	}
}

func TestRunBoundedHonorsConcurrencyCap(t *testing.T) {
	t.Parallel()

	const total = 24
	const concurrency = 4

	var active atomic.Int32
	var maxActive atomic.Int32
	var executed atomic.Int32

	RunBounded(total, concurrency, func(index int) {
		current := active.Add(1)
		defer active.Add(-1)
		executed.Add(1)

		for {
			observed := maxActive.Load()
			if current <= observed {
				break
			}
			if maxActive.CompareAndSwap(observed, current) {
				break
			}
		}

		time.Sleep(10 * time.Millisecond)
	})

	if got := executed.Load(); got != total {
		t.Fatalf("RunBounded() executed %d tasks, want %d", got, total)
	}
	if got := maxActive.Load(); got > concurrency {
		t.Fatalf("RunBounded() max concurrency = %d, want <= %d", got, concurrency)
	}
}

func TestStaticProbeLoadDefaultsExceedFallbackLimit(t *testing.T) {
	total, concurrency := StaticProbeLoad("", 100)
	if total <= 100 {
		t.Fatalf("StaticProbeLoad() total = %d, want > 100", total)
	}
	if concurrency <= 100 {
		t.Fatalf("StaticProbeLoad() concurrency = %d, want > 100", concurrency)
	}
}

func TestStaticProbeLoadUsesConfiguredRateLimitWhenProvided(t *testing.T) {
	total, concurrency := StaticProbeLoad("150", 100)
	if total <= 150 {
		t.Fatalf("StaticProbeLoad() total = %d, want > 150", total)
	}
	if concurrency <= 150 {
		t.Fatalf("StaticProbeLoad() concurrency = %d, want > 150", concurrency)
	}
}

// AC-R024: 外部 probe HTTP 客户端必须始终应用有界超时
func TestProbeHTTPClientAlwaysAppliesBoundedTimeout(t *testing.T) {
	// REG-024

	t.Run("positive timeout is preserved", func(t *testing.T) {
		client := NewHTTPClient(5 * time.Second)
		if client.Timeout != 5*time.Second {
			t.Fatalf("NewHTTPClient(5s).Timeout = %v, want 5s", client.Timeout)
		}
	})

	t.Run("zero timeout falls back to default", func(t *testing.T) {
		client := NewHTTPClient(0)
		if client.Timeout != DefaultHTTPTimeout {
			t.Fatalf("NewHTTPClient(0).Timeout = %v, want %v", client.Timeout, DefaultHTTPTimeout)
		}
	})

	t.Run("negative timeout falls back to default", func(t *testing.T) {
		client := NewHTTPClient(-10 * time.Second)
		if client.Timeout != DefaultHTTPTimeout {
			t.Fatalf("NewHTTPClient(-10s).Timeout = %v, want %v", client.Timeout, DefaultHTTPTimeout)
		}
	})

	t.Run("HTTPTimeoutFromEnv returns default for missing env", func(t *testing.T) {
		previousValue, hadPreviousValue := os.LookupEnv("ISDICT_HTTP_TIMEOUT_SECONDS")
		if err := os.Unsetenv("ISDICT_HTTP_TIMEOUT_SECONDS"); err != nil {
			t.Fatalf("os.Unsetenv() error = %v", err)
		}
		t.Cleanup(func() {
			var err error
			if hadPreviousValue {
				err = os.Setenv("ISDICT_HTTP_TIMEOUT_SECONDS", previousValue)
			} else {
				err = os.Unsetenv("ISDICT_HTTP_TIMEOUT_SECONDS")
			}
			if err != nil {
				t.Fatalf("restore ISDICT_HTTP_TIMEOUT_SECONDS error = %v", err)
			}
		})
		got := HTTPTimeoutFromEnv()
		if got != DefaultHTTPTimeout {
			t.Fatalf("HTTPTimeoutFromEnv() = %v, want %v", got, DefaultHTTPTimeout)
		}
	})

	t.Run("HTTPTimeoutFromEnv returns default for zero env value", func(t *testing.T) {
		t.Setenv("ISDICT_HTTP_TIMEOUT_SECONDS", "0")
		got := HTTPTimeoutFromEnv()
		if got != DefaultHTTPTimeout {
			t.Fatalf("HTTPTimeoutFromEnv() = %v, want %v", got, DefaultHTTPTimeout)
		}
	})

	t.Run("HTTPTimeoutFromEnv returns default for negative env value", func(t *testing.T) {
		t.Setenv("ISDICT_HTTP_TIMEOUT_SECONDS", "-5")
		got := HTTPTimeoutFromEnv()
		if got != DefaultHTTPTimeout {
			t.Fatalf("HTTPTimeoutFromEnv() = %v, want %v", got, DefaultHTTPTimeout)
		}
	})

	t.Run("HTTPTimeoutFromEnv uses valid positive env value", func(t *testing.T) {
		t.Setenv("ISDICT_HTTP_TIMEOUT_SECONDS", "15")
		got := HTTPTimeoutFromEnv()
		if got != 15*time.Second {
			t.Fatalf("HTTPTimeoutFromEnv() = %v, want 15s", got)
		}
	})

	t.Run("NewHTTPClient sets transport-level timeouts equal to client timeout", func(t *testing.T) {
		client := NewHTTPClient(10 * time.Second)
		transport, ok := client.Transport.(*http.Transport)
		if !ok {
			t.Fatal("NewHTTPClient().Transport is not *http.Transport")
		}
		if transport.ResponseHeaderTimeout != 10*time.Second {
			t.Fatalf("Transport.ResponseHeaderTimeout = %v, want 10s", transport.ResponseHeaderTimeout)
		}
	})
}

package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simp-lee/isdict-api/tests/middleware/internal/probe"
)

func TestMixedConcurrentFailsWhenPOSTTrafficAvoidsRateLimit(t *testing.T) {
	var getCount atomic.Int32
	var postCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			current := getCount.Add(1)
			if current == 1 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusTooManyRequests)
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read POST body: %v", err)
			}
			if got := strings.TrimSpace(string(body)); got != mixedPOSTRequestBody {
				t.Fatalf("POST body = %q, want %q", got, mixedPOSTRequestBody)
			}
			postCount.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	cfg := settings{
		client:           server.Client(),
		httpTimeout:      probe.DefaultHTTPTimeout,
		baseURL:          server.URL,
		rateLimitPath:    "/api/v1/search?q=test",
		postPath:         "/api/v1/words/batch",
		mixedGETTotal:    4,
		mixedPOSTTotal:   4,
		mixedConcurrency: 2,
		expectedMin429:   1,
	}

	err := testMixedConcurrent(cfg)
	if err != nil {
		t.Fatalf("testMixedConcurrent() error = %v, want nil", err)
	}
	if got := int(postCount.Load()); got <= cfg.mixedPOSTTotal {
		t.Fatalf("postCount = %d, want follow-up POST verification to run without inferring bypass", got)
	}
}

func TestMixedConcurrentAcceptsMixedBurstWithoutImmediatePOST429WhenFollowUpPOSTsAreLimited(t *testing.T) {
	var getCount atomic.Int32
	var postCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			current := getCount.Add(1)
			if current == 1 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusTooManyRequests)
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read POST body: %v", err)
			}
			if got := strings.TrimSpace(string(body)); got != mixedPOSTRequestBody {
				t.Fatalf("POST body = %q, want %q", got, mixedPOSTRequestBody)
			}
			current := postCount.Add(1)
			if current <= 4 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	cfg := settings{
		client:           server.Client(),
		httpTimeout:      probe.DefaultHTTPTimeout,
		baseURL:          server.URL,
		rateLimitPath:    "/api/v1/search?q=test",
		postPath:         "/api/v1/words/batch",
		mixedGETTotal:    4,
		mixedPOSTTotal:   4,
		mixedConcurrency: 2,
		expectedMin429:   1,
	}

	if err := testMixedConcurrent(cfg); err != nil {
		t.Fatalf("testMixedConcurrent() error = %v", err)
	}
	if got := postCount.Load(); got <= 4 {
		t.Fatalf("postCount = %d, want follow-up POST verification to run", got)
	}
}

func TestMixedConcurrentHonorsConcurrencyCapAndAcceptsPOSTRateLimit(t *testing.T) {
	var getCount atomic.Int32
	var postCount atomic.Int32
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentInFlight := inFlight.Add(1)
		for {
			observedMax := maxInFlight.Load()
			if currentInFlight <= observedMax {
				break
			}
			if maxInFlight.CompareAndSwap(observedMax, currentInFlight) {
				break
			}
		}
		defer inFlight.Add(-1)
		defer func() {
			if closeErr := r.Body.Close(); closeErr != nil {
				t.Errorf("response body close error = %v", closeErr)
			}
		}()

		time.Sleep(15 * time.Millisecond)

		switch r.Method {
		case http.MethodGet:
			current := getCount.Add(1)
			if current == 1 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusTooManyRequests)
		case http.MethodPost:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read POST body: %v", err)
			}
			if got := strings.TrimSpace(string(body)); got != mixedPOSTRequestBody {
				t.Fatalf("POST body = %q, want %q", got, mixedPOSTRequestBody)
			}
			current := postCount.Add(1)
			if current == 1 {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	const concurrencyCap = 3
	cfg := settings{
		client:           server.Client(),
		httpTimeout:      probe.DefaultHTTPTimeout,
		baseURL:          server.URL,
		rateLimitPath:    "/api/v1/search?q=test",
		postPath:         "/api/v1/words/batch",
		mixedGETTotal:    5,
		mixedPOSTTotal:   4,
		mixedConcurrency: concurrencyCap,
		expectedMin429:   2,
	}

	if err := testMixedConcurrent(cfg); err != nil {
		t.Fatalf("testMixedConcurrent() error = %v", err)
	}
	if got := maxInFlight.Load(); got > concurrencyCap {
		t.Fatalf("max in-flight = %d, want <= %d", got, concurrencyCap)
	}
}

func TestLoadSettingsDefaultsRateLimitPath(t *testing.T) {
	t.Setenv("ISDICT_RATE_LIMIT_PATH", "")

	cfg := loadSettings()
	if got, want := cfg.rateLimitPath, defaultNonExemptProbePath; got != want {
		t.Fatalf("loadSettings() rateLimitPath = %q, want %q", got, want)
	}
}

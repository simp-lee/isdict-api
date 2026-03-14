// Package middleware provides external stress checks focused on rate limiting and mixed-traffic handling.
// Run: go run ./tests/middleware/stress
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/simp-lee/isdict-api/tests/middleware/internal/probe"
)

type settings struct {
	client              *http.Client
	httpTimeout         time.Duration
	baseURL             string
	rateLimitPath       string
	postPath            string
	expectedRPSLimit    string
	expectedHourlyLimit string
	expectedDailyLimit  string
	extremeTotal        int
	extremeConcurrency  int
	sustainedWorkers    int
	sustainedSeconds    int
	mixedGETTotal       int
	mixedPOSTTotal      int
	mixedConcurrency    int
	expectedMin429      int
}

const mixedPOSTRequestBody = `{"words":["test"]}`
const mixedPOSTVerificationTotal = 4

type mixedRequest struct {
	method  string
	path    string
	headers map[string]string
	body    string
}

const defaultNonExemptProbePath = "/api/__middleware_probe__/rate-limit"

func main() {
	cfg := loadSettings()

	fmt.Println("\n========================================")
	fmt.Println("External Middleware Stress Checks")
	fmt.Println("========================================")
	fmt.Printf("Base URL: %s\n", cfg.baseURL)
	fmt.Printf("HTTP timeout: %s\n", cfg.httpTimeout)
	fmt.Printf("Rate-limit probe: %s\n", cfg.rateLimitPath)
	fmt.Println("Coverage note: these stress checks exercise rate limiting and mixed traffic only; they do not prove timeout middleware semantics.")
	fmt.Println()
	failures := runStressChecks(cfg)

	fmt.Println("========================================")
	if failures > 0 {
		fmt.Printf("%d stress scenarios failed\n", failures)
		fmt.Println("========================================")
		os.Exit(1)
	}
	fmt.Println("Stress scenarios passed")
	fmt.Println("========================================")
}

func runStressChecks(cfg settings) int {
	failures := 0
	scenarios := []struct {
		run       func(settings) error
		postDelay time.Duration
	}{
		{run: testRateLimitHeaders, postDelay: 1100 * time.Millisecond},
		{run: testExtremeBurst, postDelay: 1100 * time.Millisecond},
		{run: testSustainedLoad, postDelay: 1100 * time.Millisecond},
		{run: testMixedConcurrent},
	}
	for _, scenario := range scenarios {
		if err := scenario.run(cfg); err != nil {
			fmt.Printf("FAIL: %v\n\n", err)
			failures++
		}
		if scenario.postDelay > 0 {
			time.Sleep(scenario.postDelay)
		}
	}
	return failures
}

func testExtremeBurst(cfg settings) error {
	fmt.Printf("Test 1: Extreme Burst (%d requests)\n", cfg.extremeTotal)
	fmt.Println("-------------------------------------")

	var successCount, rateLimited, errors atomic.Int32
	start := time.Now()
	var wg sync.WaitGroup
	sem := make(chan struct{}, cfg.extremeConcurrency)

	for i := 0; i < cfg.extremeTotal; i++ {
		wg.Add(1)
		sem <- struct{}{}

		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }()

			resp, err := doRequest(http.MethodGet, cfg, cfg.rateLimitPath, nil, nil)
			if err != nil {
				errors.Add(1)
				return
			}
			status := resp.StatusCode
			drainAndClose(resp.Body)

			switch status {
			case http.StatusOK:
				successCount.Add(1)
			case http.StatusTooManyRequests:
				rateLimited.Add(1)
				if rateLimited.Load() == 1 {
					fmt.Printf("First 429 observed at request #%d\n", id+1)
				}
			default:
				errors.Add(1)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("Total requests: %d\n", cfg.extremeTotal)
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Successful: %d\n", successCount.Load())
	fmt.Printf("Rate Limited (429): %d\n", rateLimited.Load())
	fmt.Printf("Errors: %d\n", errors.Load())
	fmt.Printf("RPS: %.2f\n", float64(cfg.extremeTotal)/duration.Seconds())

	if errors.Load() > 0 {
		return fmt.Errorf("extreme burst produced %d unexpected errors", errors.Load())
	}
	if rateLimited.Load() < int32(cfg.expectedMin429) {
		return fmt.Errorf("extreme burst observed %d 429 responses, want at least %d", rateLimited.Load(), cfg.expectedMin429)
	}
	if successCount.Load() == 0 {
		return fmt.Errorf("extreme burst did not produce any successful responses")
	}

	fmt.Println("PASS")
	fmt.Println()
	return nil
}

func testRateLimitHeaders(cfg settings) error {
	fmt.Println("Test 0: Rate Limiting Headers")
	fmt.Println("-------------------------------------")

	resp, err := doRequest(http.MethodGet, cfg, cfg.rateLimitPath, nil, nil)
	if err != nil {
		return fmt.Errorf("rate-limit header probe failed: %s", probe.ExplainRequestError(err))
	}
	defer drainAndClose(resp.Body)

	ok, message := probe.ValidateRateLimitHeaders(resp, cfg.expectedRPSLimit, cfg.expectedHourlyLimit, cfg.expectedDailyLimit)
	if !ok {
		return fmt.Errorf("rate-limit header validation failed: %s", message)
	}

	fmt.Printf("PASS: %s\n", message)
	fmt.Println()
	return nil
}

func testSustainedLoad(cfg settings) error {
	fmt.Printf("Test 2: Sustained Load (%d seconds)\n", cfg.sustainedSeconds)
	fmt.Println("-------------------------------------")

	var requestCount, successCount, rateLimited, errors atomic.Int32
	duration := time.Duration(cfg.sustainedSeconds) * time.Second

	fmt.Printf("Running %d workers for %v...\n", cfg.sustainedWorkers, duration)

	var wg sync.WaitGroup
	stop := make(chan struct{})
	start := time.Now()

	for i := 0; i < cfg.sustainedWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-stop:
					return
				default:
					requestCount.Add(1)
					resp, err := doRequest(http.MethodGet, cfg, cfg.rateLimitPath, nil, nil)
					if err != nil {
						errors.Add(1)
						continue
					}
					status := resp.StatusCode
					drainAndClose(resp.Body)

					switch status {
					case http.StatusOK:
						successCount.Add(1)
					case http.StatusTooManyRequests:
						rateLimited.Add(1)
					default:
						errors.Add(1)
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()
	totalDuration := time.Since(start)

	fmt.Printf("Duration: %v\n", totalDuration)
	fmt.Printf("Total requests: %d\n", requestCount.Load())
	fmt.Printf("Successful: %d\n", successCount.Load())
	fmt.Printf("Rate Limited: %d\n", rateLimited.Load())
	fmt.Printf("Errors: %d\n", errors.Load())
	fmt.Printf("Average RPS: %.2f\n", float64(requestCount.Load())/totalDuration.Seconds())

	if requestCount.Load() == 0 || successCount.Load() == 0 {
		return fmt.Errorf("sustained load did not complete any successful requests")
	}
	if errors.Load() > 0 {
		return fmt.Errorf("sustained load produced %d unexpected errors", errors.Load())
	}
	if rateLimited.Load() < int32(cfg.expectedMin429) {
		return fmt.Errorf("sustained load observed %d 429 responses, want at least %d", rateLimited.Load(), cfg.expectedMin429)
	}

	fmt.Println("PASS")
	fmt.Println()
	return nil
}

func testMixedConcurrent(cfg settings) error {
	fmt.Printf("Test 3: Mixed Concurrent GET/POST Requests (%d GET + %d POST)\n", cfg.mixedGETTotal, cfg.mixedPOSTTotal)
	fmt.Println("-------------------------------------")
	fmt.Printf("Mixed concurrency cap: %d\n", cfg.mixedConcurrency)

	var getSuccess, postSuccess, getRateLimited, postRateLimited, errors atomic.Int32
	start := time.Now()
	requests := buildMixedRequests(cfg)
	totalRequests := len(requests)

	probe.RunBounded(len(requests), cfg.mixedConcurrency, func(index int) {
		request := requests[index]
		resp, err := doRequest(request.method, cfg, request.path, request.headers, mixedRequestReader(request))
		if err != nil {
			errors.Add(1)
			return
		}
		status := resp.StatusCode
		drainAndClose(resp.Body)
		recordMixedStatus(request.method, status, &getSuccess, &postSuccess, &getRateLimited, &postRateLimited, &errors)
	})
	duration := time.Since(start)
	totalRateLimited := getRateLimited.Load() + postRateLimited.Load()

	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("GET successful: %d/%d\n", getSuccess.Load(), cfg.mixedGETTotal)
	fmt.Printf("GET rate limited (429): %d/%d\n", getRateLimited.Load(), cfg.mixedGETTotal)
	fmt.Printf("POST successful: %d/%d\n", postSuccess.Load(), cfg.mixedPOSTTotal)
	fmt.Printf("POST rate limited (429): %d/%d\n", postRateLimited.Load(), cfg.mixedPOSTTotal)
	fmt.Printf("Total rate limited (429): %d\n", totalRateLimited)
	fmt.Printf("Errors: %d\n", errors.Load())
	fmt.Printf("Total RPS: %.2f\n", float64(totalRequests)/duration.Seconds())
	if err := validateMixedConcurrent(cfg, &getSuccess, &postSuccess, &getRateLimited, &postRateLimited, &errors, totalRateLimited); err != nil {
		return err
	}
	if cfg.mixedPOSTTotal > 0 && getRateLimited.Load() > 0 && postRateLimited.Load() == 0 {
		followUp429, err := verifyPOSTRateLimitAfterMixedBurst(cfg)
		if err != nil {
			return err
		}
		fmt.Printf("Follow-up POST verification 429s: %d/%d\n", followUp429, mixedPOSTVerificationTotal)
		if followUp429 == 0 {
			fmt.Println("Observation: follow-up POST verification saw 0 429s; with a shared bucket this is not evidence that POST bypasses rate limiting.")
		}
	}

	fmt.Println("PASS")
	fmt.Println()
	return nil
}

func buildMixedRequests(cfg settings) []mixedRequest {
	requests := make([]mixedRequest, 0, cfg.mixedGETTotal+cfg.mixedPOSTTotal)
	maxPairs := cfg.mixedGETTotal
	if cfg.mixedPOSTTotal > maxPairs {
		maxPairs = cfg.mixedPOSTTotal
	}
	for i := 0; i < maxPairs; i++ {
		if i < cfg.mixedGETTotal {
			requests = append(requests, mixedRequest{method: http.MethodGet, path: probe.AddQueryParam(cfg.rateLimitPath, "probe", fmt.Sprintf("%d", i))})
		}
		if i < cfg.mixedPOSTTotal {
			requests = append(requests, mixedRequest{method: http.MethodPost, path: cfg.postPath, headers: map[string]string{"Content-Type": "application/json"}, body: mixedPOSTRequestBody})
		}
	}
	return requests
}

func mixedRequestReader(request mixedRequest) io.Reader {
	if request.body == "" {
		return nil
	}
	return strings.NewReader(request.body)
}

func recordMixedStatus(method string, status int, getSuccess, postSuccess, getRateLimited, postRateLimited, errors *atomic.Int32) {
	switch method {
	case http.MethodGet:
		recordMixedResult(status, getSuccess, getRateLimited, errors)
	case http.MethodPost:
		recordMixedResult(status, postSuccess, postRateLimited, errors)
	default:
		errors.Add(1)
	}
}

func recordMixedResult(status int, success, rateLimited, errors *atomic.Int32) {
	switch status {
	case http.StatusOK:
		success.Add(1)
	case http.StatusTooManyRequests:
		rateLimited.Add(1)
	default:
		errors.Add(1)
	}
}

func validateMixedConcurrent(cfg settings, getSuccess, postSuccess, getRateLimited, postRateLimited, errors *atomic.Int32, totalRateLimited int32) error {
	if errors.Load() > 0 {
		return fmt.Errorf("mixed concurrent scenario produced %d unexpected errors", errors.Load())
	}
	if getSuccess.Load()+getRateLimited.Load() != int32(cfg.mixedGETTotal) {
		return fmt.Errorf("mixed concurrent scenario lost GET responses")
	}
	if postSuccess.Load()+postRateLimited.Load() != int32(cfg.mixedPOSTTotal) {
		return fmt.Errorf("mixed concurrent scenario lost POST responses")
	}
	if totalRateLimited < int32(cfg.expectedMin429) {
		return fmt.Errorf("mixed concurrent scenario observed %d total 429 responses, want at least %d", totalRateLimited, cfg.expectedMin429)
	}
	if cfg.mixedPOSTTotal > 0 && postSuccess.Load()+postRateLimited.Load() == 0 {
		return fmt.Errorf("mixed concurrent scenario did not exercise POST traffic")
	}
	return nil
}

func verifyPOSTRateLimitAfterMixedBurst(cfg settings) (int, error) {
	concurrency := cfg.mixedConcurrency
	if concurrency < 2 {
		concurrency = 2
	}
	if concurrency > mixedPOSTVerificationTotal {
		concurrency = mixedPOSTVerificationTotal
	}

	var postRateLimited, errors atomic.Int32
	probe.RunBounded(mixedPOSTVerificationTotal, concurrency, func(index int) {
		resp, err := doRequest(http.MethodPost, cfg, cfg.postPath, map[string]string{"Content-Type": "application/json"}, strings.NewReader(mixedPOSTRequestBody))
		if err != nil {
			errors.Add(1)
			return
		}
		defer drainAndClose(resp.Body)

		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			postRateLimited.Add(1)
		case http.StatusOK:
		default:
			errors.Add(1)
		}
	})

	if errors.Load() > 0 {
		return int(postRateLimited.Load()), fmt.Errorf("follow-up POST verification produced %d unexpected errors", errors.Load())
	}
	return int(postRateLimited.Load()), nil
}

func loadSettings() settings {
	httpTimeout := probe.HTTPTimeoutFromEnv()
	return settings{
		client:              probe.NewHTTPClient(httpTimeout),
		httpTimeout:         httpTimeout,
		baseURL:             probe.EnvString("ISDICT_API_BASE_URL", "http://localhost:8080"),
		rateLimitPath:       probe.EnvString("ISDICT_RATE_LIMIT_PATH", defaultNonExemptProbePath),
		postPath:            probe.EnvString("ISDICT_POST_PATH", "/api/v1/words/batch"),
		expectedRPSLimit:    strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_RPS")),
		expectedHourlyLimit: strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_PER_HOUR")),
		expectedDailyLimit:  strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_PER_DAY")),
		extremeTotal:        probe.EnvInt("ISDICT_STRESS_EXTREME_TOTAL", 1000),
		extremeConcurrency:  probe.EnvInt("ISDICT_STRESS_EXTREME_CONCURRENCY", 50),
		sustainedWorkers:    probe.EnvInt("ISDICT_STRESS_WORKERS", 20),
		sustainedSeconds:    probe.EnvInt("ISDICT_STRESS_DURATION_SECONDS", 10),
		mixedGETTotal:       probe.EnvInt("ISDICT_STRESS_MIXED_GET_TOTAL", 200),
		mixedPOSTTotal:      probe.EnvInt("ISDICT_STRESS_MIXED_POST_TOTAL", 100),
		mixedConcurrency:    probe.EnvInt("ISDICT_STRESS_MIXED_CONCURRENCY", 50),
		expectedMin429:      probe.EnvInt("ISDICT_EXPECT_MIN_429", 1),
	}
}

func doRequest(method string, cfg settings, path string, headers map[string]string, body io.Reader) (*http.Response, error) {
	return probe.DoRequest(cfg.client, method, cfg.baseURL, path, headers, body)
}

func drainAndClose(body io.ReadCloser) {
	probe.DrainAndClose(body)
}

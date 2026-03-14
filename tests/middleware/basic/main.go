// Package middleware provides basic external checks for middleware contracts visible via HTTP responses.
// Run: go run ./tests/middleware/basic
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

const defaultNonExemptProbePath = "/api/__middleware_probe__/rate-limit"

const corsPreflightProbePath = defaultNonExemptProbePath

type settings struct {
	client                     *http.Client
	httpTimeout                time.Duration
	baseURL                    string
	healthPath                 string
	readinessPath              string
	staticPath                 string
	rateLimitPath              string
	cachePath                  string
	cacheHitHeader             string
	cacheHitValue              string
	postPath                   string
	corsOrigin                 string
	enableReplayStabilityCheck bool
	expectRequestID            bool
	expectCORS                 bool
	expectedCORSOrigin         string
	expectedRPSLimit           string
	expectedHourlyLimit        string
	expectedDailyLimit         string
	expectedMin429             int
	burstTotal                 int
	burstConcurrency           int
	healthProbeTotal           int
	concurrentRequests         int
	staticProbeTotal           int
	staticProbeConcurrency     int
}

type burstResult struct {
	requestNumber int
	statusCode    int
	err           error
}

func main() {
	cfg := loadSettings()
	printBasicChecksHeader(cfg)
	failures := runBasicChecks(cfg)

	fmt.Println("\n========================================")
	if failures > 0 {
		fmt.Printf("%d basic middleware checks failed\n", failures)
		fmt.Println("========================================")
		os.Exit(1)
	}
	fmt.Println("All basic middleware checks passed")
	fmt.Println("========================================")
}

func printBasicChecksHeader(cfg settings) {
	fmt.Println("\n========================================")
	fmt.Println("isdict-API Middleware Basic External Checks")
	fmt.Println("========================================")
	fmt.Printf("Base URL: %s\n", cfg.baseURL)
	fmt.Printf("HTTP timeout: %s\n", cfg.httpTimeout)
	fmt.Printf("Health probe: %s\n", cfg.healthPath)
	printReadinessProbe(cfg)
	printCacheProbe(cfg)
	printStaticProbe(cfg)
	printReplayProbe(cfg)
	fmt.Printf("Expect Request ID: %t\n", cfg.expectRequestID)
	fmt.Printf("Expect CORS: %t\n", cfg.expectCORS)
	fmt.Printf("Expected minimum 429s: %d\n", cfg.expectedMin429)
	fmt.Printf("Rate-limit probe: %s\n\n", cfg.rateLimitPath)
}

func printReadinessProbe(cfg settings) {
	if cfg.readinessPath != "" {
		fmt.Printf("Readiness repeated-success / rate-limit-exemption probe: %s\n", cfg.readinessPath)
	}
}

func printCacheProbe(cfg settings) {
	if cfg.cachePath == "" {
		fmt.Println("Cache probe: disabled by default; set ISDICT_CACHE_PATH plus explicit cache-hit signal env vars to enable cache verification")
		return
	}
	if cfg.cacheHitHeader == "" || cfg.cacheHitValue == "" {
		fmt.Printf("Cache probe: %s (waiting for ISDICT_CACHE_HIT_HEADER and ISDICT_CACHE_HIT_VALUE)\n", cfg.cachePath)
		return
	}
	fmt.Printf("Cache probe: %s (%s -> %s)\n", cfg.cachePath, cfg.cacheHitHeader, cfg.cacheHitValue)
}

func printStaticProbe(cfg settings) {
	if cfg.staticPath == "" {
		fmt.Println("Static probe: disabled by default; set ISDICT_STATIC_PATH to a path served directly by the API process to enable it")
		return
	}
	fmt.Printf("Static probe: %s (%d requests, max %d in flight)\n", cfg.staticPath, cfg.staticProbeTotal, cfg.staticProbeConcurrency)
}

func printReplayProbe(cfg settings) {
	if cfg.enableReplayStabilityCheck {
		fmt.Println("Replay stability probe: enabled for deterministic data checks")
		return
	}
	fmt.Println("Replay stability probe: disabled by default; set ISDICT_ENABLE_REPLAY_STABILITY_CHECK=1 only for deterministic data")
}

func runBasicChecks(cfg settings) int {
	failures := 0
	checks := []struct {
		run       func(settings) bool
		postDelay time.Duration
	}{
		{run: test1RequestIDAndCORS},
		{run: test2ResponseCaching},
		{run: test3RateLimitHeaders},
		{run: test4BurstRateLimiting, postDelay: 1100 * time.Millisecond},
		{run: test5POSTNotCached},
		{run: test6RequestIDUniqueness},
		{run: test7StaticFilesSkipRateLimit},
		{run: test8RepeatedQueryReplayStability},
		{run: test9ConcurrentRequests},
	}
	for _, check := range checks {
		if !check.run(cfg) {
			failures++
		}
		if check.postDelay > 0 {
			time.Sleep(check.postDelay)
		}
	}
	return failures
}

func test1RequestIDAndCORS(cfg settings) bool {
	fmt.Println("Test 1: Request ID and CORS Headers")
	fmt.Println("-------------------------------------")

	resp, err := doRequest(http.MethodGet, cfg, cfg.healthPath, true, nil)
	if err != nil {
		fmt.Printf("FAIL: request failed: %v\n\n", err)
		return false
	}
	defer drainAndClose(resp.Body)

	requestID := resp.Header.Get("X-Request-Id")
	corsOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	corsMethods := resp.Header.Get("Access-Control-Allow-Methods")
	printRequestIDCORSStatus(resp.StatusCode, requestID, corsOrigin, corsMethods)
	if err := validateRequestIDCORS(cfg, resp.StatusCode, requestID, corsOrigin, corsMethods); err != nil {
		fmt.Printf("FAIL: %v\n\n", err)
		return false
	}

	if cfg.expectCORS {
		preflightResp, err := doPreflightRequest(cfg, corsPreflightProbePath, http.MethodGet)
		if err != nil {
			fmt.Printf("FAIL: preflight request failed: %s\n\n", probe.ExplainRequestError(err))
			return false
		}
		defer drainAndClose(preflightResp.Body)

		preflightOrigin := preflightResp.Header.Get("Access-Control-Allow-Origin")
		preflightMethods := preflightResp.Header.Get("Access-Control-Allow-Methods")
		fmt.Printf("Preflight status: %d\n", preflightResp.StatusCode)
		fmt.Printf("Preflight Allow Origin: %s\n", preflightOrigin)
		fmt.Printf("Preflight Allow Methods: %s\n", preflightMethods)
		if err := validateCORSPreflight(cfg, preflightResp.StatusCode, preflightOrigin, preflightMethods, http.MethodGet); err != nil {
			fmt.Printf("FAIL: %v\n\n", err)
			return false
		}
	}

	fmt.Println("PASS")
	fmt.Println()
	return true
}

func printRequestIDCORSStatus(status int, requestID, corsOrigin, corsMethods string) {
	fmt.Printf("Status: %d\n", status)
	fmt.Printf("Request ID: %s\n", requestID)
	fmt.Printf("CORS Allow Origin: %s\n", corsOrigin)
	fmt.Printf("CORS Allow Methods: %s\n", corsMethods)
}

func validateRequestIDCORS(cfg settings, status int, requestID, corsOrigin, corsMethods string) error {
	if status != http.StatusOK {
		return fmt.Errorf("health status = %d, want %d", status, http.StatusOK)
	}
	if cfg.expectRequestID && requestID == "" {
		return fmt.Errorf("missing X-Request-Id header")
	}
	if !cfg.expectRequestID && requestID != "" {
		return fmt.Errorf("unexpected X-Request-Id header = %q while ISDICT_EXPECT_REQUEST_ID is disabled", requestID)
	}
	if cfg.expectCORS && corsOrigin == "" {
		return fmt.Errorf("missing Access-Control-Allow-Origin on Origin-bearing GET response")
	}
	if !cfg.expectCORS && (corsOrigin != "" || corsMethods != "") {
		return fmt.Errorf("unexpected CORS headers while ISDICT_EXPECT_CORS is disabled (origin=%q methods=%q)", corsOrigin, corsMethods)
	}
	if cfg.expectCORS && cfg.expectedCORSOrigin != "" && corsOrigin != cfg.expectedCORSOrigin {
		return fmt.Errorf("Access-Control-Allow-Origin = %q, want %q", corsOrigin, cfg.expectedCORSOrigin)
	}
	return nil
}

func validateCORSPreflight(cfg settings, status int, allowOrigin, allowMethods, requestedMethod string) error {
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("preflight status = %d, want %d or %d", status, http.StatusNoContent, http.StatusOK)
	}
	if allowOrigin == "" {
		return fmt.Errorf("missing Access-Control-Allow-Origin on preflight response")
	}
	if allowMethods == "" {
		return fmt.Errorf("missing Access-Control-Allow-Methods on preflight response")
	}
	if cfg.expectedCORSOrigin != "" && allowOrigin != cfg.expectedCORSOrigin {
		return fmt.Errorf("Access-Control-Allow-Origin = %q, want %q", allowOrigin, cfg.expectedCORSOrigin)
	}
	if !headerContainsMethod(allowMethods, requestedMethod) {
		return fmt.Errorf("Access-Control-Allow-Methods = %q, want to include %q", allowMethods, requestedMethod)
	}
	return nil
}

func headerContainsMethod(headerValue, method string) bool {
	for _, candidate := range strings.Split(headerValue, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), method) {
			return true
		}
	}
	return false
}

func test2ResponseCaching(cfg settings) bool {
	fmt.Println("Test 2: Response Caching")
	fmt.Println("-------------------------------------")
	if cfg.cachePath == "" {
		fmt.Println("SKIP: cache verification requires an explicit ISDICT_CACHE_PATH plus a falsifiable external cache-hit signal")
		fmt.Println()
		return true
	}
	if cfg.cacheHitHeader == "" || cfg.cacheHitValue == "" {
		fmt.Println("SKIP: cache verification requires ISDICT_CACHE_HIT_HEADER and ISDICT_CACHE_HIT_VALUE; deterministic 200 responses alone are not proof of caching")
		fmt.Println()
		return true
	}

	endpoint := probe.EndpointURL(cfg.baseURL, cfg.cachePath)

	start1 := time.Now()
	resp1, err := doRequest(http.MethodGet, cfg, endpoint, false, nil)
	duration1 := time.Since(start1)
	if err != nil {
		fmt.Printf("FAIL: first request failed: %s\n\n", probe.ExplainRequestError(err))
		return false
	}
	body1, err := probe.ReadBodyAndClose(resp1.Body)
	if err != nil {
		fmt.Printf("FAIL: first response read failed: %v\n\n", err)
		return false
	}

	time.Sleep(100 * time.Millisecond)

	start2 := time.Now()
	resp2, err := doRequest(http.MethodGet, cfg, endpoint, false, nil)
	duration2 := time.Since(start2)
	if err != nil {
		fmt.Printf("FAIL: second request failed: %s\n\n", probe.ExplainRequestError(err))
		return false
	}
	body2, err := probe.ReadBodyAndClose(resp2.Body)
	if err != nil {
		fmt.Printf("FAIL: second response read failed: %v\n\n", err)
		return false
	}

	fmt.Printf("First request:  %v (status %d)\n", duration1, resp1.StatusCode)
	fmt.Printf("Second request: %v (status %d)\n", duration2, resp2.StatusCode)

	ok, message := probe.CacheReplayResult(resp1, body1, resp2, body2, cfg.cacheHitHeader, cfg.cacheHitValue)
	if !ok {
		fmt.Printf("FAIL: %s\n", message)
		fmt.Printf("Hint: point ISDICT_CACHE_PATH at a cacheable GET endpoint and configure %s to emit %q on the second response.\n", cfg.cacheHitHeader, cfg.cacheHitValue)
		fmt.Println()
		return false
	}

	fmt.Printf("PASS: %s\n\n", message)
	return true
}

func test3RateLimitHeaders(cfg settings) bool {
	fmt.Println("Test 3: Rate Limiting Headers")
	fmt.Println("-------------------------------------")

	resp, err := doRequest(http.MethodGet, cfg, cfg.rateLimitPath, false, nil)
	if err != nil {
		fmt.Printf("FAIL: request failed: %s\n\n", probe.ExplainRequestError(err))
		return false
	}
	defer drainAndClose(resp.Body)

	headers := probe.ReadRateLimitHeaders(resp)

	fmt.Printf("RPS Limit: %s\n", headers.RPSLimit)
	fmt.Printf("RPS Remaining: %s\n", headers.RPSRemaining)
	fmt.Printf("RPS Reset: %s\n", headers.RPSReset)
	fmt.Printf("Hourly Limit: %s\n", headers.HourlyLimit)
	fmt.Printf("Hourly Remaining: %s\n", headers.HourlyRemaining)
	fmt.Printf("Hourly Reset: %s\n", headers.HourlyReset)
	fmt.Printf("Daily Limit: %s\n", headers.DailyLimit)
	fmt.Printf("Daily Remaining: %s\n", headers.DailyRemaining)
	fmt.Printf("Daily Reset: %s\n", headers.DailyReset)

	ok, message := probe.ValidateRateLimitHeaders(resp, cfg.expectedRPSLimit, cfg.expectedHourlyLimit, cfg.expectedDailyLimit)
	if !ok {
		fmt.Printf("FAIL: %s\n\n", message)
		return false
	}

	fmt.Printf("PASS: %s\n", message)
	fmt.Println()
	return true
}

func test4BurstRateLimiting(cfg settings) bool {
	fmt.Printf("Test 4: Burst Rate Limiting (%d requests, max %d in flight)\n", cfg.burstTotal, cfg.burstConcurrency)
	fmt.Println("-------------------------------------")

	baselineStatuses, baselineStatus, err := probeBaselineStatuses(cfg)
	if err != nil {
		fmt.Printf("FAIL: baseline probe failed: %s\n\n", err)
		return false
	}

	results := make(chan burstResult, cfg.burstTotal)
	start := time.Now()

	fmt.Printf("Baseline statuses for %s: %s (preferred %d)\n", cfg.rateLimitPath, probe.FormatStatusDistribution(baselineStatuses), baselineStatus)
	fmt.Printf("Sending %d requests to %s with at most %d concurrent requests...\n", cfg.burstTotal, cfg.rateLimitPath, cfg.burstConcurrency)

	probe.RunBounded(cfg.burstTotal, cfg.burstConcurrency, func(index int) {
		resp, err := doRequest(http.MethodGet, cfg, probe.AddQueryParam(cfg.rateLimitPath, "probe", fmt.Sprintf("%d", index)), false, nil)
		if err != nil {
			results <- burstResult{requestNumber: index + 1, err: err}
			return
		}
		results <- burstResult{requestNumber: index + 1, statusCode: resp.StatusCode}
		drainAndClose(resp.Body)
	})
	close(results)

	duration := time.Since(start)
	summary := summarizeBurstResults(baselineStatuses, results)

	if first := summary.firstRateLimitedRequest; first > 0 {
		fmt.Printf("First 429 observed at request #%d\n", first)
	}
	fmt.Printf("Duration: %v\n", duration)
	fmt.Printf("Completion rate: %.2f requests/sec\n", float64(cfg.burstTotal)/duration.Seconds())
	fmt.Printf("Status distribution: %s\n", probe.FormatStatusDistribution(summary.statusCounts))
	fmt.Printf("Results: baseline %d, rate limited %d, transport errors %d, unexpected statuses %d\n", summary.baselineCount, summary.rateLimitedCount, summary.requestErrors, summary.unexpectedCount)
	if summary.requestErrors > 0 {
		fmt.Printf("FAIL: burst probe hit %d request transport error(s): %s\n\n", summary.requestErrors, probe.ExplainRequestError(summary.firstRequestErr))
		return false
	}
	if summary.unexpectedCount > 0 {
		fmt.Printf("FAIL: burst probe observed unexpected statuses outside baseline set %s and 429: %s\n\n", probe.FormatStatusDistribution(baselineStatuses), probe.FormatStatusDistribution(summary.unexpectedStatusCounts))
		return false
	}
	if summary.rateLimitedCount < cfg.expectedMin429 {
		fmt.Printf("FAIL: observed %d 429 responses, want at least %d; if this target is high-latency, raise ISDICT_BURST_CONCURRENCY above %d to keep the burst meaningful without removing the safety cap\n", summary.rateLimitedCount, cfg.expectedMin429, cfg.burstConcurrency)
		fmt.Println()
		return false
	}

	for _, path := range exemptProbePaths(cfg) {
		for i := 0; i < cfg.healthProbeTotal; i++ {
			resp, err := doRequest(http.MethodGet, cfg, path, false, nil)
			if err != nil {
				fmt.Printf("FAIL: exempt probe %s #%d failed: %s\n\n", path, i+1, probe.ExplainRequestError(err))
				return false
			}
			status := resp.StatusCode
			drainAndClose(resp.Body)
			if status != http.StatusOK {
				fmt.Printf("FAIL: exempt probe %s #%d status = %d, want %d\n\n", path, i+1, status, http.StatusOK)
				return false
			}
		}
	}

	fmt.Printf("PASS: baseline set %s stayed stable, rate limiting triggered on %s, and exempt probes stayed healthy: %s\n\n", probe.FormatStatusDistribution(baselineStatuses), cfg.rateLimitPath, strings.Join(exemptProbePaths(cfg), ", "))
	return true
}

type burstSummary struct {
	baselineCount           int
	rateLimitedCount        int
	requestErrors           int
	unexpectedCount         int
	firstRateLimitedRequest int
	firstRequestErr         error
	statusCounts            map[int]int
	unexpectedStatusCounts  map[int]int
}

func probeBaselineStatuses(cfg settings) (map[int]int, int, error) {
	const baselineSampleCount = 5

	statuses := make([]int, 0, baselineSampleCount)
	for sampleIndex := range baselineSampleCount {
		baselinePath := probe.AddQueryParam(cfg.rateLimitPath, "probe", fmt.Sprintf("baseline-%d", sampleIndex))
		resp, err := doRequest(http.MethodGet, cfg, baselinePath, false, nil)
		if err != nil {
			return nil, 0, err
		}
		statuses = append(statuses, resp.StatusCode)
		drainAndClose(resp.Body)
	}

	return selectBurstBaselineStatuses(statuses)
}

func selectBurstBaselineStatuses(statuses []int) (map[int]int, int, error) {
	if len(statuses) == 0 {
		return nil, 0, fmt.Errorf("baseline probe did not record any responses")
	}

	statusCounts := make(map[int]int, len(statuses))
	bestStatus := 0
	bestCount := 0

	for _, status := range statuses {
		if status == http.StatusTooManyRequests {
			continue
		}
		if err := validateBurstBaselineStatus(status); err != nil {
			return nil, 0, fmt.Errorf("baseline probe returned %d: %w", status, err)
		}
		statusCounts[status]++
		if statusCounts[status] > bestCount {
			bestStatus = status
			bestCount = statusCounts[status]
		}
	}

	if bestCount == 0 {
		return nil, 0, fmt.Errorf("baseline probes were all rate limited before the burst")
	}

	return statusCounts, bestStatus, nil
}

func validateBurstBaselineStatus(status int) error {
	if status == http.StatusTooManyRequests {
		return fmt.Errorf("baseline was already rate limited before the burst")
	}
	if status >= http.StatusInternalServerError {
		return fmt.Errorf("baseline is a server error and cannot be treated as normal middleware behavior")
	}
	if status < 200 {
		return fmt.Errorf("baseline is not a normal HTTP response status")
	}
	return nil
}

func summarizeBurstResults(baselineStatuses map[int]int, results <-chan burstResult) burstSummary {
	summary := burstSummary{
		statusCounts:           make(map[int]int),
		unexpectedStatusCounts: make(map[int]int),
	}

	for result := range results {
		if result.err != nil {
			summary.requestErrors++
			if summary.firstRequestErr == nil {
				summary.firstRequestErr = result.err
			}
			continue
		}

		summary.statusCounts[result.statusCode]++
		switch result.statusCode {
		case http.StatusTooManyRequests:
			summary.rateLimitedCount++
			if summary.firstRateLimitedRequest == 0 {
				summary.firstRateLimitedRequest = result.requestNumber
			}
		default:
			if _, ok := baselineStatuses[result.statusCode]; ok {
				summary.baselineCount++
				continue
			}
			if err := validateBurstBaselineStatus(result.statusCode); err == nil {
				summary.baselineCount++
				continue
			}
			summary.unexpectedCount++
			summary.unexpectedStatusCounts[result.statusCode]++
		}
	}

	return summary
}

func test5POSTNotCached(cfg settings) bool {
	fmt.Println("Test 5: POST Cache Bypass Contract")
	fmt.Println("-------------------------------------")
	fmt.Printf("SKIP: %s does not expose a contract-level signal that can prove POST cache bypass via an external probe\n", cfg.postPath)
	fmt.Println("Run the repository middleware tests for authoritative POST cache coverage.")
	fmt.Println()
	return true
}

func test6RequestIDUniqueness(cfg settings) bool {
	fmt.Println("Test 6: Request ID Uniqueness")
	fmt.Println("-------------------------------------")
	if !cfg.expectRequestID {
		fmt.Println("SKIP: request ID middleware is expected to be disabled")
		fmt.Println()
		return true
	}

	requestIDs := make(map[string]bool)
	count := 20

	for i := 0; i < count; i++ {
		resp, err := doRequest(http.MethodGet, cfg, cfg.healthPath, true, nil)
		if err != nil {
			fmt.Printf("FAIL: request %d failed: %v\n\n", i+1, err)
			return false
		}
		requestID := resp.Header.Get("X-Request-Id")
		drainAndClose(resp.Body)
		if requestID == "" {
			fmt.Printf("FAIL: request %d missing X-Request-Id\n\n", i+1)
			return false
		}
		requestIDs[requestID] = true
	}

	fmt.Printf("Generated IDs: %d\n", count)
	fmt.Printf("Unique IDs: %d\n", len(requestIDs))

	if len(requestIDs) != count {
		fmt.Println("FAIL: duplicate request IDs detected")
		fmt.Println()
		return false
	}

	fmt.Println("PASS")
	fmt.Println()
	return true
}

func test7StaticFilesSkipRateLimit(cfg settings) bool {
	fmt.Println("Test 7: Static Files Skip Rate Limiting")
	fmt.Println("-------------------------------------")
	if cfg.staticPath == "" {
		fmt.Println("SKIP: static probe disabled; set ISDICT_STATIC_PATH to a path served directly by the API process to enable it")
		fmt.Println()
		return true
	}

	var successCount, rateLimited, errors atomic.Int32
	total := cfg.staticProbeTotal

	fmt.Printf("Sending %d requests to %s with at most %d concurrent requests...\n", total, cfg.staticPath, cfg.staticProbeConcurrency)

	probe.RunBounded(total, cfg.staticProbeConcurrency, func(index int) {
		resp, err := doRequest(http.MethodGet, cfg, cfg.staticPath, false, nil)
		if err != nil {
			errors.Add(1)
			return
		}

		switch resp.StatusCode {
		case http.StatusTooManyRequests:
			rateLimited.Add(1)
		case http.StatusOK:
			successCount.Add(1)
		default:
			errors.Add(1)
		}

		drainAndClose(resp.Body)
	})

	fmt.Printf("Successful: %d, Rate Limited: %d, Errors: %d\n", successCount.Load(), rateLimited.Load(), errors.Load())

	if rateLimited.Load() > 0 {
		fmt.Println("FAIL: static file path was rate limited")
		fmt.Println()
		return false
	}
	if errors.Load() > 0 || successCount.Load() == 0 {
		fmt.Println("FAIL: static file probe did not complete cleanly")
		fmt.Println()
		return false
	}

	fmt.Println("PASS")
	fmt.Println()
	return true
}

func test8RepeatedQueryReplayStability(cfg settings) bool {
	fmt.Println("Test 8: Repeated Query Requests Stay Stable")
	fmt.Println("-------------------------------------")
	if !cfg.enableReplayStabilityCheck {
		fmt.Println("SKIP: replay stability is business-data-sensitive; set ISDICT_ENABLE_REPLAY_STABILITY_CHECK=1 only for deterministic datasets")
		fmt.Println()
		return true
	}

	queries := []string{
		"q=test&limit=5",
		"q=test&limit=10",
		"q=hello&limit=5",
	}

	for _, query := range queries {
		endpoint := "/api/v1/search?" + query

		start1 := time.Now()
		resp1, err := doRequest(http.MethodGet, cfg, endpoint, false, nil)
		duration1 := time.Since(start1)
		if err != nil {
			fmt.Printf("FAIL: first request for %q failed: %s\n\n", query, probe.ExplainRequestError(err))
			return false
		}
		body1, err := probe.ReadBodyAndClose(resp1.Body)
		if err != nil {
			fmt.Printf("FAIL: first response read for %q failed: %v\n\n", query, err)
			return false
		}

		time.Sleep(50 * time.Millisecond)

		start2 := time.Now()
		resp2, err := doRequest(http.MethodGet, cfg, endpoint, false, nil)
		duration2 := time.Since(start2)
		if err != nil {
			fmt.Printf("FAIL: second request for %q failed: %s\n\n", query, probe.ExplainRequestError(err))
			return false
		}
		body2, err := probe.ReadBodyAndClose(resp2.Body)
		if err != nil {
			fmt.Printf("FAIL: second response read for %q failed: %v\n\n", query, err)
			return false
		}

		ok, message := probe.StableReplayResult(resp1, body1, resp2, body2)
		if !ok {
			fmt.Printf("FAIL: %q -> %s\n\n", query, message)
			return false
		}

		fmt.Printf("Query: %s\n", query)
		fmt.Printf("  First: %v (status %d)\n", duration1, resp1.StatusCode)
		fmt.Printf("  Second: %v (status %d)\n", duration2, resp2.StatusCode)
		fmt.Printf("  Replay: %s\n", message)
	}

	fmt.Println("PASS: repeated identical query requests returned stable bodies; this probe does not assert cross-query cache-key isolation")
	fmt.Println()
	return true
}

func test9ConcurrentRequests(cfg settings) bool {
	fmt.Printf("Test 9: Concurrent Requests (%d goroutines)\n", cfg.concurrentRequests)
	fmt.Println("-------------------------------------")

	var wg sync.WaitGroup
	var successCount, errorCount atomic.Int32

	start := time.Now()

	for i := 0; i < cfg.concurrentRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			resp, err := doRequest(http.MethodGet, cfg, cfg.healthPath, false, nil)
			if err != nil {
				errorCount.Add(1)
				return
			}
			status := resp.StatusCode
			drainAndClose(resp.Body)

			if status == http.StatusOK {
				successCount.Add(1)
				return
			}
			errorCount.Add(1)
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("Concurrent requests: %d\n", cfg.concurrentRequests)
	fmt.Printf("Successful: %d\n", successCount.Load())
	fmt.Printf("Errors: %d\n", errorCount.Load())
	fmt.Printf("Total time: %v\n", duration)

	if errorCount.Load() > 0 || successCount.Load() != int32(cfg.concurrentRequests) {
		fmt.Println("FAIL: health endpoint did not handle all concurrent requests cleanly")
		fmt.Println()
		return false
	}

	fmt.Println("PASS")
	fmt.Println()
	return true
}

func loadSettings() settings {
	httpTimeout := probe.HTTPTimeoutFromEnv()
	staticPath := strings.TrimSpace(os.Getenv("ISDICT_STATIC_PATH"))
	if probe.EnvBool("ISDICT_SKIP_STATIC_PROBE", false) {
		staticPath = ""
	}
	return settings{
		client:                     probe.NewHTTPClient(httpTimeout),
		httpTimeout:                httpTimeout,
		baseURL:                    probe.EnvString("ISDICT_API_BASE_URL", "http://localhost:8080"),
		healthPath:                 probe.EnvString("ISDICT_HEALTH_PATH", "/health"),
		readinessPath:              probe.EnvString("ISDICT_READINESS_PATH", "/api/v1/health"),
		staticPath:                 staticPath,
		rateLimitPath:              probe.EnvString("ISDICT_RATE_LIMIT_PATH", defaultNonExemptProbePath),
		cachePath:                  strings.TrimSpace(os.Getenv("ISDICT_CACHE_PATH")),
		cacheHitHeader:             strings.TrimSpace(os.Getenv("ISDICT_CACHE_HIT_HEADER")),
		cacheHitValue:              strings.TrimSpace(os.Getenv("ISDICT_CACHE_HIT_VALUE")),
		postPath:                   probe.EnvString("ISDICT_POST_PATH", "/api/v1/words/batch"),
		corsOrigin:                 probe.EnvString("ISDICT_CORS_ORIGIN", "https://web.isdict.test"),
		enableReplayStabilityCheck: probe.EnvBool("ISDICT_ENABLE_REPLAY_STABILITY_CHECK", false),
		expectRequestID:            probe.EnvBool("ISDICT_EXPECT_REQUEST_ID", true),
		expectCORS:                 probe.EnvBool("ISDICT_EXPECT_CORS", true),
		expectedCORSOrigin:         strings.TrimSpace(os.Getenv("ISDICT_EXPECT_CORS_ORIGIN")),
		expectedRPSLimit:           strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_RPS")),
		expectedHourlyLimit:        strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_PER_HOUR")),
		expectedDailyLimit:         strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_PER_DAY")),
		expectedMin429:             probe.EnvInt("ISDICT_EXPECT_MIN_429", 1),
		burstTotal:                 probe.EnvInt("ISDICT_BURST_TOTAL", 250),
		burstConcurrency:           probe.EnvInt("ISDICT_BURST_CONCURRENCY", 50),
		healthProbeTotal:           probe.EnvInt("ISDICT_HEALTH_PROBE_TOTAL", 20),
		concurrentRequests:         probe.EnvInt("ISDICT_CONCURRENT_REQUESTS", 100),
		staticProbeTotal:           probe.EnvInt("ISDICT_STATIC_PROBE_TOTAL", staticProbeTotal(strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_RPS")))),
		staticProbeConcurrency:     probe.EnvInt("ISDICT_STATIC_PROBE_CONCURRENCY", staticProbeConcurrency(strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_RPS")))),
	}
}

func doRequest(method string, cfg settings, path string, withOrigin bool, body io.Reader) (*http.Response, error) {
	headers := map[string]string{}
	if withOrigin && cfg.corsOrigin != "" {
		headers["Origin"] = cfg.corsOrigin
	}
	if method == http.MethodPost {
		headers["Content-Type"] = "application/json"
	}
	return probe.DoRequest(cfg.client, method, cfg.baseURL, path, headers, body)
}

func doPreflightRequest(cfg settings, path string, requestedMethod string) (*http.Response, error) {
	headers := map[string]string{}
	if cfg.corsOrigin != "" {
		headers["Origin"] = cfg.corsOrigin
	}
	headers["Access-Control-Request-Method"] = requestedMethod
	return probe.DoRequest(cfg.client, http.MethodOptions, cfg.baseURL, path, headers, nil)
}

func drainAndClose(body io.ReadCloser) {
	probe.DrainAndClose(body)
}

func exemptProbePaths(cfg settings) []string {
	paths := []string{cfg.healthPath}
	if cfg.readinessPath != "" && cfg.readinessPath != cfg.healthPath {
		paths = append(paths, cfg.readinessPath)
	}
	return paths
}

func staticProbeTotal(expectedRPS string) int {
	total, _ := probe.StaticProbeLoad(expectedRPS, 100)
	return total
}

func staticProbeConcurrency(expectedRPS string) int {
	_, concurrency := probe.StaticProbeLoad(expectedRPS, 100)
	return concurrency
}

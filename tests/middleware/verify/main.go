// Package middleware provides quick verification tests for middleware contracts that external HTTP probes can prove.
// Run: go run ./tests/middleware/verify
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/simp-lee/isdict-api/tests/middleware/internal/probe"
)

type settings struct {
	client                 *http.Client
	httpTimeout            time.Duration
	baseURL                string
	healthPath             string
	readinessPath          string
	staticPath             string
	rateLimitPath          string
	cachePath              string
	cacheHitHeader         string
	cacheHitValue          string
	postPath               string
	corsOrigin             string
	expectRequestID        bool
	expectCORS             bool
	expectedCORSOrigin     string
	expectedRPSLimit       string
	expectedHourlyLimit    string
	expectedDailyLimit     string
	batchTotal             int
	batchConcurrency       int
	expectedMin429         int
	concurrentTotal        int
	staticProbeTotal       int
	staticProbeConcurrency int
	healthProbeTotal       int
}

type result struct {
	status  string
	message string
}

const defaultNonExemptProbePath = "/api/__middleware_probe__/rate-limit"

type burstResult struct {
	requestNumber int
	statusCode    int
	err           error
}

func main() {
	cfg := loadSettings()
	failures := 0
	warnings := 0

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("isdict-API External Middleware Verification")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Base URL: %s\n", cfg.baseURL)
	fmt.Printf("HTTP timeout: %s\n", cfg.httpTimeout)
	fmt.Printf("Health probe: %s\n", cfg.healthPath)
	fmt.Printf("Readiness probe: %s (default repeated success / rate-limit exemption path)\n", cfg.readinessPath)
	if cfg.cachePath == "" {
		fmt.Println("Cache probe: disabled by default; set ISDICT_CACHE_PATH plus explicit cache-hit signal env vars to enable cache verification")
	} else if cfg.cacheHitHeader == "" || cfg.cacheHitValue == "" {
		fmt.Printf("Cache probe: %s (waiting for ISDICT_CACHE_HIT_HEADER and ISDICT_CACHE_HIT_VALUE)\n", cfg.cachePath)
	} else {
		fmt.Printf("Cache probe: %s (%s -> %s)\n", cfg.cachePath, cfg.cacheHitHeader, cfg.cacheHitValue)
	}
	if cfg.staticPath == "" {
		fmt.Println("Static probe: disabled by default; set ISDICT_STATIC_PATH to a path served directly by the API process to enable it")
	} else {
		fmt.Printf("Static probe: %s (%d requests, max %d in flight)\n", cfg.staticPath, cfg.staticProbeTotal, cfg.staticProbeConcurrency)
	}
	fmt.Printf("Expect Request ID: %t\n", cfg.expectRequestID)
	fmt.Printf("Expect CORS: %t\n", cfg.expectCORS)
	fmt.Printf("Rate-limit probe: %s\n\n", cfg.rateLimitPath)

	for _, feature := range []struct {
		name string
		fn   func(settings) result
	}{
		{"1. Request ID Generation", testRequestID},
		{"2. CORS Headers", testCORS},
		{"3. Response Caching (GET)", testCache},
		{"4. Rate Limiting Headers", testRateLimitHeaders},
		{"5. Rate Limiting Protection", testRateLimitProtection},
		{"6. POST Cache Bypass Contract", testPOSTNotCached},
		{"7. Static Files Skip Rate Limit", testStaticSkipRateLimit},
		{"8. Concurrent Request Handling", testConcurrent},
		{"9. OPTIONS Preflight (CORS)", testOptions},
		{"10. Readiness Repeated Success / Rate-Limit Exemption", testReadiness},
	} {
		outcome := checkFeature(feature.name, feature.fn(cfg))
		switch outcome.status {
		case "FAIL":
			failures++
		case "WARN":
			warnings++
		}
	}

	fmt.Println("\n" + strings.Repeat("=", 60))
	if failures > 0 {
		fmt.Printf("Verification completed with %d failure(s) and %d warning(s)\n", failures, warnings)
		fmt.Println(strings.Repeat("=", 60))
		os.Exit(1)
	}
	fmt.Printf("All verification checks passed with %d warning(s)\n", warnings)
	fmt.Println(strings.Repeat("=", 60))
}

func checkFeature(name string, outcome result) result {
	fmt.Printf("%-40s ... %s\n", name, outcome.status)
	if outcome.message != "" {
		fmt.Printf("   %s\n", outcome.message)
	}
	return outcome
}

func testRequestID(cfg settings) result {
	resp, err := doRequest(http.MethodGet, cfg, cfg.healthPath, true, nil, nil)
	if err != nil {
		return result{status: "FAIL", message: err.Error()}
	}
	defer drainAndClose(resp.Body)

	requestID := resp.Header.Get("X-Request-Id")
	if resp.StatusCode != http.StatusOK {
		return result{status: "FAIL", message: fmt.Sprintf("health status = %d", resp.StatusCode)}
	}
	if cfg.expectRequestID && requestID == "" {
		return result{status: "FAIL", message: "missing X-Request-Id header"}
	}
	if !cfg.expectRequestID && requestID != "" {
		return result{status: "FAIL", message: fmt.Sprintf("unexpected X-Request-Id header = %q while ISDICT_EXPECT_REQUEST_ID is disabled", requestID)}
	}
	if !cfg.expectRequestID {
		return result{status: "PASS", message: "request ID middleware disabled as expected"}
	}
	return result{status: "PASS", message: fmt.Sprintf("ID: %s", shorten(requestID, 16))}
}

func testCORS(cfg settings) result {
	resp, err := doRequest(http.MethodGet, cfg, cfg.healthPath, true, nil, nil)
	if err != nil {
		return result{status: "FAIL", message: err.Error()}
	}
	defer drainAndClose(resp.Body)

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	methods := resp.Header.Get("Access-Control-Allow-Methods")

	if err := validateSimpleCORS(cfg, origin, methods); err != nil {
		return result{status: "FAIL", message: err.Error()}
	}
	if !cfg.expectCORS {
		return result{status: "PASS", message: "CORS middleware disabled as expected"}
	}
	message := fmt.Sprintf("Origin: %s", origin)
	if methods != "" {
		message = fmt.Sprintf("%s, Methods: %s", message, methods)
	}
	return result{status: "PASS", message: message}
}

func testCache(cfg settings) result {
	if cfg.cachePath == "" {
		return result{
			status:  "SKIP",
			message: "cache verification requires an explicit ISDICT_CACHE_PATH plus a falsifiable external cache-hit signal",
		}
	}
	if cfg.cacheHitHeader == "" || cfg.cacheHitValue == "" {
		return result{
			status:  "SKIP",
			message: "cache verification requires ISDICT_CACHE_HIT_HEADER and ISDICT_CACHE_HIT_VALUE; deterministic 200 responses alone are not proof of caching",
		}
	}

	endpoint := probe.EndpointURL(cfg.baseURL, cfg.cachePath)

	start1 := time.Now()
	resp1, err := doRequest(http.MethodGet, cfg, endpoint, false, nil, nil)
	dur1 := time.Since(start1)
	if err != nil {
		return result{status: "FAIL", message: probe.ExplainRequestError(err)}
	}
	body1, err := probe.ReadBodyAndClose(resp1.Body)
	if err != nil {
		return result{status: "FAIL", message: fmt.Sprintf("first response read failed: %v", err)}
	}

	time.Sleep(50 * time.Millisecond)

	start2 := time.Now()
	resp2, err := doRequest(http.MethodGet, cfg, endpoint, false, nil, nil)
	dur2 := time.Since(start2)
	if err != nil {
		return result{status: "FAIL", message: probe.ExplainRequestError(err)}
	}
	body2, err := probe.ReadBodyAndClose(resp2.Body)
	if err != nil {
		return result{status: "FAIL", message: fmt.Sprintf("second response read failed: %v", err)}
	}

	ok, message := probe.CacheReplayResult(resp1, body1, resp2, body2, cfg.cacheHitHeader, cfg.cacheHitValue)
	if !ok {
		return result{status: "FAIL", message: message + fmt.Sprintf("; point ISDICT_CACHE_PATH at a cacheable GET endpoint and configure %s to emit %q on the second response", cfg.cacheHitHeader, cfg.cacheHitValue)}
	}
	return result{status: "PASS", message: fmt.Sprintf("1st: %v, 2nd: %v; %s", dur1, dur2, message)}
}

func testRateLimitHeaders(cfg settings) result {
	resp, err := doRequest(http.MethodGet, cfg, cfg.rateLimitPath, false, nil, nil)
	if err != nil {
		return result{status: "FAIL", message: probe.ExplainRequestError(err)}
	}
	defer drainAndClose(resp.Body)

	ok, message := probe.ValidateRateLimitHeaders(resp, cfg.expectedRPSLimit, cfg.expectedHourlyLimit, cfg.expectedDailyLimit)
	if !ok {
		return result{status: "FAIL", message: message}
	}
	return result{status: "PASS", message: message}
}

func testRateLimitProtection(cfg settings) result {
	baselineStatuses, _, err := probeBaselineStatuses(cfg)
	if err != nil {
		return result{status: "FAIL", message: fmt.Sprintf("baseline probe failed: %s", probe.ExplainRequestError(err))}
	}

	total := cfg.batchTotal
	concurrency := cfg.batchConcurrency
	results := make(chan burstResult, total)

	probe.RunBounded(total, concurrency, func(index int) {
		resp, err := doRequest(http.MethodGet, cfg, probe.AddQueryParam(cfg.rateLimitPath, "probe", fmt.Sprintf("%d", index)), false, nil, nil)
		if err != nil {
			results <- burstResult{requestNumber: index + 1, err: err}
			return
		}
		statusCode := resp.StatusCode
		drainAndClose(resp.Body)
		results <- burstResult{requestNumber: index + 1, statusCode: statusCode}
	})
	close(results)

	summary := summarizeBurstResults(baselineStatuses, results)

	if summary.requestErrors > 0 {
		return result{status: "FAIL", message: fmt.Sprintf("%d/%d burst requests failed: %s", summary.requestErrors, total, probe.ExplainRequestError(summary.firstRequestErr))}
	}
	if summary.unexpectedCount > 0 {
		return result{status: "FAIL", message: fmt.Sprintf("burst observed unexpected statuses outside baseline set %s and 429: %s", probe.FormatStatusDistribution(baselineStatuses), probe.FormatStatusDistribution(summary.unexpectedStatusCounts))}
	}
	if summary.rateLimitedCount < cfg.expectedMin429 {
		return result{status: "FAIL", message: fmt.Sprintf("observed %d/%d rate-limited responses with baseline %s, want at least %d", summary.rateLimitedCount, total, probe.FormatStatusDistribution(summary.statusCounts), cfg.expectedMin429)}
	}
	for _, path := range exemptProbePaths(cfg) {
		for i := 0; i < cfg.healthProbeTotal; i++ {
			resp, err := doRequest(http.MethodGet, cfg, path, false, nil, nil)
			if err != nil {
				return result{status: "FAIL", message: fmt.Sprintf("exempt probe %s after burst failed: %s", path, probe.ExplainRequestError(err))}
			}
			status := resp.StatusCode
			drainAndClose(resp.Body)
			if status != http.StatusOK {
				return result{status: "FAIL", message: fmt.Sprintf("exempt probe %s after burst returned %d", path, status)}
			}
		}
	}
	time.Sleep(1100 * time.Millisecond)
	return result{status: "PASS", message: fmt.Sprintf("baseline=%d limited=%d/%d on %s with status distribution %s while exempt probes stayed healthy: %s", summary.baselineCount, summary.rateLimitedCount, total, cfg.rateLimitPath, probe.FormatStatusDistribution(summary.statusCounts), strings.Join(exemptProbePaths(cfg), ", "))}
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
		resp, err := doRequest(http.MethodGet, cfg, baselinePath, false, nil, nil)
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

	for outcome := range results {
		if outcome.err != nil {
			summary.requestErrors++
			if summary.firstRequestErr == nil {
				summary.firstRequestErr = outcome.err
			}
			continue
		}

		summary.statusCounts[outcome.statusCode]++
		switch outcome.statusCode {
		case http.StatusTooManyRequests:
			summary.rateLimitedCount++
			if summary.firstRateLimitedRequest == 0 {
				summary.firstRateLimitedRequest = outcome.requestNumber
			}
		default:
			if _, ok := baselineStatuses[outcome.statusCode]; ok {
				summary.baselineCount++
				continue
			}
			if err := validateBurstBaselineStatus(outcome.statusCode); err == nil {
				summary.baselineCount++
				continue
			}
			summary.unexpectedCount++
			summary.unexpectedStatusCounts[outcome.statusCode]++
		}
	}

	return summary
}
func testPOSTNotCached(cfg settings) result {
	return result{
		status:  "SKIP",
		message: fmt.Sprintf("%s does not expose a falsifiable external signal for POST cache bypass; use repository middleware tests for that contract", cfg.postPath),
	}
}

func testStaticSkipRateLimit(cfg settings) result {
	if cfg.staticPath == "" {
		return result{
			status:  "SKIP",
			message: "static probe disabled; set ISDICT_STATIC_PATH to a path served directly by the API process to enable it",
		}
	}

	successCount := 0
	rateLimited := 0
	errors := 0
	unexpectedStatuses := 0
	firstUnexpectedStatus := 0
	results := make(chan int, cfg.staticProbeTotal)
	probe.RunBounded(cfg.staticProbeTotal, cfg.staticProbeConcurrency, func(index int) {
		resp, err := doRequest(http.MethodGet, cfg, cfg.staticPath, false, nil, nil)
		if err != nil {
			results <- -1
			return
		}
		defer drainAndClose(resp.Body)
		results <- resp.StatusCode
	})
	close(results)

	for status := range results {
		switch status {
		case -1:
			errors++
		case http.StatusOK:
			successCount++
		case http.StatusTooManyRequests:
			rateLimited++
		default:
			unexpectedStatuses++
			if firstUnexpectedStatus == 0 {
				firstUnexpectedStatus = status
			}
		}
	}

	if errors > 0 {
		return result{status: "FAIL", message: fmt.Sprintf("%d static file requests failed", errors)}
	}
	if rateLimited != 0 {
		return result{status: "FAIL", message: fmt.Sprintf("%d/%d static file requests were rate limited", rateLimited, cfg.staticProbeTotal)}
	}
	if unexpectedStatuses > 0 {
		return result{status: "FAIL", message: fmt.Sprintf("static probe observed %d unexpected non-200 responses (first status %d)", unexpectedStatuses, firstUnexpectedStatus)}
	}
	if successCount == 0 {
		return result{status: "FAIL", message: "static probe did not receive any 200 OK responses"}
	}
	return result{status: "PASS", message: fmt.Sprintf("%d/%d static requests returned 200 with no rate limiting", successCount, cfg.staticProbeTotal)}
}

func testConcurrent(cfg settings) result {
	done := make(chan bool, cfg.concurrentTotal)
	for i := 0; i < cfg.concurrentTotal; i++ {
		go func() {
			resp, err := doRequest(http.MethodGet, cfg, cfg.healthPath, false, nil, nil)
			if err != nil {
				done <- false
				return
			}
			status := resp.StatusCode
			drainAndClose(resp.Body)
			done <- status == http.StatusOK
		}()
	}

	success := 0
	for i := 0; i < cfg.concurrentTotal; i++ {
		if <-done {
			success++
		}
	}

	if success != cfg.concurrentTotal {
		return result{status: "FAIL", message: fmt.Sprintf("only %d/%d concurrent health requests succeeded", success, cfg.concurrentTotal)}
	}
	return result{status: "PASS", message: fmt.Sprintf("%d/%d concurrent requests successful", success, cfg.concurrentTotal)}
}

func testOptions(cfg settings) result {
	if !cfg.expectCORS {
		return result{status: "SKIP", message: "CORS middleware disabled; preflight behavior is not asserted"}
	}

	resp, err := doRequest(http.MethodOptions, cfg, "/api/v1/words/test", false, nil, map[string]string{
		"Origin":                        cfg.corsOrigin,
		"Access-Control-Request-Method": http.MethodGet,
	})
	if err != nil {
		return result{status: "FAIL", message: probe.ExplainRequestError(err)}
	}
	defer drainAndClose(resp.Body)

	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	allowMethods := resp.Header.Get("Access-Control-Allow-Methods")
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return result{status: "FAIL", message: fmt.Sprintf("unexpected status: %d", resp.StatusCode)}
	}
	if err := validateCORSPreflight(cfg, allowOrigin, allowMethods, http.MethodGet); err != nil {
		return result{status: "FAIL", message: err.Error()}
	}
	return result{status: "PASS", message: fmt.Sprintf("Status: %d, Allow-Origin: %s, Allow-Methods: %s", resp.StatusCode, allowOrigin, allowMethods)}
}

func testReadiness(cfg settings) result {
	if cfg.readinessPath == "" {
		return result{
			status:  "SKIP",
			message: "set ISDICT_READINESS_PATH to add repeated-success and post-burst rate-limit-exemption checks",
		}
	}

	for i := 0; i < cfg.healthProbeTotal; i++ {
		resp, err := doRequest(http.MethodGet, cfg, cfg.readinessPath, false, nil, nil)
		if err != nil {
			return result{status: "FAIL", message: fmt.Sprintf("readiness probe #%d failed: %s", i+1, probe.ExplainRequestError(err))}
		}
		status := resp.StatusCode
		drainAndClose(resp.Body)
		if status != http.StatusOK {
			return result{status: "FAIL", message: fmt.Sprintf("readiness probe #%d returned %d", i+1, status)}
		}
	}

	return result{status: "PASS", message: fmt.Sprintf("%s returned %d for %d/%d repeated probes; timeout/cache exemption is not asserted here", cfg.readinessPath, http.StatusOK, cfg.healthProbeTotal, cfg.healthProbeTotal)}
}

func loadSettings() settings {
	httpTimeout := probe.HTTPTimeoutFromEnv()
	staticPath := strings.TrimSpace(os.Getenv("ISDICT_STATIC_PATH"))
	if probe.EnvBool("ISDICT_SKIP_STATIC_PROBE", false) {
		staticPath = ""
	}
	return settings{
		client:                 probe.NewHTTPClient(httpTimeout),
		httpTimeout:            httpTimeout,
		baseURL:                probe.EnvString("ISDICT_API_BASE_URL", "http://localhost:8080"),
		healthPath:             probe.EnvString("ISDICT_HEALTH_PATH", "/health"),
		readinessPath:          probe.EnvString("ISDICT_READINESS_PATH", "/api/v1/health"),
		staticPath:             staticPath,
		rateLimitPath:          probe.EnvString("ISDICT_RATE_LIMIT_PATH", defaultNonExemptProbePath),
		cachePath:              strings.TrimSpace(os.Getenv("ISDICT_CACHE_PATH")),
		cacheHitHeader:         strings.TrimSpace(os.Getenv("ISDICT_CACHE_HIT_HEADER")),
		cacheHitValue:          strings.TrimSpace(os.Getenv("ISDICT_CACHE_HIT_VALUE")),
		postPath:               probe.EnvString("ISDICT_POST_PATH", "/api/v1/words/batch"),
		corsOrigin:             probe.EnvString("ISDICT_CORS_ORIGIN", "https://web.isdict.test"),
		expectRequestID:        probe.EnvBool("ISDICT_EXPECT_REQUEST_ID", true),
		expectCORS:             probe.EnvBool("ISDICT_EXPECT_CORS", true),
		expectedCORSOrigin:     strings.TrimSpace(os.Getenv("ISDICT_EXPECT_CORS_ORIGIN")),
		expectedRPSLimit:       strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_RPS")),
		expectedHourlyLimit:    strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_PER_HOUR")),
		expectedDailyLimit:     strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_PER_DAY")),
		batchTotal:             probe.EnvInt("ISDICT_VERIFY_BATCH_TOTAL", 500),
		batchConcurrency:       probe.EnvInt("ISDICT_VERIFY_BATCH_CONCURRENCY", 50),
		expectedMin429:         probe.EnvInt("ISDICT_EXPECT_MIN_429", 1),
		concurrentTotal:        probe.EnvInt("ISDICT_CONCURRENT_REQUESTS", 50),
		staticProbeTotal:       probe.EnvInt("ISDICT_STATIC_PROBE_TOTAL", verifyStaticProbeTotal(strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_RPS")))),
		staticProbeConcurrency: probe.EnvInt("ISDICT_STATIC_PROBE_CONCURRENCY", verifyStaticProbeConcurrency(strings.TrimSpace(os.Getenv("ISDICT_EXPECT_RATE_LIMIT_RPS")))),
		healthProbeTotal:       probe.EnvInt("ISDICT_HEALTH_PROBE_TOTAL", 10),
	}
}

func doRequest(method string, cfg settings, path string, withOrigin bool, body io.Reader, extraHeaders map[string]string) (*http.Response, error) {
	headers := map[string]string{}
	for key, value := range extraHeaders {
		headers[key] = value
	}
	if withOrigin && cfg.corsOrigin != "" {
		headers["Origin"] = cfg.corsOrigin
	}
	if method == http.MethodPost {
		headers["Content-Type"] = "application/json"
	}
	return probe.DoRequest(cfg.client, method, cfg.baseURL, path, headers, body)
}

func drainAndClose(body io.ReadCloser) {
	probe.DrainAndClose(body)
}

func shorten(value string, max int) string {
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func validateSimpleCORS(cfg settings, origin, methods string) error {
	if cfg.expectCORS && origin == "" {
		return fmt.Errorf("missing Access-Control-Allow-Origin on Origin-bearing request")
	}
	if !cfg.expectCORS && (origin != "" || methods != "") {
		return fmt.Errorf("unexpected CORS headers while ISDICT_EXPECT_CORS is disabled (origin=%q methods=%q)", origin, methods)
	}
	if cfg.expectCORS && cfg.expectedCORSOrigin != "" && origin != cfg.expectedCORSOrigin {
		return fmt.Errorf("Access-Control-Allow-Origin = %q, want %q", origin, cfg.expectedCORSOrigin)
	}
	return nil
}

func validateCORSPreflight(cfg settings, allowOrigin, allowMethods, requestedMethod string) error {
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

func exemptProbePaths(cfg settings) []string {
	paths := []string{cfg.healthPath}
	if cfg.readinessPath != "" && cfg.readinessPath != cfg.healthPath {
		paths = append(paths, cfg.readinessPath)
	}
	return paths
}

func verifyStaticProbeTotal(expectedRPS string) int {
	total, _ := probe.StaticProbeLoad(expectedRPS, 100)
	return total
}

func verifyStaticProbeConcurrency(expectedRPS string) int {
	_, concurrency := probe.StaticProbeLoad(expectedRPS, 100)
	return concurrency
}

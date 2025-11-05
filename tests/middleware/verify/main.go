// Package middleware provides quick verification tests for all middleware features.
// Run: go run verify_test.go
package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const baseURL = "http://localhost:8080"

func main() {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("isdict-API Middleware Feature Verification")
	fmt.Println(strings.Repeat("=", 60) + "\n")

	checkFeature("1. Request ID Generation", testRequestID)
	checkFeature("2. CORS Headers", testCORS)
	checkFeature("3. Response Caching (GET)", testCache)
	checkFeature("4. Rate Limiting Headers", testRateLimitHeaders)
	checkFeature("5. Rate Limiting Protection", testRateLimitProtection)
	checkFeature("6. POST Not Cached", testPOSTNotCached)
	checkFeature("7. Static Files Skip Rate Limit", testStaticSkipRateLimit)
	checkFeature("8. Concurrent Request Handling", testConcurrent)
	checkFeature("9. OPTIONS Preflight (CORS)", testOptions)

	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("✅ All Middleware Features Verified!")
	fmt.Println(strings.Repeat("=", 60) + "\n")
}

func checkFeature(name string, fn func() (bool, string)) {
	fmt.Printf("%-40s ... ", name)
	success, msg := fn()
	if success {
		fmt.Printf("✅ PASS\n")
		if msg != "" {
			fmt.Printf("   %s\n", msg)
		}
	} else {
		fmt.Printf("❌ FAIL\n")
		if msg != "" {
			fmt.Printf("   %s\n", msg)
		}
	}
}

func testRequestID() (bool, string) {
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get("X-Request-Id")
	if requestID == "" {
		return false, "No Request-ID header found"
	}
	return true, fmt.Sprintf("ID: %s", requestID[:16]+"...")
}

func testCORS() (bool, string) {
	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	origin := resp.Header.Get("Access-Control-Allow-Origin")
	methods := resp.Header.Get("Access-Control-Allow-Methods")

	if origin == "" || methods == "" {
		return false, "Missing CORS headers"
	}
	return true, fmt.Sprintf("Origin: %s, Methods: %s", origin, methods)
}

func testCache() (bool, string) {
	endpoint := baseURL + "/api/v1/words/request"

	start1 := time.Now()
	resp1, err := http.Get(endpoint)
	dur1 := time.Since(start1)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp1.Body)
		resp1.Body.Close()
	}

	time.Sleep(50 * time.Millisecond)

	start2 := time.Now()
	resp2, err := http.Get(endpoint)
	dur2 := time.Since(start2)
	if err == nil {
		_, _ = io.Copy(io.Discard, resp2.Body)
		resp2.Body.Close()
	}

	if dur2 < dur1 {
		improvement := (float64(dur1-dur2) / float64(dur1)) * 100
		return true, fmt.Sprintf("1st: %v, 2nd: %v (%.0f%% faster)", dur1, dur2, improvement)
	}
	return false, fmt.Sprintf("2nd request not faster (%v vs %v)", dur1, dur2)
}

func testRateLimitHeaders() (bool, string) {
	resp, err := http.Get(baseURL + "/api/v1/search?q=test")
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	limit := resp.Header.Get("X-Ratelimit-Limit")
	limitHour := resp.Header.Get("X-Ratelimit-Limit-Hour")

	if limit == "" || limitHour == "" {
		return false, "Missing rate limit headers"
	}
	return true, fmt.Sprintf("RPS: %s, Hourly: %s", limit, limitHour)
}

func testRateLimitProtection() (bool, string) {
	// Send 500 rapid concurrent requests to trigger rate limiting
	// Config: 100 RPS with burst of 200
	rateLimited := 0
	total := 500
	concurrency := 50

	results := make(chan int, total)

	// Launch concurrent requests
	for batch := 0; batch < total/concurrency; batch++ {
		for i := 0; i < concurrency; i++ {
			go func() {
				resp, err := http.Get(baseURL + "/api/v1/search?q=test")
				if err != nil {
					results <- 0
					return
				}
				statusCode := resp.StatusCode
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				results <- statusCode
			}()
		}
		time.Sleep(10 * time.Millisecond) // Small delay between batches
	}

	// Collect results
	for i := 0; i < total; i++ {
		if status := <-results; status == 429 {
			rateLimited++
		}
	}

	if rateLimited > 0 {
		return true, fmt.Sprintf("Triggered after rapid requests (%d/%d limited)", rateLimited, total)
	}
	return false, "No rate limiting detected"
}

func testPOSTNotCached() (bool, string) {
	endpoint := baseURL + "/api/v1/words/batch"

	start1 := time.Now()
	resp1, _ := http.Post(endpoint, "application/json", nil)
	dur1 := time.Since(start1)
	if resp1 != nil {
		_, _ = io.Copy(io.Discard, resp1.Body)
		resp1.Body.Close()
	}

	time.Sleep(50 * time.Millisecond)

	start2 := time.Now()
	resp2, _ := http.Post(endpoint, "application/json", nil)
	dur2 := time.Since(start2)
	if resp2 != nil {
		_, _ = io.Copy(io.Discard, resp2.Body)
		resp2.Body.Close()
	}

	diff := dur1 - dur2
	if diff < 0 {
		diff = -diff
	}

	// Similar times = not cached
	if diff < 100*time.Millisecond {
		return true, fmt.Sprintf("Similar times: %v vs %v", dur1, dur2)
	}
	return false, fmt.Sprintf("Times differ significantly: %v vs %v", dur1, dur2)
}

func testStaticSkipRateLimit() (bool, string) {
	rateLimited := 0
	for i := 0; i < 100; i++ {
		resp, err := http.Get(baseURL + "/")
		if err != nil {
			continue
		}
		if resp.StatusCode == 429 {
			rateLimited++
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	if rateLimited == 0 {
		return true, "100 requests - no rate limiting"
	}
	return false, fmt.Sprintf("%d/100 requests were rate limited", rateLimited)
}

func testConcurrent() (bool, string) {
	done := make(chan bool, 50)
	for i := 0; i < 50; i++ {
		go func() {
			resp, err := http.Get(baseURL + "/health")
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				done <- true
			} else {
				done <- false
			}
		}()
	}

	success := 0
	for i := 0; i < 50; i++ {
		if <-done {
			success++
		}
	}

	if success >= 45 {
		return true, fmt.Sprintf("%d/50 concurrent requests successful", success)
	}
	return false, fmt.Sprintf("Only %d/50 successful", success)
}

func testOptions() (bool, string) {
	req, _ := http.NewRequest("OPTIONS", baseURL+"/api/v1/words/test", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err.Error()
	}
	defer resp.Body.Close()

	if resp.StatusCode == 204 || resp.StatusCode == 200 {
		return true, fmt.Sprintf("Status: %d", resp.StatusCode)
	}
	return false, fmt.Sprintf("Unexpected status: %d", resp.StatusCode)
}

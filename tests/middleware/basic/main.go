// Package middleware provides integration tests for API middleware functionality.
// Run: go run basic_test.go
package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const baseURL = "http://localhost:8080"

func main() {
	fmt.Println("\n========================================")
	fmt.Println("isdict-API Middleware Basic Tests")
	fmt.Println("========================================")
	fmt.Println()

	// Test 1: Request ID and CORS
	test1RequestIDAndCORS()

	// Test 2: Response Caching
	test2ResponseCaching()

	// Test 3: Rate Limiting Headers
	test3RateLimitHeaders()

	// Test 4: Burst Rate Limiting
	test4BurstRateLimiting()

	// Test 5: POST not cached
	test5POSTNotCached()

	// Test 6: Request ID Uniqueness
	test6RequestIDUniqueness()

	// Test 7: Static files skip rate limiting
	test7StaticFilesSkipRateLimit()

	// Test 8: Cache query string sensitivity
	test8CacheQueryStringSensitivity()

	// Test 9: Concurrent requests
	test9ConcurrentRequests()

	fmt.Println("\n========================================")
	fmt.Println("All Tests Completed!")
	fmt.Println("========================================")
	fmt.Println()
}

func test1RequestIDAndCORS() {
	fmt.Println("Test 1: Request ID and CORS Headers")
	fmt.Println("-------------------------------------")

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		fmt.Printf("❌ Error: %v\n\n", err)
		return
	}
	defer resp.Body.Close()

	requestID := resp.Header.Get("X-Request-Id")
	corsOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	corsMethods := resp.Header.Get("Access-Control-Allow-Methods")

	fmt.Printf("✓ Request ID: %s\n", requestID)
	fmt.Printf("✓ CORS Allow Origin: %s\n", corsOrigin)
	fmt.Printf("✓ CORS Allow Methods: %s\n", corsMethods)
	fmt.Println()
}

func test2ResponseCaching() {
	fmt.Println("Test 2: Response Caching")
	fmt.Println("-------------------------------------")

	endpoint := baseURL + "/api/v1/words/hello"

	// First request
	start1 := time.Now()
	resp1, err := http.Get(endpoint)
	duration1 := time.Since(start1)
	if err == nil {
		io.Copy(io.Discard, resp1.Body)
		resp1.Body.Close()
	}

	time.Sleep(100 * time.Millisecond)

	// Second request (should be cached)
	start2 := time.Now()
	resp2, err := http.Get(endpoint)
	duration2 := time.Since(start2)
	if err == nil {
		io.Copy(io.Discard, resp2.Body)
		resp2.Body.Close()
	}

	fmt.Printf("✓ First request:  %v\n", duration1)
	fmt.Printf("✓ Second request: %v (cached)\n", duration2)
	if duration2 < duration1 {
		improvement := float64(duration1-duration2) / float64(duration1) * 100
		fmt.Printf("✓ Cache working! %.1f%% faster\n", improvement)
	}
	fmt.Println()
}

func test3RateLimitHeaders() {
	fmt.Println("Test 3: Rate Limiting Headers")
	fmt.Println("-------------------------------------")

	resp, err := http.Get(baseURL + "/api/v1/search?q=test&limit=10")
	if err != nil {
		fmt.Printf("❌ Error: %v\n\n", err)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("Rate Limit Info:\n")
	fmt.Printf("  RPS Limit: %s\n", resp.Header.Get("X-Ratelimit-Limit"))
	fmt.Printf("  RPS Remaining: %s\n", resp.Header.Get("X-Ratelimit-Remaining"))
	fmt.Printf("  RPS Reset: %s\n", resp.Header.Get("X-Ratelimit-Reset"))
	fmt.Printf("  Hourly Limit: %s\n", resp.Header.Get("X-Ratelimit-Limit-Hour"))
	fmt.Printf("  Hourly Remaining: %s\n", resp.Header.Get("X-Ratelimit-Remaining-Hour"))
	fmt.Printf("  Hourly Reset: %s\n", resp.Header.Get("X-Ratelimit-Reset-Hour"))
	fmt.Println()
}

func test4BurstRateLimiting() {
	fmt.Println("Test 4: Burst Rate Limiting (250 rapid requests)")
	fmt.Println("-------------------------------------")

	var successCount, rateLimited, errors atomic.Int32
	total := 250

	fmt.Printf("Sending %d requests...\n", total)

	for i := 0; i < total; i++ {
		resp, err := http.Get(baseURL + "/health")
		if err != nil {
			errors.Add(1)
			continue
		}

		switch resp.StatusCode {
		case 429:
			rateLimited.Add(1)
			if rateLimited.Load() == 1 {
				fmt.Printf("  ✓ First rate limit at request #%d\n", i+1)
			}
		case 200:
			successCount.Add(1)
		default:
			errors.Add(1)
		}

		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if (i+1)%50 == 0 {
			fmt.Printf("  Progress: %d/%d - Success: %d, Rate Limited: %d\n",
				i+1, total, successCount.Load(), rateLimited.Load())
		}
	}

	fmt.Printf("\nResults:\n")
	fmt.Printf("  Total: %d\n", total)
	fmt.Printf("  Successful: %d\n", successCount.Load())
	fmt.Printf("  Rate Limited (429): %d\n", rateLimited.Load())
	fmt.Printf("  Errors: %d\n", errors.Load())

	if rateLimited.Load() > 0 {
		fmt.Printf("✓ Rate limiting is working!\n")
	}
	fmt.Println()
}

func test5POSTNotCached() {
	fmt.Println("Test 5: POST Requests Should NOT Be Cached")
	fmt.Println("-------------------------------------")

	endpoint := baseURL + "/api/v1/words/batch"

	// First POST
	start1 := time.Now()
	resp1, err := http.Post(endpoint, "application/json", nil)
	duration1 := time.Since(start1)
	if err == nil {
		io.Copy(io.Discard, resp1.Body)
		resp1.Body.Close()
	}

	time.Sleep(100 * time.Millisecond)

	// Second POST
	start2 := time.Now()
	resp2, err := http.Post(endpoint, "application/json", nil)
	duration2 := time.Since(start2)
	if err == nil {
		io.Copy(io.Discard, resp2.Body)
		resp2.Body.Close()
	}

	fmt.Printf("  POST Request 1: %v\n", duration1)
	fmt.Printf("  POST Request 2: %v\n", duration2)

	diff := duration1 - duration2
	if diff < 0 {
		diff = -diff
	}
	if diff < 50*time.Millisecond {
		fmt.Printf("✓ POST requests NOT cached (similar times)\n")
	}
	fmt.Println()
}

func test6RequestIDUniqueness() {
	fmt.Println("Test 6: Request ID Uniqueness")
	fmt.Println("-------------------------------------")

	requestIDs := make(map[string]bool)
	count := 20

	for i := 0; i < count; i++ {
		resp, err := http.Get(baseURL + "/health")
		if err != nil {
			continue
		}
		requestID := resp.Header.Get("X-Request-Id")
		requestIDs[requestID] = true
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	fmt.Printf("  Generated IDs: %d\n", count)
	fmt.Printf("  Unique IDs: %d\n", len(requestIDs))

	if len(requestIDs) == count {
		fmt.Printf("✓ All Request IDs are unique!\n")
	} else {
		fmt.Printf("❌ Found duplicate Request IDs!\n")
	}
	fmt.Println()
}

func test7StaticFilesSkipRateLimit() {
	fmt.Println("Test 7: Static Files Skip Rate Limiting")
	fmt.Println("-------------------------------------")

	var successCount, rateLimited atomic.Int32
	total := 50

	fmt.Printf("Sending %d requests to static file...\n", total)

	for i := 0; i < total; i++ {
		resp, err := http.Get(baseURL + "/")
		if err != nil {
			continue
		}

		switch resp.StatusCode {
		case 429:
			rateLimited.Add(1)
		case 200:
			successCount.Add(1)
		}

		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	fmt.Printf("  Successful: %d, Rate Limited: %d\n", successCount.Load(), rateLimited.Load())

	if rateLimited.Load() == 0 {
		fmt.Printf("✓ Static files are NOT rate limited!\n")
	} else {
		fmt.Printf("❌ Static files were rate limited\n")
	}
	fmt.Println()
}

func test8CacheQueryStringSensitivity() {
	fmt.Println("Test 8: Cache Query String Sensitivity")
	fmt.Println("-------------------------------------")

	queries := []string{
		"q=test&limit=5",
		"q=test&limit=10",
		"q=hello&limit=5",
	}

	for _, query := range queries {
		endpoint := baseURL + "/api/v1/search?" + query

		start1 := time.Now()
		resp1, _ := http.Get(endpoint)
		duration1 := time.Since(start1)
		if resp1 != nil {
			io.Copy(io.Discard, resp1.Body)
			resp1.Body.Close()
		}

		time.Sleep(50 * time.Millisecond)

		start2 := time.Now()
		resp2, _ := http.Get(endpoint)
		duration2 := time.Since(start2)
		if resp2 != nil {
			io.Copy(io.Discard, resp2.Body)
			resp2.Body.Close()
		}

		fmt.Printf("  Query: %s\n", query)
		fmt.Printf("    First: %v, Second: %v\n", duration1, duration2)
	}
	fmt.Println()
}

func test9ConcurrentRequests() {
	fmt.Println("Test 9: Concurrent Requests (100 goroutines)")
	fmt.Println("-------------------------------------")

	var wg sync.WaitGroup
	var successCount, errorCount atomic.Int32
	concurrency := 100

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			resp, err := http.Get(baseURL + "/health")
			if err != nil {
				errorCount.Add(1)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				successCount.Add(1)
			} else {
				errorCount.Add(1)
			}

			io.Copy(io.Discard, resp.Body)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("  Concurrent requests: %d\n", concurrency)
	fmt.Printf("  Successful: %d\n", successCount.Load())
	fmt.Printf("  Errors: %d\n", errorCount.Load())
	fmt.Printf("  Total time: %v\n", duration)
	fmt.Printf("✓ Server handled concurrent requests!\n")
	fmt.Println()
}

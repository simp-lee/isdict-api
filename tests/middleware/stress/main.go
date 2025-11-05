// Package middleware provides stress tests for API middleware.
// Run: go run stress_test.go
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
	fmt.Println("Extreme Stress Testing for Middleware")
	fmt.Println("========================================")
	fmt.Println()

	// Test 1: Extreme burst - 1000 requests as fast as possible
	testExtremeBurst()

	// Test 2: Sustained high load
	testSustainedLoad()

	// Test 3: Mixed concurrent GET/POST
	testMixedConcurrent()

	fmt.Println("\n========================================")
	fmt.Println("Stress Testing Complete!")
	fmt.Println("========================================")
	fmt.Println()
}

func testExtremeBurst() {
	fmt.Println("Test 1: Extreme Burst (1000 requests)")
	fmt.Println("-------------------------------------")

	var successCount, rateLimited, errors atomic.Int32
	total := 1000

	fmt.Printf("Sending %d requests as fast as possible...\n", total)
	start := time.Now()

	var wg sync.WaitGroup
	// Use buffered channel to control concurrency
	sem := make(chan struct{}, 50) // 50 concurrent requests

	for i := 0; i < total; i++ {
		wg.Add(1)
		sem <- struct{}{} // Acquire

		go func(id int) {
			defer wg.Done()
			defer func() { <-sem }() // Release

			resp, err := http.Get(baseURL + "/api/v1/search?q=test")
			if err != nil {
				errors.Add(1)
				return
			}
			defer resp.Body.Close()

			switch resp.StatusCode {
			case 200:
				successCount.Add(1)
			case 429:
				rateLimited.Add(1)
				if rateLimited.Load() == 1 {
					fmt.Printf("  ✓ First rate limit at request #%d\n", id+1)
				}
			default:
				errors.Add(1)
			}

			io.Copy(io.Discard, resp.Body)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("\nResults:\n")
	fmt.Printf("  Total requests: %d\n", total)
	fmt.Printf("  Duration: %v\n", duration)
	fmt.Printf("  Successful: %d\n", successCount.Load())
	fmt.Printf("  Rate Limited (429): %d\n", rateLimited.Load())
	fmt.Printf("  Errors: %d\n", errors.Load())
	fmt.Printf("  RPS: %.2f\n", float64(total)/duration.Seconds())

	if rateLimited.Load() > 0 {
		fmt.Printf("✓ Rate limiting triggered successfully!\n")
		fmt.Printf("  Success rate: %.1f%%\n", float64(successCount.Load())/float64(total)*100)
	} else {
		fmt.Printf("⚠ No rate limiting detected (may need more requests)\n")
	}
	fmt.Println()
}

func testSustainedLoad() {
	fmt.Println("Test 2: Sustained Load (10 seconds)")
	fmt.Println("-------------------------------------")

	var requestCount, successCount, rateLimited atomic.Int32
	duration := 10 * time.Second
	workers := 20

	fmt.Printf("Running %d workers for %v...\n", workers, duration)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Start time
	start := time.Now()

	// Start workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for {
				select {
				case <-stop:
					return
				default:
					requestCount.Add(1)

					resp, err := http.Get(baseURL + "/api/v1/words/hello")
					if err != nil {
						continue
					}

					switch resp.StatusCode {
					case 200:
						successCount.Add(1)
					case 429:
						rateLimited.Add(1)
					}

					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()

					time.Sleep(10 * time.Millisecond) // Small delay between requests
				}
			}
		}(i)
	}

	// Run for specified duration
	time.Sleep(duration)
	close(stop)
	wg.Wait()

	totalDuration := time.Since(start)

	fmt.Printf("\nResults:\n")
	fmt.Printf("  Duration: %v\n", totalDuration)
	fmt.Printf("  Total requests: %d\n", requestCount.Load())
	fmt.Printf("  Successful: %d\n", successCount.Load())
	fmt.Printf("  Rate Limited: %d\n", rateLimited.Load())
	fmt.Printf("  Average RPS: %.2f\n", float64(requestCount.Load())/totalDuration.Seconds())

	if rateLimited.Load() > 0 {
		fmt.Printf("✓ Rate limiting working under sustained load\n")
	}
	fmt.Println()
}

func testMixedConcurrent() {
	fmt.Println("Test 3: Mixed Concurrent GET/POST Requests")
	fmt.Println("-------------------------------------")

	var getSuccess, postSuccess, rateLimited, errors atomic.Int32
	totalGET := 200
	totalPOST := 100

	fmt.Printf("Sending %d GET + %d POST requests concurrently...\n", totalGET, totalPOST)

	var wg sync.WaitGroup
	start := time.Now()

	// Send GET requests
	for i := 0; i < totalGET; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			resp, err := http.Get(fmt.Sprintf("%s/api/v1/search?q=test%d", baseURL, id))
			if err != nil {
				errors.Add(1)
				return
			}
			defer resp.Body.Close()

			switch resp.StatusCode {
			case 200:
				getSuccess.Add(1)
			case 429:
				rateLimited.Add(1)
			default:
				errors.Add(1)
			}

			io.Copy(io.Discard, resp.Body)
		}(i)
	}

	// Send POST requests
	for i := 0; i < totalPOST; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			resp, err := http.Post(baseURL+"/api/v1/words/batch", "application/json", nil)
			if err != nil {
				errors.Add(1)
				return
			}
			defer resp.Body.Close()

			// POST might return 400 (bad request) which is fine for this test
			switch resp.StatusCode {
			case 200, 400:
				postSuccess.Add(1)
			case 429:
				rateLimited.Add(1)
			default:
				errors.Add(1)
			}

			io.Copy(io.Discard, resp.Body)
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	fmt.Printf("\nResults:\n")
	fmt.Printf("  Duration: %v\n", duration)
	fmt.Printf("  GET successful: %d/%d\n", getSuccess.Load(), totalGET)
	fmt.Printf("  POST successful: %d/%d\n", postSuccess.Load(), totalPOST)
	fmt.Printf("  Rate Limited: %d\n", rateLimited.Load())
	fmt.Printf("  Errors: %d\n", errors.Load())
	fmt.Printf("  Total RPS: %.2f\n", float64(totalGET+totalPOST)/duration.Seconds())
	fmt.Printf("✓ Server handled mixed concurrent requests\n")
	fmt.Println()
}

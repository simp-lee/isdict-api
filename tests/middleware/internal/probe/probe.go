package probe

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const DefaultHTTPTimeout = 30 * time.Second

type RateLimitHeaders struct {
	RPSLimit        string
	RPSRemaining    string
	RPSReset        string
	HourlyLimit     string
	HourlyRemaining string
	HourlyReset     string
	DailyLimit      string
	DailyRemaining  string
	DailyReset      string
}

func NewHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultHTTPTimeout
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.DialContext = (&net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.MaxIdleConns = 128
	transport.MaxIdleConnsPerHost = 64
	transport.IdleConnTimeout = 90 * time.Second
	transport.ResponseHeaderTimeout = timeout

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func HTTPTimeoutFromEnv() time.Duration {
	seconds := EnvInt("ISDICT_HTTP_TIMEOUT_SECONDS", int(DefaultHTTPTimeout/time.Second))
	if seconds <= 0 {
		seconds = int(DefaultHTTPTimeout / time.Second)
	}
	return time.Duration(seconds) * time.Second
}

func DoRequest(client *http.Client, method, baseURL, path string, headers map[string]string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, EndpointURL(baseURL, path), body)
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		if strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	return client.Do(req)
}

func EndpointURL(baseURL, path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.TrimLeft(path, "/")
}

func AddQueryParam(path, key, value string) string {
	separator := "?"
	if strings.Contains(path, "?") {
		separator = "&"
	}
	return path + separator + key + "=" + value
}

func ReadBodyAndClose(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer func() {
		_ = body.Close()
	}()
	return io.ReadAll(body)
}

func DrainAndClose(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}

func ExplainRequestError(err error) string {
	if err == nil {
		return ""
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Sprintf("request timed out: %v", err)
	}

	return err.Error()
}

func StableReplayResult(first *http.Response, firstBody []byte, second *http.Response, secondBody []byte) (bool, string) {
	if first == nil || second == nil {
		return false, "replay probe did not receive two responses"
	}
	if first.StatusCode != http.StatusOK || second.StatusCode != http.StatusOK {
		return false, fmt.Sprintf("replay probe returned statuses %d and %d, want 200/200", first.StatusCode, second.StatusCode)
	}
	if !bytes.Equal(firstBody, secondBody) {
		return false, "replay probe body changed between identical GET requests"
	}
	return true, "identical GETs returned the same 200 response body"
}

func CacheReplayResult(first *http.Response, firstBody []byte, second *http.Response, secondBody []byte, hitHeader, hitValue string) (bool, string) {
	hitHeader = strings.TrimSpace(hitHeader)
	hitValue = strings.TrimSpace(hitValue)
	if hitHeader == "" || hitValue == "" {
		return false, "cache probe requires explicit ISDICT_CACHE_HIT_HEADER and ISDICT_CACHE_HIT_VALUE settings"
	}

	ok, message := StableReplayResult(first, firstBody, second, secondBody)
	if !ok {
		return false, message
	}

	firstSignal := first.Header.Get(hitHeader)
	secondSignal := second.Header.Get(hitHeader)
	if secondSignal != hitValue {
		return false, fmt.Sprintf("second response %s = %q, want %q to prove a cache hit", hitHeader, secondSignal, hitValue)
	}
	if firstSignal == hitValue {
		return false, fmt.Sprintf("first response already reported %s = %q; probe requires a miss-to-hit transition to prove caching", hitHeader, firstSignal)
	}

	return true, fmt.Sprintf("stable body plus %s transition %q -> %q", hitHeader, firstSignal, secondSignal)
}

func ReadRateLimitHeaders(resp *http.Response) RateLimitHeaders {
	if resp == nil {
		return RateLimitHeaders{}
	}

	return RateLimitHeaders{
		RPSLimit:        resp.Header.Get("X-RateLimit-Limit"),
		RPSRemaining:    resp.Header.Get("X-RateLimit-Remaining"),
		RPSReset:        resp.Header.Get("X-RateLimit-Reset"),
		HourlyLimit:     resp.Header.Get("X-RateLimit-Limit-Hour"),
		HourlyRemaining: resp.Header.Get("X-RateLimit-Remaining-Hour"),
		HourlyReset:     resp.Header.Get("X-RateLimit-Reset-Hour"),
		DailyLimit:      resp.Header.Get("X-RateLimit-Limit-Day"),
		DailyRemaining:  resp.Header.Get("X-RateLimit-Remaining-Day"),
		DailyReset:      resp.Header.Get("X-RateLimit-Reset-Day"),
	}
}

func (h RateLimitHeaders) Summary() string {
	return fmt.Sprintf(
		"RPS(limit=%s remaining=%s reset=%s), Hourly(limit=%s remaining=%s reset=%s), Daily(limit=%s remaining=%s reset=%s)",
		displayHeaderValue(h.RPSLimit),
		displayHeaderValue(h.RPSRemaining),
		displayHeaderValue(h.RPSReset),
		displayHeaderValue(h.HourlyLimit),
		displayHeaderValue(h.HourlyRemaining),
		displayHeaderValue(h.HourlyReset),
		displayHeaderValue(h.DailyLimit),
		displayHeaderValue(h.DailyRemaining),
		displayHeaderValue(h.DailyReset),
	)
}

func ValidateRateLimitHeaders(resp *http.Response, expectedRPS, expectedHourly, expectedDaily string) (bool, string) {
	headers := ReadRateLimitHeaders(resp)
	buckets := []struct {
		label      string
		limit      string
		remaining  string
		reset      string
		expected   string
		expectName string
	}{
		{
			label:      "RPS",
			limit:      headers.RPSLimit,
			remaining:  headers.RPSRemaining,
			reset:      headers.RPSReset,
			expected:   strings.TrimSpace(expectedRPS),
			expectName: "ISDICT_EXPECT_RATE_LIMIT_RPS",
		},
		{
			label:      "hourly",
			limit:      headers.HourlyLimit,
			remaining:  headers.HourlyRemaining,
			reset:      headers.HourlyReset,
			expected:   strings.TrimSpace(expectedHourly),
			expectName: "ISDICT_EXPECT_RATE_LIMIT_PER_HOUR",
		},
		{
			label:      "daily",
			limit:      headers.DailyLimit,
			remaining:  headers.DailyRemaining,
			reset:      headers.DailyReset,
			expected:   strings.TrimSpace(expectedDaily),
			expectName: "ISDICT_EXPECT_RATE_LIMIT_PER_DAY",
		},
	}

	observedAny := false
	for _, bucket := range buckets {
		observedBucket := bucket.limit != "" || bucket.remaining != "" || bucket.reset != ""
		if observedBucket {
			observedAny = true
		}
		if !observedBucket && bucket.expected == "" {
			continue
		}
		if bucket.limit == "" {
			if observedBucket {
				return false, fmt.Sprintf("missing %s rate-limit limit header while that bucket is partially exposed", bucket.label)
			}
			return false, fmt.Sprintf("missing %s rate-limit limit header while %s is configured", bucket.label, bucket.expectName)
		}
		if bucket.expected != "" && bucket.limit != bucket.expected {
			return false, fmt.Sprintf("%s rate-limit limit header = %q, want %q", bucket.label, bucket.limit, bucket.expected)
		}
		if err := validateRateLimitBucket(bucket.label, bucket.limit, bucket.remaining, bucket.reset); err != nil {
			return false, err.Error()
		}
	}

	if !observedAny {
		return false, "response did not expose any rate-limit headers"
	}

	return true, headers.Summary()
}

func validateRateLimitBucket(label, limit, remaining, reset string) error {
	if _, err := parseHeaderInt(limit); err != nil {
		return fmt.Errorf("%s rate-limit limit header is invalid: %v", label, err)
	}
	if _, err := parseHeaderInt(remaining); err != nil {
		return fmt.Errorf("%s rate-limit remaining header is invalid: %v", label, err)
	}
	if _, err := parseHeaderInt64(reset); err != nil {
		return fmt.Errorf("%s rate-limit reset header is invalid: %v", label, err)
	}
	return nil
}

func parseHeaderInt(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("header is empty")
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", value)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%q is negative", value)
	}
	return parsed, nil
}

func parseHeaderInt64(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, errors.New("header is empty")
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not an integer", value)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%q is negative", value)
	}
	return parsed, nil
}

func displayHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func FormatStatusDistribution(counts map[int]int) string {
	if len(counts) == 0 {
		return "none"
	}

	statuses := make([]int, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)

	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		label := http.StatusText(status)
		if label == "" {
			label = "Unknown"
		}
		parts = append(parts, fmt.Sprintf("%d %s=%d", status, label, counts[status]))
	}

	return strings.Join(parts, ", ")
}

func RunBounded(total, concurrency int, fn func(index int)) {
	if total <= 0 || fn == nil {
		return
	}
	if concurrency <= 0 || concurrency > total {
		concurrency = total
	}

	jobs := make(chan int)
	var wg sync.WaitGroup

	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				fn(index)
			}
		}()
	}

	for index := range total {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
}

func RateLimitRPS(expected string, fallback int) int {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(expected)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func StaticProbeLoad(expectedRPS string, fallbackLimit int) (int, int) {
	limit := RateLimitRPS(expectedRPS, fallbackLimit)
	overshoot := limit / 5
	if overshoot < 25 {
		overshoot = 25
	}
	if overshoot > 100 {
		overshoot = 100
	}

	total := limit + overshoot
	concurrency := limit + 1
	if concurrency < 16 {
		concurrency = 16
	}
	if concurrency > 256 {
		concurrency = 256
	}
	if concurrency > total {
		concurrency = total
	}

	return total, concurrency
}

func EnvString(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func EnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid integer for %s: %q\n", key, value)
		os.Exit(2)
	}
	return parsed
}

func EnvBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}

	switch strings.ToLower(value) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		fmt.Fprintf(os.Stderr, "invalid boolean for %s: %q\n", key, value)
		os.Exit(2)
		return fallback
	}
}

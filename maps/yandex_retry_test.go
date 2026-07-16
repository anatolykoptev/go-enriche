package maps

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestFetchAndParse_RetryOnTransientError verifies that fetchAndParse retries
// on transient network errors and succeeds when a later attempt works.
func TestFetchAndParse_RetryOnTransientError(t *testing.T) {
	callCount := 0
	y := &YandexMaps{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		orgFetcher: func(ctx context.Context, orgURL string) (string, error) {
			callCount++
			if callCount < 2 {
				return "", errors.New("network timeout")
			}
			return `{"shortTitle":"Retry Cafe","status":"open","phones":[{"number":"+7 999"}],"urls":["https://retry.cafe"],"currentWorkingStatus":{"text":"Open"},"fullAddress":"SPb"}`, nil
		},
	}

	result, err := y.fetchAndParse(context.Background(), "https://yandex.ru/maps/org/test/123")
	if err != nil {
		t.Fatalf("fetchAndParse failed: %v", err)
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2 (1 fail + 1 success)", callCount)
	}
	if result.OrgData == nil || result.OrgData.Name != "Retry Cafe" {
		t.Errorf("expected parsed data, got %+v", result.OrgData)
	}
}

// TestFetchAndParse_NoRetryOnContextCancel verifies that context cancellation
// is NOT retried.
func TestFetchAndParse_NoRetryOnContextCancel(t *testing.T) {
	callCount := 0
	y := &YandexMaps{
		orgFetcher: func(ctx context.Context, orgURL string) (string, error) {
			callCount++
			return "", context.Canceled
		},
	}

	_, err := y.fetchAndParse(context.Background(), "https://yandex.ru/maps/org/test/123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retry on context cancel)", callCount)
	}
}

// TestFetchAndParse_MaxRetriesExhausted verifies that after max retries,
// the last error is returned.
func TestFetchAndParse_MaxRetriesExhausted(t *testing.T) {
	callCount := 0
	y := &YandexMaps{
		orgFetcher: func(ctx context.Context, orgURL string) (string, error) {
			callCount++
			return "", errors.New("persistent network error")
		},
	}

	_, err := y.fetchAndParse(context.Background(), "https://yandex.ru/maps/org/test/123")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// fetchMaxRetries + 1 initial attempt = 3 total
	if callCount != fetchMaxRetries+1 {
		t.Errorf("callCount = %d, want %d", callCount, fetchMaxRetries+1)
	}
}

// TestIsRetryableError verifies the retryability classification.
func TestIsRetryableError(t *testing.T) {
	if isRetryableError(nil) {
		t.Error("nil error should not be retryable")
	}
	if isRetryableError(context.Canceled) {
		t.Error("context.Canceled should not be retryable")
	}
	if isRetryableError(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should not be retryable")
	}
	if !isRetryableError(errors.New("network timeout")) {
		t.Error("generic network error should be retryable")
	}
}

// TestFetchAndParse_RetryBackoffDuration verifies that retries actually wait
// (not instant), proving the backoff is applied.
func TestFetchAndParse_RetryBackoffDuration(t *testing.T) {
	callCount := 0
	y := &YandexMaps{
		orgFetcher: func(ctx context.Context, orgURL string) (string, error) {
			callCount++
			return "", errors.New("fail")
		},
	}

	start := time.Now()
	_, _ = y.fetchAndParse(context.Background(), "https://yandex.ru/maps/org/test/123")
	elapsed := time.Since(start)

	// 2 retries with backoff: 500ms + 1000ms = 1500ms minimum
	if elapsed < 1400*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 1400ms (backoff applied)", elapsed)
	}
}

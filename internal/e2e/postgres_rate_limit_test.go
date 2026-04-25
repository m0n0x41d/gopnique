//go:build integration

package e2e

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
)

func TestPostgresRateLimitE2E(t *testing.T) {
	ctx := context.Background()
	adminURL := os.Getenv("ERROR_TRACKER_E2E_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("ERROR_TRACKER_E2E_POSTGRES_URL is required")
	}

	databaseURL := createTestDatabase(t, ctx, adminURL)
	store, storeErr := postgres.NewStore(ctx, databaseURL)
	if storeErr != nil {
		t.Fatalf("store: %v", storeErr)
	}
	defer store.Close()

	migrationResult, migrationErr := store.ApplyMigrations(ctx)
	if migrationErr != nil {
		t.Fatalf("migrate: %v", migrationErr)
	}
	if len(migrationResult.Applied) != 25 {
		t.Fatalf("expected 25 migrations, got %d", len(migrationResult.Applied))
	}

	server := httptest.NewServer(httpadapter.NewHandler(
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		e2eResolver{},
		store,
		httpadapter.AuthSettings{PublicURL: "http://example.test", SecretKey: "e2e-secret"},
	))
	defer server.Close()

	client := newE2EClient(t)
	setup := request(
		t,
		client,
		http.MethodPost,
		server.URL+"/setup",
		"application/x-www-form-urlencoded",
		strings.NewReader("organization_name=RateLimit&project_name=RateLimit&email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if setup.StatusCode != http.StatusOK {
		t.Fatalf("expected setup ok, got %d: %s", setup.StatusCode, setup.Body)
	}

	publicKey := projectPublicKey(t, ctx, databaseURL)
	enableProjectKeyRateLimit(t, ctx, databaseURL, 1, 60)

	first := postRateLimitedStoreEvent(t, client, server.URL, publicKey, "930e8400e29b41d4a716446655440000")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected first event accepted, got %d: %s", first.StatusCode, first.Body)
	}

	second := postRateLimitedStoreEvent(t, client, server.URL, publicKey, "940e8400e29b41d4a716446655440000")
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected rate limit rejection, got %d: %s", second.StatusCode, second.Body)
	}
	if secondHeader := rateLimitRetryHeader(t, server.URL, publicKey); secondHeader == "" {
		t.Fatal("expected retry header to be observable")
	}
	if !strings.Contains(second.Body, "project_key_rate_limited") {
		t.Fatalf("expected rate limit reason: %s", second.Body)
	}

	assertRateLimitPersistence(t, ctx, databaseURL)
}

func postRateLimitedStoreEvent(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
	eventID string,
) responseSnapshot {
	t.Helper()

	payload := map[string]any{
		"event_id":  eventID,
		"timestamp": "2026-04-24T13:00:00Z",
		"level":     "error",
		"platform":  "go",
		"message":   "rate limit visible issue",
	}
	body := jsonBody(t, payload)

	return request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/store/?sentry_key="+publicKey,
		"application/json",
		bytes.NewReader(body),
	)
}

func rateLimitRetryHeader(
	t *testing.T,
	baseURL string,
	publicKey string,
) string {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	payload := map[string]any{
		"event_id":  "950e8400e29b41d4a716446655440000",
		"timestamp": "2026-04-24T13:00:00Z",
		"level":     "error",
		"message":   "retry header sentinel",
	}
	body := jsonBody(t, payload)
	request, requestErr := http.NewRequest(
		http.MethodPost,
		baseURL+"/api/1/store/?sentry_key="+publicKey,
		bytes.NewReader(body),
	)
	if requestErr != nil {
		t.Fatalf("request: %v", requestErr)
	}
	request.Header.Set("Content-Type", "application/json")

	response, responseErr := client.Do(request)
	if responseErr != nil {
		t.Fatalf("rate limit request: %v", responseErr)
	}
	defer response.Body.Close()

	return response.Header.Get("Retry-After")
}

func enableProjectKeyRateLimit(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	limit int,
	windowSeconds int,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	_, updateErr := pool.Exec(
		ctx,
		`
update project_key_rate_limit_policies
set enabled = true,
    event_limit = $1,
    window_seconds = $2,
    updated_at = $3
`,
		limit,
		windowSeconds,
		time.Now().UTC(),
	)
	if updateErr != nil {
		t.Fatalf("enable rate limit: %v", updateErr)
	}
}

func assertRateLimitPersistence(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	counts := map[string]int{}
	for name, query := range map[string]string{
		"events":               "select count(*) from events",
		"issues":               "select count(*) from issues",
		"notification_intents": "select count(*) from notification_intents",
	} {
		var count int
		scanErr := pool.QueryRow(ctx, query).Scan(&count)
		if scanErr != nil {
			t.Fatalf("count %s: %v", name, scanErr)
		}
		counts[name] = count
	}

	if counts["events"] != 1 || counts["issues"] != 1 || counts["notification_intents"] != 0 {
		t.Fatalf("unexpected rate-limited persistence: %#v", counts)
	}
}

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

	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
)

func TestPostgresSecurityCSPE2E(t *testing.T) {
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
		strings.NewReader("organization_name=CSP&project_name=CSP&email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if setup.StatusCode != http.StatusOK {
		t.Fatalf("expected setup ok, got %d: %s", setup.StatusCode, setup.Body)
	}

	publicKey := projectPublicKey(t, ctx, databaseURL)
	accepted := postCSPReport(t, client, server.URL, publicKey)
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("expected csp accepted, got %d: %s", accepted.StatusCode, accepted.Body)
	}

	rejected := postInvalidCSPReport(t, client, server.URL, publicKey)
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid csp rejected, got %d: %s", rejected.StatusCode, rejected.Body)
	}

	issues := request(t, client, http.MethodGet, server.URL+"/issues", "", nil)
	if issues.StatusCode != http.StatusOK {
		t.Fatalf("expected issues ok, got %d: %s", issues.StatusCode, issues.Body)
	}
	if !strings.Contains(issues.Body, "CSP violation") {
		t.Fatalf("expected csp issue: %s", issues.Body)
	}

	assertCSPPersistence(t, ctx, databaseURL)
}

func postCSPReport(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	body := []byte(`{
		"csp-report": {
			"document-uri": "https://app.example.test/dashboard",
			"violated-directive": "script-src",
			"effective-directive": "script-src-elem",
			"blocked-uri": "https://cdn.bad.test/app.js",
			"source-file": "https://app.example.test/dashboard",
			"disposition": "enforce"
		}
	}`)

	return request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/security/?sentry_key="+publicKey,
		"application/csp-report",
		bytes.NewReader(body),
	)
}

func postInvalidCSPReport(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	return request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/security/?sentry_key="+publicKey,
		"application/csp-report",
		strings.NewReader(`{"hello":"world"}`),
	)
}

func assertCSPPersistence(
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

	var eventCount int
	eventErr := pool.QueryRow(ctx, "select count(*) from events where platform = 'security'").Scan(&eventCount)
	if eventErr != nil {
		t.Fatalf("count csp events: %v", eventErr)
	}

	var issueCount int
	issueErr := pool.QueryRow(ctx, "select count(*) from issues where title like 'CSP violation:%'").Scan(&issueCount)
	if issueErr != nil {
		t.Fatalf("count csp issues: %v", issueErr)
	}

	if eventCount != 1 || issueCount != 1 {
		t.Fatalf("unexpected csp persistence: events=%d issues=%d", eventCount, issueCount)
	}
}

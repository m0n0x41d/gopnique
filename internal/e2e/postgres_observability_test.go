//go:build integration

package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
)

func TestPostgresObservabilityAPIE2E(t *testing.T) {
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
	if len(migrationResult.Applied) != 33 {
		t.Fatalf("expected 33 migrations, got %d", len(migrationResult.Applied))
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
		store,
		store,
		store,
		e2eResolver{},
		store,
		httpadapter.IngestEnrichments{},
		httpadapter.AuthSettings{PublicURL: "http://example.test", SecretKey: "e2e-secret"},
	))
	defer server.Close()

	client := newE2EClient(t)
	unauthorized := request(t, client, http.MethodGet, server.URL+"/api/admin/observability", "", nil)
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected observability auth gate, got %d: %s", unauthorized.StatusCode, unauthorized.Body)
	}

	setup := request(
		t,
		client,
		http.MethodPost,
		server.URL+"/setup",
		"application/x-www-form-urlencoded",
		strings.NewReader("organization_name=Ops&project_name=Ops&email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if setup.StatusCode != http.StatusOK {
		t.Fatalf("expected setup ok, got %d: %s", setup.StatusCode, setup.Body)
	}

	publicKey := projectPublicKey(t, ctx, databaseURL)
	destinationID := createTelegramDestinationThroughUI(t, ctx, databaseURL, client, server.URL)
	createIssueOpenedAlertThroughUI(t, client, server.URL, destinationID)

	storeReceipt := postStoreEvent(t, client, server.URL, publicKey)
	if storeReceipt.StatusCode != http.StatusOK {
		t.Fatalf("expected store ok, got %d: %s", storeReceipt.StatusCode, storeReceipt.Body)
	}

	snapshot := requestWithResponse(t, client, http.MethodGet, server.URL+"/api/admin/observability", "", nil)
	if snapshot.StatusCode != http.StatusOK {
		t.Fatalf("expected observability ok, got %d: %s", snapshot.StatusCode, snapshot.Body)
	}
	if snapshot.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("unexpected cache header: %q", snapshot.Header.Get("Cache-Control"))
	}
	for _, expected := range []string{
		`"service_name":"error-tracker"`,
		`"applied_count":33`,
		`"events":1`,
		`"issues":1`,
		`"notification_intents":1`,
		`"provider":"telegram"`,
		`"status":"pending"`,
	} {
		if !strings.Contains(snapshot.Body, expected) {
			t.Fatalf("expected %q in observability body: %s", expected, snapshot.Body)
		}
	}

	assertObservabilityEndpoint(t, client, server.URL, "/api/admin/observability/system")
	assertObservabilityEndpoint(t, client, server.URL, "/api/admin/observability/readiness")
	assertObservabilityEndpoint(t, client, server.URL, "/api/admin/observability/migrations")
	assertObservabilityEndpoint(t, client, server.URL, "/api/admin/observability/queue")
	assertObservabilityEndpoint(t, client, server.URL, "/api/admin/observability/metrics")
}

func assertObservabilityEndpoint(
	t *testing.T,
	client *http.Client,
	baseURL string,
	path string,
) {
	t.Helper()

	response := requestWithResponse(t, client, http.MethodGet, baseURL+path, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected %s ok, got %d: %s", path, response.StatusCode, response.Body)
	}

	if response.Header.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("unexpected content type for %s: %q", path, response.Header.Get("Content-Type"))
	}
}

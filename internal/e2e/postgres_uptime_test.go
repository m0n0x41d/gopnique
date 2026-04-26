//go:build integration

package e2e

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	uptimeapp "github.com/ivanzakutnii/error-tracker/internal/app/uptime"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/runtime/worker"
)

func TestPostgresUptimeMonitorE2E(t *testing.T) {
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
	if len(migrationResult.Applied) != 30 {
		t.Fatalf("expected 30 migrations, got %d", len(migrationResult.Applied))
	}

	resolver := e2eResolver{
		"status.example.test": []netip.Addr{netip.MustParseAddr("93.184.216.34")},
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
		resolver,
		store,
		httpadapter.IngestEnrichments{},
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
		strings.NewReader("organization_name=Uptime&project_name=Uptime&email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if setup.StatusCode != http.StatusOK {
		t.Fatalf("expected setup ok, got %d: %s", setup.StatusCode, setup.Body)
	}

	before := request(t, client, http.MethodGet, server.URL+"/uptime", "", nil)
	if before.StatusCode != http.StatusOK {
		t.Fatalf("expected uptime page ok, got %d: %s", before.StatusCode, before.Body)
	}
	if !strings.Contains(before.Body, "Uptime monitors") || !strings.Contains(before.Body, "No monitors") {
		t.Fatalf("expected empty uptime page: %s", before.Body)
	}

	create := request(
		t,
		client,
		http.MethodPost,
		server.URL+"/uptime/monitors",
		"application/x-www-form-urlencoded",
		strings.NewReader("name=API+health&url=https%3A%2F%2Fstatus.example.test%2Fhealth&interval_seconds=60&timeout_seconds=5"),
	)
	if create.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected monitor create redirect, got %d: %s", create.StatusCode, create.Body)
	}

	afterCreate := request(t, client, http.MethodGet, server.URL+"/uptime", "", nil)
	if afterCreate.StatusCode != http.StatusOK {
		t.Fatalf("expected uptime page ok, got %d: %s", afterCreate.StatusCode, afterCreate.Body)
	}
	if !strings.Contains(afterCreate.Body, "API health") || !strings.Contains(afterCreate.Body, "unknown") {
		t.Fatalf("expected created monitor: %s", afterCreate.Body)
	}

	task := worker.NewUptimeTask(
		store,
		resolver,
		e2eUptimeProbe{statusCode: 500},
		worker.UptimeTaskConfig{BatchSize: 10},
	)
	taskErr := task.RunOnce(ctx)
	if taskErr != nil {
		t.Fatalf("uptime task: %v", taskErr)
	}

	afterCheck := request(t, client, http.MethodGet, server.URL+"/uptime", "", nil)
	if afterCheck.StatusCode != http.StatusOK {
		t.Fatalf("expected uptime page ok, got %d: %s", afterCheck.StatusCode, afterCheck.Body)
	}
	if !strings.Contains(afterCheck.Body, "down") || !strings.Contains(afterCheck.Body, "HTTP 500") {
		t.Fatalf("expected down monitor check: %s", afterCheck.Body)
	}

	createHeartbeat := request(
		t,
		client,
		http.MethodPost,
		server.URL+"/uptime/heartbeats",
		"application/x-www-form-urlencoded",
		strings.NewReader("name=Batch+heartbeat&interval_seconds=60&grace_seconds=60"),
	)
	if createHeartbeat.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected heartbeat create redirect, got %d: %s", createHeartbeat.StatusCode, createHeartbeat.Body)
	}

	afterHeartbeatCreate := request(t, client, http.MethodGet, server.URL+"/uptime", "", nil)
	if afterHeartbeatCreate.StatusCode != http.StatusOK {
		t.Fatalf("expected uptime page ok, got %d: %s", afterHeartbeatCreate.StatusCode, afterHeartbeatCreate.Body)
	}
	if !strings.Contains(afterHeartbeatCreate.Body, "Batch heartbeat") || !strings.Contains(afterHeartbeatCreate.Body, "/api/heartbeat/") {
		t.Fatalf("expected heartbeat monitor: %s", afterHeartbeatCreate.Body)
	}

	heartbeatEndpointID := findHeartbeatEndpoint(t, ctx, databaseURL)
	checkIn := request(t, client, http.MethodPost, server.URL+"/api/heartbeat/"+heartbeatEndpointID, "", nil)
	if checkIn.StatusCode != http.StatusAccepted {
		t.Fatalf("expected heartbeat accepted, got %d: %s", checkIn.StatusCode, checkIn.Body)
	}

	afterCheckIn := request(t, client, http.MethodGet, server.URL+"/uptime", "", nil)
	if afterCheckIn.StatusCode != http.StatusOK {
		t.Fatalf("expected uptime page ok, got %d: %s", afterCheckIn.StatusCode, afterCheckIn.Body)
	}
	if !strings.Contains(afterCheckIn.Body, "check-in") || !strings.Contains(afterCheckIn.Body, "up") {
		t.Fatalf("expected heartbeat check-in: %s", afterCheckIn.Body)
	}

	forceHeartbeatOverdue(t, ctx, databaseURL, heartbeatEndpointID)
	timeoutErr := task.RunOnce(ctx)
	if timeoutErr != nil {
		t.Fatalf("heartbeat timeout task: %v", timeoutErr)
	}

	afterTimeout := request(t, client, http.MethodGet, server.URL+"/uptime", "", nil)
	if afterTimeout.StatusCode != http.StatusOK {
		t.Fatalf("expected uptime page ok, got %d: %s", afterTimeout.StatusCode, afterTimeout.Body)
	}
	if !strings.Contains(afterTimeout.Body, "heartbeat overdue") {
		t.Fatalf("expected heartbeat timeout: %s", afterTimeout.Body)
	}

	assertUptimeStatusPages(t, ctx, databaseURL, client, server.URL)
	assertUptimeIncidentAndAudit(t, ctx, databaseURL, client, server.URL)
}

type e2eUptimeProbe struct {
	statusCode int
}

func (probe e2eUptimeProbe) Get(
	ctx context.Context,
	target outbound.DestinationURL,
	timeout time.Duration,
) result.Result[uptimeapp.HTTPProbeResult] {
	return uptimeapp.NewHTTPProbeResult(probe.statusCode, 15*time.Millisecond)
}

func assertUptimeIncidentAndAudit(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var checks int
	checkErr := pool.QueryRow(ctx, `select count(*) from uptime_monitor_checks`).Scan(&checks)
	if checkErr != nil {
		t.Fatalf("uptime checks: %v", checkErr)
	}
	if checks != 3 {
		t.Fatalf("expected three uptime checks, got %d", checks)
	}

	var incidents int
	incidentErr := pool.QueryRow(ctx, `select count(*) from uptime_monitor_incidents where resolved_at is null`).Scan(&incidents)
	if incidentErr != nil {
		t.Fatalf("uptime incidents: %v", incidentErr)
	}
	if incidents != 2 {
		t.Fatalf("expected two open uptime incidents, got %d", incidents)
	}

	var events int
	eventErr := pool.QueryRow(ctx, `select count(*) from events`).Scan(&events)
	if eventErr != nil {
		t.Fatalf("events: %v", eventErr)
	}
	if events != 0 {
		t.Fatalf("expected heartbeat flow to create no sentry events, got %d", events)
	}

	audit := request(t, client, http.MethodGet, baseURL+"/settings/audit", "", nil)
	if audit.StatusCode != http.StatusOK {
		t.Fatalf("expected audit ok, got %d: %s", audit.StatusCode, audit.Body)
	}
	if !strings.Contains(audit.Body, "monitor_created") || !strings.Contains(audit.Body, "monitor_state_changed") {
		t.Fatalf("expected monitor audit actions: %s", audit.Body)
	}

	if !strings.Contains(audit.Body, "status_page_created") {
		t.Fatalf("expected status page audit action: %s", audit.Body)
	}
}

func assertUptimeStatusPages(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
) {
	t.Helper()

	createPrivate := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/uptime/status-pages",
		"application/x-www-form-urlencoded",
		strings.NewReader("name=Internal+status&visibility=private"),
	)
	if createPrivate.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected private status page redirect, got %d: %s", createPrivate.StatusCode, createPrivate.Body)
	}

	createPublic := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/uptime/status-pages",
		"application/x-www-form-urlencoded",
		strings.NewReader("name=Public+status&visibility=public"),
	)
	if createPublic.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected public status page redirect, got %d: %s", createPublic.StatusCode, createPublic.Body)
	}

	uptime := request(t, client, http.MethodGet, baseURL+"/uptime", "", nil)
	if uptime.StatusCode != http.StatusOK {
		t.Fatalf("expected uptime with status pages ok, got %d: %s", uptime.StatusCode, uptime.Body)
	}
	if !strings.Contains(uptime.Body, "Internal status") || !strings.Contains(uptime.Body, "Public status") {
		t.Fatalf("expected status pages in uptime view: %s", uptime.Body)
	}

	privateID, publicToken := findStatusPages(t, ctx, databaseURL)
	privatePage := requestWithResponse(
		t,
		client,
		http.MethodGet,
		baseURL+"/status-pages/"+privateID,
		"",
		nil,
	)
	if privatePage.StatusCode != http.StatusOK {
		t.Fatalf("expected private status page ok, got %d: %s", privatePage.StatusCode, privatePage.Body)
	}
	if privatePage.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("expected private cache header, got %q", privatePage.Header.Get("Cache-Control"))
	}
	if !strings.Contains(privatePage.Body, "Internal status") || !strings.Contains(privatePage.Body, "API health") || !strings.Contains(privatePage.Body, "Batch heartbeat") {
		t.Fatalf("expected private status body: %s", privatePage.Body)
	}

	anonymous := newE2EClient(t)
	privateDenied := request(
		t,
		anonymous,
		http.MethodGet,
		baseURL+"/status-pages/"+privateID,
		"",
		nil,
	)
	if privateDenied.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected anonymous private redirect, got %d: %s", privateDenied.StatusCode, privateDenied.Body)
	}

	publicPage := requestWithResponse(
		t,
		anonymous,
		http.MethodGet,
		baseURL+"/status/"+publicToken,
		"",
		nil,
	)
	if publicPage.StatusCode != http.StatusOK {
		t.Fatalf("expected public status page ok, got %d: %s", publicPage.StatusCode, publicPage.Body)
	}
	if publicPage.Header.Get("Cache-Control") != "public, max-age=30" {
		t.Fatalf("expected public cache header, got %q", publicPage.Header.Get("Cache-Control"))
	}
	if !strings.Contains(publicPage.Body, "Public status") || !strings.Contains(publicPage.Body, "API health") || !strings.Contains(publicPage.Body, "active incident") {
		t.Fatalf("expected public status body: %s", publicPage.Body)
	}
	if strings.Contains(publicPage.Body, "/api/heartbeat/") || strings.Contains(publicPage.Body, "https://status.example.test/health") || strings.Contains(publicPage.Body, "heartbeat overdue") {
		t.Fatalf("public status page leaked private monitor detail: %s", publicPage.Body)
	}
}

func findHeartbeatEndpoint(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var endpointID string
	query := `
select heartbeat_endpoint_id
from uptime_monitors
where monitor_type = 'heartbeat'
limit 1
`
	scanErr := pool.QueryRow(ctx, query).Scan(&endpointID)
	if scanErr != nil {
		t.Fatalf("heartbeat endpoint: %v", scanErr)
	}

	return endpointID
}

func findStatusPages(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
) (string, string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var privateID string
	privateErr := pool.QueryRow(
		ctx,
		`select id::text from uptime_status_pages where visibility = 'private' limit 1`,
	).Scan(&privateID)
	if privateErr != nil {
		t.Fatalf("private status page: %v", privateErr)
	}

	var publicToken string
	publicErr := pool.QueryRow(
		ctx,
		`select public_token from uptime_status_pages where visibility = 'public' limit 1`,
	).Scan(&publicToken)
	if publicErr != nil {
		t.Fatalf("public status page: %v", publicErr)
	}

	return privateID, publicToken
}

func forceHeartbeatOverdue(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	endpointID string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	_, execErr := pool.Exec(
		ctx,
		`update uptime_monitors set next_check_at = $2 where heartbeat_endpoint_id = $1`,
		endpointID,
		time.Now().UTC().Add(-time.Second),
	)
	if execErr != nil {
		t.Fatalf("force heartbeat overdue: %v", execErr)
	}
}

type responseWithHeaders struct {
	StatusCode int
	Body       string
	Header     http.Header
}

func requestWithResponse(
	t *testing.T,
	client *http.Client,
	method string,
	target string,
	contentType string,
	body io.Reader,
) responseWithHeaders {
	t.Helper()

	req, reqErr := http.NewRequest(method, target, body)
	if reqErr != nil {
		t.Fatalf("request: %v", reqErr)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	res, resErr := client.Do(req)
	if resErr != nil {
		t.Fatalf("do request: %v", resErr)
	}
	defer res.Body.Close()

	responseBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}

	return responseWithHeaders{
		StatusCode: res.StatusCode,
		Body:       string(responseBody),
		Header:     res.Header,
	}
}

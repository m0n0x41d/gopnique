//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/telegram"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/webhook"
	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestPostgresM1M2E2E(t *testing.T) {
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
	if len(migrationResult.Applied) != 16 {
		t.Fatalf("expected 16 migrations, got %d", len(migrationResult.Applied))
	}

	publicURL := "http://example.test"
	resolver := e2eResolver{"hooks.example.test": []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
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
		resolver,
		store,
		httpadapter.AuthSettings{PublicURL: publicURL, SecretKey: "e2e-secret"},
	))
	defer server.Close()

	client := newE2EClient(t)
	beforeSetup := request(t, client, http.MethodGet, server.URL+"/issues", "", nil)
	if beforeSetup.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected setup redirect, got %d", beforeSetup.StatusCode)
	}

	setup := request(
		t,
		client,
		http.MethodPost,
		server.URL+"/setup",
		"application/x-www-form-urlencoded",
		strings.NewReader("organization_name=E2E&project_name=E2E&email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if setup.StatusCode != http.StatusOK {
		t.Fatalf("expected setup ok, got %d: %s", setup.StatusCode, setup.Body)
	}

	publicKey := projectPublicKey(t, ctx, databaseURL)
	projectPage := request(t, client, http.MethodGet, server.URL+"/projects", "", nil)
	if projectPage.StatusCode != http.StatusOK {
		t.Fatalf("expected projects ok, got %d", projectPage.StatusCode)
	}
	if !strings.Contains(projectPage.Body, "Projects &amp; DSN") {
		t.Fatalf("expected projects page: %s", projectPage.Body)
	}
	expectedDSN := "http://" + strings.ReplaceAll(publicKey, "-", "") + "@example.test/1"
	if !strings.Contains(projectPage.Body, expectedDSN) {
		t.Fatalf("expected project dsn %s in page: %s", expectedDSN, projectPage.Body)
	}
	if !strings.Contains(projectPage.Body, "/api/1/store/") || !strings.Contains(projectPage.Body, "/api/1/envelope/") {
		t.Fatalf("expected ingest endpoints in projects page: %s", projectPage.Body)
	}
	for _, expected := range []string{"@sentry/node", "@sentry/browser", "sentry-sdk", "sentry-go"} {
		if !strings.Contains(projectPage.Body, expected) {
			t.Fatalf("expected sdk snippet %q in projects page: %s", expected, projectPage.Body)
		}
	}

	settingsBefore := request(t, client, http.MethodGet, server.URL+"/settings/notifications", "", nil)
	if settingsBefore.StatusCode != http.StatusOK {
		t.Fatalf("expected notification settings ok, got %d", settingsBefore.StatusCode)
	}
	if !strings.Contains(settingsBefore.Body, "Alert channels") {
		t.Fatalf("expected notification settings page: %s", settingsBefore.Body)
	}
	assertMembersPage(t, client, server.URL)
	apiToken := createAPITokenThroughUI(t, client, server.URL)
	assertCurrentProjectAPI(t, client, server.URL, apiToken)
	revokeAPITokenThroughUI(t, ctx, databaseURL, client, server.URL, "ops-api")
	assertRevokedCurrentProjectAPI(t, client, server.URL, apiToken)

	destinationID := createTelegramDestinationThroughUI(t, ctx, databaseURL, client, server.URL)
	webhookServer, webhookURL := newWebhookFixtureServer(t)
	defer webhookServer.Close()
	webhookDestinationID := createWebhookDestinationThroughUI(t, ctx, databaseURL, client, server.URL, webhookURL)
	noAlertReceipt := postNoAlertStoreEvent(t, client, server.URL, publicKey)
	if !strings.Contains(noAlertReceipt.Body, `"id":"970e8400e29b41d4a716446655440000"`) {
		t.Fatalf("unexpected no-alert receipt: %s", noAlertReceipt.Body)
	}
	assertNoNotificationIntent(t, ctx, databaseURL, "970e8400-e29b-41d4-a716-446655440000")
	createIssueOpenedAlertThroughUI(t, client, server.URL, destinationID)
	createIssueOpenedWebhookAlertThroughUI(t, client, server.URL, webhookDestinationID)
	webhookRuleID := firstAlertIDByName(t, ctx, databaseURL, "Issue opened to Webhook")
	setIssueOpenedAlertThroughUI(t, client, server.URL, webhookRuleID, "disable")
	setIssueOpenedAlertThroughUI(t, client, server.URL, webhookRuleID, "enable")
	insertTenantSentinel(t, ctx, databaseURL)

	storeReceipt := postStoreEvent(t, client, server.URL, publicKey)
	if !strings.Contains(storeReceipt.Body, `"id":"980e8400e29b41d4a716446655440000"`) {
		t.Fatalf("unexpected store receipt: %s", storeReceipt.Body)
	}

	duplicate := postStoreEvent(t, client, server.URL, publicKey)
	if !strings.Contains(duplicate.Body, `"duplicate":true`) {
		t.Fatalf("expected duplicate receipt: %s", duplicate.Body)
	}

	envelopeDSN := postEnvelopeDSN(t, client, server.URL, publicKey)
	if envelopeDSN.StatusCode != http.StatusOK {
		t.Fatalf("expected envelope dsn ok, got %d: %s", envelopeDSN.StatusCode, envelopeDSN.Body)
	}

	conflict := postConflictingEnvelope(t, client, server.URL, publicKey)
	if conflict.StatusCode != http.StatusForbidden {
		t.Fatalf("expected auth conflict forbidden, got %d", conflict.StatusCode)
	}

	oversized := postOversizedStore(t, client, server.URL, publicKey)
	if oversized.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected store size limit, got %d", oversized.StatusCode)
	}

	issues := request(t, client, http.MethodGet, server.URL+"/issues", "", nil)
	if issues.StatusCode != http.StatusOK {
		t.Fatalf("expected issues ok, got %d", issues.StatusCode)
	}
	if !strings.Contains(issues.Body, "dimension persistence visible issue") {
		t.Fatalf("expected own issue in list: %s", issues.Body)
	}
	if !strings.Contains(issues.Body, "production") || !strings.Contains(issues.Body, "api@1.2.3") || !strings.Contains(issues.Body, "go") {
		t.Fatalf("expected issue list dimensions: %s", issues.Body)
	}
	if strings.Contains(issues.Body, "tenant leak sentinel") {
		t.Fatal("tenant sentinel leaked into scoped issue list")
	}

	issueID := issueIDForEvent(t, ctx, databaseURL, "980e8400-e29b-41d4-a716-446655440000")
	issueDetail := request(t, client, http.MethodGet, server.URL+"/issues/"+issueID, "", nil)
	if issueDetail.StatusCode != http.StatusOK {
		t.Fatalf("expected issue detail ok, got %d", issueDetail.StatusCode)
	}
	for _, expected := range []string{"region", "eu", "server_name", "api-1", "tier", "api", "Fingerprint"} {
		if !strings.Contains(issueDetail.Body, expected) {
			t.Fatalf("expected issue detail to contain %q: %s", expected, issueDetail.Body)
		}
	}

	eventDetail := request(
		t,
		client,
		http.MethodGet,
		server.URL+"/events/980e8400-e29b-41d4-a716-446655440000",
		"",
		nil,
	)
	if eventDetail.StatusCode != http.StatusOK {
		t.Fatalf("expected event detail ok, got %d", eventDetail.StatusCode)
	}
	for _, expected := range []string{"region", "eu", "server_name", "api-1", "tier", "api", "Canonical payload"} {
		if !strings.Contains(eventDetail.Body, expected) {
			t.Fatalf("expected event detail to contain %q: %s", expected, eventDetail.Body)
		}
	}
	if strings.Contains(eventDetail.Body, "client_ip") {
		t.Fatalf("client ip leaked into event detail: %s", eventDetail.Body)
	}

	postUserFeedbackThroughAPI(t, client, server.URL, publicKey)
	postLegacyUserReportEnvelope(t, client, server.URL, publicKey)
	postFeedbackEnvelope(t, client, server.URL, publicKey)
	assertUserReportVisible(t, client, server.URL, issueID)
	addIssueCommentThroughUI(t, client, server.URL, issueID, "E2E operator comment")
	assertIssueCommentVisible(t, client, server.URL, issueID, "E2E operator comment")
	teamID := assignIssueThroughUI(t, ctx, databaseURL, client, server.URL, issueID)
	assertIssueAssigneeVisible(t, client, server.URL, issueID, "Default team")
	assertIssueSearchFilters(t, client, server.URL, teamID)
	assertIssueSearchRejectsUnsupportedSyntax(t, client, server.URL)
	assertDimensionPages(t, client, server.URL)
	setIssueStatusThroughUI(t, client, server.URL, issueID, "resolved")
	assertIssueStatusDetail(t, client, server.URL, issueID, "resolved", "Reopen")
	assertIssueListDoesNotContain(t, client, server.URL, "unresolved", "dimension persistence visible issue")
	assertIssueListContains(t, client, server.URL, "resolved", "dimension persistence visible issue")
	setIssueStatusThroughUI(t, client, server.URL, issueID, "unresolved")
	setIssueStatusThroughUI(t, client, server.URL, issueID, "ignored")
	assertIssueListContains(t, client, server.URL, "ignored", "dimension persistence visible issue")
	setIssueStatusThroughUI(t, client, server.URL, issueID, "unresolved")
	assertIssueListContains(t, client, server.URL, "unresolved", "dimension persistence visible issue")
	assertAuditPage(t, client, server.URL)

	sentinel := request(
		t,
		client,
		http.MethodGet,
		server.URL+"/events/dddddddd-dddd-4ddd-dddd-dddddddddddd",
		"",
		nil,
	)
	if sentinel.StatusCode != http.StatusNotFound {
		t.Fatalf("expected tenant sentinel event 404, got %d", sentinel.StatusCode)
	}

	assertPersistedDimensions(t, ctx, databaseURL)
	deliverTelegram(t, ctx, store)
	assertTelegramDelivered(t, ctx, databaseURL)
	deliverWebhook(t, ctx, store, resolver, webhookURL)
	assertWebhookDelivered(t, ctx, databaseURL)
	assertDeliveryJournal(t, client, server.URL)
	setOperatorRoles(t, ctx, databaseURL, "operator@example.test", "member", "member")
	assertProjectMemberRoleGate(t, client, server.URL, issueID)
	setOperatorRoles(t, ctx, databaseURL, "operator@example.test", "owner", "owner")
	removeProjectMembership(t, ctx, databaseURL, "operator@example.test")
	assertRemovedProjectMembershipRejected(t, client, server.URL)
	restoreProjectMembership(t, ctx, databaseURL, "operator@example.test")
	disableOperator(t, ctx, databaseURL, "operator@example.test")
	assertDisabledOperatorSessionRejected(t, client, server.URL)
}

type responseSnapshot struct {
	StatusCode int
	Body       string
}

func newE2EClient(t *testing.T) *http.Client {
	t.Helper()

	jar, jarErr := cookiejar.New(nil)
	if jarErr != nil {
		t.Fatalf("cookie jar: %v", jarErr)
	}

	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func createTestDatabase(t *testing.T, ctx context.Context, adminURL string) string {
	t.Helper()

	name := fmt.Sprintf("error_tracker_e2e_%d", time.Now().UnixNano())
	adminPool, adminErr := pgxpool.New(ctx, adminURL)
	if adminErr != nil {
		t.Fatalf("admin pool: %v", adminErr)
	}
	defer adminPool.Close()

	_, createErr := adminPool.Exec(ctx, "create database "+name)
	if createErr != nil {
		t.Fatalf("create database: %v", createErr)
	}

	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "drop database if exists "+name+" with (force)")
	})

	return databaseURL(t, adminURL, name)
}

func databaseURL(t *testing.T, input string, database string) string {
	t.Helper()

	parsed, parseErr := url.Parse(input)
	if parseErr != nil {
		t.Fatalf("database url: %v", parseErr)
	}

	parsed.Path = "/" + database

	return parsed.String()
}

func request(
	t *testing.T,
	client *http.Client,
	method string,
	target string,
	contentType string,
	body io.Reader,
) responseSnapshot {
	t.Helper()

	return requestWithHeaders(t, client, method, target, contentType, body, map[string]string{})
}

func requestWithHeaders(
	t *testing.T,
	client *http.Client,
	method string,
	target string,
	contentType string,
	body io.Reader,
	headers map[string]string,
) responseSnapshot {
	t.Helper()

	req, reqErr := http.NewRequest(method, target, body)
	if reqErr != nil {
		t.Fatalf("request: %v", reqErr)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	for key, value := range headers {
		req.Header.Set(key, value)
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

	return responseSnapshot{
		StatusCode: res.StatusCode,
		Body:       string(responseBody),
	}
}

func createTelegramDestinationThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
) string {
	t.Helper()

	form := url.Values{}
	form.Set("label", "ops-telegram")
	form.Set("chat_id", "123456")
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/telegram-destinations",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected telegram destination htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, `id="notification-settings"`) {
		t.Fatalf("expected settings fragment: %s", response.Body)
	}
	if strings.Contains(response.Body, "<html") {
		t.Fatalf("expected htmx fragment, got full page: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-telegram") {
		t.Fatalf("expected created destination in fragment: %s", response.Body)
	}

	return firstTelegramDestinationID(t, ctx, databaseURL)
}

func assertMembersPage(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/settings/members", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected members ok, got %d: %s", response.StatusCode, response.Body)
	}

	for _, expected := range []string{"Members", "operator@example.test", "Default team", "manager", "owner", "admin"} {
		if !strings.Contains(response.Body, expected) {
			t.Fatalf("expected members page to contain %q: %s", expected, response.Body)
		}
	}
}

func createAPITokenThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
) string {
	t.Helper()

	missing := request(t, client, http.MethodGet, baseURL+"/api/v1/project", "", nil)
	if missing.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected missing api token unauthorized, got %d: %s", missing.StatusCode, missing.Body)
	}

	page := request(t, client, http.MethodGet, baseURL+"/settings/tokens", "", nil)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("expected api token settings ok, got %d: %s", page.StatusCode, page.Body)
	}
	if !strings.Contains(page.Body, "API tokens") {
		t.Fatalf("expected api token settings page: %s", page.Body)
	}

	form := url.Values{}
	form.Set("name", "ops-api")
	form.Set("scope", "project_read")
	response := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/tokens",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected api token create ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "ops-api") || !strings.Contains(response.Body, "project_read") {
		t.Fatalf("expected api token row in response: %s", response.Body)
	}

	token := apiTokenFromBody(t, response.Body)
	if !strings.Contains(response.Body, token[:12]) {
		t.Fatalf("expected token prefix in response: %s", response.Body)
	}

	return token
}

func apiTokenFromBody(t *testing.T, body string) string {
	t.Helper()

	pattern := regexp.MustCompile(`etp_[a-f0-9]{64}`)
	match := pattern.FindString(body)
	if match == "" {
		t.Fatalf("expected one-time api token in response: %s", body)
	}

	return match
}

func assertCurrentProjectAPI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	apiToken string,
) {
	t.Helper()

	response := requestWithHeaders(
		t,
		client,
		http.MethodGet,
		baseURL+"/api/v1/project",
		"",
		nil,
		map[string]string{"Authorization": "Bearer " + apiToken},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected current project api ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, `"organization_name":"E2E"`) ||
		!strings.Contains(response.Body, `"name":"E2E"`) ||
		!strings.Contains(response.Body, `"store_endpoint"`) {
		t.Fatalf("expected project api response: %s", response.Body)
	}
}

func revokeAPITokenThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
	name string,
) {
	t.Helper()

	tokenID := firstAPITokenIDByName(t, ctx, databaseURL, name)
	response := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/tokens/"+tokenID+"/revoke",
		"application/x-www-form-urlencoded",
		nil,
	)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected api token revoke redirect, got %d: %s", response.StatusCode, response.Body)
	}

	page := request(t, client, http.MethodGet, baseURL+"/settings/tokens", "", nil)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("expected api token settings after revoke ok, got %d: %s", page.StatusCode, page.Body)
	}
	if !strings.Contains(page.Body, "revoked") {
		t.Fatalf("expected revoked api token in settings: %s", page.Body)
	}
}

func assertRevokedCurrentProjectAPI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	apiToken string,
) {
	t.Helper()

	response := requestWithHeaders(
		t,
		client,
		http.MethodGet,
		baseURL+"/api/v1/project",
		"",
		nil,
		map[string]string{"Authorization": "Bearer " + apiToken},
	)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected revoked api token forbidden, got %d: %s", response.StatusCode, response.Body)
	}
}

func setIssueStatusThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	issueID string,
	status string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("status", status)
	response := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/issues/"+issueID+"/status",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected issue status redirect, got %d: %s", response.StatusCode, response.Body)
	}
}

func assertIssueStatusDetail(
	t *testing.T,
	client *http.Client,
	baseURL string,
	issueID string,
	status string,
	action string,
) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/issues/"+issueID, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected issue detail ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, status) || !strings.Contains(response.Body, action) {
		t.Fatalf("expected issue detail status %q action %q: %s", status, action, response.Body)
	}
}

func assertIssueListContains(
	t *testing.T,
	client *http.Client,
	baseURL string,
	status string,
	text string,
) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/issues?status="+status, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected %s issue list ok, got %d: %s", status, response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, text) {
		t.Fatalf("expected %s issue list to contain %q: %s", status, text, response.Body)
	}
}

func assertIssueListDoesNotContain(
	t *testing.T,
	client *http.Client,
	baseURL string,
	status string,
	text string,
) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/issues?status="+status, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected %s issue list ok, got %d: %s", status, response.StatusCode, response.Body)
	}
	if strings.Contains(response.Body, text) {
		t.Fatalf("expected %s issue list not to contain %q: %s", status, text, response.Body)
	}
}

func assertAuditPage(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/settings/audit", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected audit page ok, got %d: %s", response.StatusCode, response.Body)
	}

	for _, expected := range []string{
		"Audit trail",
		"bootstrap",
		"api_token_created",
		"api_token_revoked",
		"issue_assigned",
		"issue_comment_created",
		"issue_status_changed",
		"operator@example.test",
	} {
		if !strings.Contains(response.Body, expected) {
			t.Fatalf("expected audit page to contain %q: %s", expected, response.Body)
		}
	}
}

func addIssueCommentThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	issueID string,
	body string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("body", body)
	response := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/issues/"+issueID+"/comments",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected issue comment redirect, got %d: %s", response.StatusCode, response.Body)
	}
}

func assertIssueCommentVisible(
	t *testing.T,
	client *http.Client,
	baseURL string,
	issueID string,
	body string,
) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/issues/"+issueID, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected commented issue detail ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, body) || !strings.Contains(response.Body, "operator@example.test") {
		t.Fatalf("expected comment in issue detail: %s", response.Body)
	}
}

func assignIssueThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
	issueID string,
) string {
	t.Helper()

	teamID := firstDefaultTeamID(t, ctx, databaseURL)
	form := url.Values{}
	form.Set("assignee", "team:"+teamID)
	response := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/issues/"+issueID+"/assignment",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected issue assignment redirect, got %d: %s", response.StatusCode, response.Body)
	}

	return teamID
}

func assertIssueAssigneeVisible(
	t *testing.T,
	client *http.Client,
	baseURL string,
	issueID string,
	assignee string,
) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/issues/"+issueID, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected assigned issue detail ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, assignee) {
		t.Fatalf("expected assignee %q in issue detail: %s", assignee, response.Body)
	}
}

func assertIssueSearchFilters(
	t *testing.T,
	client *http.Client,
	baseURL string,
	teamID string,
) {
	t.Helper()

	query := url.Values{}
	query.Set("q", "is:unresolved environment:production release:api@1.2.3 level:error tag:region=eu assignee:team:"+teamID+" text:dimension")
	response := request(t, client, http.MethodGet, baseURL+"/issues?"+query.Encode(), "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected filtered issue list ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "dimension persistence visible issue") ||
		!strings.Contains(response.Body, "Default team") {
		t.Fatalf("expected filtered issue list to contain issue and assignee: %s", response.Body)
	}

	miss := url.Values{}
	miss.Set("q", "is:unresolved environment:staging")
	missResponse := request(t, client, http.MethodGet, baseURL+"/issues?"+miss.Encode(), "", nil)
	if missResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected missing filtered issue list ok, got %d: %s", missResponse.StatusCode, missResponse.Body)
	}
	if strings.Contains(missResponse.Body, "dimension persistence visible issue") {
		t.Fatalf("expected staging filter not to contain production issue: %s", missResponse.Body)
	}
}

func assertIssueSearchRejectsUnsupportedSyntax(
	t *testing.T,
	client *http.Client,
	baseURL string,
) {
	t.Helper()

	query := url.Values{}
	query.Set("q", "unknown:value")
	response := request(t, client, http.MethodGet, baseURL+"/issues?"+query.Encode(), "", nil)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid search bad request, got %d: %s", response.StatusCode, response.Body)
	}
}

func postUserFeedbackThroughAPI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) {
	t.Helper()

	body := strings.Join([]string{
		`{`,
		`"event_id":"980e8400e29b41d4a716446655440000",`,
		`"name":"Modal User",`,
		`"email":"modal@example.test",`,
		`"comments":"Crash modal says it broke"`,
		`}`,
	}, "")
	response := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/user-feedback/?sentry_key="+publicKey,
		"application/json",
		strings.NewReader(body),
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected user feedback ok, got %d: %s", response.StatusCode, response.Body)
	}
}

func postLegacyUserReportEnvelope(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) {
	t.Helper()

	envelope := strings.Join([]string{
		`{"event_id":"980e8400e29b41d4a716446655440000"}`,
		`{"type":"user_report"}`,
		`{"event_id":"980e8400e29b41d4a716446655440000","name":"Jane","email":"jane@example.test","comments":"Legacy report text"}`,
	}, "\n")
	response := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/envelope/?sentry_key="+publicKey,
		"application/x-sentry-envelope",
		strings.NewReader(envelope),
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected user report envelope ok, got %d: %s", response.StatusCode, response.Body)
	}
}

func postFeedbackEnvelope(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) {
	t.Helper()

	envelope := strings.Join([]string{
		`{"event_id":"990e8400e29b41d4a716446655440000"}`,
		`{"type":"feedback"}`,
		`{"event_id":"990e8400e29b41d4a716446655440000","contexts":{"feedback":{"associated_event_id":"980e8400e29b41d4a716446655440000","name":"John","contact_email":"john@example.test","message":"This error is annoying"}}}`,
	}, "\n")
	response := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/envelope/?sentry_key="+publicKey,
		"application/x-sentry-envelope",
		strings.NewReader(envelope),
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected feedback envelope ok, got %d: %s", response.StatusCode, response.Body)
	}
}

func assertUserReportVisible(
	t *testing.T,
	client *http.Client,
	baseURL string,
	issueID string,
) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/issues/"+issueID, "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected reported issue detail ok, got %d: %s", response.StatusCode, response.Body)
	}

	for _, expected := range []string{
		"User reports",
		"John",
		"john@example.test",
		"This error is annoying",
		"980e8400-e29b-41d4-a716-446655440000",
	} {
		if !strings.Contains(response.Body, expected) {
			t.Fatalf("expected user report detail to contain %q: %s", expected, response.Body)
		}
	}
}

func assertDimensionPages(
	t *testing.T,
	client *http.Client,
	baseURL string,
) {
	t.Helper()

	environments := request(t, client, http.MethodGet, baseURL+"/environments", "", nil)
	if environments.StatusCode != http.StatusOK {
		t.Fatalf("expected environments ok, got %d: %s", environments.StatusCode, environments.Body)
	}
	if !strings.Contains(environments.Body, "Environments") ||
		!strings.Contains(environments.Body, "production") ||
		!strings.Contains(environments.Body, "q=is%3Aunresolved+environment%3Aproduction") {
		t.Fatalf("expected environment dimension page: %s", environments.Body)
	}

	releases := request(t, client, http.MethodGet, baseURL+"/releases", "", nil)
	if releases.StatusCode != http.StatusOK {
		t.Fatalf("expected releases ok, got %d: %s", releases.StatusCode, releases.Body)
	}
	if !strings.Contains(releases.Body, "Releases") ||
		!strings.Contains(releases.Body, "api@1.2.3") ||
		!strings.Contains(releases.Body, "q=is%3Aunresolved+release%3Aapi%401.2.3") {
		t.Fatalf("expected release dimension page: %s", releases.Body)
	}
}

func createIssueOpenedAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	destinationID string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("destination", "telegram:"+destinationID)
	form.Set("name", "Issue opened to Telegram")
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/issue-opened-alerts",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected alert htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "Issue opened to Telegram") {
		t.Fatalf("expected created alert in fragment: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-telegram") {
		t.Fatalf("expected alert destination in fragment: %s", response.Body)
	}
}

func createWebhookDestinationThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
	webhookURL string,
) string {
	t.Helper()

	form := url.Values{}
	form.Set("label", "ops-webhook")
	form.Set("url", webhookURL)
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/webhook-destinations",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected webhook destination htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "ops-webhook") {
		t.Fatalf("expected webhook destination in fragment: %s", response.Body)
	}

	return firstWebhookDestinationID(t, ctx, databaseURL)
}

func createIssueOpenedWebhookAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	destinationID string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("destination", "webhook:"+destinationID)
	form.Set("name", "Issue opened to Webhook")
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/issue-opened-alerts",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected webhook alert htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "Issue opened to Webhook") {
		t.Fatalf("expected webhook alert in fragment: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-webhook") {
		t.Fatalf("expected webhook destination in fragment: %s", response.Body)
	}
}

func setIssueOpenedAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	ruleID string,
	action string,
) {
	t.Helper()

	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/issue-opened-alerts/"+ruleID+"/"+action,
		"application/x-www-form-urlencoded",
		nil,
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected alert %s htmx ok, got %d: %s", action, response.StatusCode, response.Body)
	}

	expected := "Issue-opened alert enabled"
	if action == "disable" {
		expected = "Issue-opened alert disabled"
	}

	if !strings.Contains(response.Body, expected) {
		t.Fatalf("expected alert status message %q: %s", expected, response.Body)
	}
}

func projectPublicKey(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var publicKey string
	scanErr := pool.QueryRow(ctx, "select public_key from project_keys order by created_at asc limit 1").Scan(&publicKey)
	if scanErr != nil {
		t.Fatalf("public key: %v", scanErr)
	}

	return publicKey
}

func firstTelegramDestinationID(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var destinationID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from telegram_destinations where label = 'ops-telegram'",
	).Scan(&destinationID)
	if scanErr != nil {
		t.Fatalf("telegram destination id: %v", scanErr)
	}

	return destinationID
}

func firstWebhookDestinationID(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var destinationID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from webhook_destinations where label = 'ops-webhook'",
	).Scan(&destinationID)
	if scanErr != nil {
		t.Fatalf("webhook destination id: %v", scanErr)
	}

	return destinationID
}

func firstAlertIDByName(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	name string,
) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var ruleID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from alert_rules where name = $1",
		name,
	).Scan(&ruleID)
	if scanErr != nil {
		t.Fatalf("alert rule id: %v", scanErr)
	}

	return ruleID
}

func firstAPITokenIDByName(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	name string,
) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var tokenID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from api_tokens where name = $1 order by created_at desc limit 1",
		name,
	).Scan(&tokenID)
	if scanErr != nil {
		t.Fatalf("api token id: %v", scanErr)
	}

	return tokenID
}

func firstDefaultTeamID(
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

	var teamID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from teams where slug = 'default' order by created_at asc limit 1",
	).Scan(&teamID)
	if scanErr != nil {
		t.Fatalf("default team id: %v", scanErr)
	}

	return teamID
}

func issueIDForEvent(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	eventID string,
) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
select i.id
from issues i
join events e on e.id = i.last_event_id
where e.event_id = $1
`
	var issueID string
	scanErr := pool.QueryRow(ctx, query, eventID).Scan(&issueID)
	if scanErr != nil {
		t.Fatalf("issue id for event: %v", scanErr)
	}

	return issueID
}

func addTelegramDestination(t *testing.T, ctx context.Context, store *postgres.Store) string {
	t.Helper()

	destination, destinationErr := store.AddTelegramDestination(ctx, postgres.TelegramDestinationInput{
		ProjectRef: "1",
		ChatID:     "123456",
		Label:      "ops-telegram",
	})
	if destinationErr != nil {
		t.Fatalf("telegram destination: %v", destinationErr)
	}

	return destination.DestinationID
}

func addIssueOpenedTelegramAlert(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	destinationID string,
) {
	t.Helper()

	_, alertErr := store.AddIssueOpenedTelegramAlert(ctx, postgres.IssueOpenedTelegramAlertInput{
		ProjectRef:    "1",
		DestinationID: destinationID,
		Name:          "Issue opened to Telegram",
	})
	if alertErr != nil {
		t.Fatalf("issue opened telegram alert: %v", alertErr)
	}
}

func postNoAlertStoreEvent(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	payload := map[string]any{
		"event_id":  "970e8400e29b41d4a716446655440000",
		"timestamp": "2026-04-24T12:59:00Z",
		"level":     "error",
		"message":   "destination alone should not notify",
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

func postStoreEvent(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	payload := map[string]any{
		"event_id":    "980e8400e29b41d4a716446655440000",
		"timestamp":   "2026-04-24T13:00:00Z",
		"level":       "error",
		"platform":    "go",
		"message":     "dimension persistence visible issue",
		"release":     "api@1.2.3",
		"environment": "production",
		"server_name": "api-1",
		"user":        map[string]any{"ip_address": "8.8.8.8"},
		"request":     map[string]any{"env": map[string]any{"REMOTE_ADDR": "1.1.1.1"}},
		"tags":        [][]string{{"region", "eu"}, {"tier", "api"}, {"client_ip", "9.9.9.9"}},
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

func postEnvelopeDSN(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	body := strings.Join([]string{
		fmt.Sprintf(`{"dsn":"http://%s@example.test/1","event_id":"990e8400e29b41d4a716446655440000"}`, publicKey),
		`{"type":"event"}`,
		`{"event_id":"990e8400e29b41d4a716446655440000","timestamp":"2026-04-24T13:01:00Z","level":"warning","message":"envelope dsn auth visible issue"}`,
	}, "\n")

	return request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/envelope/",
		"application/x-sentry-envelope",
		strings.NewReader(body),
	)
}

func postConflictingEnvelope(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	body := strings.Join([]string{
		`{"dsn":"http://660e8400e29b41d4a716446655440000@example.test/1","event_id":"991e8400e29b41d4a716446655440000"}`,
		`{"type":"event"}`,
		`{"event_id":"991e8400e29b41d4a716446655440000","timestamp":"2026-04-24T13:02:00Z","level":"warning","message":"must be rejected"}`,
	}, "\n")

	return request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/envelope/?sentry_key="+publicKey,
		"application/x-sentry-envelope",
		strings.NewReader(body),
	)
}

func postOversizedStore(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	payload := map[string]any{
		"event_id":  "992e8400e29b41d4a716446655440000",
		"timestamp": "2026-04-24T13:03:00Z",
		"level":     "error",
		"message":   strings.Repeat("x", 1024*1024+1),
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

func jsonBody(t *testing.T, payload any) []byte {
	t.Helper()

	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		t.Fatalf("json: %v", marshalErr)
	}

	return body
}

func insertTenantSentinel(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	sql := `
insert into organizations (id, slug, name, accepting_events, created_at)
values ('aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', 'tenant-sentinel', 'Tenant Sentinel', true, now());

insert into projects (id, organization_id, ingest_ref, slug, name, accepting_events, scrub_ip_addresses, next_issue_short_id, created_at)
values ('bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb', 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', 'tenant-sentinel', 'tenant-sentinel', 'Tenant Sentinel', true, true, 2, now());

insert into events (id, organization_id, project_id, event_id, kind, level, title, platform, occurred_at, received_at, fingerprint, canonical_payload)
values ('cccccccc-cccc-4ccc-cccc-cccccccccccc', 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', 'bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb', 'dddddddd-dddd-4ddd-dddd-dddddddddddd', 'default', 'error', 'tenant leak sentinel', 'other', now(), now(), 'tenant-leak-fingerprint', '{}');

insert into issues (id, organization_id, project_id, short_id, type, status, title, first_seen_at, last_seen_at, event_count, last_event_id, created_at)
values ('eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee', 'aaaaaaaa-aaaa-4aaa-aaaa-aaaaaaaaaaaa', 'bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb', 1, 'default', 'unresolved', 'tenant leak sentinel', now(), now(), 1, 'cccccccc-cccc-4ccc-cccc-cccccccccccc', now());

insert into issue_fingerprints (project_id, fingerprint, issue_id, created_at)
values ('bbbbbbbb-bbbb-4bbb-bbbb-bbbbbbbbbbbb', 'tenant-leak-fingerprint', 'eeeeeeee-eeee-4eee-eeee-eeeeeeeeeeee', now());
`
	_, execErr := pool.Exec(ctx, sql)
	if execErr != nil {
		t.Fatalf("tenant sentinel: %v", execErr)
	}
}

func assertPersistedDimensions(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
select
  (id <> event_id)::text,
  release,
  environment,
  canonical_payload->>'release',
  canonical_payload->>'environment',
  (
    select string_agg(key || '=' || value, ',' order by key)
    from event_tags
    where event_id = e.id
  ),
  (
    select count(*)
    from event_tags
    where event_id = e.id
      and key = 'client_ip'
  ),
  canonical_payload->'tags' ? 'client_ip'
from events e
where event_id = '980e8400-e29b-41d4-a716-446655440000'
`
	var separateIDs string
	var release string
	var environment string
	var payloadRelease string
	var payloadEnvironment string
	var tags string
	var clientIPTagCount int
	var payloadHasClientIP bool
	scanErr := pool.QueryRow(ctx, query).Scan(
		&separateIDs,
		&release,
		&environment,
		&payloadRelease,
		&payloadEnvironment,
		&tags,
		&clientIPTagCount,
		&payloadHasClientIP,
	)
	if scanErr != nil {
		t.Fatalf("dimensions: %v", scanErr)
	}

	if separateIDs != "true" {
		t.Fatal("expected internal event id to differ from SDK event_id")
	}

	if release != "api@1.2.3" || environment != "production" {
		t.Fatalf("unexpected dimensions: %s %s", release, environment)
	}

	if payloadRelease != release || payloadEnvironment != environment {
		t.Fatalf("unexpected canonical payload dimensions: %s %s", payloadRelease, payloadEnvironment)
	}

	if tags != "region=eu,server_name=api-1,tier=api" {
		t.Fatalf("unexpected tags: %s", tags)
	}

	if clientIPTagCount != 0 || payloadHasClientIP {
		t.Fatalf("expected client ip to be scrubbed, tag_count=%d payload_has=%t", clientIPTagCount, payloadHasClientIP)
	}
}

func assertNoNotificationIntent(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	eventID string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
select count(*)
from notification_intents ni
join events e on e.id = ni.event_id
where e.event_id = $1
`
	var count int
	scanErr := pool.QueryRow(ctx, query, eventID).Scan(&count)
	if scanErr != nil {
		t.Fatalf("notification intent count: %v", scanErr)
	}

	if count != 0 {
		t.Fatalf("expected no notification intent for %s, got %d", eventID, count)
	}
}

func deliverTelegram(t *testing.T, ctx context.Context, store *postgres.Store) {
	t.Helper()

	fakeTelegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bote2e-token/sendMessage" {
			t.Fatalf("unexpected telegram path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":901}}`))
	}))
	defer fakeTelegram.Close()

	sender, senderErr := telegram.NewSender(fakeTelegram.Client(), fakeTelegram.URL, "e2e-token")
	if senderErr != nil {
		t.Fatalf("telegram sender: %v", senderErr)
	}

	commandResult := notifications.NewTelegramBatchCommand(time.Now().UTC(), 5, "http://example.test")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("telegram command: %v", commandErr)
	}

	batchResult := notifications.DeliverTelegramBatch(ctx, command, store, sender)
	receipt, batchErr := batchResult.Value()
	if batchErr != nil {
		t.Fatalf("telegram batch: %v", batchErr)
	}

	if receipt.Claimed() < 1 || receipt.Delivered() < 1 {
		t.Fatalf("unexpected telegram receipt: claimed=%d delivered=%d failed=%d", receipt.Claimed(), receipt.Delivered(), receipt.Failed())
	}
}

func deliverWebhook(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	resolver e2eResolver,
	webhookURL string,
) {
	t.Helper()

	client := &http.Client{Transport: rewriteHookTransport(t, webhookURL)}
	sender := webhook.NewSender(client)
	commandResult := notifications.NewWebhookBatchCommand(time.Now().UTC(), 5, "http://example.test")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("webhook command: %v", commandErr)
	}

	batchResult := notifications.DeliverWebhookBatch(ctx, command, resolver, store, sender)
	receipt, batchErr := batchResult.Value()
	if batchErr != nil {
		t.Fatalf("webhook batch: %v", batchErr)
	}

	if receipt.Claimed() < 1 || receipt.Delivered() < 1 {
		t.Fatalf("unexpected webhook receipt: claimed=%d delivered=%d failed=%d", receipt.Claimed(), receipt.Delivered(), receipt.Failed())
	}
}

func assertTelegramDelivered(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var status string
	var providerMessageID string
	var attempts int
	query := `
select ni.status, ni.provider_message_id, ni.attempts
from notification_intents ni
join events e on e.id = ni.event_id
where e.event_id = '980e8400-e29b-41d4-a716-446655440000'
  and ni.provider = 'telegram'
`
	scanErr := pool.QueryRow(ctx, query).Scan(&status, &providerMessageID, &attempts)
	if scanErr != nil {
		t.Fatalf("telegram intent: %v", scanErr)
	}

	if status != "delivered" || providerMessageID != "901" || attempts != 1 {
		t.Fatalf("unexpected telegram intent: %s %s %d", status, providerMessageID, attempts)
	}
}

func assertWebhookDelivered(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var status string
	var providerStatusCode int
	var attempts int
	query := `
select ni.status, ni.provider_status_code, ni.attempts
from notification_intents ni
join events e on e.id = ni.event_id
where e.event_id = '980e8400-e29b-41d4-a716-446655440000'
  and ni.provider = 'webhook'
`
	scanErr := pool.QueryRow(ctx, query).Scan(&status, &providerStatusCode, &attempts)
	if scanErr != nil {
		t.Fatalf("webhook intent: %v", scanErr)
	}

	if status != "delivered" || providerStatusCode != http.StatusNoContent || attempts != 1 {
		t.Fatalf("unexpected webhook intent: %s %d %d", status, providerStatusCode, attempts)
	}
}

func assertDeliveryJournal(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()

	settings := request(t, client, http.MethodGet, baseURL+"/settings/notifications", "", nil)
	if settings.StatusCode != http.StatusOK {
		t.Fatalf("expected notification settings ok, got %d", settings.StatusCode)
	}

	for _, expected := range []string{"Delivery journal", "telegram", "webhook", "delivered", "204"} {
		if !strings.Contains(settings.Body, expected) {
			t.Fatalf("expected delivery journal to contain %q: %s", expected, settings.Body)
		}
	}
}

func setOperatorRoles(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	email string,
	organizationRole string,
	projectRole string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	orgQuery := `
update operator_organizations oo
set role = $2
from operators o
where oo.operator_id = o.id
  and o.email = $1
`
	orgTag, orgErr := pool.Exec(ctx, orgQuery, email, organizationRole)
	if orgErr != nil {
		t.Fatalf("set organization role: %v", orgErr)
	}
	if orgTag.RowsAffected() != 1 {
		t.Fatalf("expected one organization role update, got %d", orgTag.RowsAffected())
	}

	projectQuery := `
update project_memberships pm
set role = $2
from operators o
where pm.operator_id = o.id
  and o.email = $1
`
	projectTag, projectErr := pool.Exec(ctx, projectQuery, email, projectRole)
	if projectErr != nil {
		t.Fatalf("set project role: %v", projectErr)
	}
	if projectTag.RowsAffected() != 1 {
		t.Fatalf("expected one project role update, got %d", projectTag.RowsAffected())
	}
}

func assertProjectMemberRoleGate(
	t *testing.T,
	client *http.Client,
	baseURL string,
	issueID string,
) {
	t.Helper()

	issues := request(t, client, http.MethodGet, baseURL+"/issues", "", nil)
	if issues.StatusCode != http.StatusOK {
		t.Fatalf("expected project member issues read ok, got %d: %s", issues.StatusCode, issues.Body)
	}

	projects := request(t, client, http.MethodGet, baseURL+"/projects", "", nil)
	if projects.StatusCode != http.StatusOK {
		t.Fatalf("expected project member project read ok, got %d: %s", projects.StatusCode, projects.Body)
	}

	environments := request(t, client, http.MethodGet, baseURL+"/environments", "", nil)
	if environments.StatusCode != http.StatusOK {
		t.Fatalf("expected project member environments read ok, got %d: %s", environments.StatusCode, environments.Body)
	}

	releases := request(t, client, http.MethodGet, baseURL+"/releases", "", nil)
	if releases.StatusCode != http.StatusOK {
		t.Fatalf("expected project member releases read ok, got %d: %s", releases.StatusCode, releases.Body)
	}

	settings := request(t, client, http.MethodGet, baseURL+"/settings/notifications", "", nil)
	if settings.StatusCode != http.StatusForbidden {
		t.Fatalf("expected project member settings forbidden, got %d: %s", settings.StatusCode, settings.Body)
	}

	members := request(t, client, http.MethodGet, baseURL+"/settings/members", "", nil)
	if members.StatusCode != http.StatusForbidden {
		t.Fatalf("expected project member members forbidden, got %d: %s", members.StatusCode, members.Body)
	}

	tokens := request(t, client, http.MethodGet, baseURL+"/settings/tokens", "", nil)
	if tokens.StatusCode != http.StatusForbidden {
		t.Fatalf("expected project member tokens forbidden, got %d: %s", tokens.StatusCode, tokens.Body)
	}

	audit := request(t, client, http.MethodGet, baseURL+"/settings/audit", "", nil)
	if audit.StatusCode != http.StatusForbidden {
		t.Fatalf("expected project member audit forbidden, got %d: %s", audit.StatusCode, audit.Body)
	}

	form := url.Values{}
	form.Set("status", "resolved")
	triage := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/issues/"+issueID+"/status",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if triage.StatusCode != http.StatusForbidden {
		t.Fatalf("expected project member triage forbidden, got %d: %s", triage.StatusCode, triage.Body)
	}

	commentForm := url.Values{}
	commentForm.Set("body", "member comment")
	comment := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/issues/"+issueID+"/comments",
		"application/x-www-form-urlencoded",
		strings.NewReader(commentForm.Encode()),
	)
	if comment.StatusCode != http.StatusForbidden {
		t.Fatalf("expected project member comment forbidden, got %d: %s", comment.StatusCode, comment.Body)
	}

	assignForm := url.Values{}
	assignForm.Set("assignee", "none")
	assignment := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/issues/"+issueID+"/assignment",
		"application/x-www-form-urlencoded",
		strings.NewReader(assignForm.Encode()),
	)
	if assignment.StatusCode != http.StatusForbidden {
		t.Fatalf("expected project member assignment forbidden, got %d: %s", assignment.StatusCode, assignment.Body)
	}
}

func disableOperator(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	email string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	tag, execErr := pool.Exec(ctx, `update operators set active = false where email = $1`, email)
	if execErr != nil {
		t.Fatalf("disable operator: %v", execErr)
	}

	if tag.RowsAffected() != 1 {
		t.Fatalf("expected one disabled operator, got %d", tag.RowsAffected())
	}
}

func removeProjectMembership(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	email string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
delete from project_memberships pm
using operators o
where pm.operator_id = o.id
  and o.email = $1
`
	tag, execErr := pool.Exec(ctx, query, email)
	if execErr != nil {
		t.Fatalf("remove project membership: %v", execErr)
	}

	if tag.RowsAffected() != 1 {
		t.Fatalf("expected one removed project membership, got %d", tag.RowsAffected())
	}
}

func restoreProjectMembership(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	email string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
insert into project_memberships (
  operator_id,
  organization_id,
  project_id,
  role,
  created_at
)
select o.id, p.organization_id, p.id, 'owner', now()
from operators o
join operator_organizations oo on oo.operator_id = o.id
join projects p on p.organization_id = oo.organization_id
where o.email = $1
on conflict (operator_id, project_id) do update
set role = excluded.role
`
	tag, execErr := pool.Exec(ctx, query, email)
	if execErr != nil {
		t.Fatalf("restore project membership: %v", execErr)
	}

	if tag.RowsAffected() != 1 {
		t.Fatalf("expected one restored project membership, got %d", tag.RowsAffected())
	}
}

func assertRemovedProjectMembershipRejected(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/issues", "", nil)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected removed membership redirect, got %d: %s", response.StatusCode, response.Body)
	}
}

func assertDisabledOperatorSessionRejected(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/issues", "", nil)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected disabled operator redirect, got %d: %s", response.StatusCode, response.Body)
	}
}

func newWebhookFixtureServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hook" {
			t.Fatalf("unexpected webhook path: %s", r.URL.Path)
		}

		var payload map[string]any
		decodeErr := json.NewDecoder(r.Body).Decode(&payload)
		if decodeErr != nil {
			t.Fatalf("decode webhook payload: %v", decodeErr)
		}

		if payload["event_id"] == "" {
			t.Fatalf("unexpected webhook payload: %#v", payload)
		}

		w.WriteHeader(http.StatusNoContent)
	}))

	parsed, parseErr := url.Parse(server.URL)
	if parseErr != nil {
		t.Fatalf("parse webhook server url: %v", parseErr)
	}

	return server, parsed.Scheme + "://hooks.example.test:" + parsed.Port() + "/hook"
}

type e2eResolver map[string][]netip.Addr

func (resolver e2eResolver) LookupHost(
	ctx context.Context,
	host string,
) result.Result[[]netip.Addr] {
	addresses, ok := resolver[host]
	if !ok {
		return result.Err[[]netip.Addr](fmt.Errorf("host not found: %s", host))
	}

	return result.Ok(addresses)
}

func rewriteHookTransport(t *testing.T, webhookURL string) http.RoundTripper {
	t.Helper()

	parsed, parseErr := url.Parse(webhookURL)
	if parseErr != nil {
		t.Fatalf("parse webhook url: %v", parseErr)
	}

	targetAddress := net.JoinHostPort("127.0.0.1", parsed.Port())
	dialer := &net.Dialer{Timeout: 5 * time.Second}

	return &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			if address == parsed.Host {
				return dialer.DialContext(ctx, network, targetAddress)
			}

			return dialer.DialContext(ctx, network, address)
		},
	}
}

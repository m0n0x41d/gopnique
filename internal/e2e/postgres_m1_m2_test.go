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

	"github.com/ivanzakutnii/error-tracker/internal/adapters/discord"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/googlechat"
	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/ntfy"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/teams"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/telegram"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/webhook"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/zulip"
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
	if len(migrationResult.Applied) != 33 {
		t.Fatalf("expected 33 migrations, got %d", len(migrationResult.Applied))
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
		store,
		store,
		store,
		store,
		resolver,
		store,
		httpadapter.IngestEnrichments{},
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
	if !strings.Contains(settingsBefore.Body, "Data retention") || !strings.Contains(settingsBefore.Body, "90d") {
		t.Fatalf("expected retention policy on settings page: %s", settingsBefore.Body)
	}
	if !strings.Contains(settingsBefore.Body, "Quota policy") || !strings.Contains(settingsBefore.Body, "daily events") {
		t.Fatalf("expected quota policy on settings page: %s", settingsBefore.Body)
	}
	if !strings.Contains(settingsBefore.Body, "Rate limit") || !strings.Contains(settingsBefore.Body, "active ingest key") {
		t.Fatalf("expected rate limit policy on settings page: %s", settingsBefore.Body)
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
	emailDestinationID := createEmailDestinationThroughUI(t, ctx, databaseURL, client, server.URL)
	discordServer, discordURL := newDiscordFixtureServer(t)
	defer discordServer.Close()
	discordDestinationID := createDiscordDestinationThroughUI(t, ctx, databaseURL, client, server.URL, discordURL)
	googleChatServer, googleChatURL := newGoogleChatFixtureServer(t)
	defer googleChatServer.Close()
	googleChatDestinationID := createGoogleChatDestinationThroughUI(t, ctx, databaseURL, client, server.URL, googleChatURL)
	ntfyServer, ntfyURL := newNtfyFixtureServer(t)
	defer ntfyServer.Close()
	ntfyDestinationID := createNtfyDestinationThroughUI(t, ctx, databaseURL, client, server.URL, ntfyURL)
	teamsServer, teamsURL := newTeamsFixtureServer(t)
	defer teamsServer.Close()
	teamsDestinationID := createTeamsDestinationThroughUI(t, ctx, databaseURL, client, server.URL, teamsURL)
	zulipServer, zulipURL := newZulipFixtureServer(t)
	defer zulipServer.Close()
	zulipDestinationID := createZulipDestinationThroughUI(t, ctx, databaseURL, client, server.URL, zulipURL)
	noAlertReceipt := postNoAlertStoreEvent(t, client, server.URL, publicKey)
	if !strings.Contains(noAlertReceipt.Body, `"id":"970e8400e29b41d4a716446655440000"`) {
		t.Fatalf("unexpected no-alert receipt: %s", noAlertReceipt.Body)
	}
	assertNoNotificationIntent(t, ctx, databaseURL, "970e8400-e29b-41d4-a716-446655440000")
	createIssueOpenedAlertThroughUI(t, client, server.URL, destinationID)
	createIssueOpenedWebhookAlertThroughUI(t, client, server.URL, webhookDestinationID)
	createIssueOpenedEmailAlertThroughUI(t, client, server.URL, emailDestinationID)
	createIssueOpenedDiscordAlertThroughUI(t, client, server.URL, discordDestinationID)
	createIssueOpenedGoogleChatAlertThroughUI(t, client, server.URL, googleChatDestinationID)
	createIssueOpenedNtfyAlertThroughUI(t, client, server.URL, ntfyDestinationID)
	createIssueOpenedTeamsAlertThroughUI(t, client, server.URL, teamsDestinationID)
	createIssueOpenedZulipAlertThroughUI(t, client, server.URL, zulipDestinationID)
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

	transactionReceipt := postTransactionEnvelope(t, client, server.URL, publicKey)
	if transactionReceipt.StatusCode != http.StatusOK {
		t.Fatalf("expected transaction envelope ok, got %d: %s", transactionReceipt.StatusCode, transactionReceipt.Body)
	}
	if !strings.Contains(transactionReceipt.Body, `"id":"910e8400e29b41d4a716446655440000"`) {
		t.Fatalf("unexpected transaction receipt: %s", transactionReceipt.Body)
	}

	logReceipt := postLogEnvelope(t, client, server.URL, publicKey)
	if logReceipt.StatusCode != http.StatusOK {
		t.Fatalf("expected log envelope ok, got %d: %s", logReceipt.StatusCode, logReceipt.Body)
	}
	if !strings.Contains(logReceipt.Body, `"accepted":1`) {
		t.Fatalf("unexpected log receipt: %s", logReceipt.Body)
	}

	otelLogReceipt := postOTelLogEnvelope(t, client, server.URL, publicKey)
	if otelLogReceipt.StatusCode != http.StatusOK {
		t.Fatalf("expected otel log envelope ok, got %d: %s", otelLogReceipt.StatusCode, otelLogReceipt.Body)
	}
	if !strings.Contains(otelLogReceipt.Body, `"accepted":1`) {
		t.Fatalf("unexpected otel log receipt: %s", otelLogReceipt.Body)
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
	if strings.Contains(issues.Body, "GET /checkout") {
		t.Fatalf("transaction leaked into issue list: %s", issues.Body)
	}
	if strings.Contains(issues.Body, "checkout log failed") || strings.Contains(issues.Body, "otel checkout failed") {
		t.Fatalf("log leaked into issue list: %s", issues.Body)
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
	assertProjectStatsPage(t, client, server.URL)
	assertPerformancePages(t, ctx, databaseURL, client, server.URL)
	assertLogPages(t, ctx, databaseURL, client, server.URL)
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
	assertPersistedLogs(t, ctx, databaseURL)
	deliverTelegram(t, ctx, store)
	assertTelegramDelivered(t, ctx, databaseURL)
	deliverWebhook(t, ctx, store, resolver, webhookURL)
	assertWebhookDelivered(t, ctx, databaseURL)
	deliverEmail(t, ctx, store)
	assertEmailDelivered(t, ctx, databaseURL)
	deliverDiscord(t, ctx, store, resolver, discordURL)
	assertDiscordDelivered(t, ctx, databaseURL)
	deliverGoogleChat(t, ctx, store, resolver, googleChatURL)
	assertGoogleChatDelivered(t, ctx, databaseURL)
	deliverNtfy(t, ctx, store, resolver, ntfyURL)
	assertNtfyDelivered(t, ctx, databaseURL)
	deliverTeams(t, ctx, store, resolver, teamsURL)
	assertTeamsDelivered(t, ctx, databaseURL)
	deliverZulip(t, ctx, store, resolver, zulipURL)
	assertZulipDelivered(t, ctx, databaseURL)
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

func assertProjectStatsPage(
	t *testing.T,
	client *http.Client,
	baseURL string,
) {
	t.Helper()

	response := request(t, client, http.MethodGet, baseURL+"/stats?period=24h", "", nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected stats ok, got %d: %s", response.StatusCode, response.Body)
	}

	for _, expected := range []string{
		"Stats",
		"Project telemetry",
		"Events",
		"issue 3",
		"transaction 1",
		"1<span class=\"unit\">reports</span>",
		"Trend by hour",
	} {
		if !strings.Contains(response.Body, expected) {
			t.Fatalf("expected stats page to contain %q: %s", expected, response.Body)
		}
	}

	daily := request(t, client, http.MethodGet, baseURL+"/stats?period=14d", "", nil)
	if daily.StatusCode != http.StatusOK {
		t.Fatalf("expected daily stats ok, got %d: %s", daily.StatusCode, daily.Body)
	}
	if !strings.Contains(daily.Body, "Trend by day") {
		t.Fatalf("expected daily stats page: %s", daily.Body)
	}
}

func assertPerformancePages(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
) {
	t.Helper()

	list := request(t, client, http.MethodGet, baseURL+"/performance", "", nil)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("expected performance ok, got %d: %s", list.StatusCode, list.Body)
	}

	for _, expected := range []string{
		"Transactions",
		"GET /checkout",
		"http.server",
		"1.50s",
		"ok",
	} {
		if !strings.Contains(list.Body, expected) {
			t.Fatalf("expected performance list to contain %q: %s", expected, list.Body)
		}
	}

	groupID := transactionGroupIDForEvent(t, ctx, databaseURL, "910e8400-e29b-41d4-a716-446655440000")
	detail := request(t, client, http.MethodGet, baseURL+"/performance/"+groupID, "", nil)
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("expected performance detail ok, got %d: %s", detail.StatusCode, detail.Body)
	}

	for _, expected := range []string{
		"Recent events",
		"910e8400-e29b-41d4-a716-446655440000",
		"0123456789abcdef0123456789abcdef",
		"1111111111111111",
		"1<span class=\"unit\">events</span>",
	} {
		if !strings.Contains(detail.Body, expected) {
			t.Fatalf("expected performance detail to contain %q: %s", expected, detail.Body)
		}
	}
}

func assertLogPages(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
) {
	t.Helper()

	list := request(t, client, http.MethodGet, baseURL+"/logs", "", nil)
	if list.StatusCode != http.StatusOK {
		t.Fatalf("expected logs ok, got %d: %s", list.StatusCode, list.Body)
	}

	for _, expected := range []string{
		"Log records",
		"checkout log failed",
		"checkout.web",
		"production",
		"web@1.2.3",
		"otel checkout failed",
		"checkout.otel",
	} {
		if !strings.Contains(list.Body, expected) {
			t.Fatalf("expected log list to contain %q: %s", expected, list.Body)
		}
	}

	filtered := request(
		t,
		client,
		http.MethodGet,
		baseURL+"/logs?severity=warning&resource_key=service.name&resource_value=checkout-api",
		"",
		nil,
	)
	if filtered.StatusCode != http.StatusOK {
		t.Fatalf("expected filtered logs ok, got %d: %s", filtered.StatusCode, filtered.Body)
	}
	if !strings.Contains(filtered.Body, "otel checkout failed") || strings.Contains(filtered.Body, "checkout log failed") {
		t.Fatalf("unexpected filtered logs: %s", filtered.Body)
	}

	logID := logIDForBody(t, ctx, databaseURL, "checkout log failed")
	detail := request(t, client, http.MethodGet, baseURL+"/logs/"+logID, "", nil)
	if detail.StatusCode != http.StatusOK {
		t.Fatalf("expected log detail ok, got %d: %s", detail.StatusCode, detail.Body)
	}

	for _, expected := range []string{
		"Resource attributes",
		"service.name",
		"checkout",
		"Log attributes",
		"http.route",
		"/checkout",
		"0123456789ab",
	} {
		if !strings.Contains(detail.Body, expected) {
			t.Fatalf("expected log detail to contain %q: %s", expected, detail.Body)
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

func createEmailDestinationThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
) string {
	t.Helper()

	form := url.Values{}
	form.Set("label", "ops-email")
	form.Set("address", "ops@example.test")
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/email-destinations",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected email destination htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "ops-email") {
		t.Fatalf("expected email destination in fragment: %s", response.Body)
	}

	return firstEmailDestinationID(t, ctx, databaseURL)
}

func createIssueOpenedEmailAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	destinationID string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("destination", "email:"+destinationID)
	form.Set("name", "Issue opened to Email")
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
		t.Fatalf("expected email alert htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "Issue opened to Email") {
		t.Fatalf("expected email alert in fragment: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-email") {
		t.Fatalf("expected email destination in fragment: %s", response.Body)
	}
}

func createDiscordDestinationThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
	discordURL string,
) string {
	t.Helper()

	form := url.Values{}
	form.Set("label", "ops-discord")
	form.Set("url", discordURL)
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/discord-destinations",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected discord destination htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "ops-discord") {
		t.Fatalf("expected discord destination in fragment: %s", response.Body)
	}

	return firstDiscordDestinationID(t, ctx, databaseURL)
}

func createIssueOpenedDiscordAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	destinationID string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("destination", "discord:"+destinationID)
	form.Set("name", "Issue opened to Discord")
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
		t.Fatalf("expected discord alert htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "Issue opened to Discord") {
		t.Fatalf("expected discord alert in fragment: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-discord") {
		t.Fatalf("expected discord destination in fragment: %s", response.Body)
	}
}

func createGoogleChatDestinationThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
	googleChatURL string,
) string {
	t.Helper()

	form := url.Values{}
	form.Set("label", "ops-google-chat")
	form.Set("url", googleChatURL)
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/google-chat-destinations",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected google chat destination htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "ops-google-chat") {
		t.Fatalf("expected google chat destination in fragment: %s", response.Body)
	}

	return firstGoogleChatDestinationID(t, ctx, databaseURL)
}

func createIssueOpenedGoogleChatAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	destinationID string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("destination", "google_chat:"+destinationID)
	form.Set("name", "Issue opened to Google Chat")
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
		t.Fatalf("expected google chat alert htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "Issue opened to Google Chat") {
		t.Fatalf("expected google chat alert in fragment: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-google-chat") {
		t.Fatalf("expected google chat destination in fragment: %s", response.Body)
	}
}

func createNtfyDestinationThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
	ntfyURL string,
) string {
	t.Helper()

	form := url.Values{}
	form.Set("label", "ops-ntfy")
	form.Set("url", ntfyURL)
	form.Set("topic", "ops-alerts")
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/ntfy-destinations",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected ntfy destination htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "ops-ntfy") {
		t.Fatalf("expected ntfy destination in fragment: %s", response.Body)
	}

	return firstNtfyDestinationID(t, ctx, databaseURL)
}

func createIssueOpenedNtfyAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	destinationID string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("destination", "ntfy:"+destinationID)
	form.Set("name", "Issue opened to ntfy")
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
		t.Fatalf("expected ntfy alert htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "Issue opened to ntfy") {
		t.Fatalf("expected ntfy alert in fragment: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-ntfy") {
		t.Fatalf("expected ntfy destination in fragment: %s", response.Body)
	}
}

func createTeamsDestinationThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
	teamsURL string,
) string {
	t.Helper()

	form := url.Values{}
	form.Set("label", "ops-teams")
	form.Set("url", teamsURL)
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/teams-destinations",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected microsoft teams destination htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "ops-teams") {
		t.Fatalf("expected microsoft teams destination in fragment: %s", response.Body)
	}

	return firstTeamsDestinationID(t, ctx, databaseURL)
}

func createIssueOpenedTeamsAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	destinationID string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("destination", "microsoft_teams:"+destinationID)
	form.Set("name", "Issue opened to Microsoft Teams")
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
		t.Fatalf("expected microsoft teams alert htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "Issue opened to Microsoft Teams") {
		t.Fatalf("expected microsoft teams alert in fragment: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-teams") {
		t.Fatalf("expected microsoft teams destination in fragment: %s", response.Body)
	}
}

func createZulipDestinationThroughUI(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	client *http.Client,
	baseURL string,
	zulipURL string,
) string {
	t.Helper()

	form := url.Values{}
	form.Set("label", "ops-zulip")
	form.Set("url", zulipURL)
	form.Set("bot_email", "bot@example.test")
	form.Set("api_key", "zulip-key")
	form.Set("stream", "ops")
	form.Set("topic", "alerts")
	response := requestWithHeaders(
		t,
		client,
		http.MethodPost,
		baseURL+"/settings/notifications/zulip-destinations",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
		map[string]string{"HX-Request": "true"},
	)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected zulip destination htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "ops-zulip") {
		t.Fatalf("expected zulip destination in fragment: %s", response.Body)
	}

	return firstZulipDestinationID(t, ctx, databaseURL)
}

func createIssueOpenedZulipAlertThroughUI(
	t *testing.T,
	client *http.Client,
	baseURL string,
	destinationID string,
) {
	t.Helper()

	form := url.Values{}
	form.Set("destination", "zulip:"+destinationID)
	form.Set("name", "Issue opened to Zulip")
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
		t.Fatalf("expected zulip alert htmx ok, got %d: %s", response.StatusCode, response.Body)
	}
	if !strings.Contains(response.Body, "Issue opened to Zulip") {
		t.Fatalf("expected zulip alert in fragment: %s", response.Body)
	}
	if !strings.Contains(response.Body, "ops-zulip") {
		t.Fatalf("expected zulip destination in fragment: %s", response.Body)
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

func firstEmailDestinationID(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var destinationID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from email_destinations where label = 'ops-email'",
	).Scan(&destinationID)
	if scanErr != nil {
		t.Fatalf("email destination id: %v", scanErr)
	}

	return destinationID
}

func firstDiscordDestinationID(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var destinationID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from discord_destinations where label = 'ops-discord'",
	).Scan(&destinationID)
	if scanErr != nil {
		t.Fatalf("discord destination id: %v", scanErr)
	}

	return destinationID
}

func firstGoogleChatDestinationID(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var destinationID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from google_chat_destinations where label = 'ops-google-chat'",
	).Scan(&destinationID)
	if scanErr != nil {
		t.Fatalf("google chat destination id: %v", scanErr)
	}

	return destinationID
}

func firstNtfyDestinationID(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var destinationID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from ntfy_destinations where label = 'ops-ntfy'",
	).Scan(&destinationID)
	if scanErr != nil {
		t.Fatalf("ntfy destination id: %v", scanErr)
	}

	return destinationID
}

func firstTeamsDestinationID(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var destinationID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from teams_destinations where label = 'ops-teams'",
	).Scan(&destinationID)
	if scanErr != nil {
		t.Fatalf("microsoft teams destination id: %v", scanErr)
	}

	return destinationID
}

func firstZulipDestinationID(t *testing.T, ctx context.Context, databaseURL string) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	var destinationID string
	scanErr := pool.QueryRow(
		ctx,
		"select id from zulip_destinations where label = 'ops-zulip'",
	).Scan(&destinationID)
	if scanErr != nil {
		t.Fatalf("zulip destination id: %v", scanErr)
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

func transactionGroupIDForEvent(
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
select fingerprint
from events
where event_id = $1
  and kind = 'transaction'
`
	var groupID string
	scanErr := pool.QueryRow(ctx, query, eventID).Scan(&groupID)
	if scanErr != nil {
		t.Fatalf("transaction group id for event: %v", scanErr)
	}

	return groupID
}

func logIDForBody(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	body string,
) string {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
select id::text
from log_records
where body = $1
order by received_at desc
limit 1
`
	var logID string
	scanErr := pool.QueryRow(ctx, query, body).Scan(&logID)
	if scanErr != nil {
		t.Fatalf("log id for body: %v", scanErr)
	}

	return logID
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

func postTransactionEnvelope(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	payload := strings.Join([]string{
		`{`,
		`"event_id":"910e8400e29b41d4a716446655440000",`,
		`"type":"transaction",`,
		`"transaction":"GET /checkout",`,
		`"start_timestamp":"2026-04-24T13:03:00Z",`,
		`"timestamp":"2026-04-24T13:03:01.500Z",`,
		`"platform":"javascript",`,
		`"release":"web@1.2.3",`,
		`"environment":"production",`,
		`"contexts":{"trace":{`,
		`"trace_id":"0123456789abcdef0123456789abcdef",`,
		`"span_id":"1111111111111111",`,
		`"parent_span_id":"2222222222222222",`,
		`"op":"http.server",`,
		`"status":"ok"`,
		`}},`,
		`"spans":[{`,
		`"span_id":"3333333333333333",`,
		`"parent_span_id":"1111111111111111",`,
		`"op":"db",`,
		`"description":"select checkout",`,
		`"start_timestamp":"2026-04-24T13:03:00.250Z",`,
		`"timestamp":"2026-04-24T13:03:00.350Z",`,
		`"status":"ok"`,
		`}]`,
		`}`,
	}, "")
	body := strings.Join([]string{
		fmt.Sprintf(`{"dsn":"http://%s@example.test/1","event_id":"910e8400e29b41d4a716446655440000"}`, publicKey),
		fmt.Sprintf(`{"type":"transaction","length":%d}`, len(payload)),
		payload,
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

func postLogEnvelope(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	payload := strings.Join([]string{
		`{"items":[{`,
		`"timestamp":"2026-04-24T13:04:00Z",`,
		`"level":"error",`,
		`"body":"checkout log failed",`,
		`"logger":"checkout.web",`,
		`"trace_id":"0123456789abcdef0123456789abcdef",`,
		`"span_id":"1111111111111111",`,
		`"release":"web@1.2.3",`,
		`"environment":"production",`,
		`"resource_attributes":{"service.name":{"value":"checkout","type":"string"}},`,
		`"attributes":{"http.route":{"value":"/checkout","type":"string"}}`,
		`}]}`,
	}, "")
	body := strings.Join([]string{
		fmt.Sprintf(`{"dsn":"http://%s@example.test/1"}`, publicKey),
		fmt.Sprintf(`{"type":"log","length":%d}`, len(payload)),
		payload,
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

func postOTelLogEnvelope(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	payload := strings.Join([]string{
		`{"resourceLogs":[{`,
		`"resource":{"attributes":[`,
		`{"key":"service.name","value":{"stringValue":"checkout-api"}},`,
		`{"key":"deployment.environment","value":{"stringValue":"production"}},`,
		`{"key":"service.version","value":{"stringValue":"api@1.2.3"}}`,
		`]},`,
		`"scopeLogs":[{"scope":{"name":"checkout.otel"},`,
		`"logRecords":[{`,
		`"timeUnixNano":"1776256800000000000",`,
		`"severityText":"WARN",`,
		`"body":{"stringValue":"otel checkout failed"},`,
		`"traceId":"0123456789abcdef0123456789abcdef",`,
		`"spanId":"2222222222222222",`,
		`"attributes":[{"key":"http.route","value":{"stringValue":"/otel-checkout"}}]`,
		`}]}]`,
		`}]}`,
	}, "")
	body := strings.Join([]string{
		fmt.Sprintf(`{"dsn":"http://%s@example.test/1"}`, publicKey),
		fmt.Sprintf(`{"type":"otel_log","length":%d}`, len(payload)),
		payload,
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

func assertPersistedLogs(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
select
  count(*),
  count(*) filter (where body = 'checkout log failed' and severity = 'error'),
  count(*) filter (where body = 'otel checkout failed' and severity = 'warning'),
  count(*) filter (where resource_attributes->>'service.name' = 'checkout-api'),
  count(*) filter (where attributes->>'http.route' = '/checkout')
from log_records
`
	var total int
	var sentryLogs int
	var otelLogs int
	var otelResourceMatches int
	var sentryAttributeMatches int
	scanErr := pool.QueryRow(ctx, query).Scan(
		&total,
		&sentryLogs,
		&otelLogs,
		&otelResourceMatches,
		&sentryAttributeMatches,
	)
	if scanErr != nil {
		t.Fatalf("logs: %v", scanErr)
	}

	if total != 2 || sentryLogs != 1 || otelLogs != 1 {
		t.Fatalf("unexpected persisted logs: total=%d sentry=%d otel=%d", total, sentryLogs, otelLogs)
	}

	if otelResourceMatches != 1 || sentryAttributeMatches != 1 {
		t.Fatalf("unexpected log attributes: resource=%d attribute=%d", otelResourceMatches, sentryAttributeMatches)
	}

	var eventLeaks int
	leakErr := pool.QueryRow(
		ctx,
		`select count(*) from events where title in ('checkout log failed', 'otel checkout failed')`,
	).Scan(&eventLeaks)
	if leakErr != nil {
		t.Fatalf("log event leak query: %v", leakErr)
	}

	if eventLeaks != 0 {
		t.Fatalf("log records created canonical events: %d", eventLeaks)
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

func deliverEmail(t *testing.T, ctx context.Context, store *postgres.Store) {
	t.Helper()

	sender := &e2eEmailSender{}
	commandResult := notifications.NewEmailBatchCommand(time.Now().UTC(), 5, "http://example.test")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("email command: %v", commandErr)
	}

	batchResult := notifications.DeliverEmailBatch(ctx, command, store, sender)
	receipt, batchErr := batchResult.Value()
	if batchErr != nil {
		t.Fatalf("email batch: %v", batchErr)
	}

	if receipt.Claimed() < 1 || receipt.Delivered() < 1 {
		t.Fatalf("unexpected email receipt: claimed=%d delivered=%d failed=%d", receipt.Claimed(), receipt.Delivered(), receipt.Failed())
	}

	if !strings.Contains(sender.body, "http://example.test/issues/") {
		t.Fatalf("unexpected email body: %s", sender.body)
	}
}

func deliverDiscord(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	resolver e2eResolver,
	discordURL string,
) {
	t.Helper()

	client := &http.Client{Transport: rewriteHookTransport(t, discordURL)}
	sender := discord.NewSender(client)
	commandResult := notifications.NewDiscordBatchCommand(time.Now().UTC(), 5, "http://example.test")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("discord command: %v", commandErr)
	}

	batchResult := notifications.DeliverDiscordBatch(ctx, command, resolver, store, sender)
	receipt, batchErr := batchResult.Value()
	if batchErr != nil {
		t.Fatalf("discord batch: %v", batchErr)
	}

	if receipt.Claimed() < 1 || receipt.Delivered() < 1 {
		t.Fatalf("unexpected discord receipt: claimed=%d delivered=%d failed=%d", receipt.Claimed(), receipt.Delivered(), receipt.Failed())
	}
}

func deliverGoogleChat(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	resolver e2eResolver,
	googleChatURL string,
) {
	t.Helper()

	client := &http.Client{Transport: rewriteHookTransport(t, googleChatURL)}
	sender := googlechat.NewSender(client)
	commandResult := notifications.NewGoogleChatBatchCommand(time.Now().UTC(), 5, "http://example.test")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("google chat command: %v", commandErr)
	}

	batchResult := notifications.DeliverGoogleChatBatch(ctx, command, resolver, store, sender)
	receipt, batchErr := batchResult.Value()
	if batchErr != nil {
		t.Fatalf("google chat batch: %v", batchErr)
	}

	if receipt.Claimed() < 1 || receipt.Delivered() < 1 {
		t.Fatalf("unexpected google chat receipt: claimed=%d delivered=%d failed=%d", receipt.Claimed(), receipt.Delivered(), receipt.Failed())
	}
}

func deliverNtfy(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	resolver e2eResolver,
	ntfyURL string,
) {
	t.Helper()

	client := &http.Client{Transport: rewriteHookTransport(t, ntfyURL)}
	sender := ntfy.NewSender(client)
	commandResult := notifications.NewNtfyBatchCommand(time.Now().UTC(), 5, "http://example.test")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("ntfy command: %v", commandErr)
	}

	batchResult := notifications.DeliverNtfyBatch(ctx, command, resolver, store, sender)
	receipt, batchErr := batchResult.Value()
	if batchErr != nil {
		t.Fatalf("ntfy batch: %v", batchErr)
	}

	if receipt.Claimed() < 1 || receipt.Delivered() < 1 {
		t.Fatalf("unexpected ntfy receipt: claimed=%d delivered=%d failed=%d", receipt.Claimed(), receipt.Delivered(), receipt.Failed())
	}
}

func deliverTeams(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	resolver e2eResolver,
	teamsURL string,
) {
	t.Helper()

	client := &http.Client{Transport: rewriteHookTransport(t, teamsURL)}
	sender := teams.NewSender(client)
	commandResult := notifications.NewTeamsBatchCommand(time.Now().UTC(), 5, "http://example.test")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("microsoft teams command: %v", commandErr)
	}

	batchResult := notifications.DeliverTeamsBatch(ctx, command, resolver, store, sender)
	receipt, batchErr := batchResult.Value()
	if batchErr != nil {
		t.Fatalf("microsoft teams batch: %v", batchErr)
	}

	if receipt.Claimed() < 1 || receipt.Delivered() < 1 {
		t.Fatalf("unexpected microsoft teams receipt: claimed=%d delivered=%d failed=%d", receipt.Claimed(), receipt.Delivered(), receipt.Failed())
	}
}

func deliverZulip(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	resolver e2eResolver,
	zulipURL string,
) {
	t.Helper()

	client := &http.Client{Transport: rewriteHookTransport(t, zulipURL)}
	sender := zulip.NewSender(client)
	commandResult := notifications.NewZulipBatchCommand(time.Now().UTC(), 5, "http://example.test")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("zulip command: %v", commandErr)
	}

	batchResult := notifications.DeliverZulipBatch(ctx, command, resolver, store, sender)
	receipt, batchErr := batchResult.Value()
	if batchErr != nil {
		t.Fatalf("zulip batch: %v", batchErr)
	}

	if receipt.Claimed() < 1 || receipt.Delivered() < 1 {
		t.Fatalf("unexpected zulip receipt: claimed=%d delivered=%d failed=%d", receipt.Claimed(), receipt.Delivered(), receipt.Failed())
	}
}

type e2eEmailSender struct {
	body string
}

func (sender *e2eEmailSender) SendEmail(
	ctx context.Context,
	message notifications.EmailMessage,
) result.Result[notifications.EmailSendReceipt] {
	sender.body = message.Body().String()

	return result.Ok(notifications.NewEmailSendReceipt("<e2e-email@example.test>"))
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

func assertEmailDelivered(t *testing.T, ctx context.Context, databaseURL string) {
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
  and ni.provider = 'email'
`
	scanErr := pool.QueryRow(ctx, query).Scan(&status, &providerMessageID, &attempts)
	if scanErr != nil {
		t.Fatalf("email intent: %v", scanErr)
	}

	if status != "delivered" || providerMessageID != "<e2e-email@example.test>" || attempts != 1 {
		t.Fatalf("unexpected email intent: %s %s %d", status, providerMessageID, attempts)
	}
}

func assertDiscordDelivered(t *testing.T, ctx context.Context, databaseURL string) {
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
  and ni.provider = 'discord'
`
	scanErr := pool.QueryRow(ctx, query).Scan(&status, &providerStatusCode, &attempts)
	if scanErr != nil {
		t.Fatalf("discord intent: %v", scanErr)
	}

	if status != "delivered" || providerStatusCode != http.StatusNoContent || attempts != 1 {
		t.Fatalf("unexpected discord intent: %s %d %d", status, providerStatusCode, attempts)
	}
}

func assertGoogleChatDelivered(t *testing.T, ctx context.Context, databaseURL string) {
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
  and ni.provider = 'google_chat'
`
	scanErr := pool.QueryRow(ctx, query).Scan(&status, &providerStatusCode, &attempts)
	if scanErr != nil {
		t.Fatalf("google chat intent: %v", scanErr)
	}

	if status != "delivered" || providerStatusCode != http.StatusOK || attempts != 1 {
		t.Fatalf("unexpected google chat intent: %s %d %d", status, providerStatusCode, attempts)
	}
}

func assertNtfyDelivered(t *testing.T, ctx context.Context, databaseURL string) {
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
  and ni.provider = 'ntfy'
`
	scanErr := pool.QueryRow(ctx, query).Scan(&status, &providerStatusCode, &attempts)
	if scanErr != nil {
		t.Fatalf("ntfy intent: %v", scanErr)
	}

	if status != "delivered" || providerStatusCode != http.StatusOK || attempts != 1 {
		t.Fatalf("unexpected ntfy intent: %s %d %d", status, providerStatusCode, attempts)
	}
}

func assertTeamsDelivered(t *testing.T, ctx context.Context, databaseURL string) {
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
  and ni.provider = 'microsoft_teams'
`
	scanErr := pool.QueryRow(ctx, query).Scan(&status, &providerStatusCode, &attempts)
	if scanErr != nil {
		t.Fatalf("microsoft teams intent: %v", scanErr)
	}

	if status != "delivered" || providerStatusCode != http.StatusOK || attempts != 1 {
		t.Fatalf("unexpected microsoft teams intent: %s %d %d", status, providerStatusCode, attempts)
	}
}

func assertZulipDelivered(t *testing.T, ctx context.Context, databaseURL string) {
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
  and ni.provider = 'zulip'
`
	scanErr := pool.QueryRow(ctx, query).Scan(&status, &providerStatusCode, &attempts)
	if scanErr != nil {
		t.Fatalf("zulip intent: %v", scanErr)
	}

	if status != "delivered" || providerStatusCode != http.StatusOK || attempts != 1 {
		t.Fatalf("unexpected zulip intent: %s %d %d", status, providerStatusCode, attempts)
	}
}

func assertDeliveryJournal(t *testing.T, client *http.Client, baseURL string) {
	t.Helper()

	settings := request(t, client, http.MethodGet, baseURL+"/settings/notifications", "", nil)
	if settings.StatusCode != http.StatusOK {
		t.Fatalf("expected notification settings ok, got %d", settings.StatusCode)
	}

	for _, expected := range []string{"Delivery journal", "telegram", "webhook", "email", "discord", "google_chat", "ntfy", "microsoft_teams", "zulip", "delivered", "204"} {
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

	stats := request(t, client, http.MethodGet, baseURL+"/stats", "", nil)
	if stats.StatusCode != http.StatusOK {
		t.Fatalf("expected project member stats read ok, got %d: %s", stats.StatusCode, stats.Body)
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

func newDiscordFixtureServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/discord" {
			t.Fatalf("unexpected discord path: %s", r.URL.Path)
		}

		var payload map[string]any
		decodeErr := json.NewDecoder(r.Body).Decode(&payload)
		if decodeErr != nil {
			t.Fatalf("decode discord payload: %v", decodeErr)
		}

		if payload["embeds"] == nil {
			t.Fatalf("unexpected discord payload: %#v", payload)
		}

		w.WriteHeader(http.StatusNoContent)
	}))

	parsed, parseErr := url.Parse(server.URL)
	if parseErr != nil {
		t.Fatalf("parse discord server url: %v", parseErr)
	}

	return server, parsed.Scheme + "://hooks.example.test:" + parsed.Port() + "/discord"
}

func newGoogleChatFixtureServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat" {
			t.Fatalf("unexpected google chat path: %s", r.URL.Path)
		}

		var payload map[string]any
		decodeErr := json.NewDecoder(r.Body).Decode(&payload)
		if decodeErr != nil {
			t.Fatalf("decode google chat payload: %v", decodeErr)
		}

		if payload["cardsV2"] == nil {
			t.Fatalf("unexpected google chat payload: %#v", payload)
		}

		w.WriteHeader(http.StatusOK)
	}))

	parsed, parseErr := url.Parse(server.URL)
	if parseErr != nil {
		t.Fatalf("parse google chat server url: %v", parseErr)
	}

	return server, parsed.Scheme + "://hooks.example.test:" + parsed.Port() + "/chat"
}

func newNtfyFixtureServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ops-alerts" {
			t.Fatalf("unexpected ntfy path: %s", r.URL.Path)
		}

		if r.Header.Get("Title") == "" || r.Header.Get("Click") == "" {
			t.Fatalf("unexpected ntfy headers: title=%q click=%q", r.Header.Get("Title"), r.Header.Get("Click"))
		}

		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Fatalf("read ntfy body: %v", readErr)
		}

		if !strings.Contains(string(body), "New issue") || !strings.Contains(string(body), "Event:") {
			t.Fatalf("unexpected ntfy body: %s", string(body))
		}

		w.WriteHeader(http.StatusOK)
	}))

	parsed, parseErr := url.Parse(server.URL)
	if parseErr != nil {
		t.Fatalf("parse ntfy server url: %v", parseErr)
	}

	return server, parsed.Scheme + "://hooks.example.test:" + parsed.Port()
}

func newTeamsFixtureServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/teams" {
			t.Fatalf("unexpected microsoft teams path: %s", r.URL.Path)
		}

		var payload map[string]any
		decodeErr := json.NewDecoder(r.Body).Decode(&payload)
		if decodeErr != nil {
			t.Fatalf("decode microsoft teams payload: %v", decodeErr)
		}

		if payload["attachments"] == nil {
			t.Fatalf("unexpected microsoft teams payload: %#v", payload)
		}

		w.WriteHeader(http.StatusOK)
	}))

	parsed, parseErr := url.Parse(server.URL)
	if parseErr != nil {
		t.Fatalf("parse microsoft teams server url: %v", parseErr)
	}

	return server, parsed.Scheme + "://hooks.example.test:" + parsed.Port() + "/teams"
}

func newZulipFixtureServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/messages" {
			t.Fatalf("unexpected zulip path: %s", r.URL.Path)
		}

		user, password, authOK := r.BasicAuth()
		if !authOK || user != "bot@example.test" || password != "zulip-key" {
			t.Fatalf("unexpected zulip auth: ok=%t user=%q password=%q", authOK, user, password)
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			t.Fatalf("parse zulip form: %v", parseErr)
		}

		if r.PostForm.Get("type") != "stream" ||
			r.PostForm.Get("to") != "ops" ||
			r.PostForm.Get("topic") != "alerts" {
			t.Fatalf("unexpected zulip form: %#v", r.PostForm)
		}

		content := r.PostForm.Get("content")
		if !strings.Contains(content, "New issue") || !strings.Contains(content, "Event:") {
			t.Fatalf("unexpected zulip content: %s", content)
		}

		w.WriteHeader(http.StatusOK)
	}))

	parsed, parseErr := url.Parse(server.URL)
	if parseErr != nil {
		t.Fatalf("parse zulip server url: %v", parseErr)
	}

	return server, parsed.Scheme + "://hooks.example.test:" + parsed.Port()
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

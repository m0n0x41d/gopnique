//go:build integration

package e2e

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	oauthapp "github.com/ivanzakutnii/error-tracker/internal/app/oauth"
)

func TestPostgresOAuthE2E(t *testing.T) {
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

	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatalf("listen: %v", listenErr)
	}
	publicURL := "http://" + listener.Addr().String()
	callbackURL := publicURL + "/auth/oidc/test/callback"
	provider := newFakeOIDCProvider(t, callbackURL)
	defer provider.Close()

	_, bootstrapErr := store.Bootstrap(ctx, postgres.BootstrapInput{
		PublicURL:        publicURL,
		OrganizationName: "OAuth Org",
		ProjectName:      "OAuth Project",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	_, providerErr := store.UpsertOIDCProvider(ctx, oauthapp.ProviderConfigInput{
		Slug:                  "test",
		DisplayName:           "Test OIDC",
		Issuer:                provider.URL(),
		ClientID:              "client-1",
		AuthorizationEndpoint: provider.URL() + "/authorize",
		TokenEndpoint:         provider.URL() + "/token",
		UserInfoEndpoint:      provider.URL() + "/userinfo",
		Scopes:                oauthapp.DefaultScopes,
		Enabled:               true,
	})
	if providerErr != nil {
		t.Fatalf("upsert provider: %v", providerErr)
	}

	server := httptest.NewUnstartedServer(httpadapter.NewHandler(
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
		httpadapter.AuthSettings{PublicURL: publicURL, SecretKey: "oauth-e2e-secret"},
	))
	server.Listener = listener
	server.Start()
	defer server.Close()

	oauthClient := newRedirectingE2EClient(t)
	oauthResponse := request(t, oauthClient, http.MethodGet, server.URL+"/auth/oidc/test/start", "", nil)
	if oauthResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected oauth flow to land on issues, got %d: %s", oauthResponse.StatusCode, oauthResponse.Body)
	}
	if !strings.Contains(oauthResponse.Body, "Issues") {
		t.Fatalf("expected issues page after oauth login: %s", oauthResponse.Body)
	}

	assertOAuthIdentityLinked(t, ctx, databaseURL)
	assertOAuthStateConsumed(t, ctx, databaseURL)

	passwordClient := newE2EClient(t)
	form := url.Values{}
	form.Set("email", "operator@example.test")
	form.Set("password", "correct-horse-battery-staple")
	login := request(t, passwordClient, http.MethodPost, server.URL+"/login", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if login.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected local password login redirect, got %d: %s", login.StatusCode, login.Body)
	}

	disableOAuthProvider(t, ctx, databaseURL)
	disabledProvider := request(t, newE2EClient(t), http.MethodGet, server.URL+"/auth/oidc/test/start", "", nil)
	if disabledProvider.StatusCode != http.StatusNotFound {
		t.Fatalf("expected disabled provider not found, got %d: %s", disabledProvider.StatusCode, disabledProvider.Body)
	}

	enableOAuthProvider(t, ctx, databaseURL)
	disableOperator(t, ctx, databaseURL, "operator@example.test")
	disabledUser := request(t, newRedirectingE2EClient(t), http.MethodGet, server.URL+"/auth/oidc/test/start", "", nil)
	if disabledUser.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected disabled user oauth failure, got %d: %s", disabledUser.StatusCode, disabledUser.Body)
	}
}

type fakeOIDCProvider struct {
	server      *httptest.Server
	callbackURL string
	mu          sync.Mutex
	codes       map[string]string
	tokens      map[string]bool
}

func newFakeOIDCProvider(t *testing.T, callbackURL string) *fakeOIDCProvider {
	t.Helper()

	provider := &fakeOIDCProvider{
		callbackURL: callbackURL,
		codes:       map[string]string{},
		tokens:      map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", provider.authorize)
	mux.HandleFunc("/token", provider.token)
	mux.HandleFunc("/userinfo", provider.userInfo)
	provider.server = httptest.NewServer(mux)

	return provider
}

func (provider *fakeOIDCProvider) URL() string {
	return provider.server.URL
}

func (provider *fakeOIDCProvider) Close() {
	provider.server.Close()
}

func (provider *fakeOIDCProvider) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if query.Get("redirect_uri") != provider.callbackURL {
		http.Error(w, "redirect mismatch", http.StatusBadRequest)
		return
	}

	if query.Get("client_id") != "client-1" {
		http.Error(w, "client mismatch", http.StatusBadRequest)
		return
	}

	if query.Get("code_challenge_method") != "S256" {
		http.Error(w, "challenge method mismatch", http.StatusBadRequest)
		return
	}

	challenge := query.Get("code_challenge")

	provider.mu.Lock()
	code := fmt.Sprintf("code-%d", len(provider.codes)+1)
	provider.codes[code] = challenge
	provider.mu.Unlock()

	redirect, redirectErr := url.Parse(provider.callbackURL)
	if redirectErr != nil {
		http.Error(w, "bad callback", http.StatusInternalServerError)
		return
	}

	values := redirect.Query()
	values.Set("code", code)
	values.Set("state", query.Get("state"))
	redirect.RawQuery = values.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusSeeOther)
}

func (provider *fakeOIDCProvider) token(w http.ResponseWriter, r *http.Request) {
	parseErr := r.ParseForm()
	if parseErr != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	verifier := r.FormValue("code_verifier")
	challenge := pkceChallenge(verifier)

	provider.mu.Lock()
	expectedChallenge := provider.codes[code]
	delete(provider.codes, code)
	provider.tokens["token-1"] = expectedChallenge == challenge
	provider.mu.Unlock()

	if expectedChallenge == "" || expectedChallenge != challenge {
		http.Error(w, "bad code", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"access_token": "token-1",
		"token_type":   "Bearer",
	})
}

func (provider *fakeOIDCProvider) userInfo(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")

	provider.mu.Lock()
	valid := provider.tokens[token]
	provider.mu.Unlock()

	if !valid {
		http.Error(w, "bad token", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sub":            "subject-1",
		"email":          "operator@example.test",
		"email_verified": true,
	})
}

func newRedirectingE2EClient(t *testing.T) *http.Client {
	t.Helper()

	jar, jarErr := cookiejar.New(nil)
	if jarErr != nil {
		t.Fatalf("cookie jar: %v", jarErr)
	}

	return &http.Client{Jar: jar}
}

func pkceChallenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))

	return base64.RawURLEncoding.EncodeToString(hash[:])
}

func assertOAuthIdentityLinked(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
select count(*)
from operator_external_identities
where subject = 'subject-1'
  and email = 'operator@example.test'
`
	var count int
	scanErr := pool.QueryRow(ctx, query).Scan(&count)
	if scanErr != nil {
		t.Fatalf("oauth identity count: %v", scanErr)
	}

	if count != 1 {
		t.Fatalf("expected one linked oauth identity, got %d", count)
	}
}

func assertOAuthStateConsumed(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `select count(*) from oauth_login_states where consumed_at is not null`
	var count int
	scanErr := pool.QueryRow(ctx, query).Scan(&count)
	if scanErr != nil {
		t.Fatalf("oauth state count: %v", scanErr)
	}

	if count != 1 {
		t.Fatalf("expected one consumed oauth state, got %d", count)
	}
}

func disableOAuthProvider(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	setOAuthProviderEnabled(t, ctx, databaseURL, false)
}

func enableOAuthProvider(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	setOAuthProviderEnabled(t, ctx, databaseURL, true)
}

func setOAuthProviderEnabled(t *testing.T, ctx context.Context, databaseURL string, enabled bool) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	_, execErr := pool.Exec(ctx, `update oauth_oidc_providers set enabled = $1 where slug = 'test'`, enabled)
	if execErr != nil {
		t.Fatalf("set oauth provider enabled: %v", execErr)
	}
}

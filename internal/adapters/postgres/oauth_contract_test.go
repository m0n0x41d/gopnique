//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	oauthapp "github.com/ivanzakutnii/error-tracker/internal/app/oauth"
)

func TestPostgresOAuthOIDCWorkflow(t *testing.T) {
	ctx := context.Background()
	adminURL := repositoryAdminURL(t)
	databaseURL := createRepositoryTestDatabase(t, ctx, adminURL)

	store, storeErr := NewStore(ctx, databaseURL)
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

	bootstrap, bootstrapErr := store.Bootstrap(ctx, BootstrapInput{
		PublicURL:        "http://example.test",
		OrganizationName: "OAuth Repo",
		ProjectName:      "OAuth Repo",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}
	if bootstrap.ProjectID == "" {
		t.Fatal("expected bootstrap project id")
	}

	provider, providerErr := store.UpsertOIDCProvider(ctx, oauthapp.ProviderConfigInput{
		Slug:                  "repo",
		DisplayName:           "Repository OIDC",
		Issuer:                "https://issuer.example.test",
		ClientID:              "client",
		AuthorizationEndpoint: "https://issuer.example.test/auth",
		TokenEndpoint:         "https://issuer.example.test/token",
		UserInfoEndpoint:      "https://issuer.example.test/userinfo",
		Scopes:                oauthapp.DefaultScopes,
		Enabled:               true,
	})
	if providerErr != nil {
		t.Fatalf("upsert provider: %v", providerErr)
	}

	state, stateErr := oauthapp.NewSignedState("repo-secret", provider.ID(), "nonce-value-12345678901234567890")
	if stateErr != nil {
		t.Fatalf("state: %v", stateErr)
	}

	storeStateResult := store.StoreOIDCLoginState(ctx, oauthapp.LoginState{
		ProviderID:   provider.ID(),
		StateHash:    state.Hash(),
		CodeVerifier: "verifier-value-12345678901234567890",
		RedirectPath: "/issues",
		ExpiresAt:    time.Now().UTC().Add(10 * time.Minute),
	})
	_, storeStateErr := storeStateResult.Value()
	if storeStateErr != nil {
		t.Fatalf("store state: %v", storeStateErr)
	}

	consumeResult := store.ConsumeOIDCLoginState(ctx, oauthapp.ConsumeStateCommand{
		ProviderID: provider.ID(),
		StateHash:  state.Hash(),
		Now:        time.Now().UTC(),
	})
	consumed, consumeErr := consumeResult.Value()
	if consumeErr != nil {
		t.Fatalf("consume state: %v", consumeErr)
	}
	if consumed.CodeVerifier != "verifier-value-12345678901234567890" {
		t.Fatalf("unexpected verifier: %s", consumed.CodeVerifier)
	}

	secondConsumeResult := store.ConsumeOIDCLoginState(ctx, oauthapp.ConsumeStateCommand{
		ProviderID: provider.ID(),
		StateHash:  state.Hash(),
		Now:        time.Now().UTC(),
	})
	_, secondConsumeErr := secondConsumeResult.Value()
	if secondConsumeErr == nil {
		t.Fatal("expected oauth state to be single-use")
	}

	identity, identityErr := oauthapp.NewVerifiedIdentity(provider.ID(), "subject-1", "operator@example.test")
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	loginResult := store.LoginWithOIDCIdentity(ctx, identity)
	login, loginErr := loginResult.Value()
	if loginErr != nil {
		t.Fatalf("oauth login: %v", loginErr)
	}

	sessionResult := store.ResolveSession(ctx, login.Session)
	session, sessionErr := sessionResult.Value()
	if sessionErr != nil {
		t.Fatalf("resolve oauth session: %v", sessionErr)
	}
	if session.Email != "operator@example.test" {
		t.Fatalf("unexpected session email: %s", session.Email)
	}

	disableRepositoryOperator(t, ctx, store, "operator@example.test")
	disabledResult := store.LoginWithOIDCIdentity(ctx, identity)
	_, disabledErr := disabledResult.Value()
	if disabledErr == nil {
		t.Fatal("expected disabled operator oauth login to fail")
	}
}

func disableRepositoryOperator(
	t *testing.T,
	ctx context.Context,
	store *Store,
	email string,
) {
	t.Helper()

	_, execErr := store.pool.Exec(ctx, `update operators set active = false where email = $1`, email)
	if execErr != nil {
		t.Fatalf("disable operator: %v", execErr)
	}
}

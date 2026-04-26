package oauth

import (
	"net/url"
	"strings"
	"testing"
)

func TestSignedStateRejectsTampering(t *testing.T) {
	state, stateErr := NewSignedState("test-secret", "11111111-1111-4111-a111-111111111111", "nonce-value-12345678901234567890")
	if stateErr != nil {
		t.Fatalf("state: %v", stateErr)
	}

	verified, verifyErr := VerifySignedState("test-secret", "11111111-1111-4111-a111-111111111111", state.String())
	if verifyErr != nil {
		t.Fatalf("verify: %v", verifyErr)
	}

	if string(verified.Hash()) == "" {
		t.Fatal("expected state hash")
	}

	_, tamperErr := VerifySignedState("test-secret", "11111111-1111-4111-a111-111111111111", state.String()+"x")
	if tamperErr == nil {
		t.Fatal("expected tampered state to fail")
	}
}

func TestAuthorizationURLUsesExactRedirectAndPKCE(t *testing.T) {
	provider := testProvider(t)
	state, stateErr := NewSignedState("test-secret", provider.ID(), "nonce-value-12345678901234567890")
	if stateErr != nil {
		t.Fatalf("state: %v", stateErr)
	}

	challenge, challengeErr := CodeChallenge("verifier-value-12345678901234567890")
	if challengeErr != nil {
		t.Fatalf("challenge: %v", challengeErr)
	}

	redirectURI, redirectErr := CallbackURL("https://tracker.example.test/base", provider.Slug())
	if redirectErr != nil {
		t.Fatalf("redirect: %v", redirectErr)
	}

	authURL, authErr := AuthorizationURL(provider, redirectURI, state, challenge)
	if authErr != nil {
		t.Fatalf("authorization url: %v", authErr)
	}

	parsed, parseErr := url.Parse(authURL)
	if parseErr != nil {
		t.Fatalf("parse authorization url: %v", parseErr)
	}

	query := parsed.Query()
	if query.Get("redirect_uri") != "https://tracker.example.test/auth/oidc/test/callback" {
		t.Fatalf("unexpected redirect uri: %s", query.Get("redirect_uri"))
	}

	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("unexpected challenge method: %s", query.Get("code_challenge_method"))
	}

	if query.Get("state") != state.String() {
		t.Fatal("state missing from authorization url")
	}
}

func TestProviderConfigRequiresOpenIDEmailScopes(t *testing.T) {
	_, providerErr := NewProviderConfig(ProviderConfigInput{
		Slug:                  "test",
		DisplayName:           "Test",
		Issuer:                "https://issuer.example.test",
		ClientID:              "client",
		AuthorizationEndpoint: "https://issuer.example.test/auth",
		TokenEndpoint:         "https://issuer.example.test/token",
		UserInfoEndpoint:      "https://issuer.example.test/userinfo",
		Scopes:                "openid profile",
		Enabled:               true,
	})
	if providerErr == nil {
		t.Fatal("expected missing email scope to fail")
	}

	if !strings.Contains(providerErr.Error(), "email") {
		t.Fatalf("unexpected error: %v", providerErr)
	}
}

func TestDecodeUserInfoRequiresVerifiedEmail(t *testing.T) {
	_, userInfoErr := DecodeUserInfo([]byte(`{"sub":"u1","email":"operator@example.test","email_verified":false}`))
	if userInfoErr == nil {
		t.Fatal("expected unverified email to fail")
	}

	userInfo, verifiedErr := DecodeUserInfo([]byte(`{"sub":"u1","email":"Operator@Example.Test","email_verified":true}`))
	if verifiedErr != nil {
		t.Fatalf("decode verified userinfo: %v", verifiedErr)
	}

	if userInfo.Email != "operator@example.test" {
		t.Fatalf("unexpected normalized email: %s", userInfo.Email)
	}
}

func testProvider(t *testing.T) Provider {
	t.Helper()

	provider, providerErr := NewProviderFromStore("11111111-1111-4111-a111-111111111111", ProviderConfigInput{
		Slug:                  "test",
		DisplayName:           "Test",
		Issuer:                "https://issuer.example.test",
		ClientID:              "client",
		AuthorizationEndpoint: "https://issuer.example.test/auth",
		TokenEndpoint:         "https://issuer.example.test/token",
		UserInfoEndpoint:      "https://issuer.example.test/userinfo",
		Scopes:                DefaultScopes,
		Enabled:               true,
	})
	if providerErr != nil {
		t.Fatalf("provider: %v", providerErr)
	}

	return provider
}

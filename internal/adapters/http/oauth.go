package httpadapter

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	oauthapp "github.com/ivanzakutnii/error-tracker/internal/app/oauth"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

const oauthStateTTL = 10 * time.Minute

type oauthManager interface {
	oauthapp.Manager
}

func oidcStartHandler(
	access operators.Access,
	sessions SessionCodec,
	auth AuthSettings,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		manager, managerOK := access.(oauthManager)
		if !managerOK {
			http.Error(w, "oauth not configured", http.StatusServiceUnavailable)
			return
		}

		if hasValidSession(r, access, sessions) {
			http.Redirect(w, r, "/issues", http.StatusSeeOther)
			return
		}

		provider, providerOK := oidcProviderFromRequest(w, r, manager)
		if !providerOK {
			return
		}

		nonce, nonceErr := randomOAuthToken()
		if nonceErr != nil {
			http.Error(w, "oauth state unavailable", http.StatusServiceUnavailable)
			return
		}

		state, stateErr := oauthapp.NewSignedState(auth.SecretKey, provider.ID(), nonce)
		if stateErr != nil {
			http.Error(w, "oauth state unavailable", http.StatusServiceUnavailable)
			return
		}

		verifier, verifierErr := randomOAuthToken()
		if verifierErr != nil {
			http.Error(w, "oauth verifier unavailable", http.StatusServiceUnavailable)
			return
		}

		challenge, challengeErr := oauthapp.CodeChallenge(verifier)
		if challengeErr != nil {
			http.Error(w, "oauth verifier unavailable", http.StatusServiceUnavailable)
			return
		}

		redirectURI, redirectErr := oauthapp.CallbackURL(auth.PublicURL, provider.Slug())
		if redirectErr != nil {
			http.Error(w, "oauth redirect unavailable", http.StatusServiceUnavailable)
			return
		}

		storeResult := manager.StoreOIDCLoginState(r.Context(), oauthapp.LoginState{
			ProviderID:   provider.ID(),
			StateHash:    state.Hash(),
			CodeVerifier: verifier,
			RedirectPath: oauthRedirectPath(r.URL.Query().Get("next")),
			ExpiresAt:    time.Now().UTC().Add(oauthStateTTL),
		})
		_, storeErr := storeResult.Value()
		if storeErr != nil {
			http.Error(w, "oauth state unavailable", http.StatusServiceUnavailable)
			return
		}

		authorizationURL, authorizationErr := oauthapp.AuthorizationURL(provider, redirectURI, state, challenge)
		if authorizationErr != nil {
			http.Error(w, "oauth authorization unavailable", http.StatusServiceUnavailable)
			return
		}

		http.Redirect(w, r, authorizationURL, http.StatusSeeOther)
	}
}

func oidcCallbackHandler(
	access operators.Access,
	sessions SessionCodec,
	auth AuthSettings,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		manager, managerOK := access.(oauthManager)
		if !managerOK {
			http.Error(w, "oauth not configured", http.StatusServiceUnavailable)
			return
		}

		provider, providerOK := oidcProviderFromRequest(w, r, manager)
		if !providerOK {
			return
		}

		code := strings.TrimSpace(r.URL.Query().Get("code"))
		rawState := strings.TrimSpace(r.URL.Query().Get("state"))
		if code == "" || rawState == "" {
			renderHTMLStatus(w, r, templates.Login("OAuth login failed"), http.StatusUnauthorized)
			return
		}

		state, stateErr := oauthapp.VerifySignedState(auth.SecretKey, provider.ID(), rawState)
		if stateErr != nil {
			renderHTMLStatus(w, r, templates.Login("OAuth login failed"), http.StatusUnauthorized)
			return
		}

		now := time.Now().UTC()
		consumeResult := manager.ConsumeOIDCLoginState(r.Context(), oauthapp.ConsumeStateCommand{
			ProviderID: provider.ID(),
			StateHash:  state.Hash(),
			Now:        now,
		})
		consumed, consumeErr := consumeResult.Value()
		if consumeErr != nil {
			renderHTMLStatus(w, r, templates.Login("OAuth login failed"), http.StatusUnauthorized)
			return
		}

		redirectURI, redirectErr := oauthapp.CallbackURL(auth.PublicURL, provider.Slug())
		if redirectErr != nil {
			http.Error(w, "oauth redirect unavailable", http.StatusServiceUnavailable)
			return
		}

		token, tokenErr := exchangeOIDCToken(r.Context(), provider, redirectURI, code, consumed.CodeVerifier)
		if tokenErr != nil {
			http.Error(w, "oauth provider token failed", http.StatusBadGateway)
			return
		}

		userInfo, userInfoErr := fetchOIDCUserInfo(r.Context(), provider, token)
		if userInfoErr != nil {
			renderHTMLStatus(w, r, templates.Login("OAuth login failed"), http.StatusUnauthorized)
			return
		}

		identity, identityErr := oauthapp.NewVerifiedIdentity(provider.ID(), userInfo.Subject, userInfo.Email)
		if identityErr != nil {
			renderHTMLStatus(w, r, templates.Login("OAuth login failed"), http.StatusUnauthorized)
			return
		}

		loginResult := manager.LoginWithOIDCIdentity(r.Context(), identity)
		login, loginErr := loginResult.Value()
		if loginErr != nil {
			renderHTMLStatus(w, r, templates.Login("OAuth login failed"), http.StatusUnauthorized)
			return
		}

		setSessionCookie(w, sessions, login.Session)
		http.Redirect(w, r, consumed.RedirectPath, http.StatusSeeOther)
	}
}

func oidcProviderFromRequest(
	w http.ResponseWriter,
	r *http.Request,
	manager oauthManager,
) (oauthapp.Provider, bool) {
	slug, slugErr := oauthapp.NewProviderSlug(r.PathValue("provider_slug"))
	if slugErr != nil {
		http.NotFound(w, r)
		return oauthapp.Provider{}, false
	}

	providerResult := manager.OIDCProvider(r.Context(), slug)
	provider, providerErr := providerResult.Value()
	if providerErr != nil {
		http.NotFound(w, r)
		return oauthapp.Provider{}, false
	}

	if !provider.Enabled() {
		http.NotFound(w, r)
		return oauthapp.Provider{}, false
	}

	return provider, true
}

func exchangeOIDCToken(
	ctx context.Context,
	provider oauthapp.Provider,
	redirectURI string,
	code string,
	codeVerifier string,
) (string, error) {
	form, formErr := oauthapp.TokenRequestForm(provider, redirectURI, code, codeVerifier)
	if formErr != nil {
		return "", formErr
	}

	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, provider.TokenEndpoint(), strings.NewReader(form.Encode()))
	if requestErr != nil {
		return "", requestErr
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, responseErr := oauthHTTPClient().Do(request)
	if responseErr != nil {
		return "", responseErr
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", errors.New("oauth token endpoint rejected code")
	}

	body, bodyErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if bodyErr != nil {
		return "", bodyErr
	}

	var payload struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	decodeErr := json.Unmarshal(body, &payload)
	if decodeErr != nil {
		return "", decodeErr
	}

	accessToken := strings.TrimSpace(payload.AccessToken)
	if accessToken == "" {
		return "", errors.New("oauth token response missing access token")
	}

	return accessToken, nil
}

func fetchOIDCUserInfo(
	ctx context.Context,
	provider oauthapp.Provider,
	accessToken string,
) (oauthapp.UserInfo, error) {
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, provider.UserInfoEndpoint(), nil)
	if requestErr != nil {
		return oauthapp.UserInfo{}, requestErr
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)

	response, responseErr := oauthHTTPClient().Do(request)
	if responseErr != nil {
		return oauthapp.UserInfo{}, responseErr
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return oauthapp.UserInfo{}, errors.New("oauth userinfo endpoint rejected token")
	}

	body, bodyErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if bodyErr != nil {
		return oauthapp.UserInfo{}, bodyErr
	}

	return oauthapp.DecodeUserInfo(body)
}

func oauthHTTPClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second}
}

func randomOAuthToken() (string, error) {
	buffer := make([]byte, 32)
	_, readErr := rand.Read(buffer)
	if readErr != nil {
		return "", readErr
	}

	token, tokenErr := oauthapp.RandomTokenFromBytes(buffer)
	if tokenErr != nil {
		return "", tokenErr
	}

	return token, nil
}

func oauthRedirectPath(input string) string {
	value := strings.TrimSpace(input)
	if value == "" {
		return "/issues"
	}

	if !strings.HasPrefix(value, "/") {
		return "/issues"
	}

	if strings.HasPrefix(value, "//") {
		return "/issues"
	}

	if strings.Contains(value, "\n") || strings.Contains(value, "\r") {
		return "/issues"
	}

	return value
}

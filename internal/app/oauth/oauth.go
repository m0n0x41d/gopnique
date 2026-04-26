package oauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const DefaultScopes = "openid email profile"

var providerSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type Manager interface {
	OIDCProvider(ctx context.Context, slug ProviderSlug) result.Result[Provider]
	StoreOIDCLoginState(ctx context.Context, state LoginState) result.Result[struct{}]
	ConsumeOIDCLoginState(ctx context.Context, command ConsumeStateCommand) result.Result[ConsumedState]
	LoginWithOIDCIdentity(ctx context.Context, identity VerifiedIdentity) result.Result[operators.LoginResult]
}

type ProviderSlug struct {
	value string
}

type ProviderConfigInput struct {
	Slug                  string
	DisplayName           string
	Issuer                string
	ClientID              string
	ClientSecret          string
	AuthorizationEndpoint string
	TokenEndpoint         string
	UserInfoEndpoint      string
	Scopes                string
	Enabled               bool
}

type Provider struct {
	id                    string
	slug                  ProviderSlug
	displayName           string
	issuer                string
	clientID              string
	clientSecret          string
	authorizationEndpoint string
	tokenEndpoint         string
	userInfoEndpoint      string
	scopes                []string
	enabled               bool
}

type LoginState struct {
	ProviderID   string
	StateHash    []byte
	CodeVerifier string
	RedirectPath string
	ExpiresAt    time.Time
}

type ConsumeStateCommand struct {
	ProviderID string
	StateHash  []byte
	Now        time.Time
}

type ConsumedState struct {
	CodeVerifier string
	RedirectPath string
}

type SignedState struct {
	value string
}

type VerifiedIdentity struct {
	ProviderID string
	Subject    string
	Email      string
}

type UserInfo struct {
	Subject       string
	Email         string
	EmailVerified bool
}

func NewProviderSlug(input string) (ProviderSlug, error) {
	value := strings.TrimSpace(input)
	if !providerSlugPattern.MatchString(value) {
		return ProviderSlug{}, errors.New("oauth provider slug is invalid")
	}

	return ProviderSlug{value: value}, nil
}

func (slug ProviderSlug) String() string {
	return slug.value
}

func NewProviderConfig(input ProviderConfigInput) (Provider, error) {
	slug, slugErr := NewProviderSlug(input.Slug)
	if slugErr != nil {
		return Provider{}, slugErr
	}

	displayName, displayNameErr := nonEmptyBounded("oauth provider display name", input.DisplayName, 128)
	if displayNameErr != nil {
		return Provider{}, displayNameErr
	}

	issuer, issuerErr := oidcURL("oauth issuer", input.Issuer)
	if issuerErr != nil {
		return Provider{}, issuerErr
	}

	clientID, clientIDErr := nonEmptyBounded("oauth client id", input.ClientID, 512)
	if clientIDErr != nil {
		return Provider{}, clientIDErr
	}

	authorizationEndpoint, authorizationErr := oidcURL("oauth authorization endpoint", input.AuthorizationEndpoint)
	if authorizationErr != nil {
		return Provider{}, authorizationErr
	}

	tokenEndpoint, tokenErr := oidcURL("oauth token endpoint", input.TokenEndpoint)
	if tokenErr != nil {
		return Provider{}, tokenErr
	}

	userInfoEndpoint, userInfoErr := oidcURL("oauth userinfo endpoint", input.UserInfoEndpoint)
	if userInfoErr != nil {
		return Provider{}, userInfoErr
	}

	scopes, scopesErr := NormalizeScopes(input.Scopes)
	if scopesErr != nil {
		return Provider{}, scopesErr
	}

	provider := Provider{
		slug:                  slug,
		displayName:           displayName,
		issuer:                issuer,
		clientID:              clientID,
		clientSecret:          strings.TrimSpace(input.ClientSecret),
		authorizationEndpoint: authorizationEndpoint,
		tokenEndpoint:         tokenEndpoint,
		userInfoEndpoint:      userInfoEndpoint,
		scopes:                scopes,
		enabled:               input.Enabled,
	}

	return provider, nil
}

func NewProviderFromStore(
	id string,
	input ProviderConfigInput,
) (Provider, error) {
	provider, providerErr := NewProviderConfig(input)
	if providerErr != nil {
		return Provider{}, providerErr
	}

	providerID, providerIDErr := nonEmptyBounded("oauth provider id", id, 64)
	if providerIDErr != nil {
		return Provider{}, providerIDErr
	}

	provider.id = providerID

	return provider, nil
}

func (provider Provider) ID() string {
	return provider.id
}

func (provider Provider) Slug() ProviderSlug {
	return provider.slug
}

func (provider Provider) DisplayName() string {
	return provider.displayName
}

func (provider Provider) Issuer() string {
	return provider.issuer
}

func (provider Provider) ClientID() string {
	return provider.clientID
}

func (provider Provider) ClientSecret() string {
	return provider.clientSecret
}

func (provider Provider) AuthorizationEndpoint() string {
	return provider.authorizationEndpoint
}

func (provider Provider) TokenEndpoint() string {
	return provider.tokenEndpoint
}

func (provider Provider) UserInfoEndpoint() string {
	return provider.userInfoEndpoint
}

func (provider Provider) Scopes() []string {
	scopes := make([]string, len(provider.scopes))
	copy(scopes, provider.scopes)

	return scopes
}

func (provider Provider) ScopeString() string {
	return strings.Join(provider.scopes, " ")
}

func (provider Provider) Enabled() bool {
	return provider.enabled
}

func NormalizeScopes(input string) ([]string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		value = DefaultScopes
	}

	rawScopes := strings.Fields(value)
	seen := map[string]bool{}
	scopes := []string{}

	for _, rawScope := range rawScopes {
		scope, scopeErr := nonEmptyBounded("oauth scope", rawScope, 128)
		if scopeErr != nil {
			return nil, scopeErr
		}

		if seen[scope] {
			continue
		}

		seen[scope] = true
		scopes = append(scopes, scope)
	}

	if !seen["openid"] {
		return nil, errors.New("oauth scopes must include openid")
	}

	if !seen["email"] {
		return nil, errors.New("oauth scopes must include email")
	}

	return scopes, nil
}

func CallbackURL(publicURL string, slug ProviderSlug) (string, error) {
	base, baseErr := oidcURL("PUBLIC_URL", publicURL)
	if baseErr != nil {
		return "", baseErr
	}

	parsed, parseErr := url.Parse(base)
	if parseErr != nil {
		return "", parseErr
	}

	parsed.Path = "/auth/oidc/" + slug.String() + "/callback"
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String(), nil
}

func AuthorizationURL(
	provider Provider,
	redirectURI string,
	state SignedState,
	codeChallenge string,
) (string, error) {
	parsed, parseErr := url.Parse(provider.AuthorizationEndpoint())
	if parseErr != nil {
		return "", parseErr
	}

	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", provider.ClientID())
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", provider.ScopeString())
	query.Set("state", state.String())
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", "S256")
	parsed.RawQuery = query.Encode()

	return parsed.String(), nil
}

func NewSignedState(secret string, providerID string, nonce string) (SignedState, error) {
	stateSecret, secretErr := nonEmptyBounded("oauth state secret", secret, 4096)
	if secretErr != nil {
		return SignedState{}, secretErr
	}

	stateNonce, nonceErr := nonEmptyBounded("oauth state nonce", nonce, 256)
	if nonceErr != nil {
		return SignedState{}, nonceErr
	}

	id, idErr := nonEmptyBounded("oauth provider id", providerID, 64)
	if idErr != nil {
		return SignedState{}, idErr
	}

	signature := stateSignature(stateSecret, id, stateNonce)
	value := stateNonce + "." + signature

	return SignedState{value: value}, nil
}

func VerifySignedState(secret string, providerID string, raw string) (SignedState, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 2 {
		return SignedState{}, errors.New("oauth state is invalid")
	}

	state, stateErr := NewSignedState(secret, providerID, parts[0])
	if stateErr != nil {
		return SignedState{}, stateErr
	}

	if !hmac.Equal([]byte(state.String()), []byte(raw)) {
		return SignedState{}, errors.New("oauth state signature is invalid")
	}

	return state, nil
}

func (state SignedState) String() string {
	return state.value
}

func (state SignedState) Hash() []byte {
	hash := sha256.Sum256([]byte(state.value))

	return hash[:]
}

func CodeChallenge(verifier string) (string, error) {
	value, valueErr := nonEmptyBounded("oauth code verifier", verifier, 256)
	if valueErr != nil {
		return "", valueErr
	}

	hash := sha256.Sum256([]byte(value))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])

	return challenge, nil
}

func NewVerifiedIdentity(providerID string, subject string, email string) (VerifiedIdentity, error) {
	id, idErr := nonEmptyBounded("oauth provider id", providerID, 64)
	if idErr != nil {
		return VerifiedIdentity{}, idErr
	}

	trimmedSubject, subjectErr := nonEmptyBounded("oauth subject", subject, 512)
	if subjectErr != nil {
		return VerifiedIdentity{}, subjectErr
	}

	trimmedEmail, emailErr := nonEmptyBounded("oauth email", strings.ToLower(email), 320)
	if emailErr != nil {
		return VerifiedIdentity{}, emailErr
	}

	if !strings.Contains(trimmedEmail, "@") {
		return VerifiedIdentity{}, errors.New("oauth email is invalid")
	}

	return VerifiedIdentity{
		ProviderID: id,
		Subject:    trimmedSubject,
		Email:      trimmedEmail,
	}, nil
}

func DecodeUserInfo(payload []byte) (UserInfo, error) {
	var raw struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}

	decodeErr := json.Unmarshal(payload, &raw)
	if decodeErr != nil {
		return UserInfo{}, decodeErr
	}

	subject, subjectErr := nonEmptyBounded("oauth userinfo subject", raw.Subject, 512)
	if subjectErr != nil {
		return UserInfo{}, subjectErr
	}

	email, emailErr := nonEmptyBounded("oauth userinfo email", strings.ToLower(raw.Email), 320)
	if emailErr != nil {
		return UserInfo{}, emailErr
	}

	if !raw.EmailVerified {
		return UserInfo{}, errors.New("oauth email is not verified")
	}

	return UserInfo{
		Subject:       subject,
		Email:         email,
		EmailVerified: raw.EmailVerified,
	}, nil
}

func TokenRequestForm(
	provider Provider,
	redirectURI string,
	code string,
	codeVerifier string,
) (url.Values, error) {
	authCode, codeErr := nonEmptyBounded("oauth authorization code", code, 4096)
	if codeErr != nil {
		return url.Values{}, codeErr
	}

	verifier, verifierErr := nonEmptyBounded("oauth code verifier", codeVerifier, 256)
	if verifierErr != nil {
		return url.Values{}, verifierErr
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authCode)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", provider.ClientID())
	form.Set("code_verifier", verifier)
	if provider.ClientSecret() != "" {
		form.Set("client_secret", provider.ClientSecret())
	}

	return form, nil
}

func RandomTokenFromBytes(bytes []byte) (string, error) {
	if len(bytes) < 32 {
		return "", errors.New("oauth random token needs at least 32 bytes")
	}

	token := base64.RawURLEncoding.EncodeToString(bytes)

	return token, nil
}

func stateSignature(secret string, providerID string, nonce string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(providerID))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write([]byte(nonce))

	return hex.EncodeToString(mac.Sum(nil))
}

func nonEmptyBounded(name string, input string, limit int) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New(name + " is required")
	}

	if len(value) > limit {
		return "", errors.New(name + " is too long")
	}

	return value, nil
}

func oidcURL(name string, input string) (string, error) {
	value, valueErr := nonEmptyBounded(name, input, 2048)
	if valueErr != nil {
		return "", valueErr
	}

	parsed, parseErr := url.Parse(value)
	if parseErr != nil {
		return "", errors.New(name + " must be a valid URL")
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New(name + " must use http or https")
	}

	if parsed.Host == "" {
		return "", errors.New(name + " host is required")
	}

	if parsed.Fragment != "" {
		return "", errors.New(name + " must not include a fragment")
	}

	return parsed.String(), nil
}

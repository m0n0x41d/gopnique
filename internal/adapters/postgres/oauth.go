package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	oauthapp "github.com/ivanzakutnii/error-tracker/internal/app/oauth"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) UpsertOIDCProvider(
	ctx context.Context,
	input oauthapp.ProviderConfigInput,
) (oauthapp.Provider, error) {
	provider, providerErr := oauthapp.NewProviderConfig(input)
	if providerErr != nil {
		return oauthapp.Provider{}, providerErr
	}

	providerID, providerIDErr := randomUUID()
	if providerIDErr != nil {
		return oauthapp.Provider{}, providerIDErr
	}

	now := time.Now().UTC()
	query := `
insert into oauth_oidc_providers (
  id,
  slug,
  display_name,
  issuer,
  client_id,
  client_secret,
  authorization_endpoint,
  token_endpoint,
  userinfo_endpoint,
  scopes,
  enabled,
  created_at,
  updated_at
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12
)
on conflict (slug) do update set
  display_name = excluded.display_name,
  issuer = excluded.issuer,
  client_id = excluded.client_id,
  client_secret = excluded.client_secret,
  authorization_endpoint = excluded.authorization_endpoint,
  token_endpoint = excluded.token_endpoint,
  userinfo_endpoint = excluded.userinfo_endpoint,
  scopes = excluded.scopes,
  enabled = excluded.enabled,
  updated_at = excluded.updated_at
returning id
`
	var storedID string
	scanErr := store.pool.QueryRow(
		ctx,
		query,
		providerID,
		provider.Slug().String(),
		provider.DisplayName(),
		provider.Issuer(),
		provider.ClientID(),
		provider.ClientSecret(),
		provider.AuthorizationEndpoint(),
		provider.TokenEndpoint(),
		provider.UserInfoEndpoint(),
		provider.ScopeString(),
		provider.Enabled(),
		now,
	).Scan(&storedID)
	if scanErr != nil {
		return oauthapp.Provider{}, scanErr
	}

	stored, storedErr := oauthapp.NewProviderFromStore(storedID, input)
	if storedErr != nil {
		return oauthapp.Provider{}, storedErr
	}

	return stored, nil
}

func (store *Store) OIDCProvider(
	ctx context.Context,
	slug oauthapp.ProviderSlug,
) result.Result[oauthapp.Provider] {
	query := `
select
  id,
  slug,
  display_name,
  issuer,
  client_id,
  client_secret,
  authorization_endpoint,
  token_endpoint,
  userinfo_endpoint,
  scopes,
  enabled
from oauth_oidc_providers
where slug = $1
`
	var id string
	var storedSlug string
	var displayName string
	var issuer string
	var clientID string
	var clientSecret string
	var authorizationEndpoint string
	var tokenEndpoint string
	var userInfoEndpoint string
	var scopes string
	var enabled bool
	scanErr := store.pool.QueryRow(ctx, query, slug.String()).Scan(
		&id,
		&storedSlug,
		&displayName,
		&issuer,
		&clientID,
		&clientSecret,
		&authorizationEndpoint,
		&tokenEndpoint,
		&userInfoEndpoint,
		&scopes,
		&enabled,
	)
	if scanErr != nil {
		return result.Err[oauthapp.Provider](scanErr)
	}

	provider, providerErr := oauthapp.NewProviderFromStore(id, oauthapp.ProviderConfigInput{
		Slug:                  storedSlug,
		DisplayName:           displayName,
		Issuer:                issuer,
		ClientID:              clientID,
		ClientSecret:          clientSecret,
		AuthorizationEndpoint: authorizationEndpoint,
		TokenEndpoint:         tokenEndpoint,
		UserInfoEndpoint:      userInfoEndpoint,
		Scopes:                scopes,
		Enabled:               enabled,
	})
	if providerErr != nil {
		return result.Err[oauthapp.Provider](providerErr)
	}

	return result.Ok(provider)
}

func (store *Store) StoreOIDCLoginState(
	ctx context.Context,
	state oauthapp.LoginState,
) result.Result[struct{}] {
	query := `
insert into oauth_login_states (
  state_hash,
  provider_id,
  code_verifier,
  redirect_path,
  expires_at,
  created_at
) values (
  $1, $2, $3, $4, $5, $6
)
`
	_, execErr := store.pool.Exec(
		ctx,
		query,
		state.StateHash,
		state.ProviderID,
		state.CodeVerifier,
		state.RedirectPath,
		state.ExpiresAt,
		time.Now().UTC(),
	)
	if execErr != nil {
		return result.Err[struct{}](execErr)
	}

	return result.Ok(struct{}{})
}

func (store *Store) ConsumeOIDCLoginState(
	ctx context.Context,
	command oauthapp.ConsumeStateCommand,
) result.Result[oauthapp.ConsumedState] {
	query := `
update oauth_login_states
set consumed_at = $4
where state_hash = $1
  and provider_id = $2
  and expires_at > $3
  and consumed_at is null
returning code_verifier, redirect_path
`
	var consumed oauthapp.ConsumedState
	scanErr := store.pool.QueryRow(
		ctx,
		query,
		command.StateHash,
		command.ProviderID,
		command.Now,
		command.Now,
	).Scan(&consumed.CodeVerifier, &consumed.RedirectPath)
	if scanErr != nil {
		return result.Err[oauthapp.ConsumedState](scanErr)
	}

	return result.Ok(consumed)
}

func (store *Store) LoginWithOIDCIdentity(
	ctx context.Context,
	identity oauthapp.VerifiedIdentity,
) result.Result[operators.LoginResult] {
	token, tokenErr := randomToken()
	if tokenErr != nil {
		return result.Err[operators.LoginResult](tokenErr)
	}

	sessionToken, sessionErr := operators.NewSessionToken(token)
	if sessionErr != nil {
		return result.Err[operators.LoginResult](sessionErr)
	}

	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[operators.LoginResult](beginErr)
	}

	operatorID, loginErr := loginWithOIDCIdentityInTx(ctx, tx, identity, sessionToken)
	if loginErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[operators.LoginResult](loginErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[operators.LoginResult](commitErr)
	}

	if operatorID == "" {
		return result.Err[operators.LoginResult](errors.New("oauth operator is invalid"))
	}

	return result.Ok(operators.LoginResult{Session: sessionToken})
}

func loginWithOIDCIdentityInTx(
	ctx context.Context,
	tx pgx.Tx,
	identity oauthapp.VerifiedIdentity,
	sessionToken operators.SessionToken,
) (string, error) {
	operatorID, operatorErr := activeOperatorIDForEmail(ctx, tx, identity.Email)
	if operatorErr != nil {
		return "", operatorErr
	}

	conflictErr := ensureExternalIdentityOwner(ctx, tx, identity, operatorID)
	if conflictErr != nil {
		return "", conflictErr
	}

	linkErr := upsertExternalIdentity(ctx, tx, identity, operatorID)
	if linkErr != nil {
		return "", linkErr
	}

	sessionErr := storeSessionInTx(ctx, tx, operatorID, sessionToken)
	if sessionErr != nil {
		return "", sessionErr
	}

	return operatorID, nil
}

func activeOperatorIDForEmail(
	ctx context.Context,
	tx pgx.Tx,
	email string,
) (string, error) {
	query := `
select o.id
from operators o
join operator_organizations oo on oo.operator_id = o.id
join project_memberships pm on pm.operator_id = o.id
  and pm.organization_id = oo.organization_id
where lower(o.email) = lower($1)
  and o.active = true
order by o.created_at asc
limit 1
`
	var operatorID string
	scanErr := tx.QueryRow(ctx, query, email).Scan(&operatorID)

	return operatorID, scanErr
}

func ensureExternalIdentityOwner(
	ctx context.Context,
	tx pgx.Tx,
	identity oauthapp.VerifiedIdentity,
	operatorID string,
) error {
	query := `
select operator_id
from operator_external_identities
where provider_id = $1
  and subject = $2
`
	var linkedOperatorID string
	scanErr := tx.QueryRow(ctx, query, identity.ProviderID, identity.Subject).Scan(&linkedOperatorID)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return nil
	}

	if scanErr != nil {
		return scanErr
	}

	if linkedOperatorID != operatorID {
		return errors.New("oauth identity is linked to another operator")
	}

	return nil
}

func upsertExternalIdentity(
	ctx context.Context,
	tx pgx.Tx,
	identity oauthapp.VerifiedIdentity,
	operatorID string,
) error {
	query := `
insert into operator_external_identities (
  provider_id,
  subject,
  operator_id,
  email,
  created_at,
  last_login_at
) values (
  $1, $2, $3, $4, $5, $5
)
on conflict (provider_id, subject) do update set
  email = excluded.email,
  last_login_at = excluded.last_login_at
`
	_, execErr := tx.Exec(
		ctx,
		query,
		identity.ProviderID,
		identity.Subject,
		operatorID,
		identity.Email,
		time.Now().UTC(),
	)

	return execErr
}

func storeSessionInTx(
	ctx context.Context,
	tx pgx.Tx,
	operatorID string,
	token operators.SessionToken,
) error {
	sessionID, sessionIDErr := randomUUID()
	if sessionIDErr != nil {
		return sessionIDErr
	}

	query := `
insert into operator_sessions (id, operator_id, token_hash, expires_at, created_at)
values ($1, $2, $3, $4, $5)
`
	now := time.Now().UTC()
	_, execErr := tx.Exec(
		ctx,
		query,
		sessionID,
		operatorID,
		tokenHash(token),
		now.Add(30*24*time.Hour),
		now,
	)

	return execErr
}

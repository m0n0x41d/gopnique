package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type BootstrapInput struct {
	PublicURL        string
	OrganizationSlug string
	OrganizationName string
	ProjectSlug      string
	ProjectName      string
	OperatorEmail    string
	OperatorPassword string
}

type BootstrapResult struct {
	OrganizationID string
	ProjectID      string
	ProjectRef     string
	PublicKey      string
	DSN            string
}

func (store *Store) Bootstrap(ctx context.Context, input BootstrapInput) (BootstrapResult, error) {
	input = normalizeBootstrapInput(input)

	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return BootstrapResult{}, beginErr
	}

	result, bootstrapErr := store.bootstrapInTx(ctx, tx, input)
	if bootstrapErr != nil {
		_ = tx.Rollback(ctx)
		return BootstrapResult{}, bootstrapErr
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return BootstrapResult{}, commitErr
	}

	result.DSN = buildDSN(input.PublicURL, result.ProjectRef, result.PublicKey)

	return result, nil
}

func (store *Store) IsBootstrapped(ctx context.Context) result.Result[bool] {
	query := `select exists(select 1 from operators)`

	var exists bool
	scanErr := store.pool.QueryRow(ctx, query).Scan(&exists)
	if scanErr != nil {
		return result.Err[bool](scanErr)
	}

	return result.Ok(exists)
}

func (store *Store) BootstrapOperator(
	ctx context.Context,
	command operators.BootstrapCommand,
) result.Result[operators.BootstrapResult] {
	bootstrappedResult := store.IsBootstrapped(ctx)
	bootstrapped, bootstrappedErr := bootstrappedResult.Value()
	if bootstrappedErr != nil {
		return result.Err[operators.BootstrapResult](bootstrappedErr)
	}

	if bootstrapped {
		return result.Err[operators.BootstrapResult](errors.New("operator already bootstrapped"))
	}

	bootstrapResult, bootstrapErr := store.Bootstrap(ctx, BootstrapInput{
		PublicURL:        command.PublicURL,
		OrganizationName: command.OrganizationName,
		ProjectName:      command.ProjectName,
		OperatorEmail:    command.Email,
		OperatorPassword: command.Password,
	})
	if bootstrapErr != nil {
		return result.Err[operators.BootstrapResult](bootstrapErr)
	}

	loginResult := store.Login(ctx, operators.LoginCommand{
		Email:    command.Email,
		Password: command.Password,
	})
	login, loginErr := loginResult.Value()
	if loginErr != nil {
		return result.Err[operators.BootstrapResult](loginErr)
	}

	return result.Ok(operators.BootstrapResult{
		DSN:     bootstrapResult.DSN,
		Session: login.Session,
	})
}

func (store *Store) Login(
	ctx context.Context,
	command operators.LoginCommand,
) result.Result[operators.LoginResult] {
	query := `select id, password_hash from operators where email = $1 and active = true`

	var operatorID string
	var passwordHash string
	scanErr := store.pool.QueryRow(ctx, query, strings.TrimSpace(command.Email)).Scan(&operatorID, &passwordHash)
	if scanErr != nil {
		return result.Err[operators.LoginResult](scanErr)
	}

	compareErr := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(command.Password))
	if compareErr != nil {
		return result.Err[operators.LoginResult](errors.New("invalid credentials"))
	}

	token, tokenErr := randomToken()
	if tokenErr != nil {
		return result.Err[operators.LoginResult](tokenErr)
	}

	sessionToken, sessionErr := operators.NewSessionToken(token)
	if sessionErr != nil {
		return result.Err[operators.LoginResult](sessionErr)
	}

	storeErr := store.storeSession(ctx, operatorID, sessionToken)
	if storeErr != nil {
		return result.Err[operators.LoginResult](storeErr)
	}

	return result.Ok(operators.LoginResult{Session: sessionToken})
}

func (store *Store) ResolveSession(
	ctx context.Context,
	token operators.SessionToken,
) result.Result[operators.OperatorSession] {
	query := `
select o.id, o.email, oo.organization_id, p.id, oo.role, pm.role
from operator_sessions s
join operators o on o.id = s.operator_id
join operator_organizations oo on oo.operator_id = o.id
join project_memberships pm on pm.operator_id = o.id and pm.organization_id = oo.organization_id
join projects p on p.id = pm.project_id and p.organization_id = pm.organization_id
where s.token_hash = $1
  and s.expires_at > $2
  and o.active = true
order by p.created_at asc
limit 1
`

	var session operators.OperatorSession
	var organizationIDText string
	var projectIDText string
	scanErr := store.pool.QueryRow(ctx, query, tokenHash(token), time.Now().UTC()).Scan(
		&session.OperatorID,
		&session.Email,
		&organizationIDText,
		&projectIDText,
		&session.OrganizationRole,
		&session.ProjectRole,
	)
	if scanErr != nil {
		return result.Err[operators.OperatorSession](scanErr)
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return result.Err[operators.OperatorSession](organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return result.Err[operators.OperatorSession](projectErr)
	}

	session.OrganizationID = organizationID
	session.ProjectID = projectID

	return result.Ok(session)
}

func (store *Store) DeleteSession(ctx context.Context, token operators.SessionToken) result.Result[struct{}] {
	_, execErr := store.pool.Exec(ctx, `delete from operator_sessions where token_hash = $1`, tokenHash(token))
	if execErr != nil {
		return result.Err[struct{}](execErr)
	}

	return result.Ok(struct{}{})
}

func normalizeBootstrapInput(input BootstrapInput) BootstrapInput {
	input.PublicURL = strings.TrimRight(strings.TrimSpace(input.PublicURL), "/")
	input.OrganizationSlug = valueOr(input.OrganizationSlug, "default")
	input.OrganizationName = valueOr(input.OrganizationName, "Default Organization")
	input.ProjectSlug = valueOr(input.ProjectSlug, "default")
	input.ProjectName = valueOr(input.ProjectName, "Default Project")
	input.OperatorEmail = valueOr(input.OperatorEmail, "operator@example.local")
	input.OperatorPassword = valueOr(input.OperatorPassword, "change-me")

	return input
}

func (store *Store) bootstrapInTx(
	ctx context.Context,
	tx pgx.Tx,
	input BootstrapInput,
) (BootstrapResult, error) {
	now := time.Now().UTC()

	organizationID, organizationErr := upsertOrganization(ctx, tx, input, now)
	if organizationErr != nil {
		return BootstrapResult{}, organizationErr
	}

	organizationQuotaErr := ensureOrganizationQuotaPolicy(ctx, tx, organizationID, now)
	if organizationQuotaErr != nil {
		return BootstrapResult{}, organizationQuotaErr
	}

	operatorID, operatorErr := upsertOperator(ctx, tx, input, now)
	if operatorErr != nil {
		return BootstrapResult{}, operatorErr
	}

	linkErr := linkOperatorOrganization(ctx, tx, operatorID, organizationID, now)
	if linkErr != nil {
		return BootstrapResult{}, linkErr
	}

	projectID, projectErr := upsertProject(ctx, tx, input, organizationID, now)
	if projectErr != nil {
		return BootstrapResult{}, projectErr
	}

	projectLinkErr := linkOperatorProject(ctx, tx, operatorID, organizationID, projectID, now)
	if projectLinkErr != nil {
		return BootstrapResult{}, projectLinkErr
	}

	retentionErr := ensureProjectRetentionPolicy(ctx, tx, organizationID, projectID, now)
	if retentionErr != nil {
		return BootstrapResult{}, retentionErr
	}

	projectQuotaErr := ensureProjectQuotaPolicy(ctx, tx, organizationID, projectID, now)
	if projectQuotaErr != nil {
		return BootstrapResult{}, projectQuotaErr
	}

	teamID, teamErr := upsertDefaultTeam(ctx, tx, organizationID, now)
	if teamErr != nil {
		return BootstrapResult{}, teamErr
	}

	teamMemberErr := linkOperatorTeam(ctx, tx, operatorID, organizationID, teamID, now)
	if teamMemberErr != nil {
		return BootstrapResult{}, teamMemberErr
	}

	teamProjectErr := linkTeamProject(ctx, tx, teamID, organizationID, projectID, now)
	if teamProjectErr != nil {
		return BootstrapResult{}, teamProjectErr
	}

	publicKey, keyErr := ensureProjectKey(ctx, tx, projectID, now)
	if keyErr != nil {
		return BootstrapResult{}, keyErr
	}

	rateLimitErr := ensureProjectKeyRateLimitPolicy(ctx, tx, organizationID, projectID, publicKey, now)
	if rateLimitErr != nil {
		return BootstrapResult{}, rateLimitErr
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ActorID:        operatorID,
		Action:         auditapp.ActionBootstrap,
		TargetType:     "project",
		TargetID:       projectID,
		Metadata: map[string]string{
			"organization_slug": input.OrganizationSlug,
			"project_slug":      input.ProjectSlug,
		},
	})
	if auditErr != nil {
		return BootstrapResult{}, auditErr
	}

	return BootstrapResult{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ProjectRef:     "1",
		PublicKey:      publicKey,
	}, nil
}

func upsertOrganization(
	ctx context.Context,
	tx pgx.Tx,
	input BootstrapInput,
	now time.Time,
) (string, error) {
	id, idErr := randomUUID()
	if idErr != nil {
		return "", idErr
	}

	query := `
insert into organizations (id, slug, name, created_at)
values ($1, $2, $3, $4)
on conflict (slug) do update set name = excluded.name
returning id
`

	var organizationID string
	err := tx.QueryRow(ctx, query, id, input.OrganizationSlug, input.OrganizationName, now).Scan(&organizationID)

	return organizationID, err
}

func upsertOperator(
	ctx context.Context,
	tx pgx.Tx,
	input BootstrapInput,
	now time.Time,
) (string, error) {
	id, idErr := randomUUID()
	if idErr != nil {
		return "", idErr
	}
	passwordHash, passwordHashErr := bcrypt.GenerateFromPassword([]byte(input.OperatorPassword), bcrypt.DefaultCost)
	if passwordHashErr != nil {
		return "", passwordHashErr
	}

	query := `
insert into operators (id, email, display_name, password_hash, created_at)
values ($1, $2, $3, $4, $5)
on conflict (email) do update set display_name = excluded.display_name
returning id
`

	var operatorID string
	err := tx.QueryRow(ctx, query, id, input.OperatorEmail, input.OperatorEmail, string(passwordHash), now).Scan(&operatorID)

	return operatorID, err
}

func (store *Store) storeSession(
	ctx context.Context,
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
	_, execErr := store.pool.Exec(
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

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes), nil
}

func tokenHash(token operators.SessionToken) []byte {
	hash := sha256.Sum256([]byte(token.String()))
	return hash[:]
}

func linkOperatorOrganization(
	ctx context.Context,
	tx pgx.Tx,
	operatorID string,
	organizationID string,
	now time.Time,
) error {
	query := `
insert into operator_organizations (operator_id, organization_id, role, created_at)
values ($1, $2, 'owner', $3)
on conflict (operator_id, organization_id) do nothing
`

	_, err := tx.Exec(ctx, query, operatorID, organizationID, now)

	return err
}

func linkOperatorProject(
	ctx context.Context,
	tx pgx.Tx,
	operatorID string,
	organizationID string,
	projectID string,
	now time.Time,
) error {
	query := `
insert into project_memberships (operator_id, organization_id, project_id, role, created_at)
values ($1, $2, $3, 'owner', $4)
on conflict (operator_id, project_id) do update
set role = excluded.role
`
	_, err := tx.Exec(ctx, query, operatorID, organizationID, projectID, now)

	return err
}

func upsertDefaultTeam(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	now time.Time,
) (string, error) {
	id, idErr := randomUUID()
	if idErr != nil {
		return "", idErr
	}

	query := `
insert into teams (id, organization_id, slug, name, created_at)
values ($1, $2, 'default', 'Default team', $3)
on conflict (organization_id, slug) do update set name = excluded.name
returning id
`
	var teamID string
	err := tx.QueryRow(ctx, query, id, organizationID, now).Scan(&teamID)

	return teamID, err
}

func linkOperatorTeam(
	ctx context.Context,
	tx pgx.Tx,
	operatorID string,
	organizationID string,
	teamID string,
	now time.Time,
) error {
	query := `
insert into team_memberships (team_id, organization_id, operator_id, role, created_at)
values ($1, $2, $3, 'manager', $4)
on conflict (team_id, operator_id) do update
set role = excluded.role
`
	_, err := tx.Exec(ctx, query, teamID, organizationID, operatorID, now)

	return err
}

func linkTeamProject(
	ctx context.Context,
	tx pgx.Tx,
	teamID string,
	organizationID string,
	projectID string,
	now time.Time,
) error {
	query := `
insert into team_project_memberships (team_id, organization_id, project_id, role, created_at)
values ($1, $2, $3, 'admin', $4)
on conflict (team_id, project_id) do update
set role = excluded.role
`
	_, err := tx.Exec(ctx, query, teamID, organizationID, projectID, now)

	return err
}

func upsertProject(
	ctx context.Context,
	tx pgx.Tx,
	input BootstrapInput,
	organizationID string,
	now time.Time,
) (string, error) {
	id, idErr := randomUUID()
	if idErr != nil {
		return "", idErr
	}

	query := `
insert into projects (id, organization_id, ingest_ref, slug, name, created_at)
values ($1, $2, '1', $3, $4, $5)
on conflict (organization_id, slug) do update set name = excluded.name
returning id
`

	var projectID string
	err := tx.QueryRow(ctx, query, id, organizationID, input.ProjectSlug, input.ProjectName, now).Scan(&projectID)

	return projectID, err
}

func ensureProjectRetentionPolicy(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	now time.Time,
) error {
	query := `
insert into project_retention_policies (
  organization_id,
  project_id,
  created_at,
  updated_at
) values ($1, $2, $3, $3)
on conflict (project_id) do nothing
`
	_, err := tx.Exec(ctx, query, organizationID, projectID, now)

	return err
}

func ensureOrganizationQuotaPolicy(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	now time.Time,
) error {
	query := `
insert into organization_quota_policies (
  organization_id,
  created_at,
  updated_at
) values ($1, $2, $2)
on conflict (organization_id) do nothing
`
	_, err := tx.Exec(ctx, query, organizationID, now)

	return err
}

func ensureProjectQuotaPolicy(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	now time.Time,
) error {
	query := `
insert into project_quota_policies (
  organization_id,
  project_id,
  created_at,
  updated_at
) values ($1, $2, $3, $3)
on conflict (project_id) do nothing
`
	_, err := tx.Exec(ctx, query, organizationID, projectID, now)

	return err
}

func ensureProjectKeyRateLimitPolicy(
	ctx context.Context,
	tx pgx.Tx,
	organizationID string,
	projectID string,
	publicKey string,
	now time.Time,
) error {
	query := `
insert into project_key_rate_limit_policies (
  project_key_id,
  organization_id,
  project_id,
  created_at,
  updated_at
)
select id, $1, $2, $4, $4
from project_keys
where project_id = $2
  and public_key = $3
on conflict (project_key_id) do nothing
`
	_, err := tx.Exec(ctx, query, organizationID, projectID, publicKey, now)

	return err
}

func ensureProjectKey(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
	now time.Time,
) (string, error) {
	selectQuery := `
select public_key
from project_keys
where project_id = $1 and active = true
order by created_at asc
limit 1
`

	var existingKey string
	selectErr := tx.QueryRow(ctx, selectQuery, projectID).Scan(&existingKey)
	if selectErr == nil {
		return existingKey, nil
	}
	if !errors.Is(selectErr, pgx.ErrNoRows) {
		return "", selectErr
	}

	id, idErr := randomUUID()
	if idErr != nil {
		return "", idErr
	}

	publicKey, keyErr := randomUUID()
	if keyErr != nil {
		return "", keyErr
	}

	insertQuery := `
insert into project_keys (id, project_id, public_key, label, created_at)
values ($1, $2, $3, 'default', $4)
returning public_key
`

	var insertedKey string
	insertErr := tx.QueryRow(ctx, insertQuery, id, projectID, publicKey, now).Scan(&insertedKey)

	return insertedKey, insertErr
}

func buildDSN(publicURL string, projectRef string, publicKey string) string {
	parsed, parseErr := url.Parse(publicURL)
	if parseErr != nil {
		return ""
	}

	parsed.User = url.User(strings.ReplaceAll(publicKey, "-", ""))
	parsed.Path = path.Join(parsed.Path, projectRef)

	return parsed.String()
}

func valueOr(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}

	return value
}

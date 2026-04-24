package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) ListProjectTokens(
	ctx context.Context,
	query tokenapp.ProjectTokenQuery,
) result.Result[tokenapp.ProjectTokenView] {
	sqlQuery := `
select id, name, token_prefix, scope, revoked_at, last_used_at, created_at
from api_tokens
where organization_id = $1
  and project_id = $2
order by created_at desc
`
	rows, queryErr := store.pool.Query(
		ctx,
		sqlQuery,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
	)
	if queryErr != nil {
		return result.Err[tokenapp.ProjectTokenView](queryErr)
	}
	defer rows.Close()

	view := tokenapp.ProjectTokenView{}
	for rows.Next() {
		row, rowErr := scanProjectTokenRow(rows)
		if rowErr != nil {
			return result.Err[tokenapp.ProjectTokenView](rowErr)
		}

		view.Tokens = append(view.Tokens, row)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[tokenapp.ProjectTokenView](rowsErr)
	}

	return result.Ok(view)
}

func (store *Store) CreateProjectToken(
	ctx context.Context,
	command tokenapp.CreateProjectTokenCommand,
) result.Result[tokenapp.CreateProjectTokenResult] {
	tokenID, tokenIDErr := randomUUID()
	if tokenIDErr != nil {
		return result.Err[tokenapp.CreateProjectTokenResult](tokenIDErr)
	}

	secretText, secretErr := randomProjectToken()
	if secretErr != nil {
		return result.Err[tokenapp.CreateProjectTokenResult](secretErr)
	}

	secret, secretParseErr := tokenapp.NewProjectTokenSecret(secretText)
	if secretParseErr != nil {
		return result.Err[tokenapp.CreateProjectTokenResult](secretParseErr)
	}

	now := time.Now().UTC()
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[tokenapp.CreateProjectTokenResult](beginErr)
	}

	query := `
insert into api_tokens (
  id,
  organization_id,
  project_id,
  created_by_operator_id,
  name,
  token_hash,
  token_prefix,
  scope,
  created_at
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9
)
`
	_, execErr := tx.Exec(
		ctx,
		query,
		tokenID,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.ActorID,
		command.Name,
		projectTokenHash(secret),
		projectTokenPrefix(secret),
		string(command.TokenScope),
		now,
	)
	if execErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[tokenapp.CreateProjectTokenResult](execErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Scope.OrganizationID.String(),
		ProjectID:      command.Scope.ProjectID.String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionAPITokenCreated,
		TargetType:     "api_token",
		TargetID:       tokenID,
		Metadata: map[string]string{
			"name":   command.Name,
			"prefix": projectTokenPrefix(secret),
			"scope":  string(command.TokenScope),
		},
	})
	if auditErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[tokenapp.CreateProjectTokenResult](auditErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[tokenapp.CreateProjectTokenResult](commitErr)
	}

	return result.Ok(tokenapp.CreateProjectTokenResult{
		TokenID:      tokenID,
		OneTimeToken: secret.String(),
	})
}

func (store *Store) RevokeProjectToken(
	ctx context.Context,
	command tokenapp.RevokeProjectTokenCommand,
) result.Result[tokenapp.ProjectTokenMutationResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[tokenapp.ProjectTokenMutationResult](beginErr)
	}

	query := `
update api_tokens
set revoked_at = $4
where organization_id = $1
  and project_id = $2
  and id = $3
  and revoked_at is null
returning name, token_prefix, scope
`
	var name string
	var prefix string
	var scope string
	scanErr := tx.QueryRow(
		ctx,
		query,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.TokenID.String(),
		time.Now().UTC(),
	).Scan(&name, &prefix, &scope)
	if scanErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[tokenapp.ProjectTokenMutationResult](scanErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Scope.OrganizationID.String(),
		ProjectID:      command.Scope.ProjectID.String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionAPITokenRevoked,
		TargetType:     "api_token",
		TargetID:       command.TokenID.String(),
		Metadata: map[string]string{
			"name":   name,
			"prefix": prefix,
			"scope":  scope,
		},
	})
	if auditErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[tokenapp.ProjectTokenMutationResult](auditErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[tokenapp.ProjectTokenMutationResult](commitErr)
	}

	return result.Ok(tokenapp.ProjectTokenMutationResult{
		TokenID: command.TokenID.String(),
	})
}

func (store *Store) ResolveProjectToken(
	ctx context.Context,
	secret tokenapp.ProjectTokenSecret,
) result.Result[tokenapp.ProjectTokenAuth] {
	query := `
update api_tokens
set last_used_at = $2
where token_hash = $1
  and revoked_at is null
returning id, organization_id, project_id, scope
`
	var tokenIDText string
	var organizationIDText string
	var projectIDText string
	var scopeText string
	scanErr := store.pool.QueryRow(ctx, query, projectTokenHash(secret), time.Now().UTC()).Scan(
		&tokenIDText,
		&organizationIDText,
		&projectIDText,
		&scopeText,
	)
	if scanErr != nil {
		return result.Err[tokenapp.ProjectTokenAuth](scanErr)
	}

	tokenID, tokenIDErr := domain.NewAPITokenID(tokenIDText)
	if tokenIDErr != nil {
		return result.Err[tokenapp.ProjectTokenAuth](tokenIDErr)
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return result.Err[tokenapp.ProjectTokenAuth](organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return result.Err[tokenapp.ProjectTokenAuth](projectErr)
	}

	scope, scopeErr := tokenapp.ParseProjectTokenScope(scopeText)
	if scopeErr != nil {
		return result.Err[tokenapp.ProjectTokenAuth](scopeErr)
	}

	return result.Ok(tokenapp.ProjectTokenAuth{
		TokenID:        tokenID,
		OrganizationID: organizationID,
		ProjectID:      projectID,
		TokenScope:     scope,
	})
}

type tokenScanner interface {
	Scan(dest ...any) error
}

func scanProjectTokenRow(scanner tokenScanner) (tokenapp.ProjectTokenRow, error) {
	var row tokenapp.ProjectTokenRow
	var revokedAt sql.NullTime
	var lastUsedAt sql.NullTime
	var createdAt time.Time
	scanErr := scanner.Scan(
		&row.ID,
		&row.Name,
		&row.Prefix,
		&row.Scope,
		&revokedAt,
		&lastUsedAt,
		&createdAt,
	)
	if scanErr != nil {
		return tokenapp.ProjectTokenRow{}, scanErr
	}

	row.Status = tokenStatus(revokedAt)
	row.CreatedAt = formatTokenTime(createdAt)
	row.LastUsedAt = optionalTokenTime(lastUsedAt)
	row.RevokedAt = optionalTokenTime(revokedAt)

	return row, nil
}

func randomProjectToken() (string, error) {
	token, tokenErr := randomToken()
	if tokenErr != nil {
		return "", tokenErr
	}

	return "etp_" + token, nil
}

func projectTokenHash(secret tokenapp.ProjectTokenSecret) []byte {
	hash := sha256.Sum256([]byte(secret.String()))
	return hash[:]
}

func projectTokenPrefix(secret tokenapp.ProjectTokenSecret) string {
	value := secret.String()
	if len(value) < 12 {
		return value
	}

	return value[:12]
}

func tokenStatus(revokedAt sql.NullTime) string {
	if revokedAt.Valid {
		return "revoked"
	}

	return "active"
}

func optionalTokenTime(value sql.NullTime) string {
	if !value.Valid {
		return "none"
	}

	return formatTokenTime(value.Time)
}

func formatTokenTime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

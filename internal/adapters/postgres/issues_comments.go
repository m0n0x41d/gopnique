package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	issueapp "github.com/ivanzakutnii/error-tracker/internal/app/issues"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) AddIssueComment(
	ctx context.Context,
	command issueapp.AddCommentCommand,
) result.Result[issueapp.CommentMutationResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[issueapp.CommentMutationResult](beginErr)
	}

	existsErr := lockIssueForComment(ctx, tx, command)
	if existsErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[issueapp.CommentMutationResult](existsErr)
	}

	commentID, commentErr := insertIssueComment(ctx, tx, command)
	if commentErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[issueapp.CommentMutationResult](commentErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Scope.OrganizationID.String(),
		ProjectID:      command.Scope.ProjectID.String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionIssueCommentCreated,
		TargetType:     "issue_comment",
		TargetID:       commentID,
		Metadata: map[string]string{
			"issue_id": command.IssueID.String(),
		},
	})
	if auditErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[issueapp.CommentMutationResult](auditErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[issueapp.CommentMutationResult](commitErr)
	}

	return result.Ok(issueapp.CommentMutationResult{CommentID: commentID})
}

func lockIssueForComment(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.AddCommentCommand,
) error {
	query := `
select 1
from issues
where organization_id = $1
  and project_id = $2
  and id = $3
for update
`
	var exists int
	scanErr := tx.QueryRow(
		ctx,
		query,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.IssueID.String(),
	).Scan(&exists)
	if scanErr != nil {
		return scanErr
	}

	if exists != 1 {
		return errors.New("issue not found")
	}

	return nil
}

func insertIssueComment(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.AddCommentCommand,
) (string, error) {
	commentID, commentIDErr := randomUUID()
	if commentIDErr != nil {
		return "", commentIDErr
	}

	query := `
insert into issue_comments (
  id,
  organization_id,
  project_id,
  issue_id,
  actor_operator_id,
  body,
  created_at
) values (
  $1, $2, $3, $4, $5, $6, $7
)
`
	_, execErr := tx.Exec(
		ctx,
		query,
		commentID,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.IssueID.String(),
		command.ActorID,
		command.Body,
		time.Now().UTC(),
	)
	if execErr != nil {
		return "", execErr
	}

	return commentID, nil
}

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

func (store *Store) TransitionIssueStatus(
	ctx context.Context,
	command issueapp.StatusTransitionCommand,
) result.Result[issueapp.StatusTransitionResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[issueapp.StatusTransitionResult](beginErr)
	}

	transitionResult := transitionIssueStatusInTx(ctx, tx, command)
	transition, transitionErr := transitionResult.Value()
	if transitionErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[issueapp.StatusTransitionResult](transitionErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[issueapp.StatusTransitionResult](commitErr)
	}

	return result.Ok(transition)
}

func transitionIssueStatusInTx(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.StatusTransitionCommand,
) result.Result[issueapp.StatusTransitionResult] {
	currentResult := lockIssueStatus(ctx, tx, command)
	currentStatus, currentErr := currentResult.Value()
	if currentErr != nil {
		return result.Err[issueapp.StatusTransitionResult](currentErr)
	}

	if !issueapp.CanTransitionIssueStatus(currentStatus, command.TargetStatus) {
		return result.Err[issueapp.StatusTransitionResult](errors.New("issue status transition is invalid"))
	}

	updateErr := updateIssueStatus(ctx, tx, command)
	if updateErr != nil {
		return result.Err[issueapp.StatusTransitionResult](updateErr)
	}

	insertErr := insertIssueStatusTransition(ctx, tx, command, currentStatus)
	if insertErr != nil {
		return result.Err[issueapp.StatusTransitionResult](insertErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Scope.OrganizationID.String(),
		ProjectID:      command.Scope.ProjectID.String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionIssueStatusChanged,
		TargetType:     "issue",
		TargetID:       command.IssueID.String(),
		Metadata: map[string]string{
			"from_status": string(currentStatus),
			"to_status":   string(command.TargetStatus),
			"reason":      command.Reason,
		},
	})
	if auditErr != nil {
		return result.Err[issueapp.StatusTransitionResult](auditErr)
	}

	return result.Ok(issueapp.StatusTransitionResult{
		IssueID: command.IssueID.String(),
		Status:  command.TargetStatus,
	})
}

func lockIssueStatus(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.StatusTransitionCommand,
) result.Result[issueapp.IssueStatus] {
	query := `
select status
from issues
where organization_id = $1
  and project_id = $2
  and id = $3
for update
`
	var statusText string
	scanErr := tx.QueryRow(
		ctx,
		query,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.IssueID.String(),
	).Scan(&statusText)
	if scanErr != nil {
		return result.Err[issueapp.IssueStatus](scanErr)
	}

	status, statusErr := issueapp.ParseIssueStatus(statusText)
	if statusErr != nil {
		return result.Err[issueapp.IssueStatus](statusErr)
	}

	return result.Ok(status)
}

func updateIssueStatus(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.StatusTransitionCommand,
) error {
	query := `
update issues
set status = $4
where organization_id = $1
  and project_id = $2
  and id = $3
`
	tag, execErr := tx.Exec(
		ctx,
		query,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.IssueID.String(),
		string(command.TargetStatus),
	)
	if execErr != nil {
		return execErr
	}

	if tag.RowsAffected() != 1 {
		return errors.New("issue not found")
	}

	return nil
}

func insertIssueStatusTransition(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.StatusTransitionCommand,
	fromStatus issueapp.IssueStatus,
) error {
	transitionID, transitionIDErr := randomUUID()
	if transitionIDErr != nil {
		return transitionIDErr
	}

	query := `
insert into issue_status_transitions (
  id,
  organization_id,
  project_id,
  issue_id,
  actor_operator_id,
  from_status,
  to_status,
  reason,
  created_at
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9
)
`
	_, execErr := tx.Exec(
		ctx,
		query,
		transitionID,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.IssueID.String(),
		command.ActorID,
		string(fromStatus),
		string(command.TargetStatus),
		command.Reason,
		time.Now().UTC(),
	)

	return execErr
}

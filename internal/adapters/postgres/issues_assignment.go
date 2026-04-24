package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	issueapp "github.com/ivanzakutnii/error-tracker/internal/app/issues"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) AssignIssue(
	ctx context.Context,
	command issueapp.AssignIssueCommand,
) result.Result[issueapp.AssignmentMutationResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[issueapp.AssignmentMutationResult](beginErr)
	}

	eligibilityErr := requireAssignmentEligibility(ctx, tx, command)
	if eligibilityErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[issueapp.AssignmentMutationResult](eligibilityErr)
	}

	updateErr := updateIssueAssignment(ctx, tx, command)
	if updateErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[issueapp.AssignmentMutationResult](updateErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Scope.OrganizationID.String(),
		ProjectID:      command.Scope.ProjectID.String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionIssueAssigned,
		TargetType:     "issue",
		TargetID:       command.IssueID.String(),
		Metadata: map[string]string{
			"assignment": command.Target.Value(),
		},
	})
	if auditErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[issueapp.AssignmentMutationResult](auditErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[issueapp.AssignmentMutationResult](commitErr)
	}

	return result.Ok(issueapp.AssignmentMutationResult{
		IssueID: command.IssueID.String(),
		Target:  command.Target,
	})
}

func requireAssignmentEligibility(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.AssignIssueCommand,
) error {
	issueErr := requireIssueForAssignment(ctx, tx, command)
	if issueErr != nil {
		return issueErr
	}

	if command.Target.Kind == issueapp.AssignmentTargetNone {
		return nil
	}

	if command.Target.Kind == issueapp.AssignmentTargetOperator {
		return requireOperatorAssignmentEligibility(ctx, tx, command)
	}

	if command.Target.Kind == issueapp.AssignmentTargetTeam {
		return requireTeamAssignmentEligibility(ctx, tx, command)
	}

	return errors.New("assignment target is invalid")
}

func requireIssueForAssignment(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.AssignIssueCommand,
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

func requireOperatorAssignmentEligibility(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.AssignIssueCommand,
) error {
	query := `
select 1
from project_memberships pm
join operators o on o.id = pm.operator_id and o.active = true
where pm.organization_id = $1
  and pm.project_id = $2
  and pm.operator_id = $3
`
	var exists int
	scanErr := tx.QueryRow(
		ctx,
		query,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.Target.ID,
	).Scan(&exists)
	if scanErr != nil {
		return scanErr
	}

	if exists != 1 {
		return errors.New("assignment target is not eligible")
	}

	return nil
}

func requireTeamAssignmentEligibility(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.AssignIssueCommand,
) error {
	query := `
select 1
from team_project_memberships
where organization_id = $1
  and project_id = $2
  and team_id = $3
`
	var exists int
	scanErr := tx.QueryRow(
		ctx,
		query,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.Target.ID,
	).Scan(&exists)
	if scanErr != nil {
		return scanErr
	}

	if exists != 1 {
		return errors.New("assignment target is not eligible")
	}

	return nil
}

func updateIssueAssignment(
	ctx context.Context,
	tx pgx.Tx,
	command issueapp.AssignIssueCommand,
) error {
	operatorID := ""
	teamID := ""
	if command.Target.Kind == issueapp.AssignmentTargetOperator {
		operatorID = command.Target.ID
	}
	if command.Target.Kind == issueapp.AssignmentTargetTeam {
		teamID = command.Target.ID
	}

	query := `
update issues
set assignee_operator_id = $4,
    assignee_team_id = $5
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
		nullableUUID(operatorID),
		nullableUUID(teamID),
	)
	if execErr != nil {
		return execErr
	}

	if tag.RowsAffected() != 1 {
		return errors.New("issue not found")
	}

	return nil
}

func nullableUUID(value string) any {
	if value == "" {
		return nil
	}

	return value
}

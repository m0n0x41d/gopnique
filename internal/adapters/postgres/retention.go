package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	retentionapp "github.com/ivanzakutnii/error-tracker/internal/app/retention"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) projectRetentionPolicy(
	ctx context.Context,
	scope settingsapp.Scope,
) result.Result[settingsapp.RetentionPolicyView] {
	sql := `
select
  event_retention_days,
  payload_retention_days,
  delivery_retention_days,
  user_report_retention_days,
  enabled
from project_retention_policies
where organization_id = $1
  and project_id = $2
`
	var enabled bool
	var view settingsapp.RetentionPolicyView
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	).Scan(
		&view.EventRetentionDays,
		&view.PayloadRetentionDays,
		&view.DeliveryRetentionDays,
		&view.UserReportRetentionDays,
		&enabled,
	)
	if scanErr != nil {
		return result.Err[settingsapp.RetentionPolicyView](scanErr)
	}

	view.Status = "disabled"
	if enabled {
		view.Status = "enabled"
	}

	return result.Ok(view)
}

func (store *Store) ListRetentionPolicies(
	ctx context.Context,
) result.Result[[]retentionapp.Policy] {
	sql := `
select
  organization_id,
  project_id,
  event_retention_days,
  payload_retention_days,
  delivery_retention_days,
  user_report_retention_days,
  enabled
from project_retention_policies
where enabled = true
order by organization_id, project_id
`
	rows, queryErr := store.pool.Query(ctx, sql)
	if queryErr != nil {
		return result.Err[[]retentionapp.Policy](queryErr)
	}
	defer rows.Close()

	policies := []retentionapp.Policy{}
	for rows.Next() {
		policy, scanErr := scanRetentionPolicy(rows)
		if scanErr != nil {
			return result.Err[[]retentionapp.Policy](scanErr)
		}

		policies = append(policies, policy)
	}

	if rows.Err() != nil {
		return result.Err[[]retentionapp.Policy](rows.Err())
	}

	return result.Ok(policies)
}

func (store *Store) PurgeExpiredProjectData(
	ctx context.Context,
	plan retentionapp.ProjectPurgePlan,
) result.Result[retentionapp.PurgeResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[retentionapp.PurgeResult](beginErr)
	}

	purged, purgeErr := purgeExpiredProjectData(ctx, tx, plan)
	if purgeErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[retentionapp.PurgeResult](purgeErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[retentionapp.PurgeResult](commitErr)
	}

	return result.Ok(purged)
}

func scanRetentionPolicy(rows pgx.Rows) (retentionapp.Policy, error) {
	var organizationIDText string
	var projectIDText string
	var policy retentionapp.Policy
	scanErr := rows.Scan(
		&organizationIDText,
		&projectIDText,
		&policy.EventRetentionDays,
		&policy.PayloadRetentionDays,
		&policy.DeliveryRetentionDays,
		&policy.UserReportRetentionDays,
		&policy.Enabled,
	)
	if scanErr != nil {
		return retentionapp.Policy{}, scanErr
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return retentionapp.Policy{}, organizationErr
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return retentionapp.Policy{}, projectErr
	}

	policy.Scope = retentionapp.Scope{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	}

	return policy, nil
}

func purgeExpiredProjectData(
	ctx context.Context,
	tx pgx.Tx,
	plan retentionapp.ProjectPurgePlan,
) (retentionapp.PurgeResult, error) {
	purged := retentionapp.PurgeResult{}

	deliveries, deliveriesErr := deleteExpiredDeliveries(ctx, tx, plan)
	if deliveriesErr != nil {
		return retentionapp.PurgeResult{}, deliveriesErr
	}
	purged.DeliveryRowsDeleted = deliveries

	reports, reportsErr := deleteExpiredUserReports(ctx, tx, plan)
	if reportsErr != nil {
		return retentionapp.PurgeResult{}, reportsErr
	}
	purged.UserReportsDeleted = reports

	payloads, payloadsErr := clearExpiredEventPayloads(ctx, tx, plan)
	if payloadsErr != nil {
		return retentionapp.PurgeResult{}, payloadsErr
	}
	purged.PayloadsCleared = payloads

	staleIssueRefsErr := deleteStaleIssueReferences(ctx, tx, plan)
	if staleIssueRefsErr != nil {
		return retentionapp.PurgeResult{}, staleIssueRefsErr
	}

	issues, issuesErr := deleteExpiredIssues(ctx, tx, plan)
	if issuesErr != nil {
		return retentionapp.PurgeResult{}, issuesErr
	}
	purged.IssuesDeleted = issues

	events, eventsErr := deleteExpiredEvents(ctx, tx, plan)
	if eventsErr != nil {
		return retentionapp.PurgeResult{}, eventsErr
	}
	purged.EventsDeleted = events

	stats, statsErr := deleteExpiredStats(ctx, tx, plan)
	if statsErr != nil {
		return retentionapp.PurgeResult{}, statsErr
	}
	purged.StatsRowsDeleted = stats

	return purged, nil
}

func deleteExpiredDeliveries(
	ctx context.Context,
	tx pgx.Tx,
	plan retentionapp.ProjectPurgePlan,
) (int, error) {
	sql := `
with doomed as (
  select id
  from notification_intents
  where organization_id = $1
    and project_id = $2
    and created_at < $3
  order by created_at asc
  limit $4
)
delete from notification_intents n
using doomed
where n.id = doomed.id
`
	return execCount(ctx, tx, sql, plan.Scope.OrganizationID.String(), plan.Scope.ProjectID.String(), plan.DeliveryCutoff, plan.BatchSize)
}

func deleteExpiredUserReports(
	ctx context.Context,
	tx pgx.Tx,
	plan retentionapp.ProjectPurgePlan,
) (int, error) {
	sql := `
with doomed as (
  select id
  from user_reports
  where organization_id = $1
    and project_id = $2
    and created_at < $3
  order by created_at asc
  limit $4
)
delete from user_reports r
using doomed
where r.id = doomed.id
`
	return execCount(ctx, tx, sql, plan.Scope.OrganizationID.String(), plan.Scope.ProjectID.String(), plan.UserReportCutoff, plan.BatchSize)
}

func clearExpiredEventPayloads(
	ctx context.Context,
	tx pgx.Tx,
	plan retentionapp.ProjectPurgePlan,
) (int, error) {
	sql := `
with doomed as (
  select id
  from events
  where organization_id = $1
    and project_id = $2
    and received_at < $3
    and canonical_payload is not null
  order by received_at asc
  limit $4
)
update events e
set canonical_payload = null
from doomed
where e.id = doomed.id
`
	return execCount(ctx, tx, sql, plan.Scope.OrganizationID.String(), plan.Scope.ProjectID.String(), plan.PayloadCutoff, plan.BatchSize)
}

func deleteStaleIssueReferences(
	ctx context.Context,
	tx pgx.Tx,
	plan retentionapp.ProjectPurgePlan,
) error {
	statements := []string{
		`
with doomed as (
  select id
  from issues
  where organization_id = $1
    and project_id = $2
    and last_seen_at < $3
  order by last_seen_at asc
  limit $4
)
delete from issue_comments c
using doomed
where c.issue_id = doomed.id
`,
		`
with doomed as (
  select id
  from issues
  where organization_id = $1
    and project_id = $2
    and last_seen_at < $3
  order by last_seen_at asc
  limit $4
)
delete from issue_status_transitions t
using doomed
where t.issue_id = doomed.id
`,
		`
with doomed as (
  select id
  from issues
  where organization_id = $1
    and project_id = $2
    and last_seen_at < $3
  order by last_seen_at asc
  limit $4
)
delete from issue_fingerprints f
using doomed
where f.issue_id = doomed.id
`,
	}

	for _, sql := range statements {
		_, execErr := execCount(ctx, tx, sql, plan.Scope.OrganizationID.String(), plan.Scope.ProjectID.String(), plan.EventCutoff, plan.BatchSize)
		if execErr != nil {
			return execErr
		}
	}

	return nil
}

func deleteExpiredIssues(
	ctx context.Context,
	tx pgx.Tx,
	plan retentionapp.ProjectPurgePlan,
) (int, error) {
	sql := `
with doomed as (
  select id
  from issues
  where organization_id = $1
    and project_id = $2
    and last_seen_at < $3
  order by last_seen_at asc
  limit $4
)
delete from issues i
using doomed
where i.id = doomed.id
`
	return execCount(ctx, tx, sql, plan.Scope.OrganizationID.String(), plan.Scope.ProjectID.String(), plan.EventCutoff, plan.BatchSize)
}

func deleteExpiredEvents(
	ctx context.Context,
	tx pgx.Tx,
	plan retentionapp.ProjectPurgePlan,
) (int, error) {
	sql := `
with doomed as (
  select e.id
  from events e
  where e.organization_id = $1
    and e.project_id = $2
    and e.received_at < $3
    and not exists (
      select 1 from issues i where i.last_event_id = e.id
    )
  order by e.received_at asc
  limit $4
)
delete from events e
using doomed
where e.id = doomed.id
`
	return execCount(ctx, tx, sql, plan.Scope.OrganizationID.String(), plan.Scope.ProjectID.String(), plan.EventCutoff, plan.BatchSize)
}

func deleteExpiredStats(
	ctx context.Context,
	tx pgx.Tx,
	plan retentionapp.ProjectPurgePlan,
) (int, error) {
	sql := `
with doomed as (
  select project_id, bucket_at
  from project_hourly_stats
  where organization_id = $1
    and project_id = $2
    and bucket_at < date_trunc('hour', $3::timestamptz)
  order by bucket_at asc
  limit $4
)
delete from project_hourly_stats s
using doomed
where s.project_id = doomed.project_id
  and s.bucket_at = doomed.bucket_at
`
	return execCount(ctx, tx, sql, plan.Scope.OrganizationID.String(), plan.Scope.ProjectID.String(), plan.EventCutoff, plan.BatchSize)
}

func execCount(
	ctx context.Context,
	tx pgx.Tx,
	sql string,
	args ...any,
) (int, error) {
	tag, execErr := tx.Exec(ctx, sql, args...)
	if execErr != nil {
		return 0, execErr
	}

	return int(tag.RowsAffected()), nil
}

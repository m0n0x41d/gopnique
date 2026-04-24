package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	userreportapp "github.com/ivanzakutnii/error-tracker/internal/app/userreports"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) SubmitUserReport(
	ctx context.Context,
	command userreportapp.SubmitCommand,
) result.Result[userreportapp.SubmitReceipt] {
	reportID, reportIDErr := randomUUID()
	if reportIDErr != nil {
		return result.Err[userreportapp.SubmitReceipt](reportIDErr)
	}

	query := `
with linked_event as (
  select
    e.event_id,
    i.id as issue_id
  from events e
  left join issue_fingerprints f on f.project_id = e.project_id and f.fingerprint = e.fingerprint
  left join issues i on i.id = f.issue_id
  where e.organization_id = $1
    and e.project_id = $2
    and e.event_id = $3
)
insert into user_reports (
  id,
  organization_id,
  project_id,
  issue_id,
  event_id,
  name,
  email,
  comments,
  created_at
)
select
  $4,
  $1,
  $2,
  issue_id,
  event_id,
  $5,
  $6,
  $7,
  $8
from linked_event
on conflict (project_id, event_id) do update
set
  issue_id = excluded.issue_id,
  name = excluded.name,
  email = excluded.email,
  comments = excluded.comments,
  created_at = excluded.created_at
returning id
`
	var savedID string
	scanErr := store.pool.QueryRow(
		ctx,
		query,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.EventID.String(),
		reportID,
		command.Name,
		command.Email,
		command.Comments,
		time.Now().UTC(),
	).Scan(&savedID)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return result.Err[userreportapp.SubmitReceipt](errors.New("user report event was not found"))
	}

	if scanErr != nil {
		return result.Err[userreportapp.SubmitReceipt](scanErr)
	}

	return result.Ok(userreportapp.SubmitReceipt{
		ReportID: savedID,
		EventID:  command.EventID.String(),
	})
}

func (store *Store) ListIssueUserReports(
	ctx context.Context,
	query userreportapp.IssueReportsQuery,
) result.Result[userreportapp.IssueReportsView] {
	sql := `
select
  id,
  event_id,
  name,
  email,
  comments,
  created_at
from user_reports
where organization_id = $1
  and project_id = $2
  and issue_id = $3
order by created_at desc
limit $4
`
	rows, queryErr := store.pool.Query(
		ctx,
		sql,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
		query.IssueID.String(),
		query.Limit,
	)
	if queryErr != nil {
		return result.Err[userreportapp.IssueReportsView](queryErr)
	}
	defer rows.Close()

	itemsResult := scanUserReportRows(rows)
	items, itemsErr := itemsResult.Value()
	if itemsErr != nil {
		return result.Err[userreportapp.IssueReportsView](itemsErr)
	}

	return result.Ok(userreportapp.IssueReportsView{Items: items})
}

func scanUserReportRows(rows pgx.Rows) result.Result[[]userreportapp.IssueReportView] {
	items := []userreportapp.IssueReportView{}

	for rows.Next() {
		item, scanErr := scanUserReportRow(rows)
		if scanErr != nil {
			return result.Err[[]userreportapp.IssueReportView](scanErr)
		}

		items = append(items, item)
	}

	if rows.Err() != nil {
		return result.Err[[]userreportapp.IssueReportView](rows.Err())
	}

	return result.Ok(items)
}

func scanUserReportRow(rows pgx.Rows) (userreportapp.IssueReportView, error) {
	var item userreportapp.IssueReportView
	var createdAt time.Time
	scanErr := rows.Scan(
		&item.ID,
		&item.EventID,
		&item.Name,
		&item.Email,
		&item.Comments,
		&createdAt,
	)
	if scanErr != nil {
		return userreportapp.IssueReportView{}, scanErr
	}

	item.CreatedAt = formatTime(createdAt)

	return item, nil
}

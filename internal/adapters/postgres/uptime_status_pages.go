package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	uptimeapp "github.com/ivanzakutnii/error-tracker/internal/app/uptime"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const statusPageIncidentLimit = 25
const statusPageMonitorLimit = 100
const statusPageSummaryLimit = 20

type statusPageRecord struct {
	scope      uptimeapp.Scope
	id         string
	name       string
	visibility string
	token      string
	createdAt  string
}

func (store *Store) CreateStatusPage(
	ctx context.Context,
	command uptimeapp.CreateStatusPageCommand,
) result.Result[uptimeapp.StatusPageMutationResult] {
	name, nameErr := domain.NewStatusPageName(command.Name)
	if nameErr != nil {
		return result.Err[uptimeapp.StatusPageMutationResult](nameErr)
	}

	visibility, visibilityErr := domain.ParseStatusPageVisibility(command.Visibility)
	if visibilityErr != nil {
		return result.Err[uptimeapp.StatusPageMutationResult](visibilityErr)
	}

	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[uptimeapp.StatusPageMutationResult](beginErr)
	}

	created, createErr := createStatusPageInTx(ctx, tx, command, name, visibility)
	if createErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.StatusPageMutationResult](createErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Scope.OrganizationID.String(),
		ProjectID:      command.Scope.ProjectID.String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionStatusPageCreated,
		TargetType:     "uptime_status_page",
		TargetID:       created.PageID,
		Metadata: map[string]string{
			"name":       name.String(),
			"visibility": string(visibility),
		},
	})
	if auditErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.StatusPageMutationResult](auditErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[uptimeapp.StatusPageMutationResult](commitErr)
	}

	return result.Ok(created)
}

func (store *Store) ShowPrivateStatusPage(
	ctx context.Context,
	query uptimeapp.PrivateStatusPageQuery,
) result.Result[uptimeapp.StatusPageView] {
	recordResult := store.statusPageByID(ctx, query)
	record, recordErr := recordResult.Value()
	if recordErr != nil {
		return result.Err[uptimeapp.StatusPageView](recordErr)
	}

	return store.statusPageView(ctx, record)
}

func (store *Store) ShowPublicStatusPage(
	ctx context.Context,
	query uptimeapp.PublicStatusPageQuery,
) result.Result[uptimeapp.StatusPageView] {
	recordResult := store.statusPageByToken(ctx, query)
	record, recordErr := recordResult.Value()
	if recordErr != nil {
		return result.Err[uptimeapp.StatusPageView](recordErr)
	}

	return store.statusPageView(ctx, record)
}

func (store *Store) listMonitorViews(
	ctx context.Context,
	scope uptimeapp.Scope,
	limit int,
) result.Result[[]uptimeapp.MonitorView] {
	sqlQuery := `
select
  m.id::text,
  m.monitor_type,
  m.name,
  coalesce(m.target_url, ''),
  coalesce(m.heartbeat_endpoint_id, ''),
  m.current_state,
  case when m.enabled then 'enabled' else 'disabled' end,
  m.interval_seconds,
  m.timeout_seconds,
  m.heartbeat_grace_seconds,
  m.last_checked_at,
  m.last_check_in_at,
  m.next_check_at,
  coalesce(lc.http_status::text, ''),
  coalesce(lc.error, ''),
  coalesce(oi.id::text, '')
from uptime_monitors m
left join lateral (
  select http_status, error
  from uptime_monitor_checks
  where monitor_id = m.id
  order by checked_at desc
  limit 1
) lc on true
left join uptime_monitor_incidents oi on oi.monitor_id = m.id and oi.resolved_at is null
where m.organization_id = $1
  and m.project_id = $2
order by m.created_at desc
limit $3
`
	rows, queryErr := store.pool.Query(
		ctx,
		sqlQuery,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
		limit,
	)
	if queryErr != nil {
		return result.Err[[]uptimeapp.MonitorView](queryErr)
	}
	defer rows.Close()

	monitors := []uptimeapp.MonitorView{}
	for rows.Next() {
		monitor, scanErr := scanMonitorView(rows)
		if scanErr != nil {
			return result.Err[[]uptimeapp.MonitorView](scanErr)
		}

		monitors = append(monitors, monitor)
	}

	if rows.Err() != nil {
		return result.Err[[]uptimeapp.MonitorView](rows.Err())
	}

	return result.Ok(monitors)
}

func (store *Store) listStatusPageSummaries(
	ctx context.Context,
	scope uptimeapp.Scope,
) result.Result[[]uptimeapp.StatusPageSummaryView] {
	query := `
select
  id::text,
  name,
  visibility,
  coalesce(public_token, ''),
  case when enabled then 'enabled' else 'disabled' end,
  created_at
from uptime_status_pages
where organization_id = $1
  and project_id = $2
order by created_at desc
limit $3
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
		statusPageSummaryLimit,
	)
	if queryErr != nil {
		return result.Err[[]uptimeapp.StatusPageSummaryView](queryErr)
	}
	defer rows.Close()

	pages := []uptimeapp.StatusPageSummaryView{}
	for rows.Next() {
		page, scanErr := scanStatusPageSummary(rows)
		if scanErr != nil {
			return result.Err[[]uptimeapp.StatusPageSummaryView](scanErr)
		}

		pages = append(pages, page)
	}

	if rows.Err() != nil {
		return result.Err[[]uptimeapp.StatusPageSummaryView](rows.Err())
	}

	return result.Ok(pages)
}

func (store *Store) statusPageByID(
	ctx context.Context,
	query uptimeapp.PrivateStatusPageQuery,
) result.Result[statusPageRecord] {
	sqlQuery := `
select
  id::text,
  organization_id::text,
  project_id::text,
  name,
  visibility,
  coalesce(public_token, ''),
  created_at
from uptime_status_pages
where organization_id = $1
  and project_id = $2
  and id = $3
  and enabled = true
`
	recordResult := scanStatusPageRecord(
		store.pool.QueryRow(
			ctx,
			sqlQuery,
			query.Scope.OrganizationID.String(),
			query.Scope.ProjectID.String(),
			query.PageID,
		),
	)
	record, recordErr := recordResult.Value()
	if recordErr != nil {
		return result.Err[statusPageRecord](recordErr)
	}

	if record.visibility != string(domain.StatusPageVisibilityPrivate) {
		return result.Err[statusPageRecord](sql.ErrNoRows)
	}

	return result.Ok(record)
}

func (store *Store) statusPageByToken(
	ctx context.Context,
	query uptimeapp.PublicStatusPageQuery,
) result.Result[statusPageRecord] {
	sqlQuery := `
select
  id::text,
  organization_id::text,
  project_id::text,
  name,
  visibility,
  coalesce(public_token, ''),
  created_at
from uptime_status_pages
where public_token = $1
  and visibility = 'public'
  and enabled = true
`
	return scanStatusPageRecord(
		store.pool.QueryRow(
			ctx,
			sqlQuery,
			query.Token,
		),
	)
}

func (store *Store) statusPageView(
	ctx context.Context,
	record statusPageRecord,
) result.Result[uptimeapp.StatusPageView] {
	monitorsResult := store.listMonitorViews(ctx, record.scope, statusPageMonitorLimit)
	monitors, monitorsErr := monitorsResult.Value()
	if monitorsErr != nil {
		return result.Err[uptimeapp.StatusPageView](monitorsErr)
	}

	incidentsResult := store.listStatusPageIncidents(ctx, record.scope)
	incidents, incidentsErr := incidentsResult.Value()
	if incidentsErr != nil {
		return result.Err[uptimeapp.StatusPageView](incidentsErr)
	}

	view := uptimeapp.StatusPageView{
		ID:          record.id,
		Name:        record.name,
		Visibility:  record.visibility,
		PrivatePath: statusPagePrivatePath(record.id),
		PublicPath:  statusPagePublicPath(record.token),
		GeneratedAt: formatTime(time.Now().UTC()),
		Monitors:    monitors,
		Incidents:   incidents,
	}

	return result.Ok(view)
}

func (store *Store) listStatusPageIncidents(
	ctx context.Context,
	scope uptimeapp.Scope,
) result.Result[[]uptimeapp.StatusPageIncidentView] {
	query := `
select
  i.id::text,
  m.name,
  case when i.resolved_at is null then 'open' else 'resolved' end,
  i.reason,
  i.opened_at,
  i.resolved_at
from uptime_monitor_incidents i
join uptime_monitors m on m.id = i.monitor_id
where i.organization_id = $1
  and i.project_id = $2
order by
  case when i.resolved_at is null then 0 else 1 end,
  i.opened_at desc
limit $3
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
		statusPageIncidentLimit,
	)
	if queryErr != nil {
		return result.Err[[]uptimeapp.StatusPageIncidentView](queryErr)
	}
	defer rows.Close()

	incidents := []uptimeapp.StatusPageIncidentView{}
	for rows.Next() {
		incident, scanErr := scanStatusPageIncident(rows)
		if scanErr != nil {
			return result.Err[[]uptimeapp.StatusPageIncidentView](scanErr)
		}

		incidents = append(incidents, incident)
	}

	if rows.Err() != nil {
		return result.Err[[]uptimeapp.StatusPageIncidentView](rows.Err())
	}

	return result.Ok(incidents)
}

func createStatusPageInTx(
	ctx context.Context,
	tx pgx.Tx,
	command uptimeapp.CreateStatusPageCommand,
	name domain.StatusPageName,
	visibility domain.StatusPageVisibility,
) (uptimeapp.StatusPageMutationResult, error) {
	pageID, pageIDErr := randomUUID()
	if pageIDErr != nil {
		return uptimeapp.StatusPageMutationResult{}, pageIDErr
	}

	token, tokenErr := statusPageToken(visibility)
	if tokenErr != nil {
		return uptimeapp.StatusPageMutationResult{}, tokenErr
	}

	now := time.Now().UTC()
	query := `
insert into uptime_status_pages (
  id,
  organization_id,
  project_id,
  name,
  visibility,
  public_token,
  created_by_operator_id,
  created_at,
  updated_at
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $8
)
`
	_, execErr := tx.Exec(
		ctx,
		query,
		pageID,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		name.String(),
		string(visibility),
		nullString(token),
		command.ActorID,
		now,
	)
	if execErr != nil {
		return uptimeapp.StatusPageMutationResult{}, execErr
	}

	return uptimeapp.StatusPageMutationResult{
		PageID:      pageID,
		Visibility:  string(visibility),
		PrivatePath: statusPagePrivatePath(pageID),
		PublicPath:  statusPagePublicPath(token),
	}, nil
}

func statusPageToken(visibility domain.StatusPageVisibility) (string, error) {
	if visibility != domain.StatusPageVisibilityPublic {
		return "", nil
	}

	return randomUUID()
}

func scanStatusPageRecord(scanner rowScanner) result.Result[statusPageRecord] {
	var id string
	var organizationIDText string
	var projectIDText string
	var name string
	var visibility string
	var token string
	var createdAt time.Time
	scanErr := scanner.Scan(
		&id,
		&organizationIDText,
		&projectIDText,
		&name,
		&visibility,
		&token,
		&createdAt,
	)
	if scanErr != nil {
		return result.Err[statusPageRecord](scanErr)
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return result.Err[statusPageRecord](organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return result.Err[statusPageRecord](projectErr)
	}

	return result.Ok(statusPageRecord{
		scope: uptimeapp.Scope{
			OrganizationID: organizationID,
			ProjectID:      projectID,
		},
		id:         id,
		name:       name,
		visibility: visibility,
		token:      token,
		createdAt:  formatTime(createdAt),
	})
}

func scanStatusPageSummary(scanner rowScanner) (uptimeapp.StatusPageSummaryView, error) {
	var view uptimeapp.StatusPageSummaryView
	var token string
	var createdAt time.Time
	scanErr := scanner.Scan(
		&view.ID,
		&view.Name,
		&view.Visibility,
		&token,
		&view.Enabled,
		&createdAt,
	)
	if scanErr != nil {
		return uptimeapp.StatusPageSummaryView{}, scanErr
	}

	view.CreatedAt = formatTime(createdAt)
	view.PrivatePath = statusPagePrivatePath(view.ID)
	view.PublicPath = statusPagePublicPath(token)

	return view, nil
}

func scanStatusPageIncident(scanner rowScanner) (uptimeapp.StatusPageIncidentView, error) {
	var incident uptimeapp.StatusPageIncidentView
	var openedAt time.Time
	var resolvedAt sql.NullTime
	scanErr := scanner.Scan(
		&incident.ID,
		&incident.MonitorName,
		&incident.State,
		&incident.Reason,
		&openedAt,
		&resolvedAt,
	)
	if scanErr != nil {
		return uptimeapp.StatusPageIncidentView{}, scanErr
	}

	incident.OpenedAt = formatTime(openedAt)
	incident.ResolvedAt = formatOptionalTime(resolvedAt)

	return incident, nil
}

func statusPagePrivatePath(id string) string {
	if id == "" {
		return ""
	}

	return "/status-pages/" + id
}

func statusPagePublicPath(token string) string {
	if token == "" {
		return ""
	}

	return "/status/" + token
}

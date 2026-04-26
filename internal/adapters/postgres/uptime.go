package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	uptimeapp "github.com/ivanzakutnii/error-tracker/internal/app/uptime"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const monitorCheckLease = 5 * time.Minute

func (store *Store) ShowUptime(
	ctx context.Context,
	query uptimeapp.Query,
) result.Result[uptimeapp.View] {
	monitorsResult := store.listMonitorViews(ctx, query.Scope, query.Limit)
	monitors, monitorsErr := monitorsResult.Value()
	if monitorsErr != nil {
		return result.Err[uptimeapp.View](monitorsErr)
	}

	pagesResult := store.listStatusPageSummaries(ctx, query.Scope)
	pages, pagesErr := pagesResult.Value()
	if pagesErr != nil {
		return result.Err[uptimeapp.View](pagesErr)
	}

	return result.Ok(uptimeapp.View{
		Monitors:    monitors,
		StatusPages: pages,
	})
}

func (store *Store) CreateHTTPMonitor(
	ctx context.Context,
	command uptimeapp.CreateHTTPMonitorCommand,
) result.Result[uptimeapp.MutationResult] {
	definition, definitionErr := domain.NewHTTPMonitorDefinition(
		command.Name,
		command.URL,
		time.Duration(command.IntervalSeconds)*time.Second,
		time.Duration(command.TimeoutSeconds)*time.Second,
	)
	if definitionErr != nil {
		return result.Err[uptimeapp.MutationResult](definitionErr)
	}

	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[uptimeapp.MutationResult](beginErr)
	}

	monitorID, createErr := createHTTPMonitorInTx(ctx, tx, command, definition)
	if createErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.MutationResult](createErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Scope.OrganizationID.String(),
		ProjectID:      command.Scope.ProjectID.String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionMonitorCreated,
		TargetType:     "uptime_monitor",
		TargetID:       monitorID,
		Metadata: map[string]string{
			"name": definition.Name().String(),
			"url":  definition.URL(),
			"type": string(domain.MonitorKindHTTP),
		},
	})
	if auditErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.MutationResult](auditErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[uptimeapp.MutationResult](commitErr)
	}

	return result.Ok(uptimeapp.MutationResult{MonitorID: monitorID})
}

func (store *Store) CreateHeartbeatMonitor(
	ctx context.Context,
	command uptimeapp.CreateHeartbeatMonitorCommand,
) result.Result[uptimeapp.MutationResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[uptimeapp.MutationResult](beginErr)
	}

	monitorID, endpointID, createErr := createHeartbeatMonitorInTx(ctx, tx, command)
	if createErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.MutationResult](createErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Scope.OrganizationID.String(),
		ProjectID:      command.Scope.ProjectID.String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionMonitorCreated,
		TargetType:     "uptime_monitor",
		TargetID:       monitorID,
		Metadata: map[string]string{
			"name":        command.Name,
			"endpoint_id": endpointID,
			"type":        string(domain.MonitorKindHeartbeat),
		},
	})
	if auditErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.MutationResult](auditErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[uptimeapp.MutationResult](commitErr)
	}

	return result.Ok(uptimeapp.MutationResult{
		MonitorID:  monitorID,
		EndpointID: endpointID,
	})
}

func (store *Store) ClaimDueHTTPMonitors(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]uptimeapp.HTTPMonitorCheckTarget] {
	sqlQuery := `
with due as (
  select id
  from uptime_monitors
  where enabled = true
    and monitor_type = 'http'
    and next_check_at <= $1
    and (check_lease_until is null or check_lease_until <= $1)
  order by next_check_at asc
  limit $2
  for update skip locked
),
claimed as (
  update uptime_monitors m
  set check_lease_until = $3
  from due
  where m.id = due.id
  returning
    m.id::text,
    m.organization_id::text,
    m.project_id::text,
    m.target_url,
    m.timeout_seconds
)
select id, organization_id, project_id, target_url, timeout_seconds
from claimed
`
	rows, queryErr := store.pool.Query(ctx, sqlQuery, now, limit, now.Add(monitorCheckLease))
	if queryErr != nil {
		return result.Err[[]uptimeapp.HTTPMonitorCheckTarget](queryErr)
	}
	defer rows.Close()

	targets := []uptimeapp.HTTPMonitorCheckTarget{}
	for rows.Next() {
		target, scanErr := scanHTTPMonitorCheckTarget(rows)
		if scanErr != nil {
			return result.Err[[]uptimeapp.HTTPMonitorCheckTarget](scanErr)
		}

		targets = append(targets, target)
	}

	if rows.Err() != nil {
		return result.Err[[]uptimeapp.HTTPMonitorCheckTarget](rows.Err())
	}

	return result.Ok(targets)
}

func (store *Store) ClaimOverdueHeartbeatMonitors(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]uptimeapp.HeartbeatTimeoutTarget] {
	sqlQuery := `
with due as (
  select id
  from uptime_monitors
  where enabled = true
    and monitor_type = 'heartbeat'
    and next_check_at <= $1
    and (check_lease_until is null or check_lease_until <= $1)
  order by next_check_at asc
  limit $2
  for update skip locked
),
claimed as (
  update uptime_monitors m
  set check_lease_until = $3
  from due
  where m.id = due.id
  returning
    m.id::text,
    m.organization_id::text,
    m.project_id::text
)
select id, organization_id, project_id
from claimed
`
	rows, queryErr := store.pool.Query(ctx, sqlQuery, now, limit, now.Add(monitorCheckLease))
	if queryErr != nil {
		return result.Err[[]uptimeapp.HeartbeatTimeoutTarget](queryErr)
	}
	defer rows.Close()

	targets := []uptimeapp.HeartbeatTimeoutTarget{}
	for rows.Next() {
		target, scanErr := scanHeartbeatTimeoutTarget(rows)
		if scanErr != nil {
			return result.Err[[]uptimeapp.HeartbeatTimeoutTarget](scanErr)
		}

		targets = append(targets, target)
	}

	if rows.Err() != nil {
		return result.Err[[]uptimeapp.HeartbeatTimeoutTarget](rows.Err())
	}

	return result.Ok(targets)
}

func (store *Store) RecordHTTPMonitorCheck(
	ctx context.Context,
	observation uptimeapp.HTTPCheckObservation,
) result.Result[uptimeapp.HTTPCheckResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[uptimeapp.HTTPCheckResult](beginErr)
	}

	checkResult := recordHTTPMonitorCheckInTx(ctx, tx, observation)
	check, checkErr := checkResult.Value()
	if checkErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.HTTPCheckResult](checkErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[uptimeapp.HTTPCheckResult](commitErr)
	}

	return result.Ok(check)
}

func (store *Store) RecordHeartbeatCheckIn(
	ctx context.Context,
	command uptimeapp.HeartbeatCheckInCommand,
) result.Result[uptimeapp.HeartbeatCheckInResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[uptimeapp.HeartbeatCheckInResult](beginErr)
	}

	checkResult := recordHeartbeatCheckInInTx(ctx, tx, command)
	check, checkErr := checkResult.Value()
	if checkErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.HeartbeatCheckInResult](checkErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[uptimeapp.HeartbeatCheckInResult](commitErr)
	}

	return result.Ok(check)
}

func (store *Store) RecordHeartbeatTimeout(
	ctx context.Context,
	observation uptimeapp.HeartbeatTimeoutObservation,
) result.Result[uptimeapp.HeartbeatTimeoutResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[uptimeapp.HeartbeatTimeoutResult](beginErr)
	}

	timeoutResult := recordHeartbeatTimeoutInTx(ctx, tx, observation)
	timeout, timeoutErr := timeoutResult.Value()
	if timeoutErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[uptimeapp.HeartbeatTimeoutResult](timeoutErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[uptimeapp.HeartbeatTimeoutResult](commitErr)
	}

	return result.Ok(timeout)
}

type monitorPersistenceState struct {
	currentState          domain.MonitorState
	intervalSeconds       int
	heartbeatGraceSeconds int
}

func createHTTPMonitorInTx(
	ctx context.Context,
	tx pgx.Tx,
	command uptimeapp.CreateHTTPMonitorCommand,
	definition domain.HTTPMonitorDefinition,
) (string, error) {
	monitorID, monitorIDErr := randomUUID()
	if monitorIDErr != nil {
		return "", monitorIDErr
	}

	now := time.Now().UTC()
	query := `
insert into uptime_monitors (
  id,
  organization_id,
  project_id,
  monitor_type,
  name,
  target_url,
  interval_seconds,
  timeout_seconds,
  next_check_at,
  created_by_operator_id,
  created_at,
  updated_at
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11
)
`
	_, execErr := tx.Exec(
		ctx,
		query,
		monitorID,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		string(domain.MonitorKindHTTP),
		definition.Name().String(),
		definition.URL(),
		int(definition.Interval().Duration()/time.Second),
		int(definition.Timeout().Duration()/time.Second),
		now,
		command.ActorID,
		now,
	)
	if execErr != nil {
		return "", execErr
	}

	return monitorID, nil
}

func createHeartbeatMonitorInTx(
	ctx context.Context,
	tx pgx.Tx,
	command uptimeapp.CreateHeartbeatMonitorCommand,
) (string, string, error) {
	monitorID, monitorIDErr := randomUUID()
	if monitorIDErr != nil {
		return "", "", monitorIDErr
	}

	endpointID, endpointIDErr := randomUUID()
	if endpointIDErr != nil {
		return "", "", endpointIDErr
	}

	definition, definitionErr := domain.NewHeartbeatMonitorDefinition(
		command.Name,
		endpointID,
		time.Duration(command.IntervalSeconds)*time.Second,
		time.Duration(command.GraceSeconds)*time.Second,
	)
	if definitionErr != nil {
		return "", "", definitionErr
	}

	now := time.Now().UTC()
	nextCheckAt := now.
		Add(definition.Interval().Duration()).
		Add(definition.Grace().Duration())

	query := `
insert into uptime_monitors (
  id,
  organization_id,
  project_id,
  monitor_type,
  name,
  target_url,
  heartbeat_endpoint_id,
  interval_seconds,
  timeout_seconds,
  heartbeat_grace_seconds,
  next_check_at,
  created_by_operator_id,
  created_at,
  updated_at
) values (
  $1, $2, $3, $4, $5, null, $6, $7, $8, $9, $10, $11, $12, $12
)
`
	_, execErr := tx.Exec(
		ctx,
		query,
		monitorID,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		string(domain.MonitorKindHeartbeat),
		definition.Name().String(),
		definition.EndpointID().String(),
		int(definition.Interval().Duration()/time.Second),
		1,
		int(definition.Grace().Duration()/time.Second),
		nextCheckAt,
		command.ActorID,
		now,
	)
	if execErr != nil {
		return "", "", execErr
	}

	return monitorID, definition.EndpointID().String(), nil
}

func recordHTTPMonitorCheckInTx(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HTTPCheckObservation,
) result.Result[uptimeapp.HTTPCheckResult] {
	stateResult := lockMonitorState(ctx, tx, observation)
	state, stateErr := stateResult.Value()
	if stateErr != nil {
		return result.Err[uptimeapp.HTTPCheckResult](stateErr)
	}

	checkID, checkErr := insertMonitorCheck(ctx, tx, observation)
	if checkErr != nil {
		return result.Err[uptimeapp.HTTPCheckResult](checkErr)
	}

	updateErr := updateMonitorAfterCheck(ctx, tx, observation, state.intervalSeconds)
	if updateErr != nil {
		return result.Err[uptimeapp.HTTPCheckResult](updateErr)
	}

	checkResult := uptimeapp.HTTPCheckResult{
		MonitorID:     observation.MonitorID.String(),
		PreviousState: string(state.currentState),
		CurrentState:  string(observation.State),
	}
	if !domain.MonitorStateChanged(state.currentState, observation.State) {
		return result.Ok(checkResult)
	}

	incidentResult := applyMonitorIncidentTransition(ctx, tx, observation, checkID)
	incident, incidentErr := incidentResult.Value()
	if incidentErr != nil {
		return result.Err[uptimeapp.HTTPCheckResult](incidentErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: observation.Scope.OrganizationID.String(),
		ProjectID:      observation.Scope.ProjectID.String(),
		Action:         auditapp.ActionMonitorStateChanged,
		TargetType:     "uptime_monitor",
		TargetID:       observation.MonitorID.String(),
		Metadata: map[string]string{
			"from_state": string(state.currentState),
			"to_state":   string(observation.State),
		},
	})
	if auditErr != nil {
		return result.Err[uptimeapp.HTTPCheckResult](auditErr)
	}

	checkResult.IncidentOpened = incident.opened
	checkResult.IncidentClosed = incident.closed

	return result.Ok(checkResult)
}

func recordHeartbeatCheckInInTx(
	ctx context.Context,
	tx pgx.Tx,
	command uptimeapp.HeartbeatCheckInCommand,
) result.Result[uptimeapp.HeartbeatCheckInResult] {
	stateResult := lockHeartbeatMonitorStateByEndpoint(ctx, tx, command.EndpointID)
	state, stateErr := stateResult.Value()
	if stateErr != nil {
		return result.Err[uptimeapp.HeartbeatCheckInResult](stateErr)
	}

	observation := uptimeapp.HTTPCheckObservation{
		Scope:      state.scope,
		MonitorID:  state.monitorID,
		CheckedAt:  command.CheckedAt,
		State:      domain.MonitorStateUp,
		StatusCode: 0,
		Duration:   0,
	}
	checkID, checkErr := insertMonitorCheck(ctx, tx, observation)
	if checkErr != nil {
		return result.Err[uptimeapp.HeartbeatCheckInResult](checkErr)
	}

	updateErr := updateHeartbeatAfterCheckIn(ctx, tx, observation, state)
	if updateErr != nil {
		return result.Err[uptimeapp.HeartbeatCheckInResult](updateErr)
	}

	checkResult := uptimeapp.HeartbeatCheckInResult{
		MonitorID:     state.monitorID.String(),
		PreviousState: string(state.currentState),
		CurrentState:  string(domain.MonitorStateUp),
	}
	if !domain.MonitorStateChanged(state.currentState, domain.MonitorStateUp) {
		return result.Ok(checkResult)
	}

	closed, closeErr := closeOpenMonitorIncident(ctx, tx, observation, checkID)
	if closeErr != nil {
		return result.Err[uptimeapp.HeartbeatCheckInResult](closeErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: state.scope.OrganizationID.String(),
		ProjectID:      state.scope.ProjectID.String(),
		Action:         auditapp.ActionMonitorStateChanged,
		TargetType:     "uptime_monitor",
		TargetID:       state.monitorID.String(),
		Metadata: map[string]string{
			"from_state": string(state.currentState),
			"to_state":   string(domain.MonitorStateUp),
		},
	})
	if auditErr != nil {
		return result.Err[uptimeapp.HeartbeatCheckInResult](auditErr)
	}

	checkResult.IncidentClosed = closed

	return result.Ok(checkResult)
}

func recordHeartbeatTimeoutInTx(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HeartbeatTimeoutObservation,
) result.Result[uptimeapp.HeartbeatTimeoutResult] {
	stateResult := lockMonitorState(ctx, tx, monitorObservationFromHeartbeatTimeout(observation))
	state, stateErr := stateResult.Value()
	if stateErr != nil {
		return result.Err[uptimeapp.HeartbeatTimeoutResult](stateErr)
	}

	checkObservation := monitorObservationFromHeartbeatTimeout(observation)
	checkID, checkErr := insertMonitorCheck(ctx, tx, checkObservation)
	if checkErr != nil {
		return result.Err[uptimeapp.HeartbeatTimeoutResult](checkErr)
	}

	updateErr := updateMonitorAfterCheck(ctx, tx, checkObservation, state.intervalSeconds)
	if updateErr != nil {
		return result.Err[uptimeapp.HeartbeatTimeoutResult](updateErr)
	}

	timeoutResult := uptimeapp.HeartbeatTimeoutResult{
		MonitorID:       observation.MonitorID.String(),
		PreviousState:   string(state.currentState),
		CurrentState:    string(domain.MonitorStateDown),
		AlreadyDown:     state.currentState == domain.MonitorStateDown,
		TimeoutRecorded: true,
	}
	if !domain.MonitorStateChanged(state.currentState, domain.MonitorStateDown) {
		return result.Ok(timeoutResult)
	}

	opened, openedErr := ensureOpenMonitorIncident(ctx, tx, checkObservation, checkID)
	if openedErr != nil {
		return result.Err[uptimeapp.HeartbeatTimeoutResult](openedErr)
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: observation.Scope.OrganizationID.String(),
		ProjectID:      observation.Scope.ProjectID.String(),
		Action:         auditapp.ActionMonitorStateChanged,
		TargetType:     "uptime_monitor",
		TargetID:       observation.MonitorID.String(),
		Metadata: map[string]string{
			"from_state": string(state.currentState),
			"to_state":   string(domain.MonitorStateDown),
		},
	})
	if auditErr != nil {
		return result.Err[uptimeapp.HeartbeatTimeoutResult](auditErr)
	}

	timeoutResult.IncidentOpened = opened

	return result.Ok(timeoutResult)
}

func lockMonitorState(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HTTPCheckObservation,
) result.Result[monitorPersistenceState] {
	query := `
select current_state, interval_seconds, heartbeat_grace_seconds
from uptime_monitors
where organization_id = $1
  and project_id = $2
  and id = $3
for update
`
	var stateText string
	var intervalSeconds int
	var heartbeatGraceSeconds sql.NullInt64
	scanErr := tx.QueryRow(
		ctx,
		query,
		observation.Scope.OrganizationID.String(),
		observation.Scope.ProjectID.String(),
		observation.MonitorID.String(),
	).Scan(&stateText, &intervalSeconds, &heartbeatGraceSeconds)
	if scanErr != nil {
		return result.Err[monitorPersistenceState](scanErr)
	}

	state, stateErr := domain.ParseMonitorState(stateText)
	if stateErr != nil {
		return result.Err[monitorPersistenceState](stateErr)
	}

	return result.Ok(monitorPersistenceState{
		currentState:          state,
		intervalSeconds:       intervalSeconds,
		heartbeatGraceSeconds: int(heartbeatGraceSeconds.Int64),
	})
}

type heartbeatPersistenceState struct {
	scope                 uptimeapp.Scope
	monitorID             domain.MonitorID
	currentState          domain.MonitorState
	intervalSeconds       int
	heartbeatGraceSeconds int
}

func lockHeartbeatMonitorStateByEndpoint(
	ctx context.Context,
	tx pgx.Tx,
	endpointID string,
) result.Result[heartbeatPersistenceState] {
	query := `
select
  id::text,
  organization_id::text,
  project_id::text,
  current_state,
  interval_seconds,
  heartbeat_grace_seconds
from uptime_monitors
where monitor_type = 'heartbeat'
  and heartbeat_endpoint_id = $1
  and enabled = true
for update
`
	var monitorIDText string
	var organizationIDText string
	var projectIDText string
	var stateText string
	var intervalSeconds int
	var heartbeatGraceSeconds int
	scanErr := tx.QueryRow(ctx, query, endpointID).Scan(
		&monitorIDText,
		&organizationIDText,
		&projectIDText,
		&stateText,
		&intervalSeconds,
		&heartbeatGraceSeconds,
	)
	if scanErr != nil {
		return result.Err[heartbeatPersistenceState](scanErr)
	}

	monitorID, monitorIDErr := domain.NewMonitorID(monitorIDText)
	if monitorIDErr != nil {
		return result.Err[heartbeatPersistenceState](monitorIDErr)
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return result.Err[heartbeatPersistenceState](organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return result.Err[heartbeatPersistenceState](projectErr)
	}

	state, stateErr := domain.ParseMonitorState(stateText)
	if stateErr != nil {
		return result.Err[heartbeatPersistenceState](stateErr)
	}

	return result.Ok(heartbeatPersistenceState{
		scope: uptimeapp.Scope{
			OrganizationID: organizationID,
			ProjectID:      projectID,
		},
		monitorID:             monitorID,
		currentState:          state,
		intervalSeconds:       intervalSeconds,
		heartbeatGraceSeconds: heartbeatGraceSeconds,
	})
}

func insertMonitorCheck(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HTTPCheckObservation,
) (string, error) {
	checkID, checkIDErr := randomUUID()
	if checkIDErr != nil {
		return "", checkIDErr
	}

	query := `
insert into uptime_monitor_checks (
  id,
  organization_id,
  project_id,
  monitor_id,
  status,
  http_status,
  duration_ms,
  error,
  checked_at
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9
)
`
	_, execErr := tx.Exec(
		ctx,
		query,
		checkID,
		observation.Scope.OrganizationID.String(),
		observation.Scope.ProjectID.String(),
		observation.MonitorID.String(),
		string(observation.State),
		nullableHTTPStatus(observation.StatusCode),
		float64(observation.Duration)/float64(time.Millisecond),
		observation.Error,
		observation.CheckedAt,
	)
	if execErr != nil {
		return "", execErr
	}

	return checkID, nil
}

func updateMonitorAfterCheck(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HTTPCheckObservation,
	intervalSeconds int,
) error {
	query := `
update uptime_monitors
set current_state = $4,
    last_checked_at = $5,
    next_check_at = $6,
    check_lease_until = null,
    updated_at = $5
where organization_id = $1
  and project_id = $2
  and id = $3
`
	nextCheckAt := observation.CheckedAt.Add(time.Duration(intervalSeconds) * time.Second)
	tag, execErr := tx.Exec(
		ctx,
		query,
		observation.Scope.OrganizationID.String(),
		observation.Scope.ProjectID.String(),
		observation.MonitorID.String(),
		string(observation.State),
		observation.CheckedAt,
		nextCheckAt,
	)
	if execErr != nil {
		return execErr
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("monitor not found")
	}

	return nil
}

func updateHeartbeatAfterCheckIn(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HTTPCheckObservation,
	state heartbeatPersistenceState,
) error {
	query := `
update uptime_monitors
set current_state = $4,
    last_checked_at = $5,
    last_check_in_at = $5,
    next_check_at = $6,
    check_lease_until = null,
    updated_at = $5
where organization_id = $1
  and project_id = $2
  and id = $3
`
	delay := time.Duration(state.intervalSeconds+state.heartbeatGraceSeconds) * time.Second
	nextCheckAt := observation.CheckedAt.Add(delay)
	tag, execErr := tx.Exec(
		ctx,
		query,
		observation.Scope.OrganizationID.String(),
		observation.Scope.ProjectID.String(),
		observation.MonitorID.String(),
		string(observation.State),
		observation.CheckedAt,
		nextCheckAt,
	)
	if execErr != nil {
		return execErr
	}

	if tag.RowsAffected() != 1 {
		return fmt.Errorf("monitor not found")
	}

	return nil
}

type incidentTransition struct {
	opened bool
	closed bool
}

func applyMonitorIncidentTransition(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HTTPCheckObservation,
	checkID string,
) result.Result[incidentTransition] {
	if observation.State == domain.MonitorStateDown {
		opened, openedErr := ensureOpenMonitorIncident(ctx, tx, observation, checkID)
		if openedErr != nil {
			return result.Err[incidentTransition](openedErr)
		}

		return result.Ok(incidentTransition{opened: opened})
	}

	if observation.State == domain.MonitorStateUp {
		closed, closedErr := closeOpenMonitorIncident(ctx, tx, observation, checkID)
		if closedErr != nil {
			return result.Err[incidentTransition](closedErr)
		}

		return result.Ok(incidentTransition{closed: closed})
	}

	return result.Ok(incidentTransition{})
}

func ensureOpenMonitorIncident(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HTTPCheckObservation,
	checkID string,
) (bool, error) {
	existsQuery := `
select exists(
  select 1
  from uptime_monitor_incidents
  where monitor_id = $1
    and resolved_at is null
)
`
	var exists bool
	scanErr := tx.QueryRow(ctx, existsQuery, observation.MonitorID.String()).Scan(&exists)
	if scanErr != nil {
		return false, scanErr
	}

	if exists {
		return false, nil
	}

	incidentID, incidentIDErr := randomUUID()
	if incidentIDErr != nil {
		return false, incidentIDErr
	}

	insertQuery := `
insert into uptime_monitor_incidents (
  id,
  organization_id,
  project_id,
  monitor_id,
  opened_at,
  last_check_id,
  reason
) values (
  $1, $2, $3, $4, $5, $6, $7
)
`
	_, execErr := tx.Exec(
		ctx,
		insertQuery,
		incidentID,
		observation.Scope.OrganizationID.String(),
		observation.Scope.ProjectID.String(),
		observation.MonitorID.String(),
		observation.CheckedAt,
		checkID,
		monitorIncidentReason(observation),
	)
	if execErr != nil {
		return false, execErr
	}

	return true, nil
}

func closeOpenMonitorIncident(
	ctx context.Context,
	tx pgx.Tx,
	observation uptimeapp.HTTPCheckObservation,
	checkID string,
) (bool, error) {
	query := `
update uptime_monitor_incidents
set resolved_at = $2,
    last_check_id = $3
where monitor_id = $1
  and resolved_at is null
`
	tag, execErr := tx.Exec(ctx, query, observation.MonitorID.String(), observation.CheckedAt, checkID)
	if execErr != nil {
		return false, execErr
	}

	return tag.RowsAffected() > 0, nil
}

func monitorIncidentReason(observation uptimeapp.HTTPCheckObservation) string {
	if observation.Error != "" {
		return observation.Error
	}

	if observation.StatusCode > 0 {
		return fmt.Sprintf("HTTP %d", observation.StatusCode)
	}

	return "monitor check failed"
}

func monitorObservationFromHeartbeatTimeout(
	observation uptimeapp.HeartbeatTimeoutObservation,
) uptimeapp.HTTPCheckObservation {
	return uptimeapp.HTTPCheckObservation{
		Scope:     observation.Scope,
		MonitorID: observation.MonitorID,
		CheckedAt: observation.CheckedAt,
		State:     domain.MonitorStateDown,
		Error:     observation.Error,
	}
}

func scanMonitorView(scanner rowScanner) (uptimeapp.MonitorView, error) {
	var view uptimeapp.MonitorView
	var intervalSeconds int
	var timeoutSeconds int
	var heartbeatGraceSeconds sql.NullInt64
	var lastCheckedAt sql.NullTime
	var lastCheckInAt sql.NullTime
	var nextCheckAt time.Time
	scanErr := scanner.Scan(
		&view.ID,
		&view.Kind,
		&view.Name,
		&view.URL,
		&view.EndpointID,
		&view.State,
		&view.Enabled,
		&intervalSeconds,
		&timeoutSeconds,
		&heartbeatGraceSeconds,
		&lastCheckedAt,
		&lastCheckInAt,
		&nextCheckAt,
		&view.LastStatusCode,
		&view.LastError,
		&view.OpenIncidentID,
	)
	if scanErr != nil {
		return uptimeapp.MonitorView{}, scanErr
	}

	view.Interval = formatSeconds(intervalSeconds)
	view.Timeout = formatSeconds(timeoutSeconds)
	view.Grace = formatOptionalSeconds(heartbeatGraceSeconds)
	view.LastCheckedAt = formatOptionalTime(lastCheckedAt)
	view.LastCheckInAt = formatOptionalTime(lastCheckInAt)
	view.NextCheckAt = formatTime(nextCheckAt)
	view.CheckInPath = heartbeatCheckInPath(view.EndpointID)

	return view, nil
}

func heartbeatCheckInPath(endpointID string) string {
	if endpointID == "" {
		return ""
	}

	return "/api/heartbeat/" + endpointID
}

func scanHTTPMonitorCheckTarget(scanner rowScanner) (uptimeapp.HTTPMonitorCheckTarget, error) {
	var monitorIDText string
	var organizationIDText string
	var projectIDText string
	var targetURLText string
	var timeoutSeconds int
	scanErr := scanner.Scan(
		&monitorIDText,
		&organizationIDText,
		&projectIDText,
		&targetURLText,
		&timeoutSeconds,
	)
	if scanErr != nil {
		return uptimeapp.HTTPMonitorCheckTarget{}, scanErr
	}

	monitorID, monitorIDErr := domain.NewMonitorID(monitorIDText)
	if monitorIDErr != nil {
		return uptimeapp.HTTPMonitorCheckTarget{}, monitorIDErr
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return uptimeapp.HTTPMonitorCheckTarget{}, organizationErr
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return uptimeapp.HTTPMonitorCheckTarget{}, projectErr
	}

	destinationResult := outbound.ParseDestinationURL(targetURLText)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		return uptimeapp.HTTPMonitorCheckTarget{}, destinationErr
	}

	timeout, timeoutErr := domain.NewMonitorTimeout(time.Duration(timeoutSeconds) * time.Second)
	if timeoutErr != nil {
		return uptimeapp.HTTPMonitorCheckTarget{}, timeoutErr
	}

	return uptimeapp.HTTPMonitorCheckTarget{
		Scope: uptimeapp.Scope{
			OrganizationID: organizationID,
			ProjectID:      projectID,
		},
		MonitorID: monitorID,
		URL:       destination,
		Timeout:   timeout.Duration(),
	}, nil
}

func scanHeartbeatTimeoutTarget(scanner rowScanner) (uptimeapp.HeartbeatTimeoutTarget, error) {
	var monitorIDText string
	var organizationIDText string
	var projectIDText string
	scanErr := scanner.Scan(
		&monitorIDText,
		&organizationIDText,
		&projectIDText,
	)
	if scanErr != nil {
		return uptimeapp.HeartbeatTimeoutTarget{}, scanErr
	}

	monitorID, monitorIDErr := domain.NewMonitorID(monitorIDText)
	if monitorIDErr != nil {
		return uptimeapp.HeartbeatTimeoutTarget{}, monitorIDErr
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return uptimeapp.HeartbeatTimeoutTarget{}, organizationErr
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return uptimeapp.HeartbeatTimeoutTarget{}, projectErr
	}

	return uptimeapp.HeartbeatTimeoutTarget{
		Scope: uptimeapp.Scope{
			OrganizationID: organizationID,
			ProjectID:      projectID,
		},
		MonitorID: monitorID,
	}, nil
}

func nullableHTTPStatus(statusCode int) any {
	if statusCode <= 0 {
		return nil
	}

	return statusCode
}

func formatSeconds(seconds int) string {
	duration := time.Duration(seconds) * time.Second
	return duration.String()
}

func formatOptionalSeconds(seconds sql.NullInt64) string {
	if !seconds.Valid {
		return ""
	}

	return formatSeconds(int(seconds.Int64))
}

func formatOptionalTime(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}

	return formatTime(value.Time)
}

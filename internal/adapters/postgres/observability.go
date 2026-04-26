package postgres

import (
	"context"
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"

	observabilityapp "github.com/ivanzakutnii/error-tracker/internal/app/observability"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) QueueStatus(
	ctx context.Context,
	scope observabilityapp.Scope,
) result.Result[observabilityapp.QueueStatus] {
	query := `
select provider, status, count(*), min(next_attempt_at)
from notification_intents
where organization_id = $1
  and project_id = $2
group by provider, status
order by provider asc, status asc
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	)
	if queryErr != nil {
		return result.Err[observabilityapp.QueueStatus](queryErr)
	}
	defer rows.Close()

	groups, groupsErr := scanQueueGroups(rows)
	if groupsErr != nil {
		return result.Err[observabilityapp.QueueStatus](groupsErr)
	}

	return result.Ok(observabilityapp.QueueStatus{Groups: groups})
}

func (store *Store) AdminMetrics(
	ctx context.Context,
	scope observabilityapp.Scope,
) result.Result[observabilityapp.AdminMetrics] {
	query := `
select
  (select count(*) from events where organization_id = $1 and project_id = $2),
  (select count(*) from issues where organization_id = $1 and project_id = $2),
  (select count(*) from transaction_events where organization_id = $1 and project_id = $2),
  (select count(*) from uptime_monitors where organization_id = $1 and project_id = $2),
  (select count(*) from uptime_monitor_incidents where organization_id = $1 and project_id = $2),
  (select count(*) from uptime_status_pages where organization_id = $1 and project_id = $2),
  (select count(*) from notification_intents where organization_id = $1 and project_id = $2)
`
	var metrics observabilityapp.AdminMetrics
	scanErr := store.pool.QueryRow(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	).Scan(
		&metrics.Events,
		&metrics.Issues,
		&metrics.Transactions,
		&metrics.UptimeMonitors,
		&metrics.UptimeIncidents,
		&metrics.StatusPages,
		&metrics.NotificationIntents,
	)
	if scanErr != nil {
		return result.Err[observabilityapp.AdminMetrics](scanErr)
	}

	return result.Ok(metrics)
}

func scanQueueGroups(rows pgx.Rows) ([]observabilityapp.QueueGroup, error) {
	groups := []observabilityapp.QueueGroup{}
	for rows.Next() {
		group, groupErr := scanQueueGroup(rows)
		if groupErr != nil {
			return []observabilityapp.QueueGroup{}, groupErr
		}

		groups = append(groups, group)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return []observabilityapp.QueueGroup{}, rowsErr
	}

	return groups, nil
}

func scanQueueGroup(rows pgx.Rows) (observabilityapp.QueueGroup, error) {
	var group observabilityapp.QueueGroup
	var oldest sql.NullTime
	scanErr := rows.Scan(
		&group.Provider,
		&group.Status,
		&group.Count,
		&oldest,
	)
	if scanErr != nil {
		return observabilityapp.QueueGroup{}, scanErr
	}

	group.OldestNextAttemptAt = nullableTime(oldest)

	return group, nil
}

func nullableTime(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}

	return value.Time.UTC().Format(time.RFC3339)
}

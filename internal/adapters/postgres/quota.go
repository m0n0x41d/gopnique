package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	logapp "github.com/ivanzakutnii/error-tracker/internal/app/logs"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store txStore) CheckQuota(
	ctx context.Context,
	event domain.CanonicalEvent,
) result.Result[ingest.QuotaDecision] {
	snapshotResult := quotaSnapshotForScope(
		ctx,
		store.tx,
		event.OrganizationID(),
		event.ProjectID(),
		event.ReceivedAt().Time(),
	)
	snapshot, snapshotErr := snapshotResult.Value()
	if snapshotErr != nil {
		return result.Err[ingest.QuotaDecision](snapshotErr)
	}

	return result.Ok(quotaDecision(snapshot))
}

func (store txStore) CheckLogQuota(
	ctx context.Context,
	record domain.LogRecord,
	count int,
) result.Result[logapp.QuotaDecision] {
	snapshotResult := quotaSnapshotForScope(
		ctx,
		store.tx,
		record.OrganizationID(),
		record.ProjectID(),
		record.ReceivedAt().Time(),
	)
	snapshot, snapshotErr := snapshotResult.Value()
	if snapshotErr != nil {
		return result.Err[logapp.QuotaDecision](snapshotErr)
	}

	return result.Ok(logQuotaDecision(snapshot, count))
}

func (store *Store) projectQuotaPolicy(
	ctx context.Context,
	scope settingsapp.Scope,
) result.Result[settingsapp.QuotaPolicyView] {
	sql := `
select
  coalesce(oqp.enabled, false),
  coalesce(oqp.daily_event_limit, 0),
  coalesce(pqp.enabled, false),
  coalesce(pqp.daily_event_limit, 0)
from projects p
left join organization_quota_policies oqp on oqp.organization_id = p.organization_id
left join project_quota_policies pqp on pqp.project_id = p.id
where p.organization_id = $1
  and p.id = $2
`
	var view settingsapp.QuotaPolicyView
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	).Scan(
		&view.OrganizationEnabled,
		&view.OrganizationDailyLimit,
		&view.ProjectEnabled,
		&view.ProjectDailyLimit,
	)
	if scanErr != nil {
		return result.Err[settingsapp.QuotaPolicyView](scanErr)
	}

	return result.Ok(view)
}

type quotaSnapshot struct {
	projectEnabled      bool
	projectDailyLimit   int64
	projectDailyUsed    int64
	organizationEnabled bool
	organizationLimit   int64
	organizationUsed    int64
}

func quotaSnapshotForEvent(
	ctx context.Context,
	tx pgx.Tx,
	event domain.CanonicalEvent,
) result.Result[quotaSnapshot] {
	return quotaSnapshotForScope(
		ctx,
		tx,
		event.OrganizationID(),
		event.ProjectID(),
		event.ReceivedAt().Time(),
	)
}

func quotaSnapshotForScope(
	ctx context.Context,
	tx pgx.Tx,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	receivedAt time.Time,
) result.Result[quotaSnapshot] {
	sql := `
with bounds as (
  select
    date_trunc('day', $3::timestamptz) as start_at,
    date_trunc('day', $3::timestamptz) + '1 day'::interval as end_at
),
current_usage as (
  select
    coalesce(sum(s.event_count) filter (where s.project_id = $2), 0)::bigint as project_used,
    coalesce(sum(s.event_count), 0)::bigint as organization_used
  from bounds b
  left join project_hourly_stats s
    on s.organization_id = $1
    and s.bucket_at >= b.start_at
    and s.bucket_at < b.end_at
)
select
  coalesce(pqp.enabled, false),
  coalesce(pqp.daily_event_limit, 0),
  current_usage.project_used,
  coalesce(oqp.enabled, false),
  coalesce(oqp.daily_event_limit, 0),
  current_usage.organization_used
from current_usage
left join project_quota_policies pqp
  on pqp.organization_id = $1
  and pqp.project_id = $2
left join organization_quota_policies oqp
  on oqp.organization_id = $1
`
	var snapshot quotaSnapshot
	scanErr := tx.QueryRow(
		ctx,
		sql,
		organizationID.String(),
		projectID.String(),
		receivedAt,
	).Scan(
		&snapshot.projectEnabled,
		&snapshot.projectDailyLimit,
		&snapshot.projectDailyUsed,
		&snapshot.organizationEnabled,
		&snapshot.organizationLimit,
		&snapshot.organizationUsed,
	)
	if scanErr != nil {
		return result.Err[quotaSnapshot](scanErr)
	}

	return result.Ok(snapshot)
}

func quotaDecision(snapshot quotaSnapshot) ingest.QuotaDecision {
	if quotaExceeded(snapshot.organizationEnabled, snapshot.organizationUsed, snapshot.organizationLimit, 1) {
		return ingest.NewQuotaRejected("organization_quota_exceeded")
	}

	if quotaExceeded(snapshot.projectEnabled, snapshot.projectDailyUsed, snapshot.projectDailyLimit, 1) {
		return ingest.NewQuotaRejected("project_quota_exceeded")
	}

	return ingest.NewQuotaAllowed()
}

func logQuotaDecision(snapshot quotaSnapshot, count int) logapp.QuotaDecision {
	if count < 1 {
		count = 1
	}

	if quotaExceeded(snapshot.organizationEnabled, snapshot.organizationUsed, snapshot.organizationLimit, int64(count)) {
		return logapp.NewQuotaRejected("organization_quota_exceeded")
	}

	if quotaExceeded(snapshot.projectEnabled, snapshot.projectDailyUsed, snapshot.projectDailyLimit, int64(count)) {
		return logapp.NewQuotaRejected("project_quota_exceeded")
	}

	return logapp.NewQuotaAllowed()
}

func quotaExceeded(enabled bool, used int64, limit int64, next int64) bool {
	if !enabled {
		return false
	}

	return used+next > limit
}

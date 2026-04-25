package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store txStore) CheckQuota(
	ctx context.Context,
	event domain.CanonicalEvent,
) result.Result[ingest.QuotaDecision] {
	snapshotResult := quotaSnapshotForEvent(ctx, store.tx, event)
	snapshot, snapshotErr := snapshotResult.Value()
	if snapshotErr != nil {
		return result.Err[ingest.QuotaDecision](snapshotErr)
	}

	return result.Ok(quotaDecision(snapshot))
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
		event.OrganizationID().String(),
		event.ProjectID().String(),
		event.ReceivedAt().Time(),
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
	if snapshot.organizationEnabled && snapshot.organizationUsed >= snapshot.organizationLimit {
		return ingest.NewQuotaRejected("organization_quota_exceeded")
	}

	if snapshot.projectEnabled && snapshot.projectDailyUsed >= snapshot.projectDailyLimit {
		return ingest.NewQuotaRejected("project_quota_exceeded")
	}

	return ingest.NewQuotaAllowed()
}

package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	ratelimitapp "github.com/ivanzakutnii/error-tracker/internal/app/ratelimit"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) CheckRateLimit(
	ctx context.Context,
	command ratelimitapp.Command,
) result.Result[ratelimitapp.Decision] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[ratelimitapp.Decision](beginErr)
	}

	decisionResult := checkRateLimitInTx(ctx, tx, command)
	decision, decisionErr := decisionResult.Value()
	if decisionErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[ratelimitapp.Decision](decisionErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[ratelimitapp.Decision](commitErr)
	}

	return result.Ok(decision)
}

func (store *Store) projectRateLimitPolicy(
	ctx context.Context,
	scope settingsapp.Scope,
) result.Result[settingsapp.RateLimitPolicyView] {
	sql := `
select
  replace(pk.public_key::text, '-', ''),
  coalesce(policy.enabled, false),
  coalesce(policy.window_seconds, 0),
  coalesce(policy.event_limit, 0)
from project_keys pk
join projects p on p.id = pk.project_id
left join project_key_rate_limit_policies policy on policy.project_key_id = pk.id
where p.organization_id = $1
  and pk.project_id = $2
  and pk.active = true
order by pk.created_at asc
limit 1
`
	var view settingsapp.RateLimitPolicyView
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	).Scan(
		&view.PublicKey,
		&view.Enabled,
		&view.WindowSeconds,
		&view.EventLimit,
	)
	if scanErr != nil {
		return result.Err[settingsapp.RateLimitPolicyView](scanErr)
	}

	view.RequestsPerMin = requestsPerMinute(view.EventLimit, view.WindowSeconds)

	return result.Ok(view)
}

type rateLimitPolicySnapshot struct {
	projectKeyID string
	organization string
	project      string
	enabled      bool
	window       int
	limit        int
}

func checkRateLimitInTx(
	ctx context.Context,
	tx pgx.Tx,
	command ratelimitapp.Command,
) result.Result[ratelimitapp.Decision] {
	policyResult := rateLimitPolicyForCommand(ctx, tx, command)
	policy, policyErr := policyResult.Value()
	if policyErr != nil {
		return result.Err[ratelimitapp.Decision](policyErr)
	}

	if !policy.enabled {
		return result.Ok(ratelimitapp.NewAllowed())
	}

	bucket := rateLimitBucket(command.Now, policy.window)
	countResult := incrementRateLimitBucket(ctx, tx, policy, bucket, command.Now)
	count, countErr := countResult.Value()
	if countErr != nil {
		return result.Err[ratelimitapp.Decision](countErr)
	}

	if count <= policy.limit {
		return result.Ok(ratelimitapp.NewAllowed())
	}

	return result.Ok(ratelimitapp.NewRejected(
		"project_key_rate_limited",
		rateLimitRetryAfter(command.Now, bucket, policy.window),
	))
}

func rateLimitPolicyForCommand(
	ctx context.Context,
	tx pgx.Tx,
	command ratelimitapp.Command,
) result.Result[rateLimitPolicySnapshot] {
	sql := `
select
  pk.id,
  p.organization_id,
  pk.project_id,
  coalesce(policy.enabled, false),
  coalesce(policy.window_seconds, 60),
  coalesce(policy.event_limit, 600)
from project_keys pk
join projects p on p.id = pk.project_id
left join project_key_rate_limit_policies policy on policy.project_key_id = pk.id
where p.organization_id = $1
  and pk.project_id = $2
  and pk.public_key = $3
  and pk.active = true
`
	var policy rateLimitPolicySnapshot
	scanErr := tx.QueryRow(
		ctx,
		sql,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		command.PublicKey.String(),
	).Scan(
		&policy.projectKeyID,
		&policy.organization,
		&policy.project,
		&policy.enabled,
		&policy.window,
		&policy.limit,
	)
	if scanErr != nil {
		return result.Err[rateLimitPolicySnapshot](scanErr)
	}

	return result.Ok(policy)
}

func incrementRateLimitBucket(
	ctx context.Context,
	tx pgx.Tx,
	policy rateLimitPolicySnapshot,
	bucket time.Time,
	now time.Time,
) result.Result[int] {
	sql := `
insert into project_key_rate_limit_buckets (
  project_key_id,
  organization_id,
  project_id,
  bucket_at,
  window_seconds,
  event_count,
  created_at,
  updated_at
) values ($1, $2, $3, $4, $5, 1, $6, $6)
on conflict (project_key_id, bucket_at, window_seconds) do update
set
  event_count = project_key_rate_limit_buckets.event_count + 1,
  updated_at = excluded.updated_at
returning event_count
`
	var count int
	scanErr := tx.QueryRow(
		ctx,
		sql,
		policy.projectKeyID,
		policy.organization,
		policy.project,
		bucket,
		policy.window,
		now.UTC(),
	).Scan(&count)
	if scanErr != nil {
		return result.Err[int](scanErr)
	}

	return result.Ok(count)
}

func rateLimitBucket(now time.Time, windowSeconds int) time.Time {
	unix := now.UTC().Unix()
	window := int64(windowSeconds)
	return time.Unix((unix/window)*window, 0).UTC()
}

func rateLimitRetryAfter(now time.Time, bucket time.Time, windowSeconds int) time.Duration {
	resetAt := bucket.Add(time.Duration(windowSeconds) * time.Second)
	retryAfter := resetAt.Sub(now.UTC())
	if retryAfter < time.Second {
		return time.Second
	}

	return retryAfter
}

func requestsPerMinute(limit int, windowSeconds int) int {
	if limit <= 0 || windowSeconds <= 0 {
		return 0
	}

	return (limit * 60) / windowSeconds
}

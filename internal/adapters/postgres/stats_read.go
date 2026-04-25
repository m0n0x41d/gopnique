package postgres

import (
	"context"
	"fmt"
	"time"

	statsapp "github.com/ivanzakutnii/error-tracker/internal/app/stats"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) ShowProjectStats(
	ctx context.Context,
	query statsapp.Query,
) result.Result[statsapp.View] {
	bucketsResult := store.projectStatsBuckets(ctx, query)
	buckets, bucketsErr := bucketsResult.Value()
	if bucketsErr != nil {
		return result.Err[statsapp.View](bucketsErr)
	}

	issuesResult := store.projectIssueStatusCounts(ctx, query.Scope)
	issues, issuesErr := issuesResult.Value()
	if issuesErr != nil {
		return result.Err[statsapp.View](issuesErr)
	}

	userReportsResult := store.projectUserReportCount(ctx, query.Scope)
	userReports, userReportsErr := userReportsResult.Value()
	if userReportsErr != nil {
		return result.Err[statsapp.View](userReportsErr)
	}

	return result.Ok(projectStatsView(query, buckets, issues, userReports))
}

type issueStatusCounts struct {
	unresolved int
	resolved   int
	ignored    int
}

func (store *Store) projectStatsBuckets(
	ctx context.Context,
	query statsapp.Query,
) result.Result[[]statsapp.BucketView] {
	bounds := statsBounds(query)
	sql := statsBucketSQL(query.Period)
	rows, queryErr := store.pool.Query(
		ctx,
		sql,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
		bounds.start,
		bounds.end,
	)
	if queryErr != nil {
		return result.Err[[]statsapp.BucketView](queryErr)
	}
	defer rows.Close()

	buckets := []statsapp.BucketView{}
	for rows.Next() {
		var bucketAt time.Time
		var bucket statsapp.BucketView
		scanErr := rows.Scan(
			&bucketAt,
			&bucket.EventCount,
			&bucket.IssueEvents,
			&bucket.TransactionEvents,
		)
		if scanErr != nil {
			return result.Err[[]statsapp.BucketView](scanErr)
		}

		bucket.Start = bucketAt.UTC().Format(time.RFC3339)
		bucket.Label = statsBucketLabel(query.Period, bucketAt)
		buckets = append(buckets, bucket)
	}

	if rows.Err() != nil {
		return result.Err[[]statsapp.BucketView](rows.Err())
	}

	return result.Ok(buckets)
}

func (store *Store) projectIssueStatusCounts(
	ctx context.Context,
	scope statsapp.Scope,
) result.Result[issueStatusCounts] {
	sql := `
select
  count(*) filter (where status = 'unresolved'),
  count(*) filter (where status = 'resolved'),
  count(*) filter (where status = 'ignored')
from issues
where organization_id = $1
  and project_id = $2
`
	var counts issueStatusCounts
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	).Scan(&counts.unresolved, &counts.resolved, &counts.ignored)
	if scanErr != nil {
		return result.Err[issueStatusCounts](scanErr)
	}

	return result.Ok(counts)
}

func (store *Store) projectUserReportCount(
	ctx context.Context,
	scope statsapp.Scope,
) result.Result[int] {
	sql := `
select count(*)
from user_reports
where organization_id = $1
  and project_id = $2
`
	var count int
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	).Scan(&count)
	if scanErr != nil {
		return result.Err[int](scanErr)
	}

	return result.Ok(count)
}

type statsRange struct {
	start time.Time
	end   time.Time
}

func statsBounds(query statsapp.Query) statsRange {
	if query.Period == statsapp.Period14d {
		end := midnightUTC(query.Now)
		return statsRange{start: end.AddDate(0, 0, -13), end: end}
	}

	end := query.Now.Truncate(time.Hour)
	return statsRange{start: end.Add(-23 * time.Hour), end: end}
}

func statsBucketSQL(period statsapp.Period) string {
	if period == statsapp.Period14d {
		return `
select
  series.bucket_at,
  coalesce(sum(s.event_count), 0),
  coalesce(sum(s.issue_event_count), 0),
  coalesce(sum(s.transaction_event_count), 0)
from generate_series($3::timestamptz, $4::timestamptz, '1 day'::interval) as series(bucket_at)
left join project_hourly_stats s
  on s.organization_id = $1
  and s.project_id = $2
  and s.bucket_at >= series.bucket_at
  and s.bucket_at < series.bucket_at + '1 day'::interval
group by series.bucket_at
order by series.bucket_at asc
`
	}

	return `
select
  series.bucket_at,
  coalesce(s.event_count, 0),
  coalesce(s.issue_event_count, 0),
  coalesce(s.transaction_event_count, 0)
from generate_series($3::timestamptz, $4::timestamptz, '1 hour'::interval) as series(bucket_at)
left join project_hourly_stats s
  on s.organization_id = $1
  and s.project_id = $2
  and s.bucket_at = series.bucket_at
order by series.bucket_at asc
`
}

func projectStatsView(
	query statsapp.Query,
	buckets []statsapp.BucketView,
	issues issueStatusCounts,
	userReports int,
) statsapp.View {
	view := statsapp.View{
		Period:           string(query.Period),
		Granularity:      query.Period.Granularity(),
		UnresolvedIssues: issues.unresolved,
		ResolvedIssues:   issues.resolved,
		IgnoredIssues:    issues.ignored,
		UserReports:      userReports,
		Buckets:          buckets,
	}

	for _, bucket := range buckets {
		view.TotalEvents += bucket.EventCount
		view.IssueEvents += bucket.IssueEvents
		view.TransactionEvents += bucket.TransactionEvents
		if bucket.EventCount > view.MaxBucketEvents {
			view.MaxBucketEvents = bucket.EventCount
		}
	}

	return view
}

func midnightUTC(value time.Time) time.Time {
	utc := value.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}

func statsBucketLabel(period statsapp.Period, value time.Time) string {
	if period == statsapp.Period14d {
		return value.UTC().Format("Jan 02")
	}

	return fmt.Sprintf("%02d:00", value.UTC().Hour())
}

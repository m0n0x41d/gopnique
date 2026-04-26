package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	performanceapp "github.com/ivanzakutnii/error-tracker/internal/app/performance"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) ListTransactionGroups(
	ctx context.Context,
	query performanceapp.Query,
) result.Result[performanceapp.ListView] {
	sql := `
select
  e.fingerprint,
  te.transaction_name,
  te.operation,
  count(*),
  avg(te.duration_ms),
  percentile_cont(0.95) within group (order by te.duration_ms),
  (array_agg(te.status order by te.received_at desc))[1],
  max(te.received_at)
from transaction_events te
join events e on e.id = te.event_id
where te.organization_id = $1
  and te.project_id = $2
group by e.fingerprint, te.transaction_name, te.operation
order by max(te.received_at) desc
limit $3
`
	rows, rowsErr := store.pool.Query(
		ctx,
		sql,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
		query.Limit,
	)
	if rowsErr != nil {
		return result.Err[performanceapp.ListView](rowsErr)
	}
	defer rows.Close()

	groups := []performanceapp.GroupSummaryView{}
	for rows.Next() {
		group, scanErr := scanTransactionGroupSummary(rows)
		if scanErr != nil {
			return result.Err[performanceapp.ListView](scanErr)
		}

		groups = append(groups, group)
	}

	if rows.Err() != nil {
		return result.Err[performanceapp.ListView](rows.Err())
	}

	return result.Ok(performanceapp.ListView{Groups: groups})
}

func (store *Store) ShowTransactionGroup(
	ctx context.Context,
	query performanceapp.DetailQuery,
) result.Result[performanceapp.DetailView] {
	summaryResult := store.transactionGroupSummary(ctx, query)
	summary, summaryErr := summaryResult.Value()
	if summaryErr != nil {
		return result.Err[performanceapp.DetailView](summaryErr)
	}

	eventsResult := store.transactionGroupEvents(ctx, query)
	events, eventsErr := eventsResult.Value()
	if eventsErr != nil {
		return result.Err[performanceapp.DetailView](eventsErr)
	}

	view := performanceapp.DetailView{
		ID:              summary.ID,
		Name:            summary.Name,
		Operation:       summary.Operation,
		Count:           summary.Count,
		AverageDuration: summary.AverageDuration,
		P95Duration:     summary.P95Duration,
		LatestStatus:    summary.LatestStatus,
		LatestSeen:      summary.LatestSeen,
		RecentEvents:    events,
	}

	return result.Ok(view)
}

func (store *Store) transactionGroupSummary(
	ctx context.Context,
	query performanceapp.DetailQuery,
) result.Result[performanceapp.GroupSummaryView] {
	sql := `
select
  e.fingerprint,
  te.transaction_name,
  te.operation,
  count(*),
  avg(te.duration_ms),
  percentile_cont(0.95) within group (order by te.duration_ms),
  (array_agg(te.status order by te.received_at desc))[1],
  max(te.received_at)
from transaction_events te
join events e on e.id = te.event_id
where te.organization_id = $1
  and te.project_id = $2
  and e.fingerprint = $3
group by e.fingerprint, te.transaction_name, te.operation
`
	var group transactionGroupRow
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
		query.GroupID,
	).Scan(
		&group.id,
		&group.name,
		&group.operation,
		&group.count,
		&group.averageDuration,
		&group.p95Duration,
		&group.latestStatus,
		&group.latestSeen,
	)
	if scanErr != nil {
		return result.Err[performanceapp.GroupSummaryView](scanErr)
	}

	return result.Ok(group.view())
}

func (store *Store) transactionGroupEvents(
	ctx context.Context,
	query performanceapp.DetailQuery,
) result.Result[[]performanceapp.EventView] {
	sql := `
select
  e.event_id::text,
  te.duration_ms,
  te.status,
  coalesce(te.trace_id, ''),
  coalesce(te.span_id, ''),
  te.span_count,
  te.received_at
from transaction_events te
join events e on e.id = te.event_id
where te.organization_id = $1
  and te.project_id = $2
  and e.fingerprint = $3
order by te.received_at desc
limit $4
`
	rows, rowsErr := store.pool.Query(
		ctx,
		sql,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
		query.GroupID,
		query.RecentLimit,
	)
	if rowsErr != nil {
		return result.Err[[]performanceapp.EventView](rowsErr)
	}
	defer rows.Close()

	events := []performanceapp.EventView{}
	for rows.Next() {
		event, scanErr := scanTransactionEvent(rows)
		if scanErr != nil {
			return result.Err[[]performanceapp.EventView](scanErr)
		}

		events = append(events, event)
	}

	if rows.Err() != nil {
		return result.Err[[]performanceapp.EventView](rows.Err())
	}

	return result.Ok(events)
}

type rowScanner interface {
	Scan(dest ...any) error
}

type transactionGroupRow struct {
	id              string
	name            string
	operation       string
	count           int
	averageDuration float64
	p95Duration     float64
	latestStatus    string
	latestSeen      time.Time
}

type transactionEventRow struct {
	eventID    string
	duration   float64
	status     string
	traceID    string
	spanID     string
	spanCount  int
	receivedAt time.Time
}

func scanTransactionGroupSummary(scanner rowScanner) (performanceapp.GroupSummaryView, error) {
	var row transactionGroupRow
	scanErr := scanner.Scan(
		&row.id,
		&row.name,
		&row.operation,
		&row.count,
		&row.averageDuration,
		&row.p95Duration,
		&row.latestStatus,
		&row.latestSeen,
	)
	if scanErr != nil {
		return performanceapp.GroupSummaryView{}, scanErr
	}

	return row.view(), nil
}

func scanTransactionEvent(scanner rowScanner) (performanceapp.EventView, error) {
	var row transactionEventRow
	scanErr := scanner.Scan(
		&row.eventID,
		&row.duration,
		&row.status,
		&row.traceID,
		&row.spanID,
		&row.spanCount,
		&row.receivedAt,
	)
	if scanErr != nil {
		return performanceapp.EventView{}, scanErr
	}

	return performanceapp.EventView{
		EventID:    row.eventID,
		Duration:   formatDurationMilliseconds(row.duration),
		Status:     row.status,
		TraceID:    row.traceID,
		SpanID:     row.spanID,
		SpanCount:  row.spanCount,
		ReceivedAt: formatTime(row.receivedAt),
	}, nil
}

func (row transactionGroupRow) view() performanceapp.GroupSummaryView {
	return performanceapp.GroupSummaryView{
		ID:              row.id,
		Name:            row.name,
		Operation:       row.operation,
		Count:           row.count,
		AverageDuration: formatDurationMilliseconds(row.averageDuration),
		P95Duration:     formatDurationMilliseconds(row.p95Duration),
		LatestStatus:    row.latestStatus,
		LatestSeen:      formatTime(row.latestSeen),
	}
}

func formatDurationMilliseconds(milliseconds float64) string {
	if milliseconds >= 1000 {
		return fmt.Sprintf("%.2fs", milliseconds/1000)
	}

	if milliseconds < 10 {
		return fmt.Sprintf("%.2fms", milliseconds)
	}

	return fmt.Sprintf("%.0fms", math.Round(milliseconds))
}

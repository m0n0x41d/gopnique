package postgres

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	logapp "github.com/ivanzakutnii/error-tracker/internal/app/logs"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) RunLogIngest(
	ctx context.Context,
	program logapp.IngestProgram,
) result.Result[logapp.IngestTransactionResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[logapp.IngestTransactionResult](beginErr)
	}

	ports := txStore{tx: tx}
	programResult := program(ctx, ports)
	value, programErr := programResult.Value()
	if programErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[logapp.IngestTransactionResult](programErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[logapp.IngestTransactionResult](commitErr)
	}

	return result.Ok(value)
}

func (store txStore) AppendLogRecords(
	ctx context.Context,
	records []domain.LogRecord,
) result.Result[logapp.AppendResult] {
	for _, record := range records {
		appendErr := store.appendLogRecord(ctx, record)
		if appendErr != nil {
			return result.Err[logapp.AppendResult](appendErr)
		}
	}

	return result.Ok(logapp.NewAppendResult(len(records)))
}

func (store txStore) appendLogRecord(
	ctx context.Context,
	record domain.LogRecord,
) error {
	recordID, recordIDErr := randomUUID()
	if recordIDErr != nil {
		return recordIDErr
	}

	resourceAttributes, resourceErr := json.Marshal(record.ResourceAttributes())
	if resourceErr != nil {
		return resourceErr
	}

	attributes, attributesErr := json.Marshal(record.Attributes())
	if attributesErr != nil {
		return attributesErr
	}

	query := `
insert into log_records (
  id,
  organization_id,
  project_id,
  timestamp_at,
  received_at,
  severity,
  body,
  logger,
  trace_id,
  span_id,
  release,
  environment,
  resource_attributes,
  attributes
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, coalesce($13::jsonb, '{}'::jsonb), coalesce($14::jsonb, '{}'::jsonb)
)
`
	_, execErr := store.tx.Exec(
		ctx,
		query,
		recordID,
		record.OrganizationID().String(),
		record.ProjectID().String(),
		record.Timestamp().Time(),
		record.ReceivedAt().Time(),
		record.Severity().String(),
		record.Body(),
		nullableText(record.Logger()),
		nullableText(record.TraceID()),
		nullableText(record.SpanID()),
		nullableText(record.Release()),
		nullableText(record.Environment()),
		resourceAttributes,
		attributes,
	)
	if execErr != nil {
		return execErr
	}

	return store.upsertProjectHourlyLogStats(ctx, record)
}

func (store txStore) upsertProjectHourlyLogStats(
	ctx context.Context,
	record domain.LogRecord,
) error {
	query := `
insert into project_hourly_stats (
  organization_id,
  project_id,
  bucket_at,
  event_count,
  issue_event_count,
  transaction_event_count,
  log_event_count
) values (
  $1,
  $2,
  date_trunc('hour', $3::timestamptz),
  1,
  0,
  0,
  1
)
on conflict (project_id, bucket_at) do update
set
  event_count = project_hourly_stats.event_count + excluded.event_count,
  log_event_count = project_hourly_stats.log_event_count + excluded.log_event_count
`
	_, execErr := store.tx.Exec(
		ctx,
		query,
		record.OrganizationID().String(),
		record.ProjectID().String(),
		record.ReceivedAt().Time(),
	)

	return execErr
}

func (store *Store) ListLogRecords(
	ctx context.Context,
	query logapp.Query,
) result.Result[logapp.ListView] {
	sql, args := logListSQL(query)
	rows, rowsErr := store.pool.Query(ctx, sql, args...)
	if rowsErr != nil {
		return result.Err[logapp.ListView](rowsErr)
	}
	defer rows.Close()

	logs := []logapp.RecordView{}
	for rows.Next() {
		record, scanErr := scanLogRecord(rows)
		if scanErr != nil {
			return result.Err[logapp.ListView](scanErr)
		}

		logs = append(logs, record)
	}

	if rows.Err() != nil {
		return result.Err[logapp.ListView](rows.Err())
	}

	view := logapp.ListView{
		Logs:    logs,
		Filters: filterView(query),
	}

	return result.Ok(view)
}

func (store *Store) ShowLogRecord(
	ctx context.Context,
	query logapp.DetailQuery,
) result.Result[logapp.DetailView] {
	sql := `
select
  id::text,
  timestamp_at,
  received_at,
  severity,
  body,
  coalesce(logger, ''),
  coalesce(trace_id, ''),
  coalesce(span_id, ''),
  coalesce(release, ''),
  coalesce(environment, ''),
  resource_attributes,
  attributes
from log_records
where organization_id = $1
  and project_id = $2
  and id = $3
`
	record, scanErr := scanLogRecord(store.pool.QueryRow(
		ctx,
		sql,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
		query.ID,
	))
	if scanErr != nil {
		return result.Err[logapp.DetailView](scanErr)
	}

	return result.Ok(logapp.DetailView{Record: record})
}

func logListSQL(query logapp.Query) (string, []any) {
	args := []any{
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
	}
	conditions := []string{
		"organization_id = $1",
		"project_id = $2",
	}

	conditions, args = appendTextFilter(conditions, args, "severity", query.Severity)
	conditions, args = appendTextFilter(conditions, args, "logger", query.Logger)
	conditions, args = appendTextFilter(conditions, args, "environment", query.Environment)
	conditions, args = appendTextFilter(conditions, args, "release", query.Release)
	conditions, args = appendJSONFilter(conditions, args, "resource_attributes", query.ResourceAttribute)
	conditions, args = appendJSONFilter(conditions, args, "attributes", query.LogAttribute)
	args = append(args, query.Limit)

	sql := `
select
  id::text,
  timestamp_at,
  received_at,
  severity,
  body,
  coalesce(logger, ''),
  coalesce(trace_id, ''),
  coalesce(span_id, ''),
  coalesce(release, ''),
  coalesce(environment, ''),
  resource_attributes,
  attributes
from log_records
where ` + strings.Join(conditions, "\n  and ") + `
order by received_at desc
limit $` + parameterIndex(len(args)) + `
`

	return sql, args
}

func appendTextFilter(
	conditions []string,
	args []any,
	column string,
	value string,
) ([]string, []any) {
	if strings.TrimSpace(value) == "" {
		return conditions, args
	}

	args = append(args, strings.TrimSpace(value))
	condition := column + " = $" + parameterIndex(len(args))
	conditions = append(conditions, condition)

	return conditions, args
}

func appendJSONFilter(
	conditions []string,
	args []any,
	column string,
	filter logapp.AttributeFilter,
) ([]string, []any) {
	if strings.TrimSpace(filter.Key) == "" || strings.TrimSpace(filter.Value) == "" {
		return conditions, args
	}

	args = append(args, strings.TrimSpace(filter.Key))
	keyParam := parameterIndex(len(args))
	args = append(args, strings.TrimSpace(filter.Value))
	valueParam := parameterIndex(len(args))
	condition := column + " ->> $" + keyParam + " = $" + valueParam
	conditions = append(conditions, condition)

	return conditions, args
}

func parameterIndex(value int) string {
	return strconv.Itoa(value)
}

type logRow struct {
	id                 string
	timestamp          time.Time
	receivedAt         time.Time
	severity           string
	body               string
	logger             string
	traceID            string
	spanID             string
	release            string
	environment        string
	resourceAttributes []byte
	attributes         []byte
}

func scanLogRecord(scanner rowScanner) (logapp.RecordView, error) {
	var row logRow
	scanErr := scanner.Scan(
		&row.id,
		&row.timestamp,
		&row.receivedAt,
		&row.severity,
		&row.body,
		&row.logger,
		&row.traceID,
		&row.spanID,
		&row.release,
		&row.environment,
		&row.resourceAttributes,
		&row.attributes,
	)
	if scanErr != nil {
		return logapp.RecordView{}, scanErr
	}

	return logapp.RecordView{
		ID:                 row.id,
		Timestamp:          formatTime(row.timestamp),
		ReceivedAt:         formatTime(row.receivedAt),
		Severity:           row.severity,
		Body:               row.body,
		Logger:             row.logger,
		TraceID:            row.traceID,
		SpanID:             row.spanID,
		Release:            row.release,
		Environment:        row.environment,
		ResourceAttributes: attributeViews(row.resourceAttributes),
		Attributes:         attributeViews(row.attributes),
	}, nil
}

func attributeViews(payload []byte) []logapp.AttributeView {
	values := map[string]string{}
	decodeErr := json.Unmarshal(payload, &values)
	if decodeErr != nil {
		return []logapp.AttributeView{}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	views := []logapp.AttributeView{}
	for _, key := range keys {
		value := values[key]
		views = append(views, logapp.AttributeView{
			Key:   key,
			Value: value,
		})
	}

	return views
}

func filterView(query logapp.Query) logapp.FilterView {
	return logapp.FilterView{
		Severity:           query.Severity,
		Logger:             query.Logger,
		Environment:        query.Environment,
		Release:            query.Release,
		ResourceKey:        query.ResourceAttribute.Key,
		ResourceValue:      query.ResourceAttribute.Value,
		AttributeKey:       query.LogAttribute.Key,
		AttributeValue:     query.LogAttribute.Value,
		HasResourceFilter:  query.ResourceAttribute.Key != "",
		HasAttributeFilter: query.LogAttribute.Key != "",
	}
}

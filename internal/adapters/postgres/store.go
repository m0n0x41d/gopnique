package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanzakutnii/error-tracker/internal/app/health"
	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct {
	pool *pgxpool.Pool
}

type txStore struct {
	tx pgx.Tx
}

type MigrationResult struct {
	Applied []string
	Skipped []string
}

func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}

	return &Store{pool: pool}, nil
}

func NewProbe(databaseURL string) (*Store, error) {
	return NewStore(context.Background(), databaseURL)
}

func (store *Store) Close() error {
	store.pool.Close()
	return nil
}

func (store *Store) Ping(ctx context.Context) error {
	return store.pool.Ping(ctx)
}

func (store *Store) MigrationStatus(ctx context.Context) (health.MigrationStatus, error) {
	status := health.MigrationStatus{}
	query := `select count(*) from schema_migrations`

	scanErr := store.pool.QueryRow(ctx, query).Scan(&status.AppliedCount)
	if scanErr != nil {
		return health.MigrationStatus{}, scanErr
	}

	status.Ready = status.AppliedCount >= 33

	return status, nil
}

func (store *Store) ApplyMigrations(ctx context.Context) (MigrationResult, error) {
	files, filesErr := migrationNames()
	if filesErr != nil {
		return MigrationResult{}, filesErr
	}

	outcome := MigrationResult{}

	for _, name := range files {
		content, readErr := migrationFiles.ReadFile("migrations/" + name)
		if readErr != nil {
			return MigrationResult{}, readErr
		}

		checksum := checksum(content)
		state, stateErr := store.migrationState(ctx, name)
		if stateErr != nil {
			return MigrationResult{}, stateErr
		}

		if state.applied && state.checksum == checksum {
			outcome.Skipped = append(outcome.Skipped, name)
			continue
		}

		if state.applied && state.checksum != checksum {
			return MigrationResult{}, fmt.Errorf("migration checksum mismatch for %s", name)
		}

		applyErr := store.applyMigration(ctx, name, checksum, string(content))
		if applyErr != nil {
			return MigrationResult{}, applyErr
		}

		outcome.Applied = append(outcome.Applied, name)
	}

	return outcome, nil
}

func (store *Store) ResolveProjectKey(
	ctx context.Context,
	ref domain.ProjectRef,
	key domain.ProjectPublicKey,
) result.Result[domain.ProjectAuth] {
	query := `
select p.id, p.organization_id, p.scrub_ip_addresses
from projects p
join organizations o on o.id = p.organization_id
join project_keys pk on pk.project_id = p.id
where p.ingest_ref = $1
  and pk.public_key = $2
  and pk.active = true
  and p.accepting_events = true
  and o.accepting_events = true
`

	var projectIDText string
	var organizationIDText string
	var scrubIPAddresses bool
	scanErr := store.pool.QueryRow(ctx, query, ref.String(), key.String()).Scan(&projectIDText, &organizationIDText, &scrubIPAddresses)
	if scanErr != nil {
		return result.Err[domain.ProjectAuth](scanErr)
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return result.Err[domain.ProjectAuth](projectErr)
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return result.Err[domain.ProjectAuth](organizationErr)
	}

	auth, authErr := domain.NewProjectAuthWithPolicy(organizationID, projectID, scrubIPAddresses)
	if authErr != nil {
		return result.Err[domain.ProjectAuth](authErr)
	}

	return result.Ok(auth)
}

func (store *Store) Run(
	ctx context.Context,
	program ingest.IngestProgram,
) result.Result[ingest.IngestTransactionResult] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[ingest.IngestTransactionResult](beginErr)
	}

	ports := txStore{tx: tx}
	programResult := program(ctx, ports)
	value, programErr := programResult.Value()
	if programErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[ingest.IngestTransactionResult](programErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[ingest.IngestTransactionResult](commitErr)
	}

	return result.Ok(value)
}

func (store txStore) Exists(
	ctx context.Context,
	projectID domain.ProjectID,
	eventID domain.EventID,
) result.Result[bool] {
	query := `select exists(select 1 from events where project_id = $1 and event_id = $2)`

	var exists bool
	scanErr := store.tx.QueryRow(ctx, query, projectID.String(), eventID.String()).Scan(&exists)
	if scanErr != nil {
		return result.Err[bool](scanErr)
	}

	return result.Ok(exists)
}

func (store txStore) Append(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
) result.Result[ingest.EventAppendResult] {
	canonical := event.Event()
	eventRowID, eventRowIDErr := randomUUID()
	if eventRowIDErr != nil {
		return result.Err[ingest.EventAppendResult](eventRowIDErr)
	}

	query := `
insert into events (
  id,
  organization_id,
  project_id,
  event_id,
  kind,
  level,
  title,
  platform,
  occurred_at,
  received_at,
  release,
  environment,
  transaction_name,
  fingerprint,
  canonical_payload
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
)
on conflict (project_id, event_id) do nothing
`
	payload, payloadErr := canonicalPayloadJSON(event)
	if payloadErr != nil {
		return result.Err[ingest.EventAppendResult](payloadErr)
	}

	tag, execErr := store.tx.Exec(
		ctx,
		query,
		eventRowID,
		canonical.OrganizationID().String(),
		canonical.ProjectID().String(),
		canonical.EventID().String(),
		string(canonical.Kind()),
		string(canonical.Level()),
		canonical.Title().String(),
		canonical.Platform(),
		canonical.OccurredAt().Time(),
		canonical.ReceivedAt().Time(),
		nullableText(canonical.Release()),
		nullableText(canonical.Environment()),
		nullableText(transactionName(canonical)),
		event.Fingerprint().Value(),
		payload,
	)
	if execErr != nil {
		return result.Err[ingest.EventAppendResult](execErr)
	}

	if tag.RowsAffected() == 0 {
		return result.Ok(ingest.NewDuplicateEvent())
	}

	statsErr := store.upsertProjectHourlyStats(ctx, canonical)
	if statsErr != nil {
		return result.Err[ingest.EventAppendResult](statsErr)
	}

	tagsErr := store.insertEventTags(ctx, eventRowID, canonical)
	if tagsErr != nil {
		return result.Err[ingest.EventAppendResult](tagsErr)
	}

	transactionErr := store.insertTransactionEvent(ctx, eventRowID, canonical)
	if transactionErr != nil {
		return result.Err[ingest.EventAppendResult](transactionErr)
	}

	return result.Ok(ingest.NewAppendedEvent())
}

func (store txStore) upsertProjectHourlyStats(
	ctx context.Context,
	event domain.CanonicalEvent,
) error {
	query := `
insert into project_hourly_stats (
  organization_id,
  project_id,
  bucket_at,
  event_count,
  issue_event_count,
  transaction_event_count
) values (
  $1,
  $2,
  date_trunc('hour', $3::timestamptz),
  1,
  $4,
  $5
)
on conflict (project_id, bucket_at) do update
set
  event_count = project_hourly_stats.event_count + excluded.event_count,
  issue_event_count = project_hourly_stats.issue_event_count + excluded.issue_event_count,
  transaction_event_count = project_hourly_stats.transaction_event_count + excluded.transaction_event_count
`
	_, execErr := store.tx.Exec(
		ctx,
		query,
		event.OrganizationID().String(),
		event.ProjectID().String(),
		event.ReceivedAt().Time(),
		boolInt(event.CreatesIssue()),
		boolInt(event.Kind() == domain.EventKindTransaction),
	)

	return execErr
}

func canonicalPayloadJSON(event ingestplan.AcceptedEvent) ([]byte, error) {
	canonical := event.Event()
	payload := map[string]any{
		"event_id":    canonical.EventID().Hex(),
		"kind":        string(canonical.Kind()),
		"level":       string(canonical.Level()),
		"title":       canonical.Title().String(),
		"platform":    canonical.Platform(),
		"occurred_at": canonical.OccurredAt().Time().Format(time.RFC3339Nano),
		"received_at": canonical.ReceivedAt().Time().Format(time.RFC3339Nano),
		"release":     canonical.Release(),
		"environment": canonical.Environment(),
		"tags":        canonical.Tags(),
		"fingerprint": map[string]any{
			"algorithm": event.Fingerprint().Algorithm(),
			"value":     event.Fingerprint().Value(),
		},
	}

	attachments := canonical.Attachments()
	if len(attachments) > 0 {
		encoded := make([]map[string]any, 0, len(attachments))
		for _, attachment := range attachments {
			entry := map[string]any{
				"kind":      attachment.Kind().String(),
				"name":      attachment.Name().String(),
				"byte_size": attachment.ByteSize(),
			}

			if contentType := attachment.ContentType(); contentType != "" {
				entry["content_type"] = contentType
			}

			encoded = append(encoded, entry)
		}

		payload["attachments"] = encoded
	}

	if encoded := encodeJsStacktrace(canonical.JsStacktrace()); encoded != nil {
		payload["js_stacktrace"] = encoded
	}

	if encoded := encodeNativeReferences(canonical.NativeModules(), canonical.NativeFrames()); encoded != nil {
		payload["native"] = encoded
	}

	if encoded := encodeTransaction(canonical); encoded != nil {
		payload["transaction"] = encoded
	}

	return json.Marshal(payload)
}

func transactionName(event domain.CanonicalEvent) string {
	transaction, ok := event.Transaction()
	if !ok {
		return ""
	}

	return transaction.Name()
}

func encodeTransaction(event domain.CanonicalEvent) map[string]any {
	transaction, ok := event.Transaction()
	if !ok {
		return nil
	}

	encoded := map[string]any{
		"name":        transaction.Name(),
		"operation":   transaction.Operation(),
		"duration_ms": transaction.DurationMilliseconds(),
		"status":      transaction.Status(),
		"span_count":  transaction.SpanCount(),
	}

	if trace, hasTrace := transaction.Trace(); hasTrace {
		encoded["trace"] = map[string]any{
			"trace_id": trace.TraceID(),
			"span_id":  trace.SpanID(),
		}

		if parentSpanID := trace.ParentSpanID(); parentSpanID != "" {
			encoded["trace"].(map[string]any)["parent_span_id"] = parentSpanID
		}
	}

	if spans := encodeTransactionSpans(transaction.Spans()); spans != nil {
		encoded["spans"] = spans
	}

	return encoded
}

func encodeTransactionSpans(spans []domain.TransactionSpan) []map[string]any {
	if len(spans) == 0 {
		return nil
	}

	encoded := make([]map[string]any, 0, len(spans))
	for _, span := range spans {
		entry := map[string]any{
			"span_id":     span.SpanID(),
			"operation":   span.Operation(),
			"duration_ms": span.DurationMilliseconds(),
			"status":      span.Status(),
		}

		if parentSpanID := span.ParentSpanID(); parentSpanID != "" {
			entry["parent_span_id"] = parentSpanID
		}

		if description := span.Description(); description != "" {
			entry["description"] = description
		}

		encoded = append(encoded, entry)
	}

	return encoded
}

func encodeJsStacktrace(frames []domain.JsStacktraceFrame) []map[string]any {
	if len(frames) == 0 {
		return nil
	}

	encoded := make([]map[string]any, 0, len(frames))
	for _, frame := range frames {
		entry := map[string]any{
			"abs_path": frame.AbsPath(),
			"generated": map[string]any{
				"line":   frame.GeneratedLine(),
				"column": frame.GeneratedColumn(),
			},
		}

		if function := frame.Function(); function != "" {
			entry["function"] = function
		}

		if resolution, hasResolution := frame.Resolution(); hasResolution {
			resolved := map[string]any{
				"source": resolution.Source(),
				"line":   resolution.OriginalLine(),
				"column": resolution.OriginalColumn(),
			}

			if symbol := resolution.Symbol(); symbol != "" {
				resolved["symbol"] = symbol
			}

			entry["resolved"] = resolved
		}

		encoded = append(encoded, entry)
	}

	return encoded
}

func encodeNativeReferences(
	modules []domain.NativeModule,
	frames []domain.NativeFrame,
) map[string]any {
	if len(modules) == 0 && len(frames) == 0 {
		return nil
	}

	native := map[string]any{}

	if len(modules) > 0 {
		encoded := make([]map[string]any, 0, len(modules))
		for _, module := range modules {
			encoded = append(encoded, map[string]any{
				"debug_id":   module.DebugID().String(),
				"code_file":  module.CodeFile(),
				"image_addr": module.ImageAddr(),
				"image_size": module.ImageSize(),
			})
		}

		native["modules"] = encoded
	}

	if len(frames) > 0 {
		encoded := make([]map[string]any, 0, len(frames))
		for _, frame := range frames {
			entry := map[string]any{
				"instruction_addr": frame.InstructionAddr(),
			}

			if debugID, hasModule := frame.ModuleDebugID(); hasModule {
				entry["module_debug_id"] = debugID.String()
			}

			if function := frame.Function(); function != "" {
				entry["function"] = function
			}

			if pkg := frame.Package(); pkg != "" {
				entry["package"] = pkg
			}

			encoded = append(encoded, entry)
		}

		native["frames"] = encoded
	}

	return native
}

func (store txStore) insertEventTags(
	ctx context.Context,
	eventRowID string,
	event domain.CanonicalEvent,
) error {
	query := `
insert into event_tags (event_id, project_id, key, value)
values ($1, $2, $3, $4)
on conflict (event_id, key) do update set value = excluded.value
`
	for key, value := range event.Tags() {
		_, execErr := store.tx.Exec(
			ctx,
			query,
			eventRowID,
			event.ProjectID().String(),
			key,
			value,
		)
		if execErr != nil {
			return execErr
		}
	}

	return nil
}

func (store txStore) insertTransactionEvent(
	ctx context.Context,
	eventRowID string,
	event domain.CanonicalEvent,
) error {
	transaction, ok := event.Transaction()
	if !ok {
		return nil
	}

	encodedSpans := encodeTransactionSpans(transaction.Spans())
	if encodedSpans == nil {
		encodedSpans = []map[string]any{}
	}

	spans, spansErr := json.Marshal(encodedSpans)
	if spansErr != nil {
		return spansErr
	}

	traceID := ""
	spanID := ""
	parentSpanID := ""
	if trace, hasTrace := transaction.Trace(); hasTrace {
		traceID = trace.TraceID()
		spanID = trace.SpanID()
		parentSpanID = trace.ParentSpanID()
	}

	query := `
insert into transaction_events (
  event_id,
  organization_id,
  project_id,
  transaction_name,
  operation,
  duration_ms,
  status,
  trace_id,
  span_id,
  parent_span_id,
  span_count,
  spans,
  occurred_at,
  received_at
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, coalesce($12::jsonb, '[]'::jsonb), $13, $14
)
on conflict (event_id) do nothing
`
	_, execErr := store.tx.Exec(
		ctx,
		query,
		eventRowID,
		event.OrganizationID().String(),
		event.ProjectID().String(),
		transaction.Name(),
		transaction.Operation(),
		transaction.DurationMilliseconds(),
		transaction.Status(),
		nullableText(traceID),
		nullableText(spanID),
		nullableText(parentSpanID),
		transaction.SpanCount(),
		spans,
		event.OccurredAt().Time(),
		event.ReceivedAt().Time(),
	)

	return execErr
}

func boolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func nullableText(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	return value
}

func (store txStore) Apply(
	ctx context.Context,
	plan ingestplan.IssuePlan,
) result.Result[ingest.IssueChange] {
	accepted := plan.Event()
	eventRowIDResult := store.eventRowID(ctx, accepted.Event().ProjectID(), accepted.Event().EventID())
	eventRowID, eventRowIDErr := eventRowIDResult.Value()
	if eventRowIDErr != nil {
		return result.Err[ingest.IssueChange](eventRowIDErr)
	}

	existingResult := store.findIssueByFingerprint(ctx, accepted)
	existingIssue, existingErr := existingResult.Value()
	if existingErr != nil {
		return result.Err[ingest.IssueChange](existingErr)
	}

	if existingIssue != "" {
		return store.updateIssue(ctx, accepted, existingIssue, eventRowID)
	}

	return store.createIssue(ctx, accepted, eventRowID)
}

func (store txStore) eventRowID(
	ctx context.Context,
	projectID domain.ProjectID,
	eventID domain.EventID,
) result.Result[string] {
	query := `select id from events where project_id = $1 and event_id = $2`

	var rowID string
	scanErr := store.tx.QueryRow(ctx, query, projectID.String(), eventID.String()).Scan(&rowID)
	if scanErr != nil {
		return result.Err[string](scanErr)
	}

	return result.Ok(rowID)
}

type migrationState struct {
	applied  bool
	checksum string
}

func migrationNames() ([]string, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		names = append(names, entry.Name())
	}

	sort.Strings(names)

	return names, nil
}

func checksum(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])
}

func (store *Store) migrationState(ctx context.Context, name string) (migrationState, error) {
	query := `select checksum from schema_migrations where version = $1`

	var checksum string
	scanErr := store.pool.QueryRow(ctx, query, name).Scan(&checksum)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return migrationState{}, nil
	}

	if scanErr != nil {
		if isUndefinedTable(scanErr) {
			return migrationState{}, nil
		}

		return migrationState{}, scanErr
	}

	return migrationState{applied: true, checksum: checksum}, nil
}

func (store *Store) applyMigration(
	ctx context.Context,
	name string,
	checksum string,
	sql string,
) error {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return beginErr
	}

	_, execErr := tx.Exec(ctx, sql)
	if execErr != nil {
		_ = tx.Rollback(ctx)
		return execErr
	}

	insert := `
insert into schema_migrations (version, applied_at, checksum)
values ($1, $2, $3)
on conflict (version) do nothing
`
	_, insertErr := tx.Exec(ctx, insert, name, time.Now().UTC(), checksum)
	if insertErr != nil {
		_ = tx.Rollback(ctx)
		return insertErr
	}

	return tx.Commit(ctx)
}

func (store txStore) findIssueByFingerprint(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
) result.Result[string] {
	query := `
select issue_id
from issue_fingerprints
where project_id = $1 and fingerprint = $2
`

	var issueID string
	scanErr := store.tx.QueryRow(
		ctx,
		query,
		event.Event().ProjectID().String(),
		event.Fingerprint().Value(),
	).Scan(&issueID)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return result.Ok("")
	}

	if scanErr != nil {
		return result.Err[string](scanErr)
	}

	return result.Ok(issueID)
}

func (store txStore) updateIssue(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
	issueID string,
	eventRowID string,
) result.Result[ingest.IssueChange] {
	query := `
update issues
set event_count = event_count + 1,
    last_seen_at = greatest(last_seen_at, $2),
    last_event_id = $3,
    release = coalesce($4, release),
    environment = coalesce($5, environment)
where id = $1
returning id
`

	var updatedIDText string
	scanErr := store.tx.QueryRow(
		ctx,
		query,
		issueID,
		event.Event().OccurredAt().Time(),
		eventRowID,
		nullableText(event.Event().Release()),
		nullableText(event.Event().Environment()),
	).Scan(&updatedIDText)
	if scanErr != nil {
		return result.Err[ingest.IssueChange](scanErr)
	}

	updatedID, updatedIDErr := domain.NewIssueID(updatedIDText)
	if updatedIDErr != nil {
		return result.Err[ingest.IssueChange](updatedIDErr)
	}

	return result.Ok(ingest.NewIssueChange(updatedID, false))
}

func (store txStore) createIssue(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
	eventRowID string,
) result.Result[ingest.IssueChange] {
	issueID, issueIDErr := randomUUID()
	if issueIDErr != nil {
		return result.Err[ingest.IssueChange](issueIDErr)
	}

	shortIDResult := store.nextIssueShortID(ctx, event.Event().ProjectID())
	shortID, shortIDErr := shortIDResult.Value()
	if shortIDErr != nil {
		return result.Err[ingest.IssueChange](shortIDErr)
	}

	insertIssueErr := store.insertIssue(ctx, event, issueID, shortID, eventRowID)
	if insertIssueErr != nil {
		return result.Err[ingest.IssueChange](insertIssueErr)
	}

	tag, insertFingerprintErr := store.tx.Exec(
		ctx,
		`
insert into issue_fingerprints (project_id, fingerprint, issue_id, created_at)
values ($1, $2, $3, $4)
on conflict (project_id, fingerprint) do nothing
`,
		event.Event().ProjectID().String(),
		event.Fingerprint().Value(),
		issueID,
		time.Now().UTC(),
	)
	if insertFingerprintErr != nil {
		return result.Err[ingest.IssueChange](insertFingerprintErr)
	}

	if tag.RowsAffected() == 1 {
		domainIssueID, domainIssueErr := domain.NewIssueID(issueID)
		if domainIssueErr != nil {
			return result.Err[ingest.IssueChange](domainIssueErr)
		}

		return result.Ok(ingest.NewIssueChange(domainIssueID, true))
	}

	_, _ = store.tx.Exec(ctx, `delete from issues where id = $1`, issueID)

	existingResult := store.findIssueByFingerprint(ctx, event)
	existingID, existingErr := existingResult.Value()
	if existingErr != nil {
		return result.Err[ingest.IssueChange](existingErr)
	}

	return store.updateIssue(ctx, event, existingID, eventRowID)
}

func (store txStore) nextIssueShortID(
	ctx context.Context,
	projectID domain.ProjectID,
) result.Result[int64] {
	query := `
update projects
set next_issue_short_id = next_issue_short_id + 1
where id = $1
returning next_issue_short_id - 1
`

	var shortID int64
	scanErr := store.tx.QueryRow(ctx, query, projectID.String()).Scan(&shortID)
	if scanErr != nil {
		return result.Err[int64](scanErr)
	}

	return result.Ok(shortID)
}

func (store txStore) insertIssue(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
	issueID string,
	shortID int64,
	eventRowID string,
) error {
	query := `
insert into issues (
  id,
  organization_id,
  project_id,
  short_id,
  type,
  status,
  title,
  first_seen_at,
  last_seen_at,
  event_count,
  last_event_id,
  release,
  environment,
  created_at
) values (
  $1, $2, $3, $4, $5, 'unresolved', $6, $7, $7, 1, $8, $9, $10, $11
)
`

	_, execErr := store.tx.Exec(
		ctx,
		query,
		issueID,
		event.Event().OrganizationID().String(),
		event.Event().ProjectID().String(),
		shortID,
		string(event.Event().Kind()),
		event.Event().Title().String(),
		event.Event().OccurredAt().Time(),
		eventRowID,
		nullableText(event.Event().Release()),
		nullableText(event.Event().Environment()),
		time.Now().UTC(),
	)

	return execErr
}

func randomUUID() (string, error) {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}

	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	value := hex.EncodeToString(bytes)
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		value[0:8],
		value[8:12],
		value[12:16],
		value[16:20],
		value[20:32],
	), nil
}

func isUndefinedTable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "42P01"
}

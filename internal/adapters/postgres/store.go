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

	status.Ready = status.AppliedCount >= 15

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
  fingerprint,
  canonical_payload
) values (
  $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
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
		event.Fingerprint().Value(),
		payload,
	)
	if execErr != nil {
		return result.Err[ingest.EventAppendResult](execErr)
	}

	if tag.RowsAffected() == 0 {
		return result.Ok(ingest.NewDuplicateEvent())
	}

	tagsErr := store.insertEventTags(ctx, eventRowID, canonical)
	if tagsErr != nil {
		return result.Err[ingest.EventAppendResult](tagsErr)
	}

	return result.Ok(ingest.NewAppendedEvent())
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

	return json.Marshal(payload)
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

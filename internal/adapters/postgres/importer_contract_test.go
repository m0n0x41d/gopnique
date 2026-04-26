//go:build integration

package postgres

import (
	"context"
	"strings"
	"testing"

	importapp "github.com/ivanzakutnii/error-tracker/internal/app/importer"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestPostgresImporterWorkflow(t *testing.T) {
	ctx := context.Background()
	adminURL := repositoryAdminURL(t)
	databaseURL := createRepositoryTestDatabase(t, ctx, adminURL)
	store, storeErr := NewStore(ctx, databaseURL)
	if storeErr != nil {
		t.Fatalf("store: %v", storeErr)
	}
	defer store.Close()

	migrationResult, migrationErr := store.ApplyMigrations(ctx)
	if migrationErr != nil {
		t.Fatalf("migrate: %v", migrationErr)
	}
	if len(migrationResult.Applied) != 33 {
		t.Fatalf("expected 33 migrations, got %d", len(migrationResult.Applied))
	}

	bootstrap, bootstrapErr := store.Bootstrap(ctx, BootstrapInput{
		PublicURL:        "http://example.test",
		OrganizationName: "Import Org",
		ProjectName:      "Import API",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	scope, scopeErr := store.LookupProjectByRef(ctx, bootstrap.ProjectRef)
	if scopeErr != nil {
		t.Fatalf("lookup project: %v", scopeErr)
	}

	dryRunManifest := importerManifest(t, scope, domain.ImportModeDryRun)
	dryRunParsed := parseImporterFixture(t, dryRunManifest, importerFixtureJSONL())
	dryRun := importapp.DryRun(dryRunParsed)
	if dryRun.TotalRows != 2 || dryRun.ValidRows != 2 || dryRun.InvalidRows != 0 {
		t.Fatalf("unexpected dry-run summary: %#v", dryRun)
	}
	assertImportTableCount(t, ctx, store, "import_runs", 0)

	applyManifest := importerManifest(t, scope, domain.ImportModeApply)
	applyParsed := parseImporterFixture(t, applyManifest, importerFixtureJSONL())
	applyResult := importapp.Apply(ctx, store, importapp.ApplyCommand{
		Manifest: applyManifest,
		Records:  applyParsed.Records(),
	})
	applied, applyErr := applyResult.Value()
	if applyErr != nil {
		t.Fatalf("apply import: %v", applyErr)
	}

	if applied.AppliedRows != 2 || applied.DuplicateRows != 0 || applied.SkippedRows != 0 || applied.FailedRows != 0 {
		t.Fatalf("unexpected apply result: %#v", applied)
	}

	assertImportedIssueState(t, ctx, store, scope.ProjectID.String(), 2)
	assertImportRecordsByStatus(t, ctx, store, importRecordStatusApplied, 2)
	assertAuditActionCount(t, ctx, store, "import_run_completed", 1)

	reapplyResult := importapp.Apply(ctx, store, importapp.ApplyCommand{
		Manifest: applyManifest,
		Records:  applyParsed.Records(),
	})
	reapplied, reapplyErr := reapplyResult.Value()
	if reapplyErr != nil {
		t.Fatalf("reapply import: %v", reapplyErr)
	}

	if reapplied.AppliedRows != 0 || reapplied.SkippedRows != 2 || reapplied.FailedRows != 0 {
		t.Fatalf("unexpected reapply result: %#v", reapplied)
	}

	duplicateParsed := parseImporterFixture(t, applyManifest, duplicateEventImporterFixtureJSONL())
	duplicateResult := importapp.Apply(ctx, store, importapp.ApplyCommand{
		Manifest: applyManifest,
		Records:  duplicateParsed.Records(),
	})
	duplicated, duplicateErr := duplicateResult.Value()
	if duplicateErr != nil {
		t.Fatalf("duplicate import: %v", duplicateErr)
	}

	if duplicated.AppliedRows != 0 || duplicated.DuplicateRows != 1 || duplicated.FailedRows != 0 {
		t.Fatalf("unexpected duplicate result: %#v", duplicated)
	}

	assertImportedIssueState(t, ctx, store, scope.ProjectID.String(), 2)
	assertImportRecordsByStatus(t, ctx, store, importRecordStatusDuplicate, 1)
}

func importerManifest(
	t *testing.T,
	scope ProjectScope,
	mode domain.ImportMode,
) domain.ImportManifest {
	t.Helper()

	source := mustRepositoryValue(t, domain.NewImportSourceSystem, "jsonl")
	manifest, manifestErr := domain.NewImportManifest(domain.ImportManifestParams{
		OrganizationID: scope.OrganizationID,
		ProjectID:      scope.ProjectID,
		SourceSystem:   source,
		Mode:           mode,
	})
	if manifestErr != nil {
		t.Fatalf("manifest: %v", manifestErr)
	}

	return manifest
}

func parseImporterFixture(
	t *testing.T,
	manifest domain.ImportManifest,
	jsonl string,
) importapp.ParseResult {
	t.Helper()

	parseResult := importapp.ParseJSONL(strings.NewReader(jsonl), manifest)
	parsed, parseErr := parseResult.Value()
	if parseErr != nil {
		t.Fatalf("parse import fixture: %v", parseErr)
	}

	if len(parsed.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %#v", parsed.Errors())
	}

	return parsed
}

func importerFixtureJSONL() string {
	return strings.Join([]string{
		`{"type":"issue","external_id":"issue-1","kind":"error","level":"error","title":"imported checkout failure","platform":"go","occurred_at":"2026-04-26T10:00:00Z","release":"api@9.9.9","environment":"production"}`,
		`{"type":"event","external_id":"event-1","issue_external_id":"issue-1","event_id":"550e8400e29b41d4a716446655440000","kind":"error","level":"error","title":"imported checkout failure","platform":"go","occurred_at":"2026-04-26T10:05:00Z","release":"api@9.9.9","environment":"production","tags":{"component":"checkout"}}`,
	}, "\n")
}

func duplicateEventImporterFixtureJSONL() string {
	return `{"type":"event","external_id":"event-duplicate","issue_external_id":"issue-1","event_id":"550e8400e29b41d4a716446655440000","kind":"error","level":"error","title":"imported checkout failure","platform":"go","occurred_at":"2026-04-26T10:10:00Z"}`
}

func assertImportTableCount(
	t *testing.T,
	ctx context.Context,
	store *Store,
	table string,
	expected int,
) {
	t.Helper()

	query := "select count(*) from " + table
	var count int
	scanErr := store.pool.QueryRow(ctx, query).Scan(&count)
	if scanErr != nil {
		t.Fatalf("count %s: %v", table, scanErr)
	}

	if count != expected {
		t.Fatalf("expected %s count %d, got %d", table, expected, count)
	}
}

func assertImportedIssueState(
	t *testing.T,
	ctx context.Context,
	store *Store,
	projectID string,
	expectedEventCount int,
) {
	t.Helper()

	var issueCount int
	var eventCount int
	query := `
select count(*), coalesce(sum(event_count), 0)::int
from issues
where project_id = $1
`
	scanErr := store.pool.QueryRow(ctx, query, projectID).Scan(&issueCount, &eventCount)
	if scanErr != nil {
		t.Fatalf("issue state: %v", scanErr)
	}

	if issueCount != 1 || eventCount != expectedEventCount {
		t.Fatalf("unexpected issue state: count=%d event_count=%d", issueCount, eventCount)
	}
}

func assertImportRecordsByStatus(
	t *testing.T,
	ctx context.Context,
	store *Store,
	status string,
	expected int,
) {
	t.Helper()

	var count int
	query := `select count(*) from import_records where status = $1`
	scanErr := store.pool.QueryRow(ctx, query, status).Scan(&count)
	if scanErr != nil {
		t.Fatalf("import records by status: %v", scanErr)
	}

	if count != expected {
		t.Fatalf("expected status %s count %d, got %d", status, expected, count)
	}
}

func assertAuditActionCount(
	t *testing.T,
	ctx context.Context,
	store *Store,
	action string,
	expected int,
) {
	t.Helper()

	var count int
	query := `select count(*) from audit_events where action = $1`
	scanErr := store.pool.QueryRow(ctx, query, action).Scan(&count)
	if scanErr != nil {
		t.Fatalf("audit action count: %v", scanErr)
	}

	if count != expected {
		t.Fatalf("expected audit action %s count %d, got %d", action, expected, count)
	}
}

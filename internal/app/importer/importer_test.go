package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestParseJSONLBuildsIssueAndEventRecords(t *testing.T) {
	manifest := testManifest(t, domain.ImportModeDryRun)
	input := strings.NewReader(strings.Join([]string{
		`{"type":"issue","external_id":"issue-1","kind":"error","level":"error","title":"imported issue","occurred_at":"2026-04-26T10:00:00Z"}`,
		`{"type":"event","external_id":"event-1","issue_external_id":"issue-1","event_id":"550e8400e29b41d4a716446655440000","kind":"error","level":"error","title":"imported issue","occurred_at":"2026-04-26T10:05:00Z","tags":{"component":"api"}}`,
	}, "\n"))

	parseResult := ParseJSONL(input, manifest)
	parsed, parseErr := parseResult.Value()
	if parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}

	if parsed.TotalRows() != 2 {
		t.Fatalf("unexpected total rows: %d", parsed.TotalRows())
	}

	if len(parsed.Errors()) != 0 {
		t.Fatalf("unexpected parse errors: %#v", parsed.Errors())
	}

	records := parsed.Records()
	if len(records) != 2 {
		t.Fatalf("unexpected record count: %d", len(records))
	}

	if records[0].Kind() != domain.ImportRecordKindIssue {
		t.Fatalf("unexpected first record kind: %s", records[0].Kind())
	}

	if records[1].Event().ExplicitFingerprint()[2] != "issue-1" {
		t.Fatalf("expected event to attach to imported issue fingerprint: %#v", records[1].Event().ExplicitFingerprint())
	}
}

func TestDryRunReportsInvalidRowsWithoutDroppingValidRows(t *testing.T) {
	manifest := testManifest(t, domain.ImportModeDryRun)
	input := strings.NewReader(strings.Join([]string{
		`{"type":"event","external_id":"event-1","event_id":"550e8400e29b41d4a716446655440000","kind":"error","level":"error","title":"ok","occurred_at":"2026-04-26T10:05:00Z"}`,
		`{"type":"event","external_id":"","event_id":"650e8400e29b41d4a716446655440000","kind":"error","level":"error","title":"missing external","occurred_at":"2026-04-26T10:05:00Z"}`,
	}, "\n"))

	parseResult := ParseJSONL(input, manifest)
	parsed, parseErr := parseResult.Value()
	if parseErr != nil {
		t.Fatalf("parse: %v", parseErr)
	}

	dryRun := DryRun(parsed)
	if dryRun.TotalRows != 2 || dryRun.ValidRows != 1 || dryRun.InvalidRows != 1 {
		t.Fatalf("unexpected dry-run summary: %#v", dryRun)
	}
}

func TestApplyRequiresApplyManifest(t *testing.T) {
	manifest := testManifest(t, domain.ImportModeDryRun)
	ledger := fakeLedger{}

	applyResult := Apply(context.Background(), ledger, ApplyCommand{
		Manifest: manifest,
	})
	_, applyErr := applyResult.Value()
	if applyErr == nil {
		t.Fatal("expected dry-run manifest to fail apply")
	}
}

type fakeLedger struct{}

func (ledger fakeLedger) ApplyImport(
	ctx context.Context,
	command ApplyCommand,
) result.Result[ApplyResult] {
	return result.Ok(ApplyResult{TotalRows: len(command.Records)})
}

func testManifest(t *testing.T, mode domain.ImportMode) domain.ImportManifest {
	t.Helper()

	organizationID := mustImportValue(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustImportValue(t, domain.NewProjectID, "2222222222224222a222222222222222")
	source := mustImportValue(t, domain.NewImportSourceSystem, "jsonl")
	manifest, manifestErr := domain.NewImportManifest(domain.ImportManifestParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		SourceSystem:   source,
		Mode:           mode,
	})
	if manifestErr != nil {
		t.Fatalf("manifest: %v", manifestErr)
	}

	return manifest
}

func mustImportValue[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	value, valueErr := constructor(input)
	if valueErr != nil {
		t.Fatalf("domain value: %v", valueErr)
	}

	return value
}

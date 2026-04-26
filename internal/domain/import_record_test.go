package domain

import (
	"testing"
)

func TestImportSourceSystemNormalizesBoundedSlug(t *testing.T) {
	source, sourceErr := NewImportSourceSystem(" Sentry.Export_1 ")
	if sourceErr != nil {
		t.Fatalf("source system: %v", sourceErr)
	}

	if source.String() != "sentry.export_1" {
		t.Fatalf("unexpected source system: %s", source.String())
	}
}

func TestImportRecordRequiresIssueCreatingEvent(t *testing.T) {
	source := mustImportDomainValue(t, NewImportSourceSystem, "sentry")
	externalID := mustImportDomainValue(t, NewImportExternalID, "evt-1")
	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:  EventKindTransaction,
		Level: EventLevelInfo,
		Title: mustTitle(t, "GET /checkout"),
	})

	_, recordErr := NewImportRecord(ImportRecordParams{
		SourceSystem: source,
		ExternalID:   externalID,
		Kind:         ImportRecordKindEvent,
		Event:        event,
	})
	if recordErr == nil {
		t.Fatal("expected transaction import record to fail")
	}
}

func TestImportManifestRequiresApplyOrDryRunMode(t *testing.T) {
	source := mustImportDomainValue(t, NewImportSourceSystem, "jsonl")
	organizationID := mustImportDomainValue(t, NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustImportDomainValue(t, NewProjectID, "2222222222224222a222222222222222")

	_, manifestErr := NewImportManifest(ImportManifestParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		SourceSystem:   source,
		Mode:           ImportMode("preview"),
	})
	if manifestErr == nil {
		t.Fatal("expected invalid mode to fail")
	}
}

func mustImportDomainValue[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	value, valueErr := constructor(input)
	if valueErr != nil {
		t.Fatalf("domain value: %v", valueErr)
	}

	return value
}

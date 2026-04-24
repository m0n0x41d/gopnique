package grouping

import (
	"strings"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestComputeFingerprintUsesExplicitSource(t *testing.T) {
	event := testEvent(t, domain.CanonicalEventParams{
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title(t, "TypeError: bad operand"),
		DefaultGroupingParts: []string{"TypeError", "handler.go", "42"},
		ExplicitFingerprint:  []string{"custom", "{{ default }}"},
	})

	canonicalResult := CanonicalString(event)
	canonical, canonicalErr := canonicalResult.Value()
	if canonicalErr != nil {
		t.Fatalf("canonical string: %v", canonicalErr)
	}

	if !strings.Contains(canonical, "source:explicit") {
		t.Fatalf("expected explicit source: %s", canonical)
	}

	if !strings.Contains(canonical, "TypeError") || !strings.Contains(canonical, "handler.go") {
		t.Fatalf("expected default parts to be expanded: %s", canonical)
	}

	fingerprintResult := ComputeFingerprint(event)
	fingerprint, fingerprintErr := fingerprintResult.Value()
	if fingerprintErr != nil {
		t.Fatalf("fingerprint: %v", fingerprintErr)
	}

	if fingerprint.Algorithm() != Algorithm {
		t.Fatalf("unexpected algorithm: %s", fingerprint.Algorithm())
	}

	if len(fingerprint.Value()) != 64 {
		t.Fatalf("unexpected fingerprint length: %d", len(fingerprint.Value()))
	}
}

func TestComputeFingerprintIsProjectScoped(t *testing.T) {
	firstProject := mustID(t, domain.NewProjectID, "2222222222224222a222222222222222")
	secondProject := mustID(t, domain.NewProjectID, "99999999999949999999999999999999")
	first := testEvent(t, domain.CanonicalEventParams{
		ProjectID:            firstProject,
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title(t, "TypeError: bad operand"),
		DefaultGroupingParts: []string{"TypeError", "bad operand", "handler.go", "handle", "42"},
	})
	second := testEvent(t, domain.CanonicalEventParams{
		ProjectID:            secondProject,
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title(t, "TypeError: bad operand"),
		DefaultGroupingParts: []string{"TypeError", "bad operand", "handler.go", "handle", "42"},
	})

	firstFingerprint := mustFingerprint(t, first)
	secondFingerprint := mustFingerprint(t, second)
	if firstFingerprint.Value() == secondFingerprint.Value() {
		t.Fatal("expected same error in different projects to have different fingerprints")
	}
}

func TestCanonicalStringUsesSourceForEachEventKind(t *testing.T) {
	cases := []struct {
		name           string
		event          domain.CanonicalEvent
		expectedSource string
	}{
		{
			name: "error",
			event: testEvent(t, domain.CanonicalEventParams{
				Kind:                 domain.EventKindError,
				Level:                domain.EventLevelError,
				Title:                title(t, "TypeError: bad operand"),
				DefaultGroupingParts: []string{"TypeError", "bad operand", "handler.go", "handle", "42"},
			}),
			expectedSource: "source:exception",
		},
		{
			name: "message",
			event: testEvent(t, domain.CanonicalEventParams{
				Kind:                 domain.EventKindDefault,
				Level:                domain.EventLevelError,
				Title:                title(t, "worker failed"),
				DefaultGroupingParts: []string{"error", "jobs", "worker failed"},
			}),
			expectedSource: "source:message",
		},
		{
			name: "transaction",
			event: testEvent(t, domain.CanonicalEventParams{
				Kind:                 domain.EventKindTransaction,
				Level:                domain.EventLevelInfo,
				Title:                title(t, "GET /checkout"),
				DefaultGroupingParts: []string{"GET /checkout"},
			}),
			expectedSource: "source:transaction",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			canonicalResult := CanonicalString(tc.event)
			canonical, canonicalErr := canonicalResult.Value()
			if canonicalErr != nil {
				t.Fatalf("canonical: %v", canonicalErr)
			}

			if !strings.Contains(canonical, tc.expectedSource) {
				t.Fatalf("expected %s in %s", tc.expectedSource, canonical)
			}
		})
	}
}

func TestEnsureIssueFingerprintRejectsTransaction(t *testing.T) {
	event := testEvent(t, domain.CanonicalEventParams{
		Kind:                 domain.EventKindTransaction,
		Level:                domain.EventLevelInfo,
		Title:                title(t, "GET /checkout"),
		DefaultGroupingParts: []string{"GET /checkout"},
	})

	result := EnsureIssueFingerprint(event)
	_, err := result.Value()
	if err == nil {
		t.Fatal("expected transaction to be rejected for issue fingerprint")
	}
}

func testEvent(t *testing.T, params domain.CanonicalEventParams) domain.CanonicalEvent {
	t.Helper()

	organizationID := mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustID(t, domain.NewProjectID, "2222222222224222a222222222222222")
	eventID := mustID(t, domain.NewEventID, "550e8400e29b41d4a716446655440000")
	occurredAt := timePoint(t, time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
	receivedAt := timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC))

	if params.OrganizationID.String() == "" {
		params.OrganizationID = organizationID
	}

	if params.ProjectID.String() == "" {
		params.ProjectID = projectID
	}

	if params.EventID.String() == "" {
		params.EventID = eventID
	}

	if params.OccurredAt.Time().IsZero() {
		params.OccurredAt = occurredAt
	}

	if params.ReceivedAt.Time().IsZero() {
		params.ReceivedAt = receivedAt
	}

	event, err := domain.NewCanonicalEvent(params)
	if err != nil {
		t.Fatalf("canonical event: %v", err)
	}

	return event
}

func mustFingerprint(t *testing.T, event domain.CanonicalEvent) domain.Fingerprint {
	t.Helper()

	fingerprintResult := ComputeFingerprint(event)
	fingerprint, fingerprintErr := fingerprintResult.Value()
	if fingerprintErr != nil {
		t.Fatalf("fingerprint: %v", fingerprintErr)
	}

	return fingerprint
}

func mustID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	id, err := constructor(input)
	if err != nil {
		t.Fatalf("id: %v", err)
	}

	return id
}

func timePoint(t *testing.T, value time.Time) domain.TimePoint {
	t.Helper()

	point, err := domain.NewTimePoint(value)
	if err != nil {
		t.Fatalf("time point: %v", err)
	}

	return point
}

func title(t *testing.T, input string) domain.EventTitle {
	t.Helper()

	value, err := domain.NewEventTitle(input)
	if err != nil {
		t.Fatalf("title: %v", err)
	}

	return value
}

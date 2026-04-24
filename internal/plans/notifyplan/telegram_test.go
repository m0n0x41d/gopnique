package notifyplan

import (
	"strings"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestTelegramIssueOpenedTextBuildsCanonicalMessage(t *testing.T) {
	openedResult := NewIssueOpened(issueEvent(t), issueID(t), 42)
	opened, openedErr := openedResult.Value()
	if openedErr != nil {
		t.Fatalf("issue opened: %v", openedErr)
	}

	textResult := TelegramIssueOpenedText(opened, "http://127.0.0.1:8085/")
	text, textErr := textResult.Value()
	if textErr != nil {
		t.Fatalf("telegram text: %v", textErr)
	}

	expectedParts := []string{
		"New issue #42",
		"panic: broken pipe",
		"Level: error",
		"Event: 950e8400-e29b-41d4-a716-446655440000",
		"http://127.0.0.1:8085/issues/33333333-3333-4333-a333-333333333333",
	}
	for _, part := range expectedParts {
		if !strings.Contains(text.String(), part) {
			t.Fatalf("expected message to contain %q, got %q", part, text.String())
		}
	}
}

func issueEvent(t *testing.T) domain.CanonicalEvent {
	t.Helper()

	organizationID := mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustID(t, domain.NewProjectID, "2222222222224222a222222222222222")
	eventID := mustID(t, domain.NewEventID, "950e8400e29b41d4a716446655440000")
	occurredAt := timePoint(t, time.Date(2026, 4, 24, 12, 30, 0, 0, time.UTC))
	receivedAt := timePoint(t, time.Date(2026, 4, 24, 12, 30, 1, 0, time.UTC))
	title := eventTitle(t, "panic: broken pipe")

	event, eventErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		EventID:              eventID,
		OccurredAt:           occurredAt,
		ReceivedAt:           receivedAt,
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title,
		Platform:             "go",
		DefaultGroupingParts: []string{"panic", "worker.go", "12"},
	})
	if eventErr != nil {
		t.Fatalf("event: %v", eventErr)
	}

	return event
}

func issueID(t *testing.T) domain.IssueID {
	t.Helper()

	id, idErr := domain.NewIssueID("3333333333334333a333333333333333")
	if idErr != nil {
		t.Fatalf("issue id: %v", idErr)
	}

	return id
}

func mustID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	id, idErr := constructor(input)
	if idErr != nil {
		t.Fatalf("id: %v", idErr)
	}

	return id
}

func timePoint(t *testing.T, value time.Time) domain.TimePoint {
	t.Helper()

	point, pointErr := domain.NewTimePoint(value)
	if pointErr != nil {
		t.Fatalf("time point: %v", pointErr)
	}

	return point
}

func eventTitle(t *testing.T, input string) domain.EventTitle {
	t.Helper()

	title, titleErr := domain.NewEventTitle(input)
	if titleErr != nil {
		t.Fatalf("title: %v", titleErr)
	}

	return title
}

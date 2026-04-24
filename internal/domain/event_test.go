package domain

import (
	"testing"
	"time"
)

func TestEventIDNormalizesDashedUUID(t *testing.T) {
	eventID, err := NewEventID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatalf("expected event id: %v", err)
	}

	if eventID.Hex() != "550e8400e29b41d4a716446655440000" {
		t.Fatalf("unexpected hex: %s", eventID.Hex())
	}

	if eventID.String() != "550e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("unexpected string: %s", eventID.String())
	}
}

func TestCanonicalEventUsesTitleAsFallbackGroupingPart(t *testing.T) {
	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:  EventKindError,
		Level: EventLevelError,
		Title: mustTitle(t, "TypeError: bad operand"),
	})

	parts := event.DefaultGroupingParts()
	if len(parts) != 1 {
		t.Fatalf("expected one grouping part, got %d", len(parts))
	}

	if parts[0] != "TypeError: bad operand" {
		t.Fatalf("unexpected grouping part: %s", parts[0])
	}
}

func mustCanonicalEvent(t *testing.T, params CanonicalEventParams) CanonicalEvent {
	t.Helper()

	organizationID, orgErr := NewOrganizationID("1111111111114111a111111111111111")
	if orgErr != nil {
		t.Fatalf("organization id: %v", orgErr)
	}

	projectID, projectErr := NewProjectID("2222222222224222a222222222222222")
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	eventID, eventErr := NewEventID("550e8400e29b41d4a716446655440000")
	if eventErr != nil {
		t.Fatalf("event id: %v", eventErr)
	}

	occurredAt, occurredErr := NewTimePoint(time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
	if occurredErr != nil {
		t.Fatalf("occurred at: %v", occurredErr)
	}

	receivedAt, receivedErr := NewTimePoint(time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC))
	if receivedErr != nil {
		t.Fatalf("received at: %v", receivedErr)
	}

	if params.OrganizationID.value == "" {
		params.OrganizationID = organizationID
	}

	if params.ProjectID.value == "" {
		params.ProjectID = projectID
	}

	if params.EventID.value == "" {
		params.EventID = eventID
	}

	if params.OccurredAt.value.IsZero() {
		params.OccurredAt = occurredAt
	}

	if params.ReceivedAt.value.IsZero() {
		params.ReceivedAt = receivedAt
	}

	event, err := NewCanonicalEvent(params)
	if err != nil {
		t.Fatalf("canonical event: %v", err)
	}

	return event
}

func mustTitle(t *testing.T, input string) EventTitle {
	t.Helper()

	title, err := NewEventTitle(input)
	if err != nil {
		t.Fatalf("title: %v", err)
	}

	return title
}

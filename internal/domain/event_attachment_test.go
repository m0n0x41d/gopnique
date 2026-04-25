package domain

import (
	"strings"
	"testing"
)

func TestNewEventAttachmentAcceptsTypicalValues(t *testing.T) {
	name, nameErr := NewArtifactName("crash-2026-04-25.dmp")
	if nameErr != nil {
		t.Fatalf("artifact name: %v", nameErr)
	}

	attachment, err := NewEventAttachment(
		ArtifactKindMinidump(),
		name,
		1024,
		"application/octet-stream",
	)
	if err != nil {
		t.Fatalf("event attachment: %v", err)
	}

	if attachment.Kind().String() != "minidump" {
		t.Fatalf("unexpected kind: %s", attachment.Kind().String())
	}

	if attachment.Name().String() != "crash-2026-04-25.dmp" {
		t.Fatalf("unexpected name: %s", attachment.Name().String())
	}

	if attachment.ByteSize() != 1024 {
		t.Fatalf("unexpected byte size: %d", attachment.ByteSize())
	}

	if attachment.ContentType() != "application/octet-stream" {
		t.Fatalf("unexpected content type: %s", attachment.ContentType())
	}
}

func TestNewEventAttachmentAcceptsEmptyContentType(t *testing.T) {
	name, nameErr := NewArtifactName("app.min.js.map")
	if nameErr != nil {
		t.Fatalf("artifact name: %v", nameErr)
	}

	attachment, err := NewEventAttachment(
		ArtifactKindSourceMap(),
		name,
		0,
		"   ",
	)
	if err != nil {
		t.Fatalf("event attachment: %v", err)
	}

	if attachment.ContentType() != "" {
		t.Fatalf("expected empty content type, got %q", attachment.ContentType())
	}

	if attachment.ByteSize() != 0 {
		t.Fatalf("expected zero byte size, got %d", attachment.ByteSize())
	}
}

func TestNewEventAttachmentRejectsInvalidValues(t *testing.T) {
	name, nameErr := NewArtifactName("crash.dmp")
	if nameErr != nil {
		t.Fatalf("artifact name: %v", nameErr)
	}

	cases := []struct {
		label       string
		kind        ArtifactKind
		name        ArtifactName
		byteSize    int64
		contentType string
	}{
		{label: "missing kind", kind: ArtifactKind{}, name: name, byteSize: 1, contentType: "application/octet-stream"},
		{label: "missing name", kind: ArtifactKindMinidump(), name: ArtifactName{}, byteSize: 1, contentType: ""},
		{label: "negative byte size", kind: ArtifactKindMinidump(), name: name, byteSize: -1, contentType: ""},
		{label: "control character in content type", kind: ArtifactKindMinidump(), name: name, byteSize: 0, contentType: "application/\toctet"},
		{label: "oversized content type", kind: ArtifactKindMinidump(), name: name, byteSize: 0, contentType: strings.Repeat("a", eventAttachmentContentTypeMaxBytes+1)},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := NewEventAttachment(tc.kind, tc.name, tc.byteSize, tc.contentType)
			if err == nil {
				t.Fatalf("expected %s to be rejected", tc.label)
			}
		})
	}
}

func TestCanonicalEventReturnsAttachmentsCopy(t *testing.T) {
	name, nameErr := NewArtifactName("crash.dmp")
	if nameErr != nil {
		t.Fatalf("artifact name: %v", nameErr)
	}

	attachment, attachmentErr := NewEventAttachment(
		ArtifactKindMinidump(),
		name,
		2048,
		"application/octet-stream",
	)
	if attachmentErr != nil {
		t.Fatalf("event attachment: %v", attachmentErr)
	}

	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:        EventKindError,
		Level:       EventLevelError,
		Title:       mustTitle(t, "TypeError: bad operand"),
		Attachments: []EventAttachment{attachment},
	})

	first := event.Attachments()
	if len(first) != 1 {
		t.Fatalf("expected one attachment, got %d", len(first))
	}

	first[0] = EventAttachment{}

	second := event.Attachments()
	if len(second) != 1 {
		t.Fatalf("expected one attachment after mutation, got %d", len(second))
	}

	if second[0].Name().String() != "crash.dmp" {
		t.Fatalf("expected attachment to remain stable, got %q", second[0].Name().String())
	}
}

func TestCanonicalEventWithoutAttachmentsReturnsEmpty(t *testing.T) {
	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:  EventKindDefault,
		Level: EventLevelInfo,
		Title: mustTitle(t, "no attachments"),
	})

	attachments := event.Attachments()
	if len(attachments) != 0 {
		t.Fatalf("expected no attachments, got %d", len(attachments))
	}
}

package domain

import (
	"strings"
	"testing"
)

func TestNewMinidumpAttachmentNameAcceptsTypicalValues(t *testing.T) {
	cases := []string{
		"upload_file_minidump",
		"crash.dmp",
		"reports/2026/04/crash.dmp",
		"漢字.dmp",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			name, err := NewMinidumpAttachmentName(input)
			if err != nil {
				t.Fatalf("attachment name: %v", err)
			}

			if name.String() != strings.TrimSpace(input) {
				t.Fatalf("expected %q, got %q", input, name.String())
			}
		})
	}
}

func TestNewMinidumpAttachmentNameRejectsUnsafeValues(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"only spaces", "   "},
		{"path traversal", "../../etc/passwd"},
		{"embedded traversal", "reports/../etc/passwd"},
		{"absolute path", "/var/crashes/app.dmp"},
		{"backslash", "reports\\crash.dmp"},
		{"null byte", "reports/crash\x00.dmp"},
		{"control char", "reports/crash\n.dmp"},
		{"too long", strings.Repeat("a", minidumpAttachmentNameMaxBytes+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMinidumpAttachmentName(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
		})
	}
}

func TestMinidumpIdentityArtifactNameIsDeterministic(t *testing.T) {
	eventID, _ := NewEventID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	name, _ := NewMinidumpAttachmentName("upload_file_minidump")

	identity, identityErr := NewMinidumpIdentity(eventID, name)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	first := identity.ArtifactName().String()
	second := identity.ArtifactName().String()

	if first != second {
		t.Fatalf("expected deterministic artifact name, got %q and %q", first, second)
	}

	if !strings.HasSuffix(first, ".mdmp") {
		t.Fatalf("expected .mdmp suffix, got %q", first)
	}

	_, err := NewArtifactName(first)
	if err != nil {
		t.Fatalf("derived name must be a valid artifact name: %v", err)
	}
}

func TestMinidumpIdentityArtifactNameDistinguishesFields(t *testing.T) {
	eventID, _ := NewEventID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	otherEventID, _ := NewEventID("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	name, _ := NewMinidumpAttachmentName("upload_file_minidump")
	otherName, _ := NewMinidumpAttachmentName("crash.dmp")

	base, _ := NewMinidumpIdentity(eventID, name)
	eventChange, _ := NewMinidumpIdentity(otherEventID, name)
	nameChange, _ := NewMinidumpIdentity(eventID, otherName)

	if base.ArtifactName().String() == eventChange.ArtifactName().String() {
		t.Fatal("event id change must produce different artifact name")
	}

	if base.ArtifactName().String() == nameChange.ArtifactName().String() {
		t.Fatal("attachment name change must produce different artifact name")
	}
}

func TestNewMinidumpIdentityRequiresAllFields(t *testing.T) {
	eventID, _ := NewEventID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	name, _ := NewMinidumpAttachmentName("upload_file_minidump")

	cases := []struct {
		name           string
		eventID        EventID
		attachmentName MinidumpAttachmentName
	}{
		{"missing event id", EventID{}, name},
		{"missing attachment name", eventID, MinidumpAttachmentName{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMinidumpIdentity(tc.eventID, tc.attachmentName)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

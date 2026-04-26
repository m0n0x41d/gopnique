package domain

import (
	"strings"
	"testing"
)

func TestParseArtifactKindAcceptsKnownValues(t *testing.T) {
	cases := []struct {
		input    string
		expected ArtifactKind
	}{
		{"source_map", ArtifactKindSourceMap()},
		{"  Source_Map ", ArtifactKindSourceMap()},
		{"DEBUG_FILE", ArtifactKindDebugFile()},
		{"minidump", ArtifactKindMinidump()},
		{"attachment", ArtifactKindAttachment()},
		{"upload_chunk", ArtifactKindUploadChunk()},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			parsed, err := ParseArtifactKind(tc.input)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if parsed.String() != tc.expected.String() {
				t.Fatalf("expected %q, got %q", tc.expected.String(), parsed.String())
			}
		})
	}
}

func TestParseArtifactKindRejectsUnknown(t *testing.T) {
	_, err := ParseArtifactKind("event")
	if err == nil {
		t.Fatal("expected unknown artifact kind to be rejected")
	}
}

func TestNewArtifactNameAcceptsSafeNames(t *testing.T) {
	cases := []string{
		"app.min.js.map",
		"libfoo.dSYM",
		"crash-2026-04-25.dmp",
		"漢字.txt",
		"with spaces.bin",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			name, err := NewArtifactName(input)
			if err != nil {
				t.Fatalf("name: %v", err)
			}

			if name.String() != strings.TrimSpace(input) {
				t.Fatalf("expected %q, got %q", input, name.String())
			}
		})
	}
}

func TestNewArtifactNameRejectsUnsafeNames(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"only spaces", "   "},
		{"path separator", "foo/bar"},
		{"backslash", "foo\\bar"},
		{"leading dot", ".hidden"},
		{"single dot", "."},
		{"double dot", ".."},
		{"path traversal forward", "../etc/passwd"},
		{"embedded traversal", "foo/../bar"},
		{"null byte", "foo\x00bar"},
		{"control char", "foo\nbar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewArtifactName(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
		})
	}
}

func TestNewArtifactNameRejectsTooLong(t *testing.T) {
	long := strings.Repeat("a", artifactNameMaxBytes+1)

	_, err := NewArtifactName(long)
	if err == nil {
		t.Fatal("expected long name to be rejected")
	}
}

func TestNewArtifactKeyRequiresAllParts(t *testing.T) {
	orgID, _ := NewOrganizationID("11111111-1111-1111-1111-111111111111")
	projectID, _ := NewProjectID("22222222-2222-2222-2222-222222222222")
	name, _ := NewArtifactName("file.bin")

	cases := []struct {
		name string
		org  OrganizationID
		proj ProjectID
		kind ArtifactKind
		nm   ArtifactName
	}{
		{"missing org", OrganizationID{}, projectID, ArtifactKindAttachment(), name},
		{"missing project", orgID, ProjectID{}, ArtifactKindAttachment(), name},
		{"missing kind", orgID, projectID, ArtifactKind{}, name},
		{"missing name", orgID, projectID, ArtifactKindAttachment(), ArtifactName{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewArtifactKey(tc.org, tc.proj, tc.kind, tc.nm)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestNewArtifactKeyAccessors(t *testing.T) {
	orgID, _ := NewOrganizationID("11111111-1111-1111-1111-111111111111")
	projectID, _ := NewProjectID("22222222-2222-2222-2222-222222222222")
	name, _ := NewArtifactName("file.bin")

	key, err := NewArtifactKey(orgID, projectID, ArtifactKindSourceMap(), name)
	if err != nil {
		t.Fatalf("key: %v", err)
	}

	if key.OrganizationID().String() != orgID.String() {
		t.Fatal("org id mismatch")
	}

	if key.ProjectID().String() != projectID.String() {
		t.Fatal("project id mismatch")
	}

	if key.Kind().String() != "source_map" {
		t.Fatalf("unexpected kind: %s", key.Kind().String())
	}

	if key.Name().String() != "file.bin" {
		t.Fatalf("unexpected name: %s", key.Name().String())
	}
}

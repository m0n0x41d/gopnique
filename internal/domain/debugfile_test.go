package domain

import (
	"strings"
	"testing"
)

func TestParseDebugFileKindAcceptsKnownValues(t *testing.T) {
	cases := []struct {
		input    string
		expected DebugFileKind
	}{
		{"breakpad", DebugFileKindBreakpad()},
		{"  Breakpad ", DebugFileKindBreakpad()},
		{"ELF", DebugFileKindELF()},
		{"macho", DebugFileKindMachO()},
		{"PDB", DebugFileKindPDB()},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			parsed, err := ParseDebugFileKind(tc.input)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}

			if parsed.String() != tc.expected.String() {
				t.Fatalf("expected %q, got %q", tc.expected.String(), parsed.String())
			}
		})
	}
}

func TestParseDebugFileKindRejectsUnknown(t *testing.T) {
	_, err := ParseDebugFileKind("dwarf")
	if err == nil {
		t.Fatal("expected unknown debug file kind to be rejected")
	}
}

func TestNewDebugIdentifierAcceptsTypicalValues(t *testing.T) {
	cases := []struct {
		input      string
		normalized string
	}{
		{"deadbeefcafef00ddeadbeefcafef00d", "deadbeefcafef00ddeadbeefcafef00d"},
		{"DEADBEEF-CAFE-F00D-DEAD-BEEFCAFEF00D", "deadbeefcafef00ddeadbeefcafef00d"},
		{"abcdef0123456789abcdef0123456789abcdef01", "abcdef0123456789abcdef0123456789abcdef01"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			id, err := NewDebugIdentifier(tc.input)
			if err != nil {
				t.Fatalf("identifier: %v", err)
			}

			if id.String() != tc.normalized {
				t.Fatalf("expected %q, got %q", tc.normalized, id.String())
			}
		})
	}
}

func TestNewDebugIdentifierRejectsUnsafeValues(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"only spaces", "   "},
		{"too short", "deadbeef"},
		{"too long", strings.Repeat("a", debugIdentifierMaxHexBytes+1)},
		{"non hex characters", strings.Repeat("g", debugIdentifierMinHexBytes)},
		{"control char", "deadbeefcafef00ddeadbeefcafef0\n0d"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDebugIdentifier(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
		})
	}
}

func TestNewDebugFileNameAcceptsTypicalValues(t *testing.T) {
	cases := []string{
		"libapp.so.dbg",
		"AppName.dSYM/Contents/Resources/DWARF/AppName",
		"app.pdb",
		"漢字.dbg",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			name, err := NewDebugFileName(input)
			if err != nil {
				t.Fatalf("name: %v", err)
			}

			if name.String() != strings.TrimSpace(input) {
				t.Fatalf("expected %q, got %q", input, name.String())
			}
		})
	}
}

func TestNewDebugFileNameRejectsUnsafeValues(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"path traversal", "../etc/passwd"},
		{"embedded traversal", "syms/../etc/passwd"},
		{"absolute path", "/var/log/app.dbg"},
		{"backslash", "syms\\app.dbg"},
		{"null byte", "syms/app\x00.dbg"},
		{"control char", "syms/app\n.dbg"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDebugFileName(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
		})
	}
}

func TestDebugFileIdentityArtifactNameIsDeterministic(t *testing.T) {
	id, _ := NewDebugIdentifier("deadbeefcafef00ddeadbeefcafef00d")
	kind := DebugFileKindBreakpad()
	name, _ := NewDebugFileName("libapp.so.sym")

	identity, identityErr := NewDebugFileIdentity(id, kind, name)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	first := identity.ArtifactName().String()
	second := identity.ArtifactName().String()

	if first != second {
		t.Fatalf("expected deterministic artifact name, got %q and %q", first, second)
	}

	if !strings.HasSuffix(first, ".dif") {
		t.Fatalf("expected .dif suffix, got %q", first)
	}

	_, err := NewArtifactName(first)
	if err != nil {
		t.Fatalf("derived name must be a valid artifact name: %v", err)
	}
}

func TestDebugFileIdentityArtifactNameDistinguishesFields(t *testing.T) {
	id, _ := NewDebugIdentifier("deadbeefcafef00ddeadbeefcafef00d")
	otherID, _ := NewDebugIdentifier("ffffffffffffffffffffffffffffffff")
	kind := DebugFileKindBreakpad()
	otherKind := DebugFileKindELF()
	name, _ := NewDebugFileName("libapp.so.sym")
	otherName, _ := NewDebugFileName("libapp.so.dbg")

	base, _ := NewDebugFileIdentity(id, kind, name)
	idChange, _ := NewDebugFileIdentity(otherID, kind, name)
	kindChange, _ := NewDebugFileIdentity(id, otherKind, name)
	nameChange, _ := NewDebugFileIdentity(id, kind, otherName)

	baseName := base.ArtifactName().String()
	if baseName == idChange.ArtifactName().String() {
		t.Fatal("identifier change must produce different artifact name")
	}

	if baseName == kindChange.ArtifactName().String() {
		t.Fatal("kind change must produce different artifact name")
	}

	if baseName == nameChange.ArtifactName().String() {
		t.Fatal("file name change must produce different artifact name")
	}
}

func TestNewDebugFileIdentityRequiresAllFields(t *testing.T) {
	id, _ := NewDebugIdentifier("deadbeefcafef00ddeadbeefcafef00d")
	kind := DebugFileKindBreakpad()
	name, _ := NewDebugFileName("libapp.so.sym")

	cases := []struct {
		name     string
		debugID  DebugIdentifier
		kind     DebugFileKind
		fileName DebugFileName
	}{
		{"missing identifier", DebugIdentifier{}, kind, name},
		{"missing kind", id, DebugFileKind{}, name},
		{"missing file", id, kind, DebugFileName{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDebugFileIdentity(tc.debugID, tc.kind, tc.fileName)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

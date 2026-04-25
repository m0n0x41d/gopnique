package domain

import (
	"strings"
	"testing"
)

func TestNewReleaseNameAcceptsTypicalValues(t *testing.T) {
	cases := []string{
		"frontend@1.2.3",
		"frontend@1.2.3+build.42",
		"abcdef0123",
		"漢字-release",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			release, err := NewReleaseName(input)
			if err != nil {
				t.Fatalf("release: %v", err)
			}

			if release.String() != strings.TrimSpace(input) {
				t.Fatalf("expected %q, got %q", input, release.String())
			}
		})
	}
}

func TestNewReleaseNameRejectsUnsafeValues(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"only spaces", "   "},
		{"control char", "rel\nease"},
		{"null byte", "rel\x00ease"},
		{"too long", strings.Repeat("a", releaseNameMaxBytes+1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewReleaseName(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
		})
	}
}

func TestNewOptionalDistNameAcceptsEmptyAsAbsent(t *testing.T) {
	dist, err := NewOptionalDistName("")
	if err != nil {
		t.Fatalf("optional dist: %v", err)
	}

	if dist.HasValue() {
		t.Fatal("empty dist must be absent")
	}
}

func TestNewOptionalDistNameAcceptsValue(t *testing.T) {
	dist, err := NewOptionalDistName("ios-arm64")
	if err != nil {
		t.Fatalf("dist: %v", err)
	}

	if !dist.HasValue() {
		t.Fatal("dist must be present")
	}

	if dist.String() != "ios-arm64" {
		t.Fatalf("unexpected dist: %s", dist.String())
	}
}

func TestNewSourceMapFileNameAcceptsTypicalValues(t *testing.T) {
	cases := []string{
		"app.min.js",
		"chunks/main.42.js",
		"static/js/bundle.js",
		"漢字.js",
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			file, err := NewSourceMapFileName(input)
			if err != nil {
				t.Fatalf("file: %v", err)
			}

			if file.String() != strings.TrimSpace(input) {
				t.Fatalf("expected %q, got %q", input, file.String())
			}
		})
	}
}

func TestNewSourceMapFileNameRejectsUnsafeValues(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"path traversal", "../etc/passwd"},
		{"embedded traversal", "chunks/../etc/passwd"},
		{"absolute path", "/etc/passwd"},
		{"backslash", "chunks\\main.js"},
		{"null byte", "chunks/main\x00.js"},
		{"control char", "chunks/main\n.js"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSourceMapFileName(tc.input)
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
		})
	}
}

func TestSourceMapIdentityArtifactNameIsDeterministic(t *testing.T) {
	release, _ := NewReleaseName("frontend@1.0.0")
	dist, _ := NewOptionalDistName("")
	file, _ := NewSourceMapFileName("static/js/app.min.js")

	identity, identityErr := NewSourceMapIdentity(release, dist, file)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	first := identity.ArtifactName().String()
	second := identity.ArtifactName().String()

	if first != second {
		t.Fatalf("expected deterministic artifact name, got %q and %q", first, second)
	}

	if !strings.HasSuffix(first, ".map") {
		t.Fatalf("expected .map suffix, got %q", first)
	}

	_, err := NewArtifactName(first)
	if err != nil {
		t.Fatalf("derived name must be a valid artifact name: %v", err)
	}
}

func TestSourceMapIdentityArtifactNameDistinguishesFields(t *testing.T) {
	release, _ := NewReleaseName("frontend@1.0.0")
	otherRelease, _ := NewReleaseName("frontend@2.0.0")
	noDist, _ := NewOptionalDistName("")
	dist, _ := NewOptionalDistName("ios-arm64")
	file, _ := NewSourceMapFileName("static/js/app.min.js")
	otherFile, _ := NewSourceMapFileName("static/js/vendor.min.js")

	base, _ := NewSourceMapIdentity(release, noDist, file)
	releaseChange, _ := NewSourceMapIdentity(otherRelease, noDist, file)
	distChange, _ := NewSourceMapIdentity(release, dist, file)
	fileChange, _ := NewSourceMapIdentity(release, noDist, otherFile)

	baseName := base.ArtifactName().String()
	if baseName == releaseChange.ArtifactName().String() {
		t.Fatal("release change must produce different artifact name")
	}

	if baseName == distChange.ArtifactName().String() {
		t.Fatal("dist change must produce different artifact name")
	}

	if baseName == fileChange.ArtifactName().String() {
		t.Fatal("file change must produce different artifact name")
	}
}

func TestNewSourceMapIdentityRequiresReleaseAndFileName(t *testing.T) {
	release, _ := NewReleaseName("frontend@1.0.0")
	noDist, _ := NewOptionalDistName("")
	file, _ := NewSourceMapFileName("static/js/app.min.js")

	cases := []struct {
		name     string
		release  ReleaseName
		dist     DistName
		fileName SourceMapFileName
	}{
		{"missing release", ReleaseName{}, noDist, file},
		{"missing file", release, noDist, SourceMapFileName{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewSourceMapIdentity(tc.release, tc.dist, tc.fileName)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

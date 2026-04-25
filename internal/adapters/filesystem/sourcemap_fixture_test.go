package filesystem_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/adapters/filesystem"
	"github.com/ivanzakutnii/error-tracker/internal/app/sourcemaps"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

const stacktraceFixture = `{
  "version": 3,
  "file": "app.min.js",
  "sourceRoot": "",
  "sources": ["original.js"],
  "names": ["computeTotal", "items", "sum", "value"],
  "mappings": "AAAA,SAASA,aAAaC,GACpB,OAAOA,EAAMC,OAAO,SAACC,EAAKC,GAClB,OAAOD,EAAMC,GACd,IAAG"
}`

func TestStacktraceFixtureResolvesAndDegrades(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	vault, vaultErr := filesystem.NewVault(filepath.Clean(root))
	if vaultErr != nil {
		t.Fatalf("vault: %v", vaultErr)
	}

	service, serviceErr := sourcemaps.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("service: %v", serviceErr)
	}

	organizationID, _ := domain.NewOrganizationID("11111111-1111-1111-1111-111111111111")
	projectID, _ := domain.NewProjectID("22222222-2222-2222-2222-222222222222")

	release, _ := domain.NewReleaseName("frontend@1.0.0")
	dist, _ := domain.NewOptionalDistName("")
	fileName, _ := domain.NewSourceMapFileName("static/js/app.min.js")
	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	uploadResult := service.Upload(
		ctx,
		organizationID,
		projectID,
		identity,
		bytes.NewReader([]byte(stacktraceFixture)),
	)
	if _, err := uploadResult.Value(); err != nil {
		t.Fatalf("upload: %v", err)
	}

	resolveResult := service.Resolve(
		ctx,
		organizationID,
		projectID,
		identity,
		sourcemaps.NewGeneratedPosition(0, 9),
	)
	resolved, resolveErr := resolveResult.Value()
	if resolveErr != nil {
		t.Fatalf("resolve known frame: %v", resolveErr)
	}

	if resolved.Source() != "original.js" {
		t.Fatalf("expected resolved source original.js, got %q", resolved.Source())
	}

	name, hasName := resolved.Name()
	if !hasName || name != "computeTotal" {
		t.Fatalf("expected name computeTotal, got %q hasName=%v", name, hasName)
	}

	if resolved.OriginalLine() != 0 {
		t.Fatalf("expected original line 0, got %d", resolved.OriginalLine())
	}

	missingResolveResult := service.Resolve(
		ctx,
		organizationID,
		projectID,
		identity,
		sourcemaps.NewGeneratedPosition(99, 0),
	)
	if _, err := missingResolveResult.Value(); !errors.Is(err, sourcemaps.ErrSourceMapNotFound) {
		t.Fatalf("expected unresolved frame to surface ErrSourceMapNotFound, got %v", err)
	}

	otherFile, _ := domain.NewSourceMapFileName("static/js/missing.min.js")
	otherIdentity, _ := domain.NewSourceMapIdentity(release, dist, otherFile)
	missingMapResult := service.Resolve(
		ctx,
		organizationID,
		projectID,
		otherIdentity,
		sourcemaps.NewGeneratedPosition(0, 0),
	)
	if _, err := missingMapResult.Value(); !errors.Is(err, sourcemaps.ErrSourceMapNotFound) {
		t.Fatalf("expected missing source map to surface ErrSourceMapNotFound, got %v", err)
	}
}

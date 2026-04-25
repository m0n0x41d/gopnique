package filesystem

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestNewVaultRejectsRelativeRoot(t *testing.T) {
	_, err := NewVault("artifacts")
	if err == nil {
		t.Fatal("expected relative root to be rejected")
	}
}

func TestNewVaultRejectsEmptyRoot(t *testing.T) {
	_, err := NewVault("   ")
	if err == nil {
		t.Fatal("expected empty root to be rejected")
	}
}

func TestNewVaultCreatesRootDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")

	vault, err := NewVault(root)
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	info, statErr := os.Stat(vault.Root())
	if statErr != nil {
		t.Fatalf("stat root: %v", statErr)
	}

	if !info.IsDir() {
		t.Fatal("root must be a directory")
	}
}

func TestVaultRoundTripsArtifact(t *testing.T) {
	vault := newTestVault(t)
	ctx := context.Background()
	key := newTestKey(t, "app.min.js.map", domain.ArtifactKindSourceMap())
	contents := []byte("//# sourceMappingURL=app.min.js")

	stored, putErr := vault.PutArtifact(ctx, key, bytes.NewReader(contents)).Value()
	if putErr != nil {
		t.Fatalf("put: %v", putErr)
	}

	if stored.Size() != int64(len(contents)) {
		t.Fatalf("unexpected size: %d", stored.Size())
	}

	reader, getErr := vault.GetArtifact(ctx, key).Value()
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}

	defer func() {
		_ = reader.Close()
	}()

	read, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}

	if !bytes.Equal(read, contents) {
		t.Fatalf("contents differ: %q vs %q", read, contents)
	}
}

func TestVaultPutOverwritesExistingArtifact(t *testing.T) {
	vault := newTestVault(t)
	ctx := context.Background()
	key := newTestKey(t, "frame.dSYM", domain.ArtifactKindDebugFile())

	_, firstErr := vault.PutArtifact(ctx, key, strings.NewReader("first")).Value()
	if firstErr != nil {
		t.Fatalf("first put: %v", firstErr)
	}

	_, secondErr := vault.PutArtifact(ctx, key, strings.NewReader("second")).Value()
	if secondErr != nil {
		t.Fatalf("second put: %v", secondErr)
	}

	reader, getErr := vault.GetArtifact(ctx, key).Value()
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}

	defer func() {
		_ = reader.Close()
	}()

	body, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}

	if string(body) != "second" {
		t.Fatalf("expected overwrite, got %q", body)
	}
}

func TestVaultGetReturnsNotFoundForMissingArtifact(t *testing.T) {
	vault := newTestVault(t)
	ctx := context.Background()
	key := newTestKey(t, "missing.bin", domain.ArtifactKindAttachment())

	_, err := vault.GetArtifact(ctx, key).Value()
	if !errors.Is(err, artifacts.ErrArtifactNotFound) {
		t.Fatalf("expected ErrArtifactNotFound, got %v", err)
	}
}

func TestVaultDeleteIsIdempotent(t *testing.T) {
	vault := newTestVault(t)
	ctx := context.Background()
	key := newTestKey(t, "to-delete.bin", domain.ArtifactKindAttachment())

	_, putErr := vault.PutArtifact(ctx, key, strings.NewReader("payload")).Value()
	if putErr != nil {
		t.Fatalf("put: %v", putErr)
	}

	_, firstErr := vault.DeleteArtifact(ctx, key).Value()
	if firstErr != nil {
		t.Fatalf("first delete: %v", firstErr)
	}

	_, secondErr := vault.DeleteArtifact(ctx, key).Value()
	if secondErr != nil {
		t.Fatalf("second delete should be idempotent: %v", secondErr)
	}

	_, getErr := vault.GetArtifact(ctx, key).Value()
	if !errors.Is(getErr, artifacts.ErrArtifactNotFound) {
		t.Fatalf("expected artifact to be gone, got %v", getErr)
	}
}

func TestVaultListReturnsScopedArtifacts(t *testing.T) {
	vault := newTestVault(t)
	ctx := context.Background()

	orgID := mustOrg(t, "11111111-1111-1111-1111-111111111111")
	projectID := mustProject(t, "22222222-2222-2222-2222-222222222222")
	otherProjectID := mustProject(t, "33333333-3333-3333-3333-333333333333")

	mustPut(t, vault, ctx, mustKey(t, orgID, projectID, domain.ArtifactKindSourceMap(), "a.js.map"), "alpha")
	mustPut(t, vault, ctx, mustKey(t, orgID, projectID, domain.ArtifactKindSourceMap(), "b.js.map"), "beta-payload")
	mustPut(t, vault, ctx, mustKey(t, orgID, projectID, domain.ArtifactKindDebugFile(), "frame.dSYM"), "debug")
	mustPut(t, vault, ctx, mustKey(t, orgID, otherProjectID, domain.ArtifactKindSourceMap(), "leak.js.map"), "leak")

	scope, scopeErr := artifacts.NewArtifactScope(orgID, projectID, domain.ArtifactKindSourceMap())
	if scopeErr != nil {
		t.Fatalf("scope: %v", scopeErr)
	}

	list, listErr := vault.ListArtifacts(ctx, scope).Value()
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}

	if len(list) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(list))
	}

	if list[0].Key().Name().String() != "a.js.map" || list[1].Key().Name().String() != "b.js.map" {
		t.Fatalf("unexpected order: %v", list)
	}

	if list[1].Size() != int64(len("beta-payload")) {
		t.Fatalf("unexpected size: %d", list[1].Size())
	}
}

func TestVaultListReturnsEmptyWhenScopeMissing(t *testing.T) {
	vault := newTestVault(t)
	ctx := context.Background()

	scope, scopeErr := artifacts.NewArtifactScope(
		mustOrg(t, "11111111-1111-1111-1111-111111111111"),
		mustProject(t, "22222222-2222-2222-2222-222222222222"),
		domain.ArtifactKindAttachment(),
	)
	if scopeErr != nil {
		t.Fatalf("scope: %v", scopeErr)
	}

	list, err := vault.ListArtifacts(ctx, scope).Value()
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestVaultListIgnoresInProgressTempFiles(t *testing.T) {
	vault := newTestVault(t)
	ctx := context.Background()
	orgID := mustOrg(t, "11111111-1111-1111-1111-111111111111")
	projectID := mustProject(t, "22222222-2222-2222-2222-222222222222")

	mustPut(t, vault, ctx, mustKey(t, orgID, projectID, domain.ArtifactKindAttachment(), "real.bin"), "ok")

	scope, _ := artifacts.NewArtifactScope(orgID, projectID, domain.ArtifactKindAttachment())
	scopeDir := filepath.Join(
		vault.Root(),
		orgID.String(),
		projectID.String(),
		domain.ArtifactKindAttachment().String(),
	)

	tempPath := filepath.Join(scopeDir, ".artifact-leftover")
	tempErr := os.WriteFile(tempPath, []byte("partial"), 0o644)
	if tempErr != nil {
		t.Fatalf("temp file: %v", tempErr)
	}

	list, err := vault.ListArtifacts(ctx, scope).Value()
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(list) != 1 || list[0].Key().Name().String() != "real.bin" {
		t.Fatalf("expected only real.bin, got %v", list)
	}
}

func TestVaultPutCreatesFileUnderScopedPath(t *testing.T) {
	vault := newTestVault(t)
	ctx := context.Background()
	orgID := mustOrg(t, "11111111-1111-1111-1111-111111111111")
	projectID := mustProject(t, "22222222-2222-2222-2222-222222222222")
	key := mustKey(t, orgID, projectID, domain.ArtifactKindSourceMap(), "bundle.js.map")

	mustPut(t, vault, ctx, key, "payload")

	expected := filepath.Join(
		vault.Root(),
		orgID.String(),
		projectID.String(),
		domain.ArtifactKindSourceMap().String(),
		"bundle.js.map",
	)

	_, statErr := os.Stat(expected)
	if statErr != nil {
		t.Fatalf("expected artifact at %s: %v", expected, statErr)
	}
}

func newTestVault(t *testing.T) *Vault {
	t.Helper()

	vault, err := NewVault(t.TempDir())
	if err != nil {
		t.Fatalf("vault: %v", err)
	}

	return vault
}

func newTestKey(t *testing.T, name string, kind domain.ArtifactKind) domain.ArtifactKey {
	t.Helper()

	orgID := mustOrg(t, "11111111-1111-1111-1111-111111111111")
	projectID := mustProject(t, "22222222-2222-2222-2222-222222222222")

	return mustKey(t, orgID, projectID, kind, name)
}

func mustKey(
	t *testing.T,
	orgID domain.OrganizationID,
	projectID domain.ProjectID,
	kind domain.ArtifactKind,
	name string,
) domain.ArtifactKey {
	t.Helper()

	artifactName, nameErr := domain.NewArtifactName(name)
	if nameErr != nil {
		t.Fatalf("name: %v", nameErr)
	}

	key, keyErr := domain.NewArtifactKey(orgID, projectID, kind, artifactName)
	if keyErr != nil {
		t.Fatalf("key: %v", keyErr)
	}

	return key
}

func mustOrg(t *testing.T, value string) domain.OrganizationID {
	t.Helper()

	id, err := domain.NewOrganizationID(value)
	if err != nil {
		t.Fatalf("org id: %v", err)
	}

	return id
}

func mustProject(t *testing.T, value string) domain.ProjectID {
	t.Helper()

	id, err := domain.NewProjectID(value)
	if err != nil {
		t.Fatalf("project id: %v", err)
	}

	return id
}

func mustPut(
	t *testing.T,
	vault *Vault,
	ctx context.Context,
	key domain.ArtifactKey,
	body string,
) {
	t.Helper()

	_, err := vault.PutArtifact(ctx, key, strings.NewReader(body)).Value()
	if err != nil {
		t.Fatalf("put: %v", err)
	}
}

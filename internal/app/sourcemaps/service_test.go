package sourcemaps

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type memoryVault struct {
	contents map[string][]byte
}

func newMemoryVault() *memoryVault {
	return &memoryVault{contents: map[string][]byte{}}
}

func (vault *memoryVault) PutArtifact(
	_ context.Context,
	key domain.ArtifactKey,
	contents io.Reader,
) result.Result[artifacts.StoredArtifact] {
	body, readErr := io.ReadAll(contents)
	if readErr != nil {
		return result.Err[artifacts.StoredArtifact](readErr)
	}

	vault.contents[memoryKey(key)] = body
	return result.Ok(artifacts.NewStoredArtifact(key, int64(len(body))))
}

func (vault *memoryVault) GetArtifact(
	_ context.Context,
	key domain.ArtifactKey,
) result.Result[io.ReadCloser] {
	body, present := vault.contents[memoryKey(key)]
	if !present {
		return result.Err[io.ReadCloser](artifacts.ErrArtifactNotFound)
	}

	var reader io.ReadCloser = io.NopCloser(bytes.NewReader(body))
	return result.Ok(reader)
}

func (vault *memoryVault) DeleteArtifact(
	_ context.Context,
	key domain.ArtifactKey,
) result.Result[struct{}] {
	delete(vault.contents, memoryKey(key))
	return result.Ok(struct{}{})
}

func (vault *memoryVault) ListArtifacts(
	_ context.Context,
	_ artifacts.ArtifactScope,
) result.Result[[]artifacts.StoredArtifact] {
	return result.Ok[[]artifacts.StoredArtifact](nil)
}

func memoryKey(key domain.ArtifactKey) string {
	return strings.Join(
		[]string{
			key.OrganizationID().String(),
			key.ProjectID().String(),
			key.Kind().String(),
			key.Name().String(),
		},
		"|",
	)
}

func TestServiceUploadAndResolveResolvesAcrossRoundTrip(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{"computeTotal"},
		"AAAAA",
	)

	uploadResult := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(payload),
	)
	stored, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		t.Fatalf("upload: %v", uploadErr)
	}

	if stored.Identity().FileName().String() != "static/js/app.min.js" {
		t.Fatalf("unexpected stored identity: %+v", stored.Identity())
	}

	resolveResult := service.Resolve(
		context.Background(),
		organizationID,
		projectID,
		identity,
		NewGeneratedPosition(0, 0),
	)
	resolved, resolveErr := resolveResult.Value()
	if resolveErr != nil {
		t.Fatalf("resolve: %v", resolveErr)
	}

	if resolved.Source() != "original.js" {
		t.Fatalf("unexpected source: %q", resolved.Source())
	}

	name, hasName := resolved.Name()
	if !hasName || name != "computeTotal" {
		t.Fatalf("expected name computeTotal, got %q hasName=%v", name, hasName)
	}
}

func TestServiceResolveReturnsNotFoundWhenIdentityMissing(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	resolveResult := service.Resolve(
		context.Background(),
		organizationID,
		projectID,
		identity,
		NewGeneratedPosition(0, 0),
	)
	_, resolveErr := resolveResult.Value()
	if !errors.Is(resolveErr, ErrSourceMapNotFound) {
		t.Fatalf("expected ErrSourceMapNotFound, got %v", resolveErr)
	}
}

func TestServiceUploadRejectsOversizedPayload(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	body := bytes.Repeat([]byte("x"), sourceMapMaxBytes+1)

	uploadResult := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(body),
	)
	_, uploadErr := uploadResult.Value()
	if !errors.Is(uploadErr, ErrSourceMapTooLarge) {
		t.Fatalf("expected ErrSourceMapTooLarge, got %v", uploadErr)
	}
}

func TestServiceUploadRequiresContents(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	uploadResult := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		nil,
	)
	_, uploadErr := uploadResult.Value()
	if uploadErr == nil {
		t.Fatal("expected error for nil contents")
	}
}

func TestNewServiceRequiresVault(t *testing.T) {
	_, err := NewService(nil)
	if err == nil {
		t.Fatal("expected error when vault is nil")
	}
}

func TestServiceResolveDegradesWhenLookupMisses(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{},
		"EAAA",
	)

	uploadResult := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(payload),
	)
	if _, err := uploadResult.Value(); err != nil {
		t.Fatalf("upload: %v", err)
	}

	resolveResult := service.Resolve(
		context.Background(),
		organizationID,
		projectID,
		identity,
		NewGeneratedPosition(0, 0),
	)
	_, resolveErr := resolveResult.Value()
	if !errors.Is(resolveErr, ErrSourceMapNotFound) {
		t.Fatalf("expected ErrSourceMapNotFound for unmapped position, got %v", resolveErr)
	}
}

func newServiceFixture(t *testing.T) (*Service, domain.SourceMapIdentity, domain.OrganizationID, domain.ProjectID) {
	t.Helper()

	vault := newMemoryVault()
	service, serviceErr := NewService(vault)
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

	return service, identity, organizationID, projectID
}

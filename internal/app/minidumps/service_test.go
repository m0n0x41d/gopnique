package minidumps

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
	scope artifacts.ArtifactScope,
) result.Result[[]artifacts.StoredArtifact] {
	stored := make([]artifacts.StoredArtifact, 0, len(vault.contents))
	prefix := strings.Join(
		[]string{
			scope.OrganizationID().String(),
			scope.ProjectID().String(),
			scope.Kind().String(),
		},
		"|",
	) + "|"

	for key, body := range vault.contents {
		if !strings.HasPrefix(key, prefix) {
			continue
		}

		nameValue := strings.TrimPrefix(key, prefix)
		name, nameErr := domain.NewArtifactName(nameValue)
		if nameErr != nil {
			continue
		}

		artifactKey, keyErr := domain.NewArtifactKey(
			scope.OrganizationID(),
			scope.ProjectID(),
			scope.Kind(),
			name,
		)
		if keyErr != nil {
			continue
		}

		stored = append(stored, artifacts.NewStoredArtifact(artifactKey, int64(len(body))))
	}

	return result.Ok(stored)
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

func TestNewServiceRequiresVault(t *testing.T) {
	_, err := NewService(nil)
	if err == nil {
		t.Fatal("expected error when vault is nil")
	}
}

func TestServiceUploadRoundTripsThroughGet(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	body := buildMinidumpFixture(256)

	stored, uploadErr := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(body),
	).Value()
	if uploadErr != nil {
		t.Fatalf("upload: %v", uploadErr)
	}

	if stored.Size() != int64(len(body)) {
		t.Fatalf("unexpected size %d, want %d", stored.Size(), len(body))
	}

	reader, getErr := service.Get(
		context.Background(),
		organizationID,
		projectID,
		identity,
	).Value()
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	defer reader.Close()

	readBack, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}

	if !bytes.Equal(readBack, body) {
		t.Fatalf("payload mismatch: got %q want %q", readBack, body)
	}
}

func TestServiceUploadRejectsUnsupportedPayload(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	body := []byte("ZIP\x03\x04 not a real minidump")

	_, err := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(body),
	).Value()
	if !errors.Is(err, ErrUnsupportedMinidump) {
		t.Fatalf("expected ErrUnsupportedMinidump, got %v", err)
	}
}

func TestServiceUploadRequiresContents(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	_, err := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		nil,
	).Value()
	if err == nil {
		t.Fatal("expected error for nil contents")
	}
}

func TestServiceGetReturnsNotFoundWhenIdentityMissing(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	_, err := service.Get(
		context.Background(),
		organizationID,
		projectID,
		identity,
	).Value()
	if !errors.Is(err, ErrMinidumpNotFound) {
		t.Fatalf("expected ErrMinidumpNotFound, got %v", err)
	}
}

func TestServiceDeleteRemovesStoredMinidump(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	if _, err := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(buildMinidumpFixture(64)),
	).Value(); err != nil {
		t.Fatalf("upload: %v", err)
	}

	if _, err := service.Delete(
		context.Background(),
		organizationID,
		projectID,
		identity,
	).Value(); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, getErr := service.Get(
		context.Background(),
		organizationID,
		projectID,
		identity,
	).Value()
	if !errors.Is(getErr, ErrMinidumpNotFound) {
		t.Fatalf("expected ErrMinidumpNotFound after delete, got %v", getErr)
	}
}

func TestServiceUploadRejectsOversizedPayload(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	header := []byte{'M', 'D', 'M', 'P', 0x93, 0xa7, 0x00, 0x00}
	rest := bytes.Repeat([]byte("x"), minidumpMaxBytes-len(header)+1)
	body := append(header, rest...)

	_, err := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(body),
	).Value()
	if !errors.Is(err, ErrMinidumpTooLarge) {
		t.Fatalf("expected ErrMinidumpTooLarge, got %v", err)
	}
}

func TestServiceListIncludesUploadedArtifacts(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)

	body := buildMinidumpFixture(64)

	if _, err := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(body),
	).Value(); err != nil {
		t.Fatalf("upload: %v", err)
	}

	listings, listErr := service.List(
		context.Background(),
		organizationID,
		projectID,
	).Value()
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}

	if len(listings) != 1 {
		t.Fatalf("expected 1 listing, got %d", len(listings))
	}

	listing := listings[0]
	if listing.Size() != int64(len(body)) {
		t.Fatalf("unexpected size %d", listing.Size())
	}

	if listing.ArtifactKey().Kind().String() != "minidump" {
		t.Fatalf("unexpected kind %q", listing.ArtifactKey().Kind().String())
	}

	if listing.ArtifactKey().Name().String() != identity.ArtifactName().String() {
		t.Fatalf("unexpected artifact name %q", listing.ArtifactKey().Name().String())
	}
}

func newServiceFixture(t *testing.T) (*Service, domain.MinidumpIdentity, domain.OrganizationID, domain.ProjectID) {
	t.Helper()

	vault := newMemoryVault()
	service, serviceErr := NewService(vault)
	if serviceErr != nil {
		t.Fatalf("service: %v", serviceErr)
	}

	organizationID, _ := domain.NewOrganizationID("11111111-1111-1111-1111-111111111111")
	projectID, _ := domain.NewProjectID("22222222-2222-2222-2222-222222222222")

	eventID, _ := domain.NewEventID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	attachmentName, _ := domain.NewMinidumpAttachmentName("upload_file_minidump")

	identity, identityErr := domain.NewMinidumpIdentity(eventID, attachmentName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	return service, identity, organizationID, projectID
}

func buildMinidumpFixture(totalSize int) []byte {
	header := []byte{'M', 'D', 'M', 'P', 0x93, 0xa7, 0x00, 0x00}
	if totalSize <= len(header) {
		return header
	}

	body := make([]byte, totalSize)
	copy(body, header)
	for index := len(header); index < totalSize; index++ {
		body[index] = byte(index % 251)
	}

	return body
}

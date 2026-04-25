package filesystem_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/adapters/filesystem"
	"github.com/ivanzakutnii/error-tracker/internal/app/minidumps"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

// minidumpFixture is the smallest payload that round-trips the magic + version
// + stream count + directory RVA bytes of a Microsoft Minidump container.
// It is not a valid crash report, but it is sniffable as MDMP and lets the
// vault prove a non-trivial body lands intact on disk.
var minidumpFixture = func() []byte {
	header := []byte{'M', 'D', 'M', 'P', 0x93, 0xa7, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00}
	body := bytes.Repeat([]byte{0xab}, 64)
	return append(header, body...)
}()

func TestMinidumpFixtureRoundTripsAndDegrades(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	vault, vaultErr := filesystem.NewVault(filepath.Clean(root))
	if vaultErr != nil {
		t.Fatalf("vault: %v", vaultErr)
	}

	service, serviceErr := minidumps.NewService(vault)
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

	uploadResult := service.Upload(
		ctx,
		organizationID,
		projectID,
		identity,
		bytes.NewReader(minidumpFixture),
	)
	stored, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		t.Fatalf("upload: %v", uploadErr)
	}

	if stored.Size() != int64(len(minidumpFixture)) {
		t.Fatalf("unexpected size %d, want %d", stored.Size(), len(minidumpFixture))
	}

	getResult := service.Get(ctx, organizationID, projectID, identity)
	reader, getErr := getResult.Value()
	if getErr != nil {
		t.Fatalf("get: %v", getErr)
	}
	defer reader.Close()

	readBack, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read: %v", readErr)
	}

	if !bytes.Equal(readBack, minidumpFixture) {
		t.Fatalf("payload mismatch:\nwant: %x\n got: %x", minidumpFixture, readBack)
	}

	listResult := service.List(ctx, organizationID, projectID)
	listings, listErr := listResult.Value()
	if listErr != nil {
		t.Fatalf("list: %v", listErr)
	}

	if len(listings) != 1 {
		t.Fatalf("expected 1 listing, got %d", len(listings))
	}

	if listings[0].ArtifactKey().Name().String() != identity.ArtifactName().String() {
		t.Fatalf(
			"unexpected artifact name %q, want %q",
			listings[0].ArtifactKey().Name().String(),
			identity.ArtifactName().String(),
		)
	}

	missingEventID, _ := domain.NewEventID("ffffffffffffffffffffffffffffffff")
	missingIdentity, _ := domain.NewMinidumpIdentity(missingEventID, attachmentName)
	missingResult := service.Get(ctx, organizationID, projectID, missingIdentity)
	if _, err := missingResult.Value(); !errors.Is(err, minidumps.ErrMinidumpNotFound) {
		t.Fatalf("expected ErrMinidumpNotFound, got %v", err)
	}
}

func TestMinidumpFixtureRejectsUnsupportedPayload(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	vault, vaultErr := filesystem.NewVault(filepath.Clean(root))
	if vaultErr != nil {
		t.Fatalf("vault: %v", vaultErr)
	}

	service, serviceErr := minidumps.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("service: %v", serviceErr)
	}

	organizationID, _ := domain.NewOrganizationID("11111111-1111-1111-1111-111111111111")
	projectID, _ := domain.NewProjectID("22222222-2222-2222-2222-222222222222")

	eventID, _ := domain.NewEventID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	attachmentName, _ := domain.NewMinidumpAttachmentName("upload_file_minidump")
	identity, _ := domain.NewMinidumpIdentity(eventID, attachmentName)

	uploadResult := service.Upload(
		ctx,
		organizationID,
		projectID,
		identity,
		bytes.NewReader([]byte("PK\x03\x04 not a real minidump")),
	)
	if _, err := uploadResult.Value(); !errors.Is(err, minidumps.ErrUnsupportedMinidump) {
		t.Fatalf("expected ErrUnsupportedMinidump, got %v", err)
	}
}

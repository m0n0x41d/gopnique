//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanzakutnii/error-tracker/internal/adapters/filesystem"
	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	"github.com/ivanzakutnii/error-tracker/internal/app/minidumps"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestPostgresMinidumpE2E(t *testing.T) {
	ctx := context.Background()
	adminURL := os.Getenv("ERROR_TRACKER_E2E_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("ERROR_TRACKER_E2E_POSTGRES_URL is required")
	}

	databaseURL := createTestDatabase(t, ctx, adminURL)
	store, storeErr := postgres.NewStore(ctx, databaseURL)
	if storeErr != nil {
		t.Fatalf("store: %v", storeErr)
	}
	defer store.Close()

	migrationResult, migrationErr := store.ApplyMigrations(ctx)
	if migrationErr != nil {
		t.Fatalf("migrate: %v", migrationErr)
	}
	if len(migrationResult.Applied) != 33 {
		t.Fatalf("expected 33 migrations, got %d", len(migrationResult.Applied))
	}

	vault, vaultErr := filesystem.NewVault(filepath.Clean(t.TempDir()))
	if vaultErr != nil {
		t.Fatalf("vault: %v", vaultErr)
	}

	minidumpStore, minidumpErr := minidumps.NewService(vault)
	if minidumpErr != nil {
		t.Fatalf("minidump store: %v", minidumpErr)
	}

	debugFileStore, debugFileErr := debugfiles.NewService(vault)
	if debugFileErr != nil {
		t.Fatalf("debug file store: %v", debugFileErr)
	}

	server := httptest.NewServer(httpadapter.NewHandler(
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		e2eResolver{},
		store,
		httpadapter.IngestEnrichments{DebugFileStore: debugFileStore, MinidumpStore: minidumpStore},
		httpadapter.AuthSettings{PublicURL: "http://example.test", SecretKey: "e2e-secret"},
	))
	defer server.Close()

	client := newE2EClient(t)
	setup := request(
		t,
		client,
		http.MethodPost,
		server.URL+"/setup",
		"application/x-www-form-urlencoded",
		strings.NewReader("organization_name=Minidump&project_name=Minidump&email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if setup.StatusCode != http.StatusOK {
		t.Fatalf("expected setup ok, got %d: %s", setup.StatusCode, setup.Body)
	}

	scope := projectScope(t, ctx, databaseURL)
	uploadBreakpadSymbolsE2E(t, ctx, debugFileStore, scope)

	publicKey := projectPublicKey(t, ctx, databaseURL)
	acceptedPayload := buildE2ENativeMinidumpFixture()
	accepted := postMinidump(
		t,
		client,
		server.URL,
		publicKey,
		"670e8400e29b41d4a716446655440000",
		acceptedPayload,
	)
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("expected minidump accepted, got %d: %s", accepted.StatusCode, accepted.Body)
	}
	if !strings.Contains(accepted.Body, `"id":"670e8400e29b41d4a716446655440000"`) {
		t.Fatalf("unexpected minidump receipt: %s", accepted.Body)
	}

	rejected := postMinidump(
		t,
		client,
		server.URL,
		publicKey,
		"680e8400e29b41d4a716446655440000",
		[]byte("not a minidump"),
	)
	if rejected.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected invalid minidump rejected, got %d: %s", rejected.StatusCode, rejected.Body)
	}
	if !strings.Contains(rejected.Body, "invalid_minidump") {
		t.Fatalf("expected invalid minidump reason: %s", rejected.Body)
	}

	observation := assertMinidumpPersistence(t, ctx, databaseURL, int64(len(acceptedPayload)))
	eventDetail := request(
		t,
		client,
		http.MethodGet,
		server.URL+"/events/670e8400-e29b-41d4-a716-446655440000",
		"",
		nil,
	)
	if eventDetail.StatusCode != http.StatusOK {
		t.Fatalf("expected minidump event detail ok, got %d: %s", eventDetail.StatusCode, eventDetail.Body)
	}
	if !strings.Contains(eventDetail.Body, observation.ArtifactName) || !strings.Contains(eventDetail.Body, "Native crash minidump") {
		t.Fatalf("expected minidump attachment in event detail: %s", eventDetail.Body)
	}

	assertMinidumpArtifact(t, ctx, minidumpStore, observation, acceptedPayload)
}

func postMinidump(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
	eventID string,
	payload []byte,
) responseSnapshot {
	t.Helper()

	body, contentType := minidumpE2EMultipartBody(t, payload, `{
		"event_id":"`+eventID+`",
		"release":"native@2.0.0",
		"environment":"production"
	}`)

	return request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/minidump/?sentry_key="+publicKey,
		contentType,
		bytes.NewReader(body),
	)
}

type minidumpE2EObservation struct {
	OrganizationID string
	ProjectID      string
	ArtifactName   string
}

func assertMinidumpPersistence(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
	expectedSize int64,
) minidumpE2EObservation {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
select organization_id, project_id, canonical_payload
from events
where event_id = '670e8400-e29b-41d4-a716-446655440000'
`
	var organizationID string
	var projectID string
	var payloadBytes []byte
	scanErr := pool.QueryRow(ctx, query).Scan(&organizationID, &projectID, &payloadBytes)
	if scanErr != nil {
		t.Fatalf("minidump payload: %v", scanErr)
	}

	var payload struct {
		Platform    string `json:"platform"`
		Release     string `json:"release"`
		Environment string `json:"environment"`
		Attachments []struct {
			Kind        string `json:"kind"`
			Name        string `json:"name"`
			ByteSize    int64  `json:"byte_size"`
			ContentType string `json:"content_type"`
		} `json:"attachments"`
		Native struct {
			Frames []struct {
				Function string `json:"function"`
				Package  string `json:"package"`
			} `json:"frames"`
		} `json:"native"`
	}
	decodeErr := json.Unmarshal(payloadBytes, &payload)
	if decodeErr != nil {
		t.Fatalf("decode canonical payload: %v", decodeErr)
	}

	if payload.Platform != "native" || payload.Release != "native@2.0.0" || payload.Environment != "production" {
		t.Fatalf("unexpected minidump dimensions: %#v", payload)
	}

	if len(payload.Attachments) != 1 {
		t.Fatalf("expected one minidump attachment: %#v", payload.Attachments)
	}

	attachment := payload.Attachments[0]
	if attachment.Kind != "minidump" {
		t.Fatalf("unexpected attachment kind: %s", attachment.Kind)
	}

	if attachment.ByteSize != expectedSize {
		t.Fatalf("unexpected attachment size: %d", attachment.ByteSize)
	}

	if attachment.ContentType != "application/octet-stream" {
		t.Fatalf("unexpected content type: %s", attachment.ContentType)
	}

	if len(payload.Native.Frames) != 1 {
		t.Fatalf("expected one native frame: %#v", payload.Native.Frames)
	}

	frame := payload.Native.Frames[0]
	if frame.Function != "render_home" {
		t.Fatalf("expected symbolicated minidump frame, got %q", frame.Function)
	}

	if frame.Package != "/usr/lib/libapp.so" {
		t.Fatalf("unexpected native frame package: %q", frame.Package)
	}

	var eventCount int
	countErr := pool.QueryRow(ctx, "select count(*) from events").Scan(&eventCount)
	if countErr != nil {
		t.Fatalf("count events: %v", countErr)
	}

	if eventCount != 1 {
		t.Fatalf("invalid minidump must not persist an event, got %d events", eventCount)
	}

	return minidumpE2EObservation{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		ArtifactName:   attachment.Name,
	}
}

func assertMinidumpArtifact(
	t *testing.T,
	ctx context.Context,
	store *minidumps.Service,
	observation minidumpE2EObservation,
	expected []byte,
) {
	t.Helper()

	organizationID, organizationErr := domain.NewOrganizationID(observation.OrganizationID)
	if organizationErr != nil {
		t.Fatalf("organization id: %v", organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(observation.ProjectID)
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	eventID, _ := domain.NewEventID("670e8400e29b41d4a716446655440000")
	attachmentName, _ := domain.NewMinidumpAttachmentName("upload_file_minidump")
	identity, identityErr := domain.NewMinidumpIdentity(eventID, attachmentName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	if identity.ArtifactName().String() != observation.ArtifactName {
		t.Fatalf("unexpected artifact name: %s", observation.ArtifactName)
	}

	reader, getErr := store.Get(ctx, organizationID, projectID, identity).Value()
	if getErr != nil {
		t.Fatalf("get minidump artifact: %v", getErr)
	}
	defer reader.Close()

	body, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read artifact: %v", readErr)
	}

	if !bytes.Equal(body, expected) {
		t.Fatalf("stored artifact mismatch: %x", body)
	}
}

func minidumpE2EMultipartBody(t *testing.T, payload []byte, sentryMetadata string) ([]byte, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if strings.TrimSpace(sentryMetadata) != "" {
		if err := writer.WriteField("sentry", sentryMetadata); err != nil {
			t.Fatalf("write sentry field: %v", err)
		}
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="upload_file_minidump"; filename="crash.dmp"`)
	header.Set("Content-Type", "application/octet-stream")
	part, partErr := writer.CreatePart(header)
	if partErr != nil {
		t.Fatalf("create file part: %v", partErr)
	}

	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write minidump: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	return body.Bytes(), writer.FormDataContentType()
}

func buildE2EMinidumpFixture() []byte {
	header := []byte{'M', 'D', 'M', 'P', 0x93, 0xa7, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00}
	body := bytes.Repeat([]byte{0xef}, 64)
	return append(header, body...)
}

func buildE2ENativeMinidumpFixture() []byte {
	const (
		directoryRVA         = 32
		moduleListRVA        = 64
		exceptionRVA         = 256
		nameRVA              = 512
		codeViewRVA          = 640
		contextRVA           = 768
		moduleListStream     = 4
		exceptionStream      = 6
		moduleBytes          = 108
		moduleBase           = 0
		moduleSize           = 8
		moduleNameRVA        = 20
		moduleCodeViewSize   = 76
		moduleCodeViewRVA    = 80
		exceptionContextSize = 160
		exceptionContextRVA  = 164
		x64RIPOffset         = 248
		fixtureSize          = 1024
	)

	body := make([]byte, fixtureSize)
	copy(body[0:4], []byte{'M', 'D', 'M', 'P'})
	binary.LittleEndian.PutUint32(body[4:8], 0x0000a793)
	binary.LittleEndian.PutUint32(body[8:12], 2)
	binary.LittleEndian.PutUint32(body[12:16], directoryRVA)

	writeE2EMinidumpDirectoryEntry(body[directoryRVA:directoryRVA+12], moduleListStream, 4+moduleBytes, moduleListRVA)
	writeE2EMinidumpDirectoryEntry(body[directoryRVA+12:directoryRVA+24], exceptionStream, 168, exceptionRVA)

	binary.LittleEndian.PutUint32(body[moduleListRVA:moduleListRVA+4], 1)
	module := body[moduleListRVA+4 : moduleListRVA+4+moduleBytes]
	binary.LittleEndian.PutUint64(module[moduleBase:moduleBase+8], 0x10000000)
	binary.LittleEndian.PutUint32(module[moduleSize:moduleSize+4], 0x2000)
	binary.LittleEndian.PutUint32(module[moduleNameRVA:moduleNameRVA+4], nameRVA)
	binary.LittleEndian.PutUint32(module[moduleCodeViewSize:moduleCodeViewSize+4], 24)
	binary.LittleEndian.PutUint32(module[moduleCodeViewRVA:moduleCodeViewRVA+4], codeViewRVA)

	writeE2EMinidumpString(body[nameRVA:], "/usr/lib/libapp.so")
	writeE2EMinidumpCodeViewPDB70(body[codeViewRVA : codeViewRVA+24])

	exception := body[exceptionRVA : exceptionRVA+168]
	binary.LittleEndian.PutUint32(exception[exceptionContextSize:exceptionContextSize+4], 256)
	binary.LittleEndian.PutUint32(exception[exceptionContextRVA:exceptionContextRVA+4], contextRVA)
	binary.LittleEndian.PutUint64(body[contextRVA+x64RIPOffset:contextRVA+x64RIPOffset+8], 0x10001004)

	return body
}

func writeE2EMinidumpDirectoryEntry(entry []byte, streamType uint32, dataSize uint32, rva uint32) {
	binary.LittleEndian.PutUint32(entry[0:4], streamType)
	binary.LittleEndian.PutUint32(entry[4:8], dataSize)
	binary.LittleEndian.PutUint32(entry[8:12], rva)
}

func writeE2EMinidumpString(target []byte, value string) {
	encoded := utf16.Encode([]rune(value))
	binary.LittleEndian.PutUint32(target[0:4], uint32(len(encoded)*2))
	for index, unit := range encoded {
		offset := 4 + index*2
		binary.LittleEndian.PutUint16(target[offset:offset+2], unit)
	}
}

func writeE2EMinidumpCodeViewPDB70(target []byte) {
	copy(target[0:4], []byte{'R', 'S', 'D', 'S'})
	copy(target[4:20], []byte{
		0xef, 0xbe, 0xad, 0xde,
		0xfe, 0xca,
		0x0d, 0xf0,
		0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xf0, 0x0d,
	})
}

package httpadapter

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/minidumps"
)

func TestSentryMinidumpRouteStoresArtifactAndIngestsAttachment(t *testing.T) {
	vault := newCapturingVault()
	service, serviceErr := minidumps.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("minidump service: %v", serviceErr)
	}

	captured := &capturingBackend{
		fakeSentryBackend: newFakeSentryBackend(t),
	}
	mux := newMux(
		nil,
		captured,
		captured,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		captured,
		IngestEnrichments{MinidumpStore: service},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	body, contentType := minidumpMultipartBody(t, buildHTTPMinidumpFixture(), `{
		"event_id":"660e8400e29b41d4a716446655440000",
		"release":"native@1.0.0",
		"environment":"production"
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/minidump/?sentry_key=550e8400e29b41d4a716446655440000",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	if !strings.Contains(response.Body.String(), "660e8400e29b41d4a716446655440000") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}

	captured.mu.Lock()
	event := captured.event
	appended := captured.appended
	captured.mu.Unlock()

	if !appended {
		t.Fatal("expected backend to receive an Append call")
	}

	if event.Platform() != "native" {
		t.Fatalf("unexpected platform: %s", event.Platform())
	}

	if event.Release() != "native@1.0.0" || event.Environment() != "production" {
		t.Fatalf("unexpected dimensions: release=%q environment=%q", event.Release(), event.Environment())
	}

	attachments := event.Attachments()
	if len(attachments) != 1 {
		t.Fatalf("expected one attachment, got %d", len(attachments))
	}

	attachment := attachments[0]
	if attachment.Kind().String() != "minidump" {
		t.Fatalf("unexpected attachment kind: %s", attachment.Kind().String())
	}

	if attachment.ByteSize() != int64(len(buildHTTPMinidumpFixture())) {
		t.Fatalf("unexpected attachment size: %d", attachment.ByteSize())
	}

	if attachment.ContentType() != "application/octet-stream" {
		t.Fatalf("unexpected content type: %s", attachment.ContentType())
	}

	vault.mu.Lock()
	defer vault.mu.Unlock()
	if len(vault.contents) != 1 {
		t.Fatalf("expected one stored artifact, got %d", len(vault.contents))
	}

	for _, stored := range vault.contents {
		if !bytes.Equal(stored, buildHTTPMinidumpFixture()) {
			t.Fatalf("stored minidump mismatch: %x", stored)
		}
	}
}

func TestSentryMinidumpRouteRejectsInvalidPayload(t *testing.T) {
	vault := newCapturingVault()
	service, serviceErr := minidumps.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("minidump service: %v", serviceErr)
	}

	backend := newFakeSentryBackend(t)
	mux := newMux(
		nil,
		backend,
		backend,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		backend,
		IngestEnrichments{MinidumpStore: service},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	body, contentType := minidumpMultipartBody(t, []byte("not a minidump"), "")
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/minidump/?sentry_key=550e8400e29b41d4a716446655440000",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	if !strings.Contains(response.Body.String(), "invalid_minidump") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}

	vault.mu.Lock()
	defer vault.mu.Unlock()
	if len(vault.contents) != 0 {
		t.Fatalf("invalid minidump must not store an artifact: %#v", vault.contents)
	}
}

func TestSentryMinidumpRouteRequiresConfiguredStore(t *testing.T) {
	backend := newFakeSentryBackend(t)
	mux := newMux(
		nil,
		backend,
		backend,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		backend,
		IngestEnrichments{},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	body, contentType := minidumpMultipartBody(t, buildHTTPMinidumpFixture(), "")
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/minidump/?sentry_key=550e8400e29b41d4a716446655440000",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}
}

func minidumpMultipartBody(t *testing.T, payload []byte, sentryMetadata string) ([]byte, string) {
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

func buildHTTPMinidumpFixture() []byte {
	header := []byte{'M', 'D', 'M', 'P', 0x93, 0xa7, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00}
	body := bytes.Repeat([]byte{0xcd}, 64)
	return append(header, body...)
}

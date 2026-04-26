package httpadapter

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	projectapp "github.com/ivanzakutnii/error-tracker/internal/app/projects"
	"github.com/ivanzakutnii/error-tracker/internal/app/sourcemaps"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const artifactUploadToken = "etp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestSourceMapUploadRouteStoresProjectScopedArtifact(t *testing.T) {
	vault := newCapturingVault()
	service, serviceErr := sourcemaps.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("source map service: %v", serviceErr)
	}

	backend := newArtifactUploadBackend(t)
	mux := newMux(
		nil,
		nil,
		nil,
		nil, nil, nil,
		nil,
		backend,
		nil, nil,
		backend,
		nil, nil, nil,
		IngestEnrichments{SourceMapResolver: service},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	payload := buildSourceMapPayloadFixture("original.js", "computeTotal", "AAAAA")
	body, contentType := artifactMultipartBody(
		t,
		"file",
		"static/js/app.min.js.map",
		"application/json",
		payload,
		map[string]string{"name": "static/js/app.min.js.map"},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/0/projects/default/default/releases/frontend@1.0.0/files/?dist=web",
		bytes.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+artifactUploadToken)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	if !strings.Contains(response.Body.String(), `"name":"static/js/app.min.js"`) {
		t.Fatalf("unexpected response body: %s", response.Body.String())
	}

	release, _ := domain.NewReleaseName("frontend@1.0.0")
	dist, _ := domain.NewOptionalDistName("web")
	fileName, _ := domain.NewSourceMapFileName("static/js/app.min.js")
	identity, _ := domain.NewSourceMapIdentity(release, dist, fileName)

	resolveResult := service.Resolve(
		context.Background(),
		backend.organizationID,
		backend.projectID,
		identity,
		sourcemaps.NewGeneratedPosition(0, 0),
	)
	resolved, resolveErr := resolveResult.Value()
	if resolveErr != nil {
		t.Fatalf("resolve uploaded source map: %v", resolveErr)
	}

	if resolved.Source() != "original.js" {
		t.Fatalf("unexpected resolved source: %s", resolved.Source())
	}
}

func TestSourceMapUploadRouteRejectsWrongProjectPath(t *testing.T) {
	vault := newCapturingVault()
	service, serviceErr := sourcemaps.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("source map service: %v", serviceErr)
	}

	backend := newArtifactUploadBackend(t)
	mux := newMux(
		nil,
		nil,
		nil,
		nil, nil, nil,
		nil,
		backend,
		nil, nil,
		backend,
		nil, nil, nil,
		IngestEnrichments{SourceMapResolver: service},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	body, contentType := artifactMultipartBody(
		t,
		"file",
		"static/js/app.min.js.map",
		"application/json",
		buildSourceMapPayloadFixture("original.js", "computeTotal", "AAAAA"),
		map[string]string{},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/0/projects/default/other/releases/frontend@1.0.0/files/",
		bytes.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+artifactUploadToken)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	vault.mu.Lock()
	defer vault.mu.Unlock()
	if len(vault.contents) != 0 {
		t.Fatalf("wrong project path must not store artifacts: %#v", vault.contents)
	}
}

func TestDebugFileUploadRouteStoresProjectScopedArtifact(t *testing.T) {
	vault := newCapturingVault()
	service, serviceErr := debugfiles.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("debug file service: %v", serviceErr)
	}

	backend := newArtifactUploadBackend(t)
	mux := newMux(
		nil,
		nil,
		nil,
		nil, nil, nil,
		nil,
		backend,
		nil, nil,
		backend,
		nil, nil, nil,
		IngestEnrichments{DebugFileStore: service},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	fields := map[string]string{
		"debug_id": "deadbeefcafef00ddeadbeefcafef00d",
		"kind":     "breakpad",
		"name":     "libapp.so.sym",
	}
	body, contentType := artifactMultipartBody(
		t,
		"file",
		"libapp.so.sym",
		"text/plain",
		[]byte(breakpadHTTPFixture),
		fields,
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/0/projects/default/default/files/difs/",
		bytes.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+artifactUploadToken)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	debugID, _ := domain.NewDebugIdentifier("deadbeefcafef00ddeadbeefcafef00d")
	fileName, _ := domain.NewDebugFileName("libapp.so.sym")
	identity, _ := domain.NewDebugFileIdentity(debugID, domain.DebugFileKindBreakpad(), fileName)

	getResult := service.Get(context.Background(), backend.organizationID, backend.projectID, identity)
	reader, getErr := getResult.Value()
	if getErr != nil {
		t.Fatalf("get uploaded debug file: %v", getErr)
	}
	defer reader.Close()

	stored, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read debug file: %v", readErr)
	}

	if string(stored) != breakpadHTTPFixture {
		t.Fatalf("unexpected debug file payload: %q", stored)
	}
}

func TestDebugFileUploadRouteRejectsMislabeledPayload(t *testing.T) {
	vault := newCapturingVault()
	service, serviceErr := debugfiles.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("debug file service: %v", serviceErr)
	}

	backend := newArtifactUploadBackend(t)
	mux := newMux(
		nil,
		nil,
		nil,
		nil, nil, nil,
		nil,
		backend,
		nil, nil,
		backend,
		nil, nil, nil,
		IngestEnrichments{DebugFileStore: service},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	fields := map[string]string{
		"debug_id": "deadbeefcafef00ddeadbeefcafef00d",
		"kind":     "elf",
		"name":     "libapp.so.sym",
	}
	body, contentType := artifactMultipartBody(
		t,
		"file",
		"libapp.so.sym",
		"text/plain",
		[]byte(breakpadHTTPFixture),
		fields,
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/0/projects/default/default/files/difs/",
		bytes.NewReader(body),
	)
	request.Header.Set("Authorization", "Bearer "+artifactUploadToken)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	if !strings.Contains(response.Body.String(), "invalid_debug_file") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}

	vault.mu.Lock()
	defer vault.mu.Unlock()
	if len(vault.contents) != 0 {
		t.Fatalf("invalid debug file must not store artifacts: %#v", vault.contents)
	}
}

func TestDebugFileReprocessingRouteIsAuthenticatedNoop(t *testing.T) {
	backend := newArtifactUploadBackend(t)
	mux := newMux(
		nil,
		nil,
		nil,
		nil, nil, nil,
		nil,
		backend,
		nil, nil,
		backend,
		nil, nil, nil,
		IngestEnrichments{},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/0/projects/default/default/reprocessing/",
		strings.NewReader("{}"),
	)
	request.Header.Set("Authorization", "Bearer "+artifactUploadToken)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}
}

func TestArtifactBundleAssembleStoresSourceMapFromSentryCLIChunks(t *testing.T) {
	vault := newCapturingVault()
	service, serviceErr := sourcemaps.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("source map service: %v", serviceErr)
	}

	backend := newArtifactUploadBackend(t)
	mux := newMux(
		nil,
		nil,
		nil,
		nil, nil, nil,
		nil,
		backend,
		nil, nil,
		backend,
		nil, nil, nil,
		IngestEnrichments{ArtifactVault: vault, SourceMapResolver: service},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	sourceMapPath := "files/_/_/static/js/app.js.map"
	sourceMapPayload := buildSourceMapPayloadFixture("original.js", "computeTotal", "AAAAA")
	bundle := artifactBundleZip(t, artifactBundleManifest{
		Org:     "default",
		Project: "default",
		Release: "cli@1",
		Dist:    "web",
		Files: map[string]artifactBundleManifestFile{
			sourceMapPath: {
				Type: "source_map",
				URL:  "~/static/js/app.js.map",
			},
		},
	}, map[string][]byte{
		sourceMapPath: sourceMapPayload,
	})
	checksum := sha1Hex(bundle)
	assembleBody := `{"checksum":"` + checksum + `","chunks":["` + checksum + `"],"projects":["default"],"version":"cli@1","dist":"web"}`

	missingResponse := postArtifactJSON(
		t,
		mux,
		"/api/0/organizations/default/artifactbundle/assemble/",
		assembleBody,
	)
	if missingResponse.Code != http.StatusOK {
		t.Fatalf("unexpected missing status: %d body=%s", missingResponse.Code, missingResponse.Body.String())
	}
	if !strings.Contains(missingResponse.Body.String(), `"state":"not_found"`) {
		t.Fatalf("expected missing chunk response, got %s", missingResponse.Body.String())
	}

	chunkBody, chunkType := artifactMultipartBody(
		t,
		"file_gzip",
		checksum,
		"application/gzip",
		gzipBytes(t, bundle),
		map[string]string{},
	)
	chunkRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/0/organizations/default/chunk-upload/",
		bytes.NewReader(chunkBody),
	)
	chunkRequest.Header.Set("Authorization", "Bearer "+artifactUploadToken)
	chunkRequest.Header.Set("Content-Type", chunkType)
	chunkResponse := httptest.NewRecorder()

	mux.ServeHTTP(chunkResponse, chunkRequest)

	if chunkResponse.Code != http.StatusOK {
		t.Fatalf("unexpected chunk status: %d body=%s", chunkResponse.Code, chunkResponse.Body.String())
	}

	createdResponse := postArtifactJSON(
		t,
		mux,
		"/api/0/organizations/default/artifactbundle/assemble/",
		assembleBody,
	)
	if createdResponse.Code != http.StatusOK {
		t.Fatalf("unexpected created status: %d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	if !strings.Contains(createdResponse.Body.String(), `"state":"created"`) {
		t.Fatalf("expected created response, got %s", createdResponse.Body.String())
	}

	release, _ := domain.NewReleaseName("cli@1")
	dist, _ := domain.NewOptionalDistName("web")
	fileName, _ := domain.NewSourceMapFileName("static/js/app.js")
	identity, _ := domain.NewSourceMapIdentity(release, dist, fileName)
	resolveResult := service.Resolve(
		context.Background(),
		backend.organizationID,
		backend.projectID,
		identity,
		sourcemaps.NewGeneratedPosition(0, 0),
	)
	resolved, resolveErr := resolveResult.Value()
	if resolveErr != nil {
		t.Fatalf("resolve assembled source map: %v", resolveErr)
	}

	if resolved.Source() != "original.js" {
		t.Fatalf("unexpected resolved source: %s", resolved.Source())
	}
}

func TestDebugFileAssembleStoresBreakpadChunk(t *testing.T) {
	vault := newCapturingVault()
	service, serviceErr := debugfiles.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("debug file service: %v", serviceErr)
	}

	backend := newArtifactUploadBackend(t)
	mux := newMux(
		nil,
		nil,
		nil,
		nil, nil, nil,
		nil,
		backend,
		nil, nil,
		backend,
		nil, nil, nil,
		IngestEnrichments{ArtifactVault: vault, DebugFileStore: service},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	payload := []byte(breakpadHTTPFixture)
	checksum := sha1Hex(payload)
	debugID := "deadbeef-cafe-f00d-dead-beefcafef00d"
	assembleBody := `{"` + checksum + `":{"name":"libapp.so.sym","debug_id":"` + debugID + `","chunks":["` + checksum + `"]}}`

	missingResponse := postArtifactJSON(
		t,
		mux,
		"/api/0/projects/default/default/files/difs/assemble/",
		assembleBody,
	)
	if missingResponse.Code != http.StatusOK {
		t.Fatalf("unexpected missing status: %d body=%s", missingResponse.Code, missingResponse.Body.String())
	}
	if !strings.Contains(missingResponse.Body.String(), `"state":"not_found"`) {
		t.Fatalf("expected missing response, got %s", missingResponse.Body.String())
	}

	chunkBody, chunkType := artifactMultipartBody(
		t,
		"file_gzip",
		checksum,
		"application/gzip",
		gzipBytes(t, payload),
		map[string]string{},
	)
	chunkRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/0/organizations/default/chunk-upload/",
		bytes.NewReader(chunkBody),
	)
	chunkRequest.Header.Set("Authorization", "Bearer "+artifactUploadToken)
	chunkRequest.Header.Set("Content-Type", chunkType)
	chunkResponse := httptest.NewRecorder()

	mux.ServeHTTP(chunkResponse, chunkRequest)

	if chunkResponse.Code != http.StatusOK {
		t.Fatalf("unexpected chunk status: %d body=%s", chunkResponse.Code, chunkResponse.Body.String())
	}

	createdResponse := postArtifactJSON(
		t,
		mux,
		"/api/0/projects/default/default/files/difs/assemble/",
		assembleBody,
	)
	if createdResponse.Code != http.StatusOK {
		t.Fatalf("unexpected created status: %d body=%s", createdResponse.Code, createdResponse.Body.String())
	}
	if !strings.Contains(createdResponse.Body.String(), `"state":"created"`) {
		t.Fatalf("expected created response, got %s", createdResponse.Body.String())
	}

	parsedDebugID, _ := domain.NewDebugIdentifier(debugID)
	fileName, _ := domain.NewDebugFileName("libapp.so.sym")
	identity, _ := domain.NewDebugFileIdentity(parsedDebugID, domain.DebugFileKindBreakpad(), fileName)
	getResult := service.Get(context.Background(), backend.organizationID, backend.projectID, identity)
	reader, getErr := getResult.Value()
	if getErr != nil {
		t.Fatalf("get assembled debug file: %v", getErr)
	}
	defer reader.Close()

	stored, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read assembled debug file: %v", readErr)
	}

	if string(stored) != breakpadHTTPFixture {
		t.Fatalf("unexpected debug file payload: %q", stored)
	}
}

type artifactUploadBackend struct {
	organizationID domain.OrganizationID
	projectID      domain.ProjectID
	tokenID        domain.APITokenID
}

func newArtifactUploadBackend(t *testing.T) *artifactUploadBackend {
	t.Helper()

	organizationID := mustDomainID(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustDomainID(t, domain.NewProjectID, "2222222222224222a222222222222222")
	tokenID := mustDomainID(t, domain.NewAPITokenID, "4444444444444444a444444444444444")

	return &artifactUploadBackend{
		organizationID: organizationID,
		projectID:      projectID,
		tokenID:        tokenID,
	}
}

func (backend *artifactUploadBackend) ResolveProjectToken(
	ctx context.Context,
	secret tokenapp.ProjectTokenSecret,
) result.Result[tokenapp.ProjectTokenAuth] {
	if secret.String() != artifactUploadToken {
		return result.Err[tokenapp.ProjectTokenAuth](errors.New("token not found"))
	}

	return result.Ok(tokenapp.ProjectTokenAuth{
		TokenID:        backend.tokenID,
		OrganizationID: backend.organizationID,
		ProjectID:      backend.projectID,
		TokenScope:     tokenapp.ProjectTokenScopeAdmin,
	})
}

func (backend *artifactUploadBackend) ListProjectTokens(
	ctx context.Context,
	query tokenapp.ProjectTokenQuery,
) result.Result[tokenapp.ProjectTokenView] {
	return result.Ok(tokenapp.ProjectTokenView{})
}

func (backend *artifactUploadBackend) CreateProjectToken(
	ctx context.Context,
	command tokenapp.CreateProjectTokenCommand,
) result.Result[tokenapp.CreateProjectTokenResult] {
	return result.Err[tokenapp.CreateProjectTokenResult](errors.New("not implemented"))
}

func (backend *artifactUploadBackend) RevokeProjectToken(
	ctx context.Context,
	command tokenapp.RevokeProjectTokenCommand,
) result.Result[tokenapp.ProjectTokenMutationResult] {
	return result.Err[tokenapp.ProjectTokenMutationResult](errors.New("not implemented"))
}

func (backend *artifactUploadBackend) FindCurrentProject(
	ctx context.Context,
	query projectapp.ProjectQuery,
) result.Result[projectapp.ProjectRecord] {
	if query.Scope.OrganizationID.String() != backend.organizationID.String() {
		return result.Err[projectapp.ProjectRecord](errors.New("project not found"))
	}

	if query.Scope.ProjectID.String() != backend.projectID.String() {
		return result.Err[projectapp.ProjectRecord](errors.New("project not found"))
	}

	return result.Ok(projectapp.ProjectRecord{
		OrganizationSlug: "default",
		OrganizationName: "Default Organization",
		ProjectID:        backend.projectID,
		Name:             "Default Project",
		Slug:             "default",
		IngestRef:        "1",
	})
}

func artifactMultipartBody(
	t *testing.T,
	fieldName string,
	fileName string,
	contentType string,
	payload []byte,
	fields map[string]string,
) ([]byte, string) {
	t.Helper()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			t.Fatalf("write field %s: %v", name, err)
		}
	}

	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="`+fieldName+`"; filename="`+fileName+`"`)
	header.Set("Content-Type", contentType)
	part, partErr := writer.CreatePart(header)
	if partErr != nil {
		t.Fatalf("create file part: %v", partErr)
	}

	if _, err := part.Write(payload); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	return body.Bytes(), writer.FormDataContentType()
}

const breakpadHTTPFixture = "MODULE Linux x86_64 deadbeefcafef00ddeadbeefcafef00d libapp.so\n" +
	"FILE 0 src/app.c\n" +
	"FUNC 0 4 0 main\n" +
	"0 4 1 0\n"

func postArtifactJSON(
	t *testing.T,
	mux http.Handler,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+artifactUploadToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	return response
}

func artifactBundleZip(
	t *testing.T,
	manifest artifactBundleManifest,
	files map[string][]byte,
) []byte {
	t.Helper()

	body := &bytes.Buffer{}
	writer := zip.NewWriter(body)
	for name, payload := range files {
		fileWriter, createErr := writer.Create(name)
		if createErr != nil {
			t.Fatalf("create zip file %s: %v", name, createErr)
		}

		_, writeErr := fileWriter.Write(payload)
		if writeErr != nil {
			t.Fatalf("write zip file %s: %v", name, writeErr)
		}
	}

	manifestPayload, marshalErr := json.Marshal(manifest)
	if marshalErr != nil {
		t.Fatalf("manifest: %v", marshalErr)
	}

	manifestWriter, manifestErr := writer.Create("manifest.json")
	if manifestErr != nil {
		t.Fatalf("create manifest: %v", manifestErr)
	}

	_, writeErr := manifestWriter.Write(manifestPayload)
	if writeErr != nil {
		t.Fatalf("write manifest: %v", writeErr)
	}

	closeErr := writer.Close()
	if closeErr != nil {
		t.Fatalf("close zip: %v", closeErr)
	}

	return body.Bytes()
}

func gzipBytes(t *testing.T, payload []byte) []byte {
	t.Helper()

	body := &bytes.Buffer{}
	writer := gzip.NewWriter(body)
	_, writeErr := writer.Write(payload)
	if writeErr != nil {
		t.Fatalf("write gzip: %v", writeErr)
	}

	closeErr := writer.Close()
	if closeErr != nil {
		t.Fatalf("close gzip: %v", closeErr)
	}

	return body.Bytes()
}

func sha1Hex(payload []byte) string {
	sum := sha1.Sum(payload)
	return hex.EncodeToString(sum[:])
}

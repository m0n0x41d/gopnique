package httpadapter

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSentryStoreRouteAppliesDebugFileSymbolicationAtIngest(t *testing.T) {
	vault := newCapturingVault()
	debugFileStore, storeErr := debugfiles.NewService(vault)
	if storeErr != nil {
		t.Fatalf("debug file store: %v", storeErr)
	}

	uploadFixtureBreakpadSymbols(
		t,
		debugFileStore,
		"1111111111114111a111111111111111",
		"2222222222224222a222222222222222",
		"deadbeefcafef00ddeadbeefcafef00d",
		"libapp.so.sym",
		[]byte(
			"MODULE Linux x86_64 deadbeefcafef00ddeadbeefcafef00d libapp.so\n"+
				"FUNC 1000 20 0 render_home\n",
		),
	)

	captured := &capturingBackend{
		fakeSentryBackend: newFakeSentryBackend(t),
	}

	mux := newMux(
		nil,
		captured,
		captured,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		captured,
		IngestEnrichments{DebugFileStore: debugFileStore},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	body := `{
		"event_id":"550e8400e29b41d4a716446655440000",
		"timestamp":"2026-04-25T10:00:00Z",
		"platform":"native",
		"level":"fatal",
		"debug_meta":{
			"images":[{
				"debug_id":"deadbeef-cafe-f00d-dead-beefcafef00d",
				"code_file":"/usr/lib/libapp.so",
				"image_addr":"0x10000000",
				"image_size":"0x2000"
			}]
		},
		"exception":{
			"values":[{
				"type":"SIGSEGV",
				"value":"segmentation fault",
				"stacktrace":{
					"frames":[{
						"instruction_addr":"0x10001004",
						"package":"/usr/lib/libapp.so"
					}]
				}
			}]
		}
	}`

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/store/?sentry_key=550e8400e29b41d4a716446655440000",
		strings.NewReader(body),
	)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	captured.mu.Lock()
	defer captured.mu.Unlock()

	if !captured.appended {
		t.Fatal("expected backend to receive an Append call")
	}

	frames := captured.event.NativeFrames()
	if len(frames) != 1 {
		t.Fatalf("expected one native frame, got %d", len(frames))
	}

	if frames[0].Function() != "render_home" {
		t.Fatalf("unexpected symbolicated function: %q", frames[0].Function())
	}

	if frames[0].Package() != "/usr/lib/libapp.so" {
		t.Fatalf("unexpected frame package: %q", frames[0].Package())
	}
}

func uploadFixtureBreakpadSymbols(
	t *testing.T,
	store *debugfiles.Service,
	organizationIDInput string,
	projectIDInput string,
	debugIDInput string,
	fileNameInput string,
	payload []byte,
) {
	t.Helper()

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDInput)
	if organizationErr != nil {
		t.Fatalf("organization id: %v", organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(projectIDInput)
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	debugID, debugIDErr := domain.NewDebugIdentifier(debugIDInput)
	if debugIDErr != nil {
		t.Fatalf("debug id: %v", debugIDErr)
	}

	fileName, fileNameErr := domain.NewDebugFileName(fileNameInput)
	if fileNameErr != nil {
		t.Fatalf("file name: %v", fileNameErr)
	}

	identity, identityErr := domain.NewDebugFileIdentity(debugID, domain.DebugFileKindBreakpad(), fileName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	uploadResult := store.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(payload),
	)
	if _, err := uploadResult.Value(); err != nil {
		t.Fatalf("upload: %v", err)
	}
}

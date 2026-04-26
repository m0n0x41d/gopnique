package httpadapter

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	"github.com/ivanzakutnii/error-tracker/internal/app/sourcemaps"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

func TestSentryStoreRouteAppliesSourceMapResolutionAtIngest(t *testing.T) {
	vault := newCapturingVault()
	uploadFixtureSourceMap(
		t,
		vault,
		"1111111111114111a111111111111111",
		"2222222222224222a222222222222222",
		"frontend@1.0.0",
		"static/js/app.min.js",
		buildSourceMapPayloadFixture("original.js", "computeTotal", "AAAAA"),
	)

	resolver, resolverErr := sourcemaps.NewService(vault)
	if resolverErr != nil {
		t.Fatalf("resolver: %v", resolverErr)
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
		IngestEnrichments{SourceMapResolver: resolver},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	body := `{
		"event_id":"550e8400e29b41d4a716446655440000",
		"timestamp":"2026-04-25T10:00:00Z",
		"platform":"javascript",
		"release":"frontend@1.0.0",
		"exception":{
			"values":[
				{
					"type":"TypeError",
					"value":"bad operand",
					"stacktrace":{
						"frames":[
							{
								"abs_path":"https://cdn.example.com/static/js/app.min.js",
								"function":"r",
								"lineno":1,
								"colno":0
							}
						]
					}
				}
			]
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

	frames := captured.event.JsStacktrace()
	if len(frames) != 1 {
		t.Fatalf("expected 1 JS stacktrace frame, got %d", len(frames))
	}

	resolution, hasResolution := frames[0].Resolution()
	if !hasResolution {
		t.Fatalf("expected ingest to apply source map and produce a resolved frame")
	}

	if resolution.Source() != "original.js" {
		t.Fatalf("unexpected resolved source: %q", resolution.Source())
	}

	if resolution.Symbol() != "computeTotal" {
		t.Fatalf("unexpected resolved symbol: %q", resolution.Symbol())
	}
}

type capturingBackend struct {
	fakeSentryBackend
	mu       sync.Mutex
	appended bool
	event    domain.CanonicalEvent
}

func (backend *capturingBackend) Run(
	ctx context.Context,
	program ingest.IngestProgram,
) result.Result[ingest.IngestTransactionResult] {
	ports := capturingPorts{backend: backend, issueID: backend.issueID, quota: backend.quota}
	return program(ctx, ports)
}

type capturingPorts struct {
	backend *capturingBackend
	issueID domain.IssueID
	quota   ingest.QuotaDecision
}

func (ports capturingPorts) Exists(
	ctx context.Context,
	projectID domain.ProjectID,
	eventID domain.EventID,
) result.Result[bool] {
	return result.Ok(false)
}

func (ports capturingPorts) Append(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
) result.Result[ingest.EventAppendResult] {
	ports.backend.mu.Lock()
	ports.backend.appended = true
	ports.backend.event = event.Event()
	ports.backend.mu.Unlock()
	return result.Ok(ingest.NewAppendedEvent())
}

func (ports capturingPorts) CheckQuota(
	ctx context.Context,
	event domain.CanonicalEvent,
) result.Result[ingest.QuotaDecision] {
	if ports.quota.Reason() != "" {
		return result.Ok(ports.quota)
	}

	return result.Ok(ingest.NewQuotaAllowed())
}

func (ports capturingPorts) Apply(
	ctx context.Context,
	plan ingestplan.IssuePlan,
) result.Result[ingest.IssueChange] {
	return result.Ok(ingest.NewIssueChange(ports.issueID, true))
}

func (ports capturingPorts) EnqueueIssueOpened(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
	change ingest.IssueChange,
) result.Result[ingest.IssueOpenedEnqueueResult] {
	return result.Ok(ingest.NewIssueOpenedEnqueueResult(0))
}

type capturingVault struct {
	mu       sync.Mutex
	contents map[string][]byte
}

func newCapturingVault() *capturingVault {
	return &capturingVault{contents: map[string][]byte{}}
}

func (vault *capturingVault) PutArtifact(
	_ context.Context,
	key domain.ArtifactKey,
	contents io.Reader,
) result.Result[artifacts.StoredArtifact] {
	body, readErr := io.ReadAll(contents)
	if readErr != nil {
		return result.Err[artifacts.StoredArtifact](readErr)
	}

	vault.mu.Lock()
	vault.contents[vaultKey(key)] = body
	vault.mu.Unlock()

	return result.Ok(artifacts.NewStoredArtifact(key, int64(len(body))))
}

func (vault *capturingVault) GetArtifact(
	_ context.Context,
	key domain.ArtifactKey,
) result.Result[io.ReadCloser] {
	vault.mu.Lock()
	body, present := vault.contents[vaultKey(key)]
	vault.mu.Unlock()

	if !present {
		return result.Err[io.ReadCloser](artifacts.ErrArtifactNotFound)
	}

	return result.Ok[io.ReadCloser](io.NopCloser(bytes.NewReader(body)))
}

func (vault *capturingVault) DeleteArtifact(
	_ context.Context,
	key domain.ArtifactKey,
) result.Result[struct{}] {
	vault.mu.Lock()
	delete(vault.contents, vaultKey(key))
	vault.mu.Unlock()
	return result.Ok(struct{}{})
}

func (vault *capturingVault) ListArtifacts(
	_ context.Context,
	scope artifacts.ArtifactScope,
) result.Result[[]artifacts.StoredArtifact] {
	stored := []artifacts.StoredArtifact{}
	prefix := strings.Join(
		[]string{
			scope.OrganizationID().String(),
			scope.ProjectID().String(),
			scope.Kind().String(),
		},
		"|",
	) + "|"

	vault.mu.Lock()
	defer vault.mu.Unlock()

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

func vaultKey(key domain.ArtifactKey) string {
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

func uploadFixtureSourceMap(
	t *testing.T,
	vault *capturingVault,
	organizationIDInput string,
	projectIDInput string,
	releaseInput string,
	fileNameInput string,
	payload []byte,
) {
	t.Helper()

	organizationID, orgErr := domain.NewOrganizationID(organizationIDInput)
	if orgErr != nil {
		t.Fatalf("organization id: %v", orgErr)
	}

	projectID, projectErr := domain.NewProjectID(projectIDInput)
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	release, releaseErr := domain.NewReleaseName(releaseInput)
	if releaseErr != nil {
		t.Fatalf("release: %v", releaseErr)
	}

	dist, _ := domain.NewOptionalDistName("")

	fileName, fileErr := domain.NewSourceMapFileName(fileNameInput)
	if fileErr != nil {
		t.Fatalf("file name: %v", fileErr)
	}

	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	service, serviceErr := sourcemaps.NewService(vault)
	if serviceErr != nil {
		t.Fatalf("service: %v", serviceErr)
	}

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
}

func buildSourceMapPayloadFixture(source string, name string, mappings string) []byte {
	body := strings.Builder{}
	body.WriteString(`{"version":3,"sources":["`)
	body.WriteString(source)
	body.WriteString(`"],"names":["`)
	body.WriteString(name)
	body.WriteString(`"],"mappings":"`)
	body.WriteString(mappings)
	body.WriteString(`"}`)
	return []byte(body.String())
}

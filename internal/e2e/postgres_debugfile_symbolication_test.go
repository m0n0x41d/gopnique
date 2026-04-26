//go:build integration

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanzakutnii/error-tracker/internal/adapters/filesystem"
	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestPostgresDebugFileSymbolicationE2E(t *testing.T) {
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
		httpadapter.IngestEnrichments{DebugFileStore: debugFileStore},
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
		strings.NewReader("organization_name=DebugFiles&project_name=DebugFiles&email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if setup.StatusCode != http.StatusOK {
		t.Fatalf("expected setup ok, got %d: %s", setup.StatusCode, setup.Body)
	}

	scope := projectScope(t, ctx, databaseURL)
	uploadBreakpadSymbolsE2E(t, ctx, debugFileStore, scope)

	publicKey := projectPublicKey(t, ctx, databaseURL)
	accepted := postNativeDebugEvent(t, client, server.URL, publicKey)
	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("expected native event accepted, got %d: %s", accepted.StatusCode, accepted.Body)
	}

	assertSymbolicatedNativePayload(t, ctx, databaseURL)
}

type e2eProjectScope struct {
	OrganizationID string
	ProjectID      string
}

func projectScope(t *testing.T, ctx context.Context, databaseURL string) e2eProjectScope {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := "select organization_id, id from projects order by created_at asc limit 1"
	scope := e2eProjectScope{}
	scanErr := pool.QueryRow(ctx, query).Scan(&scope.OrganizationID, &scope.ProjectID)
	if scanErr != nil {
		t.Fatalf("project scope: %v", scanErr)
	}

	return scope
}

func uploadBreakpadSymbolsE2E(
	t *testing.T,
	ctx context.Context,
	store *debugfiles.Service,
	scope e2eProjectScope,
) {
	t.Helper()

	organizationID, organizationErr := domain.NewOrganizationID(scope.OrganizationID)
	if organizationErr != nil {
		t.Fatalf("organization id: %v", organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(scope.ProjectID)
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	debugID, _ := domain.NewDebugIdentifier("deadbeefcafef00ddeadbeefcafef00d")
	fileName, _ := domain.NewDebugFileName("libapp.so.sym")
	identity, identityErr := domain.NewDebugFileIdentity(debugID, domain.DebugFileKindBreakpad(), fileName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	payload := []byte(
		"MODULE Linux x86_64 deadbeefcafef00ddeadbeefcafef00d libapp.so\n" +
			"FUNC 1000 20 0 render_home\n",
	)
	uploadResult := store.Upload(ctx, organizationID, projectID, identity, bytes.NewReader(payload))
	if _, uploadErr := uploadResult.Value(); uploadErr != nil {
		t.Fatalf("upload symbols: %v", uploadErr)
	}
}

func postNativeDebugEvent(
	t *testing.T,
	client *http.Client,
	baseURL string,
	publicKey string,
) responseSnapshot {
	t.Helper()

	body := strings.NewReader(`{
		"event_id":"690e8400e29b41d4a716446655440000",
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
	}`)

	return request(
		t,
		client,
		http.MethodPost,
		baseURL+"/api/1/store/?sentry_key="+publicKey,
		"application/json",
		body,
	)
}

func assertSymbolicatedNativePayload(
	t *testing.T,
	ctx context.Context,
	databaseURL string,
) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	query := `
select canonical_payload
from events
where event_id = '690e8400-e29b-41d4-a716-446655440000'
`
	var payloadBytes []byte
	scanErr := pool.QueryRow(ctx, query).Scan(&payloadBytes)
	if scanErr != nil {
		t.Fatalf("native payload: %v", scanErr)
	}

	var payload struct {
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

	if len(payload.Native.Frames) != 1 {
		t.Fatalf("expected one native frame: %#v", payload.Native.Frames)
	}

	frame := payload.Native.Frames[0]
	if frame.Function != "render_home" {
		t.Fatalf("expected symbolicated function, got %q", frame.Function)
	}

	if frame.Package != "/usr/lib/libapp.so" {
		t.Fatalf("unexpected package: %q", frame.Package)
	}
}

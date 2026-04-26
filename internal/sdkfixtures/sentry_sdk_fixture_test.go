//go:build sdkfixtures

package sdkfixtures

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ivanzakutnii/error-tracker/internal/adapters/filesystem"
	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/app/sourcemaps"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

const (
	nodeSentryVersion    = "10.50.0"
	browserSentryVersion = "10.50.0"
	pythonSentryVersion  = "2.58.0"
	goSentryVersion      = "v0.46.0"
	playwrightVersion    = "1.59.1"
	esbuildVersion       = "0.28.0"

	sourceMapFixtureRelease    = "frontend@sourcemap"
	sourceMapFixtureDist       = "web"
	sourceMapFixtureBundlePath = "/sdk-fixtures/source-map/app.min.js"
	sourceMapFixtureFileName   = "sdk-fixtures/source-map/app.min.js"
	sourceMapFixtureMarker     = "source mapped browser sdk fixture error"
)

func TestSentrySDKFixtureReplay(t *testing.T) {
	ctx := context.Background()
	adminURL := os.Getenv("ERROR_TRACKER_SDK_FIXTURE_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("ERROR_TRACKER_SDK_FIXTURE_POSTGRES_URL is required")
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
	if len(migrationResult.Applied) != 30 {
		t.Fatalf("expected 30 migrations, got %d", len(migrationResult.Applied))
	}

	handler := httpadapter.NewHandler(
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
		nil,
		store,
		httpadapter.IngestEnrichments{},
		httpadapter.AuthSettings{PublicURL: "http://example.test", SecretKey: "sdk-fixture-secret"},
	)
	server := httptest.NewServer(logIngestRequests(t, handler))
	defer server.Close()

	bootstrap, bootstrapErr := store.Bootstrap(ctx, postgres.BootstrapInput{
		PublicURL:        server.URL,
		OrganizationName: "SDK Fixture Organization",
		ProjectName:      "SDK Fixture Project",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	dsn := bootstrap.DSN
	runNodeSDK(t, dsn)
	runBrowserSDK(t, dsn, server.URL)
	runPythonSDK(t, dsn)
	runGoSDK(t, dsn)

	assertSDKEventsPersisted(t, ctx, databaseURL)
	assertSDKIssuesVisible(t, server.URL)
}

func TestSentryCLIArtifactUploadReplay(t *testing.T) {
	ctx := context.Background()
	adminURL := os.Getenv("ERROR_TRACKER_SDK_FIXTURE_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("ERROR_TRACKER_SDK_FIXTURE_POSTGRES_URL is required")
	}

	requireCommand(t, "npx")

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
	if len(migrationResult.Applied) != 30 {
		t.Fatalf("expected 30 migrations, got %d", len(migrationResult.Applied))
	}

	vault, vaultErr := filesystem.NewVault(t.TempDir())
	if vaultErr != nil {
		t.Fatalf("vault: %v", vaultErr)
	}

	sourceMapService, sourceMapErr := sourcemaps.NewService(vault)
	if sourceMapErr != nil {
		t.Fatalf("source map service: %v", sourceMapErr)
	}

	debugFileService, debugFileErr := debugfiles.NewService(vault)
	if debugFileErr != nil {
		t.Fatalf("debug file service: %v", debugFileErr)
	}

	enrichments := httpadapter.IngestEnrichments{
		ArtifactVault:     vault,
		SourceMapResolver: sourceMapService,
		DebugFileStore:    debugFileService,
	}
	handler := httpadapter.NewHandler(
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
		nil,
		store,
		enrichments,
		httpadapter.AuthSettings{PublicURL: "http://example.test", SecretKey: "sdk-fixture-secret"},
	)
	server := httptest.NewServer(logIngestRequests(t, handler))
	defer server.Close()

	bootstrap, bootstrapErr := store.Bootstrap(ctx, postgres.BootstrapInput{
		PublicURL:        server.URL,
		OrganizationName: "SDK Fixture Organization",
		ProjectName:      "SDK Fixture Project",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	token := createSDKFixtureProjectToken(t, ctx, store, bootstrap)
	runSentryCLISourceMapUpload(t, server.URL, token)
	assertSentryCLISourceMapStored(t, ctx, sourceMapService, bootstrap)

	runSentryCLIDebugFileUpload(t, server.URL, token)
	assertSentryCLIDebugFileStored(t, ctx, debugFileService, bootstrap)
}

func TestSentryJavaScriptSourceMapFixtureReplay(t *testing.T) {
	ctx := context.Background()
	adminURL := os.Getenv("ERROR_TRACKER_SDK_FIXTURE_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("ERROR_TRACKER_SDK_FIXTURE_POSTGRES_URL is required")
	}

	requireCommand(t, "node")
	requireCommand(t, "npm")
	requireCommand(t, "npx")

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
	if len(migrationResult.Applied) != 30 {
		t.Fatalf("expected 30 migrations, got %d", len(migrationResult.Applied))
	}

	vault, vaultErr := filesystem.NewVault(t.TempDir())
	if vaultErr != nil {
		t.Fatalf("vault: %v", vaultErr)
	}

	sourceMapService, sourceMapErr := sourcemaps.NewService(vault)
	if sourceMapErr != nil {
		t.Fatalf("source map service: %v", sourceMapErr)
	}

	fixture := buildBrowserSourceMapSDKFixture(t)
	enrichments := httpadapter.IngestEnrichments{
		ArtifactVault:     vault,
		SourceMapResolver: sourceMapService,
	}
	handler := httpadapter.NewHandler(
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
		nil,
		store,
		enrichments,
		httpadapter.AuthSettings{PublicURL: "http://example.test", SecretKey: "sdk-fixture-secret"},
	)
	server := httptest.NewServer(logIngestRequests(t, serveSourceMapFixture(handler, fixture)))
	defer server.Close()

	bootstrap, bootstrapErr := store.Bootstrap(ctx, postgres.BootstrapInput{
		PublicURL:        server.URL,
		OrganizationName: "SDK Fixture Organization",
		ProjectName:      "SDK Fixture Project",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	uploadBrowserSourceMapFixture(t, ctx, sourceMapService, bootstrap, fixture.sourceMap)
	runBrowserSourceMapSDK(t, fixture.dir, bootstrap.DSN, server.URL)
	assertSourceMappedSDKEventPersisted(t, ctx, databaseURL)
}

type browserSourceMapFixture struct {
	dir       string
	bundle    []byte
	sourceMap []byte
}

func logIngestRequests(t *testing.T, handler http.Handler) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(requestBody))

		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)

		for key, values := range recorder.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())

		if strings.HasPrefix(r.URL.Path, "/api/") {
			t.Logf("%s %s -> %d %s", r.Method, r.URL.Path, recorder.Code, strings.TrimSpace(recorder.Body.String()))
			if recorder.Code >= 400 {
				t.Logf("request body: %s", string(requestBody))
			}
		}
	})
}

func createTestDatabase(t *testing.T, ctx context.Context, adminURL string) string {
	t.Helper()

	name := fmt.Sprintf("error_tracker_sdk_%d", time.Now().UnixNano())
	adminPool, adminErr := pgxpool.New(ctx, adminURL)
	if adminErr != nil {
		t.Fatalf("admin pool: %v", adminErr)
	}
	defer adminPool.Close()

	_, createErr := adminPool.Exec(ctx, "create database "+name)
	if createErr != nil {
		t.Fatalf("create database: %v", createErr)
	}

	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "drop database if exists "+name+" with (force)")
	})

	return databaseURL(t, adminURL, name)
}

func databaseURL(t *testing.T, input string, database string) string {
	t.Helper()

	parsed, parseErr := url.Parse(input)
	if parseErr != nil {
		t.Fatalf("database url: %v", parseErr)
	}

	parsed.Path = "/" + database

	return parsed.String()
}

func runNodeSDK(t *testing.T, dsn string) {
	t.Helper()

	requireCommand(t, "node")
	requireCommand(t, "npm")

	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
  "private": true,
  "dependencies": {
    "@sentry/node": "`+nodeSentryVersion+`"
  }
}
`)
	writeFile(t, dir, "fixture.cjs", `
const Sentry = require("@sentry/node");

Sentry.init({
  dsn: process.env.SENTRY_DSN,
  tracesSampleRate: 0,
});

Sentry.captureException(new Error("node sdk fixture error"));

Sentry.flush(5000).then((flushed) => {
  if (!flushed) {
    console.error("sentry flush timed out");
    process.exit(2);
  }
}).catch((err) => {
  console.error(err);
  process.exit(1);
});
`)

	run(t, dir, nil, "npm", "install", "--silent")
	run(t, dir, []string{"SENTRY_DSN=" + dsn}, "node", "fixture.cjs")
}

func runBrowserSDK(t *testing.T, dsn string, serverURL string) {
	t.Helper()

	requireCommand(t, "node")
	requireCommand(t, "npm")

	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{
  "private": true,
  "dependencies": {
    "@sentry/browser": "`+browserSentryVersion+`",
    "esbuild": "`+esbuildVersion+`",
    "playwright": "`+playwrightVersion+`"
  }
}
`)
	writeFile(t, dir, "browser-entry.js", `
import * as Sentry from "@sentry/browser";

export async function run(dsn) {
  Sentry.init({
    dsn,
    tracesSampleRate: 0,
  });

  Sentry.captureException(new Error("browser sdk fixture error"));
  return await Sentry.flush(5000);
}
`)
	writeFile(t, dir, "fixture.cjs", `
const { chromium } = require("playwright");

async function main() {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  await page.goto(process.env.ERROR_TRACKER_BASE_URL + "/");

  await page.addScriptTag({ path: "fixture-bundle.js" });

  const flushed = await page.evaluate(async (dsn) => {
    return await window.Fixture.run(dsn);
  }, process.env.SENTRY_DSN);

  await browser.close();

  if (!flushed) {
    console.error("sentry browser flush timed out");
    process.exit(2);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`)

	run(t, dir, nil, "npm", "install", "--silent")
	run(t, dir, nil, "npx", "playwright", "install", "chromium")
	run(t, dir, nil, "npx", "esbuild", "browser-entry.js", "--bundle", "--format=iife", "--global-name=Fixture", "--outfile=fixture-bundle.js")
	run(
		t,
		dir,
		[]string{
			"SENTRY_DSN=" + dsn,
			"ERROR_TRACKER_BASE_URL=" + serverURL,
		},
		"node",
		"fixture.cjs",
	)
}

func buildBrowserSourceMapSDKFixture(t *testing.T) browserSourceMapFixture {
	t.Helper()

	dir := t.TempDir()
	makeDir(t, dir, "sdk-fixtures/source-map")
	writeFile(t, dir, "package.json", `{
  "private": true,
  "dependencies": {
    "@sentry/browser": "`+browserSentryVersion+`",
    "esbuild": "`+esbuildVersion+`",
    "playwright": "`+playwrightVersion+`"
  }
}
`)
	writeFile(t, dir, "browser-entry.js", `
import * as Sentry from "@sentry/browser";

export async function run(dsn) {
  Sentry.init({
    dsn,
    release: "`+sourceMapFixtureRelease+`",
    dist: "`+sourceMapFixtureDist+`",
    environment: "production",
    tracesSampleRate: 0,
  });

  captureSourceMappedFixture();
  return await Sentry.flush(5000);
}

function captureSourceMappedFixture() {
  try {
    throwSourceMappedFixture();
  } catch (err) {
    Sentry.captureException(err);
  }
}

function throwSourceMappedFixture() {
  throw new Error("`+sourceMapFixtureMarker+`");
}
`)
	writeFile(t, dir, "fixture.cjs", `
const { chromium } = require("playwright");

async function main() {
  const browser = await chromium.launch({ headless: true });
  const page = await browser.newPage();
  await page.goto(process.env.ERROR_TRACKER_BASE_URL + "/");

  await page.addScriptTag({
    url: process.env.ERROR_TRACKER_BASE_URL + "`+sourceMapFixtureBundlePath+`",
  });

  const flushed = await page.evaluate(async (dsn) => {
    return await window.SourceMapFixture.run(dsn);
  }, process.env.SENTRY_DSN);

  await browser.close();

  if (!flushed) {
    console.error("sentry browser source-map flush timed out");
    process.exit(2);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
`)

	run(t, dir, nil, "npm", "install", "--silent")
	run(t, dir, nil, "npx", "playwright", "install", "chromium")
	run(
		t,
		dir,
		nil,
		"npx",
		"esbuild",
		"browser-entry.js",
		"--bundle",
		"--format=iife",
		"--global-name=SourceMapFixture",
		"--outfile="+strings.TrimPrefix(sourceMapFixtureBundlePath, "/"),
		"--sourcemap=external",
		"--minify",
		"--sources-content=true",
	)

	return browserSourceMapFixture{
		dir:       dir,
		bundle:    readFile(t, dir, strings.TrimPrefix(sourceMapFixtureBundlePath, "/")),
		sourceMap: readFile(t, dir, strings.TrimPrefix(sourceMapFixtureBundlePath, "/")+".map"),
	}
}

func serveSourceMapFixture(handler http.Handler, fixture browserSourceMapFixture) http.Handler {
	files := map[string]staticFixtureFile{
		sourceMapFixtureBundlePath: {
			contentType: "application/javascript; charset=utf-8",
			body:        fixture.bundle,
		},
		sourceMapFixtureBundlePath + ".map": {
			contentType: "application/json; charset=utf-8",
			body:        fixture.sourceMap,
		},
	}

	return serveStaticFixture(handler, files)
}

type staticFixtureFile struct {
	contentType string
	body        []byte
}

func serveStaticFixture(handler http.Handler, files map[string]staticFixtureFile) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, ok := files[r.URL.Path]
		if !ok {
			handler.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Content-Type", file.contentType)
		_, _ = w.Write(file.body)
	})
}

func runBrowserSourceMapSDK(t *testing.T, dir string, dsn string, serverURL string) {
	t.Helper()

	run(
		t,
		dir,
		[]string{
			"SENTRY_DSN=" + dsn,
			"ERROR_TRACKER_BASE_URL=" + serverURL,
		},
		"node",
		"fixture.cjs",
	)
}

func runPythonSDK(t *testing.T, dsn string) {
	t.Helper()

	requireCommand(t, "python3")

	dir := t.TempDir()
	venvDir := filepath.Join(dir, ".venv")
	run(t, dir, nil, "python3", "-m", "venv", venvDir)

	python := filepath.Join(venvDir, "bin", "python")
	pip := filepath.Join(venvDir, "bin", "pip")
	run(t, dir, nil, pip, "install", "--quiet", "sentry-sdk=="+pythonSentryVersion)

	writeFile(t, dir, "fixture.py", `
import os
import sentry_sdk

sentry_sdk.init(
    dsn=os.environ["SENTRY_DSN"],
    traces_sample_rate=0,
)

try:
    raise RuntimeError("python sdk fixture error")
except Exception as exc:
    sentry_sdk.capture_exception(exc)

sentry_sdk.flush(timeout=5)
`)

	run(t, dir, []string{"SENTRY_DSN=" + dsn}, python, "fixture.py")
}

func runGoSDK(t *testing.T, dsn string) {
	t.Helper()

	requireCommand(t, "go")

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", `module sdkfixture

go 1.25
`)
	writeFile(t, dir, "main.go", `
package main

import (
	"errors"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

func main() {
	err := sentry.Init(sentry.ClientOptions{
		Dsn: os.Getenv("SENTRY_DSN"),
	})
	if err != nil {
		panic(err)
	}

	sentry.CaptureException(errors.New("go sdk fixture error"))
	sentry.Flush(5 * time.Second)
}
`)

	run(t, dir, []string{"GOWORK=off"}, "go", "get", "github.com/getsentry/sentry-go@"+goSentryVersion)
	run(t, dir, []string{"GOWORK=off", "SENTRY_DSN=" + dsn}, "go", "run", ".")
}

func createSDKFixtureProjectToken(
	t *testing.T,
	ctx context.Context,
	store *postgres.Store,
	bootstrap postgres.BootstrapResult,
) string {
	t.Helper()

	loginResult := store.Login(ctx, operators.LoginCommand{
		Email:    "operator@example.test",
		Password: "correct-horse-battery-staple",
	})
	login, loginErr := loginResult.Value()
	if loginErr != nil {
		t.Fatalf("login: %v", loginErr)
	}

	sessionResult := store.ResolveSession(ctx, login.Session)
	session, sessionErr := sessionResult.Value()
	if sessionErr != nil {
		t.Fatalf("session: %v", sessionErr)
	}

	organizationID, organizationErr := domain.NewOrganizationID(bootstrap.OrganizationID)
	if organizationErr != nil {
		t.Fatalf("organization id: %v", organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(bootstrap.ProjectID)
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	createResult := tokenapp.CreateProjectToken(
		ctx,
		store,
		tokenapp.CreateProjectTokenCommand{
			Scope: tokenapp.Scope{
				OrganizationID: organizationID,
				ProjectID:      projectID,
			},
			ActorID:    session.OperatorID,
			Name:       "sentry-cli fixture",
			TokenScope: tokenapp.ProjectTokenScopeAdmin,
		},
	)
	created, createErr := createResult.Value()
	if createErr != nil {
		t.Fatalf("create project token: %v", createErr)
	}

	return created.OneTimeToken
}

func uploadBrowserSourceMapFixture(
	t *testing.T,
	ctx context.Context,
	service *sourcemaps.Service,
	bootstrap postgres.BootstrapResult,
	payload []byte,
) {
	t.Helper()

	organizationID, organizationErr := domain.NewOrganizationID(bootstrap.OrganizationID)
	if organizationErr != nil {
		t.Fatalf("organization id: %v", organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(bootstrap.ProjectID)
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	release, releaseErr := domain.NewReleaseName(sourceMapFixtureRelease)
	if releaseErr != nil {
		t.Fatalf("release: %v", releaseErr)
	}

	dist, distErr := domain.NewOptionalDistName(sourceMapFixtureDist)
	if distErr != nil {
		t.Fatalf("dist: %v", distErr)
	}

	fileName, fileNameErr := domain.NewSourceMapFileName(sourceMapFixtureFileName)
	if fileNameErr != nil {
		t.Fatalf("file name: %v", fileNameErr)
	}

	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	uploadResult := service.Upload(ctx, organizationID, projectID, identity, bytes.NewReader(payload))
	if _, uploadErr := uploadResult.Value(); uploadErr != nil {
		t.Fatalf("upload source map fixture: %v", uploadErr)
	}
}

func runSentryCLISourceMapUpload(t *testing.T, serverURL string, token string) {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, dir, "app.js", `function boom(){throw new Error("cli sourcemap fixture")}
//# sourceMappingURL=app.js.map
`)
	writeFile(t, dir, "app.js.map", `{"version":3,"file":"app.js","sources":["src/app.js"],"sourcesContent":["export function boom(){}"],"names":["boom"],"mappings":"AAAA,SAASA,OAAO"}
`)

	run(
		t,
		dir,
		nil,
		"npx",
		"--yes",
		"@sentry/cli",
		"--url",
		serverURL,
		"--auth-token",
		token,
		"sourcemaps",
		"upload",
		"--org",
		"default",
		"--project",
		"default",
		"--release",
		"cli@1",
		"--dist",
		"web",
		"--url-prefix",
		"~/static/js",
		dir,
	)
}

func assertSentryCLISourceMapStored(
	t *testing.T,
	ctx context.Context,
	service *sourcemaps.Service,
	bootstrap postgres.BootstrapResult,
) {
	t.Helper()

	organizationID, organizationErr := domain.NewOrganizationID(bootstrap.OrganizationID)
	if organizationErr != nil {
		t.Fatalf("organization id: %v", organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(bootstrap.ProjectID)
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	release, releaseErr := domain.NewReleaseName("cli@1")
	if releaseErr != nil {
		t.Fatalf("release: %v", releaseErr)
	}

	dist, distErr := domain.NewOptionalDistName("web")
	if distErr != nil {
		t.Fatalf("dist: %v", distErr)
	}

	fileName, fileNameErr := domain.NewSourceMapFileName("static/js/app.js")
	if fileNameErr != nil {
		t.Fatalf("file name: %v", fileNameErr)
	}

	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	resolvedResult := service.Resolve(
		ctx,
		organizationID,
		projectID,
		identity,
		sourcemaps.NewGeneratedPosition(0, 0),
	)
	resolved, resolvedErr := resolvedResult.Value()
	if resolvedErr != nil {
		t.Fatalf("resolve sentry-cli source map: %v", resolvedErr)
	}

	if resolved.Source() == "" {
		t.Fatal("resolved source must not be empty")
	}
}

func runSentryCLIDebugFileUpload(t *testing.T, serverURL string, token string) {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, dir, "libapp.so.sym", `MODULE Linux x86_64 deadbeefcafef00ddeadbeefcafef00d libapp.so
FILE 0 src/app.c
FUNC 0 4 0 main
0 4 1 0
`)

	run(
		t,
		dir,
		nil,
		"npx",
		"--yes",
		"@sentry/cli",
		"--url",
		serverURL,
		"--auth-token",
		token,
		"debug-files",
		"upload",
		"--org",
		"default",
		"--project",
		"default",
		"--type",
		"breakpad",
		dir,
	)
}

func assertSentryCLIDebugFileStored(
	t *testing.T,
	ctx context.Context,
	service *debugfiles.Service,
	bootstrap postgres.BootstrapResult,
) {
	t.Helper()

	organizationID, organizationErr := domain.NewOrganizationID(bootstrap.OrganizationID)
	if organizationErr != nil {
		t.Fatalf("organization id: %v", organizationErr)
	}

	projectID, projectErr := domain.NewProjectID(bootstrap.ProjectID)
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	debugID, debugIDErr := domain.NewDebugIdentifier("deadbeef-cafe-f00d-dead-beefcafef00d")
	if debugIDErr != nil {
		t.Fatalf("debug id: %v", debugIDErr)
	}

	fileName, fileNameErr := domain.NewDebugFileName("libapp.so.sym")
	if fileNameErr != nil {
		t.Fatalf("debug file name: %v", fileNameErr)
	}

	identity, identityErr := domain.NewDebugFileIdentity(debugID, domain.DebugFileKindBreakpad(), fileName)
	if identityErr != nil {
		t.Fatalf("debug file identity: %v", identityErr)
	}

	readerResult := service.Get(ctx, organizationID, projectID, identity)
	reader, readerErr := readerResult.Value()
	if readerErr != nil {
		t.Fatalf("get sentry-cli debug file: %v", readerErr)
	}
	defer reader.Close()

	payload, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("read sentry-cli debug file: %v", readErr)
	}

	if !strings.Contains(string(payload), "MODULE Linux x86_64") {
		t.Fatalf("unexpected debug file payload: %q", string(payload))
	}
}

func assertSourceMappedSDKEventPersisted(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	payloads := sourceMappedSDKPayloads(t, ctx, pool)
	if len(payloads) != 1 {
		t.Fatalf("expected one source-mapped SDK event, got %d", len(payloads))
	}

	payload := payloads[0]
	if payload["release"] != sourceMapFixtureRelease {
		t.Fatalf("unexpected release in canonical payload: %#v", payload["release"])
	}

	tags, ok := payload["tags"].(map[string]any)
	if !ok {
		t.Fatalf("expected canonical payload tags object, got %T", payload["tags"])
	}

	if tags["dist"] != sourceMapFixtureDist {
		t.Fatalf("unexpected dist tag in canonical payload: %#v", tags)
	}

	assertSourceMappedSDKFrameResolved(t, payload)
}

func sourceMappedSDKPayloads(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
) []map[string]any {
	t.Helper()

	query := `
select canonical_payload::text
from events
where title ilike '%' || $1 || '%'
order by received_at asc
`
	rows, rowsErr := pool.Query(ctx, query, sourceMapFixtureMarker)
	if rowsErr != nil {
		t.Fatalf("query source-mapped SDK events: %v", rowsErr)
	}
	defer rows.Close()

	payloads := []map[string]any{}
	for rows.Next() {
		var payloadText string
		scanErr := rows.Scan(&payloadText)
		if scanErr != nil {
			t.Fatalf("scan canonical payload: %v", scanErr)
		}

		var payload map[string]any
		decodeErr := json.Unmarshal([]byte(payloadText), &payload)
		if decodeErr != nil {
			t.Fatalf("decode canonical payload: %v", decodeErr)
		}

		payloads = append(payloads, payload)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		t.Fatalf("read source-mapped SDK events: %v", rowsErr)
	}

	return payloads
}

func assertSourceMappedSDKFrameResolved(t *testing.T, payload map[string]any) {
	t.Helper()

	rawFrames, ok := payload["js_stacktrace"].([]any)
	if !ok {
		t.Fatalf("expected js_stacktrace array, got %T: %#v", payload["js_stacktrace"], payload)
	}

	for _, rawFrame := range rawFrames {
		frame, ok := rawFrame.(map[string]any)
		if !ok {
			continue
		}

		resolved, ok := frame["resolved"].(map[string]any)
		if !ok {
			continue
		}

		source, ok := resolved["source"].(string)
		if !ok || !strings.Contains(source, "browser-entry.js") {
			continue
		}

		line, lineOK := resolved["line"].(float64)
		column, columnOK := resolved["column"].(float64)
		if !lineOK || !columnOK || line < 1 || column < 0 {
			t.Fatalf("unexpected resolved position: %#v", resolved)
		}

		return
	}

	encoded, encodeErr := json.Marshal(payload)
	if encodeErr != nil {
		t.Fatalf("expected a resolved browser-entry.js frame: %#v", payload)
	}

	t.Fatalf("expected a resolved browser-entry.js frame: %s", encoded)
}

func assertSDKEventsPersisted(t *testing.T, ctx context.Context, databaseURL string) {
	t.Helper()

	pool, poolErr := pgxpool.New(ctx, databaseURL)
	if poolErr != nil {
		t.Fatalf("pool: %v", poolErr)
	}
	defer pool.Close()

	expected := []string{
		"node sdk fixture error",
		"browser sdk fixture error",
		"python sdk fixture error",
		"go sdk fixture error",
	}
	for _, marker := range expected {
		var count int
		query := `select count(*) from events where title ilike '%' || $1 || '%'`
		scanErr := pool.QueryRow(ctx, query, marker).Scan(&count)
		if scanErr != nil {
			t.Fatalf("query marker %q: %v", marker, scanErr)
		}

		if count != 1 {
			t.Fatalf("expected one persisted SDK event for %q, got %d; titles: %s", marker, count, allEventTitles(t, ctx, pool))
		}
	}
}

func allEventTitles(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()

	rows, rowsErr := pool.Query(ctx, "select title from events order by received_at asc")
	if rowsErr != nil {
		return rowsErr.Error()
	}
	defer rows.Close()

	titles := []string{}
	for rows.Next() {
		var title string
		scanErr := rows.Scan(&title)
		if scanErr != nil {
			return scanErr.Error()
		}

		titles = append(titles, title)
	}

	return strings.Join(titles, " | ")
}

func assertSDKIssuesVisible(t *testing.T, baseURL string) {
	t.Helper()

	client := newHTTPClient(t)
	login := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/login",
		"application/x-www-form-urlencoded",
		strings.NewReader("email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if login.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected login redirect, got %d: %s", login.StatusCode, login.Body)
	}

	issues := request(t, client, http.MethodGet, baseURL+"/issues", "", nil)
	if issues.StatusCode != http.StatusOK {
		t.Fatalf("expected issues ok, got %d: %s", issues.StatusCode, issues.Body)
	}

	expected := []string{
		"node sdk fixture error",
		"browser sdk fixture error",
		"python sdk fixture error",
		"go sdk fixture error",
	}
	for _, marker := range expected {
		if !strings.Contains(issues.Body, marker) {
			t.Fatalf("expected issue list to contain %q", marker)
		}
	}
}

type responseSnapshot struct {
	StatusCode int
	Body       string
}

func newHTTPClient(t *testing.T) *http.Client {
	t.Helper()

	jar, jarErr := cookiejar.New(nil)
	if jarErr != nil {
		t.Fatalf("cookie jar: %v", jarErr)
	}

	return &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func request(
	t *testing.T,
	client *http.Client,
	method string,
	target string,
	contentType string,
	body io.Reader,
) responseSnapshot {
	t.Helper()

	req, reqErr := http.NewRequest(method, target, body)
	if reqErr != nil {
		t.Fatalf("request: %v", reqErr)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	res, resErr := client.Do(req)
	if resErr != nil {
		t.Fatalf("do request: %v", resErr)
	}
	defer res.Body.Close()

	responseBody, readErr := io.ReadAll(res.Body)
	if readErr != nil {
		t.Fatalf("read response: %v", readErr)
	}

	return responseSnapshot{
		StatusCode: res.StatusCode,
		Body:       string(responseBody),
	}
}

func requireCommand(t *testing.T, command string) {
	t.Helper()

	_, lookupErr := exec.LookPath(command)
	if lookupErr != nil {
		t.Skipf("%s is required", command)
	}
}

func writeFile(t *testing.T, dir string, name string, content string) {
	t.Helper()

	writeErr := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
	if writeErr != nil {
		t.Fatalf("write %s: %v", name, writeErr)
	}
}

func readFile(t *testing.T, dir string, name string) []byte {
	t.Helper()

	content, readErr := os.ReadFile(filepath.Join(dir, name))
	if readErr != nil {
		t.Fatalf("read %s: %v", name, readErr)
	}

	return content
}

func makeDir(t *testing.T, dir string, name string) {
	t.Helper()

	mkdirErr := os.MkdirAll(filepath.Join(dir, name), 0o700)
	if mkdirErr != nil {
		t.Fatalf("mkdir %s: %v", name, mkdirErr)
	}
}

func run(
	t *testing.T,
	dir string,
	extraEnv []string,
	name string,
	args ...string,
) {
	t.Helper()

	command := exec.Command(name, args...)
	command.Dir = dir
	command.Env = append(os.Environ(), extraEnv...)

	output, runErr := command.CombinedOutput()
	if runErr != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), runErr, string(output))
	}
}

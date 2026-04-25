//go:build sdkfixtures

package sdkfixtures

import (
	"bytes"
	"context"
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

	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
)

const (
	nodeSentryVersion    = "10.50.0"
	browserSentryVersion = "10.50.0"
	pythonSentryVersion  = "2.58.0"
	goSentryVersion      = "v0.46.0"
	playwrightVersion    = "1.59.1"
	esbuildVersion       = "0.28.0"
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
	if len(migrationResult.Applied) != 25 {
		t.Fatalf("expected 25 migrations, got %d", len(migrationResult.Applied))
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
		nil,
		store,
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

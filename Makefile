.PHONY: build test check run migrate templ license-audit repository-tests all-in-one-smoke e2e sdk-fixtures

E2E_POSTGRES_URL ?= postgres://error_tracker:error_tracker@127.0.0.1:55432/postgres?sslmode=disable
SDK_FIXTURE_POSTGRES_URL ?= $(E2E_POSTGRES_URL)
REPOSITORY_POSTGRES_URL ?= $(E2E_POSTGRES_URL)

build:
	go build -o bin/error-tracker ./cmd/error-tracker

test:
	go test ./...

check:
	go test ./...
	go run ./cmd/error-tracker admin check-imports
	sh scripts/license_audit.sh

run:
	go run ./cmd/error-tracker all-in-one

migrate:
	go run ./cmd/error-tracker migrate up

templ:
	templ generate

license-audit:
	sh scripts/license_audit.sh

repository-tests:
	ERROR_TRACKER_REPOSITORY_POSTGRES_URL="$(REPOSITORY_POSTGRES_URL)" go test -tags=integration ./internal/adapters/postgres -run 'TestPostgres(RepositoryContract|IssueShortIDConcurrency|RetentionPurgesScopedProjectData|QuotaRejectsBeforePersistence|UptimeMonitorWorkflow|OAuthOIDCWorkflow|ImporterWorkflow)' -count=1 -v

all-in-one-smoke:
	ERROR_TRACKER_E2E_POSTGRES_URL="$(E2E_POSTGRES_URL)" go test -tags=integration ./internal/e2e -run TestAllInOneProcessSmoke -count=1 -v

e2e:
	ERROR_TRACKER_E2E_POSTGRES_URL="$(E2E_POSTGRES_URL)" go test -tags=integration ./internal/e2e -run 'TestPostgres(M1M2E2E|RateLimitE2E|SecurityCSPE2E|MinidumpE2E|DebugFileSymbolicationE2E|UptimeMonitorE2E|ObservabilityAPIE2E|OAuthE2E)' -count=1 -v

sdk-fixtures:
	ERROR_TRACKER_SDK_FIXTURE_POSTGRES_URL="$(SDK_FIXTURE_POSTGRES_URL)" go test -tags=sdkfixtures ./internal/sdkfixtures -run 'TestSentry(SDKFixtureReplay|CLIArtifactUploadReplay|JavaScriptSourceMapFixtureReplay)' -count=1 -v

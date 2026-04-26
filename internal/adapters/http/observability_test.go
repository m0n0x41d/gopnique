package httpadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/health"
	observabilityapp "github.com/ivanzakutnii/error-tracker/internal/app/observability"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestObservabilityAPIRequiresSession(t *testing.T) {
	backend := newFakeObservabilityBackend(t, "admin")
	mux := newObservabilityTestMux(backend)
	request := httptest.NewRequest(http.MethodGet, "/api/admin/observability", nil)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d", response.Code)
	}

	if !strings.Contains(response.Body.String(), "unauthorized") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestObservabilityAPIRequiresOpsPermission(t *testing.T) {
	backend := newFakeObservabilityBackend(t, "member")
	mux := newObservabilityTestMux(backend)
	request := observabilityRequest(http.MethodGet, "/api/admin/observability")
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", response.Code)
	}
}

func TestObservabilityAPIRendersScopedSnapshot(t *testing.T) {
	backend := newFakeObservabilityBackend(t, "admin")
	mux := newObservabilityTestMux(backend)
	request := observabilityRequest(http.MethodGet, "/api/admin/observability")
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("unexpected cache header: %q", response.Header().Get("Cache-Control"))
	}

	for _, expected := range []string{
		`"service_name":"error-tracker"`,
		`"applied_count":30`,
		`"provider":"telegram"`,
		`"status":"pending"`,
		`"events":5`,
		`"notification_intents":2`,
	} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("expected %q in body: %s", expected, response.Body.String())
		}
	}
}

func TestObservabilityAPIRendersExplicitEndpoints(t *testing.T) {
	backend := newFakeObservabilityBackend(t, "admin")
	mux := newObservabilityTestMux(backend)
	paths := []string{
		"/api/admin/observability/system",
		"/api/admin/observability/readiness",
		"/api/admin/observability/migrations",
		"/api/admin/observability/queue",
		"/api/admin/observability/metrics",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			request := observabilityRequest(http.MethodGet, path)
			response := httptest.NewRecorder()

			mux.ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
			}

			if response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
				t.Fatalf("unexpected content type: %q", response.Header().Get("Content-Type"))
			}
		})
	}
}

func newObservabilityTestMux(backend fakeObservabilityBackend) http.Handler {
	return newMux(
		backend,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		backend,
		IngestEnrichments{},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)
}

func observabilityRequest(method string, target string) *http.Request {
	codec := NewSessionCodec("test-secret")
	token := mustSessionToken("operator@example.test")
	request := httptest.NewRequest(method, target, nil)
	request.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: codec.Encode(token),
	})

	return request
}

type fakeObservabilityBackend struct {
	session operators.OperatorSession
}

func newFakeObservabilityBackend(t *testing.T, projectRole string) fakeObservabilityBackend {
	t.Helper()

	organizationID := mustDomainID(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustDomainID(t, domain.NewProjectID, "2222222222224222a222222222222222")

	return fakeObservabilityBackend{
		session: operators.OperatorSession{
			OperatorID:       "operator",
			Email:            "operator@example.test",
			OrganizationID:   organizationID,
			ProjectID:        projectID,
			OrganizationRole: "",
			ProjectRole:      projectRole,
		},
	}
}

func (backend fakeObservabilityBackend) Ping(ctx context.Context) error {
	return nil
}

func (backend fakeObservabilityBackend) MigrationStatus(ctx context.Context) (health.MigrationStatus, error) {
	return health.MigrationStatus{
		AppliedCount: 30,
		Ready:        true,
	}, nil
}

func (backend fakeObservabilityBackend) QueueStatus(
	ctx context.Context,
	scope observabilityapp.Scope,
) result.Result[observabilityapp.QueueStatus] {
	return result.Ok(observabilityapp.QueueStatus{
		Groups: []observabilityapp.QueueGroup{
			{
				Provider: "telegram",
				Status:   "pending",
				Count:    2,
			},
		},
	})
}

func (backend fakeObservabilityBackend) AdminMetrics(
	ctx context.Context,
	scope observabilityapp.Scope,
) result.Result[observabilityapp.AdminMetrics] {
	return result.Ok(observabilityapp.AdminMetrics{
		Events:              5,
		Issues:              1,
		NotificationIntents: 2,
	})
}

func (backend fakeObservabilityBackend) IsBootstrapped(ctx context.Context) result.Result[bool] {
	return result.Ok(true)
}

func (backend fakeObservabilityBackend) BootstrapOperator(
	ctx context.Context,
	command operators.BootstrapCommand,
) result.Result[operators.BootstrapResult] {
	return result.Ok(operators.BootstrapResult{})
}

func (backend fakeObservabilityBackend) Login(
	ctx context.Context,
	command operators.LoginCommand,
) result.Result[operators.LoginResult] {
	return result.Ok(operators.LoginResult{})
}

func (backend fakeObservabilityBackend) ResolveSession(
	ctx context.Context,
	token operators.SessionToken,
) result.Result[operators.OperatorSession] {
	return result.Ok(backend.session)
}

func (backend fakeObservabilityBackend) DeleteSession(
	ctx context.Context,
	token operators.SessionToken,
) result.Result[struct{}] {
	return result.Ok(struct{}{})
}

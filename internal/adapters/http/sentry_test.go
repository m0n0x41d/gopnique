package httpadapter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	issueapp "github.com/ivanzakutnii/error-tracker/internal/app/issues"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	userreportapp "github.com/ivanzakutnii/error-tracker/internal/app/userreports"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

func TestSentryStoreRouteIngestsAfterAuth(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/store/?sentry_key=550e8400e29b41d4a716446655440000",
		strings.NewReader(`{"event_id":"550e8400e29b41d4a716446655440000","timestamp":"2026-04-24T10:00:00Z","message":"hello"}`),
	)
	response := httptest.NewRecorder()
	backend := newFakeSentryBackend(t)
	mux := newMux(nil, backend, backend, nil, nil, nil, nil, nil, nil, nil, nil, backend, NewSessionCodec("test-secret"), AuthSettings{PublicURL: "http://example.test"})

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", response.Code)
	}

	if !strings.Contains(response.Body.String(), "550e8400e29b41d4a716446655440000") {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestSentryEnvelopeRouteDeniesMissingAuth(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/envelope/",
		strings.NewReader(`{}`),
	)
	response := httptest.NewRecorder()
	backend := newFakeSentryBackend(t)
	mux := newMux(nil, backend, backend, nil, nil, nil, nil, nil, nil, nil, nil, backend, NewSessionCodec("test-secret"), AuthSettings{PublicURL: "http://example.test"})

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("unexpected status: %d", response.Code)
	}
}

func TestDecodeSentryAuthCarrierReadsHeaderAuth(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/store/",
		strings.NewReader(`{}`),
	)
	request.SetPathValue("project_ref", "42")
	request.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key=550e8400e29b41d4a716446655440000")

	carrierResult := decodeSentryAuthCarrier(request, nil, sentryStoreRequest)
	carrier, err := carrierResult.Value()
	if err != nil {
		t.Fatalf("decode auth: %v", err)
	}

	if carrier.projectRef != "42" {
		t.Fatalf("unexpected project ref: %s", carrier.projectRef)
	}

	if carrier.publicKey != "550e8400e29b41d4a716446655440000" {
		t.Fatalf("unexpected public key: %s", carrier.publicKey)
	}
}

func TestDecodeSentryAuthCarrierReadsEnvelopeDSN(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/envelope/",
		strings.NewReader(`{}`),
	)
	request.SetPathValue("project_ref", "42")
	body := []byte(`{"dsn":"http://550e8400e29b41d4a716446655440000@example.test/42"}
{"type":"event"}
{}`)

	carrierResult := decodeSentryAuthCarrier(request, body, sentryEnvelopeRequest)
	carrier, err := carrierResult.Value()
	if err != nil {
		t.Fatalf("decode auth: %v", err)
	}

	if carrier.projectRef != "42" {
		t.Fatalf("unexpected project ref: %s", carrier.projectRef)
	}

	if carrier.publicKey != "550e8400e29b41d4a716446655440000" {
		t.Fatalf("unexpected public key: %s", carrier.publicKey)
	}
}

func TestDecodeSentryAuthCarrierRejectsConflictingSources(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/envelope/?sentry_key=550e8400e29b41d4a716446655440000",
		strings.NewReader(`{}`),
	)
	request.SetPathValue("project_ref", "42")
	body := []byte(`{"dsn":"http://660e8400e29b41d4a716446655440000@example.test/42"}
{"type":"event"}
{}`)

	carrierResult := decodeSentryAuthCarrier(request, body, sentryEnvelopeRequest)
	_, err := carrierResult.Value()
	if err == nil {
		t.Fatal("expected conflicting auth sources to fail")
	}
}

type fakeSentryBackend struct {
	auth    domain.ProjectAuth
	issueID domain.IssueID
}

func newFakeSentryBackend(t *testing.T) fakeSentryBackend {
	t.Helper()

	organizationID := mustDomainID(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustDomainID(t, domain.NewProjectID, "2222222222224222a222222222222222")
	auth, authErr := domain.NewProjectAuth(organizationID, projectID)
	if authErr != nil {
		t.Fatalf("project auth: %v", authErr)
	}

	issueID := mustDomainID(t, domain.NewIssueID, "3333333333334333a333333333333333")

	return fakeSentryBackend{
		auth:    auth,
		issueID: issueID,
	}
}

func (backend fakeSentryBackend) ResolveProjectKey(
	ctx context.Context,
	ref domain.ProjectRef,
	key domain.ProjectPublicKey,
) result.Result[domain.ProjectAuth] {
	return result.Ok(backend.auth)
}

func (backend fakeSentryBackend) Run(
	ctx context.Context,
	program ingest.IngestProgram,
) result.Result[ingest.IngestTransactionResult] {
	ports := fakeSentryPorts{issueID: backend.issueID}

	return program(ctx, ports)
}

func (backend fakeSentryBackend) ListIssues(
	ctx context.Context,
	query issueapp.IssueListQuery,
) result.Result[issueapp.IssueListView] {
	return result.Ok(issueapp.IssueListView{})
}

func (backend fakeSentryBackend) ShowIssue(
	ctx context.Context,
	query issueapp.IssueDetailQuery,
) result.Result[issueapp.IssueDetailView] {
	return result.Ok(issueapp.IssueDetailView{})
}

func (backend fakeSentryBackend) ShowEvent(
	ctx context.Context,
	query issueapp.EventDetailQuery,
) result.Result[issueapp.EventDetailView] {
	return result.Ok(issueapp.EventDetailView{})
}

func (backend fakeSentryBackend) SubmitUserReport(
	ctx context.Context,
	command userreportapp.SubmitCommand,
) result.Result[userreportapp.SubmitReceipt] {
	return result.Ok(userreportapp.SubmitReceipt{
		ReportID: "55555555-5555-4555-a555-555555555555",
		EventID:  command.EventID.String(),
	})
}

func (backend fakeSentryBackend) ListIssueUserReports(
	ctx context.Context,
	query userreportapp.IssueReportsQuery,
) result.Result[userreportapp.IssueReportsView] {
	return result.Ok(userreportapp.IssueReportsView{})
}

func (backend fakeSentryBackend) TransitionIssueStatus(
	ctx context.Context,
	command issueapp.StatusTransitionCommand,
) result.Result[issueapp.StatusTransitionResult] {
	return result.Ok(issueapp.StatusTransitionResult{
		IssueID: command.IssueID.String(),
		Status:  command.TargetStatus,
	})
}

func (backend fakeSentryBackend) AddIssueComment(
	ctx context.Context,
	command issueapp.AddCommentCommand,
) result.Result[issueapp.CommentMutationResult] {
	return result.Ok(issueapp.CommentMutationResult{
		CommentID: "44444444-4444-4444-8444-444444444444",
	})
}

func (backend fakeSentryBackend) AssignIssue(
	ctx context.Context,
	command issueapp.AssignIssueCommand,
) result.Result[issueapp.AssignmentMutationResult] {
	return result.Ok(issueapp.AssignmentMutationResult{
		IssueID: command.IssueID.String(),
		Target:  command.Target,
	})
}

func (backend fakeSentryBackend) IsBootstrapped(ctx context.Context) result.Result[bool] {
	return result.Ok(true)
}

func (backend fakeSentryBackend) BootstrapOperator(
	ctx context.Context,
	command operators.BootstrapCommand,
) result.Result[operators.BootstrapResult] {
	token := mustSessionToken(command.Email)

	return result.Ok(operators.BootstrapResult{DSN: "http://example.test/1", Session: token})
}

func (backend fakeSentryBackend) Login(
	ctx context.Context,
	command operators.LoginCommand,
) result.Result[operators.LoginResult] {
	return result.Ok(operators.LoginResult{Session: mustSessionToken(command.Email)})
}

func (backend fakeSentryBackend) ResolveSession(
	ctx context.Context,
	token operators.SessionToken,
) result.Result[operators.OperatorSession] {
	return result.Ok(operators.OperatorSession{
		OperatorID:     "operator",
		Email:          "operator@example.test",
		OrganizationID: backend.auth.OrganizationID(),
		ProjectID:      backend.auth.ProjectID(),
	})
}

func (backend fakeSentryBackend) DeleteSession(
	ctx context.Context,
	token operators.SessionToken,
) result.Result[struct{}] {
	return result.Ok(struct{}{})
}

func mustSessionToken(seed string) operators.SessionToken {
	token, err := operators.NewSessionToken(strings.Repeat("a", 64))
	if err != nil {
		panic(err)
	}

	return token
}

type fakeSentryPorts struct {
	issueID domain.IssueID
}

func (ports fakeSentryPorts) Exists(
	ctx context.Context,
	projectID domain.ProjectID,
	eventID domain.EventID,
) result.Result[bool] {
	return result.Ok(false)
}

func (ports fakeSentryPorts) Append(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
) result.Result[ingest.EventAppendResult] {
	return result.Ok(ingest.NewAppendedEvent())
}

func (ports fakeSentryPorts) Apply(
	ctx context.Context,
	plan ingestplan.IssuePlan,
) result.Result[ingest.IssueChange] {
	return result.Ok(ingest.NewIssueChange(ports.issueID, true))
}

func (ports fakeSentryPorts) EnqueueIssueOpened(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
	change ingest.IssueChange,
) result.Result[ingest.IssueOpenedEnqueueResult] {
	return result.Ok(ingest.NewIssueOpenedEnqueueResult(0))
}

func mustDomainID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	value, err := constructor(input)
	if err != nil {
		t.Fatalf("domain id: %v", err)
	}

	return value
}

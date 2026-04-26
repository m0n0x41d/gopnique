package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

func TestIngestCanonicalEventAppendsEventAndAppliesIssuePlan(t *testing.T) {
	ports := &fakePorts{
		issueID:      issueID(t),
		issueCreated: true,
	}
	transaction := fakeTransaction{ports: ports}
	command := NewIngestCommand(issueEvent(t))

	receiptResult := IngestCanonicalEvent(context.Background(), command, transaction)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest receipt: %v", receiptErr)
	}

	if receipt.Kind() != ReceiptAcceptedIssueEvent {
		t.Fatalf("unexpected receipt kind: %s", receipt.Kind())
	}

	if !ports.appended {
		t.Fatal("expected event append")
	}

	if !ports.applied {
		t.Fatal("expected issue plan apply")
	}

	if !ports.enqueued {
		t.Fatal("expected issue opened enqueue")
	}

	if _, ok := receipt.IssueID(); !ok {
		t.Fatal("expected issue id")
	}
}

func TestIngestCanonicalEventDoesNotApplyIssuePlanForDuplicate(t *testing.T) {
	ports := &fakePorts{
		exists:  true,
		issueID: issueID(t),
	}
	transaction := fakeTransaction{ports: ports}
	command := NewIngestCommand(issueEvent(t))

	receiptResult := IngestCanonicalEvent(context.Background(), command, transaction)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest receipt: %v", receiptErr)
	}

	if receipt.Kind() != ReceiptDuplicateEvent {
		t.Fatalf("unexpected receipt kind: %s", receipt.Kind())
	}

	if ports.appended {
		t.Fatal("did not expect append after duplicate exists check")
	}

	if ports.applied {
		t.Fatal("did not expect issue plan apply")
	}

	if ports.enqueued {
		t.Fatal("did not expect issue opened enqueue")
	}
}

func TestIngestCanonicalEventRejectsQuotaBeforePersistence(t *testing.T) {
	ports := &fakePorts{
		quota:   NewQuotaRejected("project_quota_exceeded"),
		issueID: issueID(t),
	}
	transaction := fakeTransaction{ports: ports}
	command := NewIngestCommand(issueEvent(t))

	receiptResult := IngestCanonicalEvent(context.Background(), command, transaction)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest receipt: %v", receiptErr)
	}

	if receipt.Kind() != ReceiptQuotaRejected {
		t.Fatalf("unexpected receipt kind: %s", receipt.Kind())
	}

	if receipt.Reason() != "project_quota_exceeded" {
		t.Fatalf("unexpected quota reason: %s", receipt.Reason())
	}

	if ports.appended || ports.applied || ports.enqueued {
		t.Fatalf("quota rejection must not persist: %#v", ports)
	}
}

func TestIngestCanonicalEventKeepsTransactionOutOfIssueIndex(t *testing.T) {
	ports := &fakePorts{
		issueID:      issueID(t),
		issueCreated: true,
	}
	transaction := fakeTransaction{ports: ports}
	command := NewIngestCommand(transactionEvent(t))

	receiptResult := IngestCanonicalEvent(context.Background(), command, transaction)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest receipt: %v", receiptErr)
	}

	if receipt.Kind() != ReceiptAcceptedNonIssueEvent {
		t.Fatalf("unexpected receipt kind: %s", receipt.Kind())
	}

	if !ports.appended {
		t.Fatal("expected transaction event append")
	}

	if ports.applied {
		t.Fatal("did not expect issue plan apply")
	}

	if ports.enqueued {
		t.Fatal("did not expect issue opened enqueue")
	}
}

func TestIngestCanonicalEventDoesNotEnqueueForExistingIssue(t *testing.T) {
	ports := &fakePorts{
		issueID:      issueID(t),
		issueCreated: false,
	}
	transaction := fakeTransaction{ports: ports}
	command := NewIngestCommand(issueEvent(t))

	receiptResult := IngestCanonicalEvent(context.Background(), command, transaction)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest receipt: %v", receiptErr)
	}

	if receipt.Kind() != ReceiptAcceptedIssueEvent {
		t.Fatalf("unexpected receipt kind: %s", receipt.Kind())
	}

	if !ports.applied {
		t.Fatal("expected issue plan apply")
	}

	if ports.enqueued {
		t.Fatal("did not expect issue opened enqueue")
	}
}

func TestIngestCanonicalEventWithAppendEffectRunsBeforeIssuePlan(t *testing.T) {
	ports := &fakePorts{
		issueID:      issueID(t),
		issueCreated: true,
	}
	transaction := fakeTransaction{ports: ports}
	command := NewIngestCommand(issueEvent(t))
	effectCalls := 0
	effectSawAppend := false
	effectSawApply := false

	effect := func(ctx context.Context, event ingestplan.AcceptedEvent) result.Result[struct{}] {
		effectCalls++
		effectSawAppend = ports.appended
		effectSawApply = ports.applied
		return result.Ok(struct{}{})
	}

	receiptResult := IngestCanonicalEventWithAppendEffect(context.Background(), command, transaction, effect)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest receipt: %v", receiptErr)
	}

	if receipt.Kind() != ReceiptAcceptedIssueEvent {
		t.Fatalf("unexpected receipt kind: %s", receipt.Kind())
	}

	if effectCalls != 1 {
		t.Fatalf("expected one append effect call, got %d", effectCalls)
	}

	if !effectSawAppend {
		t.Fatal("expected append effect to run after event append")
	}

	if effectSawApply {
		t.Fatal("expected append effect to run before issue apply")
	}

	if !ports.applied {
		t.Fatal("expected issue plan apply after append effect")
	}
}

func TestIngestCanonicalEventWithAppendEffectSkipsDuplicateAndQuotaRejection(t *testing.T) {
	tests := []struct {
		name  string
		ports *fakePorts
	}{
		{
			name: "duplicate exists",
			ports: &fakePorts{
				exists:  true,
				issueID: issueID(t),
			},
		},
		{
			name: "append duplicate",
			ports: &fakePorts{
				appendDuplicate: true,
				issueID:         issueID(t),
			},
		},
		{
			name: "quota rejected",
			ports: &fakePorts{
				quota:   NewQuotaRejected("project_quota_exceeded"),
				issueID: issueID(t),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			transaction := fakeTransaction{ports: tc.ports}
			command := NewIngestCommand(issueEvent(t))
			effectCalls := 0

			effect := func(ctx context.Context, event ingestplan.AcceptedEvent) result.Result[struct{}] {
				effectCalls++
				return result.Ok(struct{}{})
			}

			receiptResult := IngestCanonicalEventWithAppendEffect(context.Background(), command, transaction, effect)
			_, receiptErr := receiptResult.Value()
			if receiptErr != nil {
				t.Fatalf("ingest receipt: %v", receiptErr)
			}

			if effectCalls != 0 {
				t.Fatalf("expected no append effect calls, got %d", effectCalls)
			}
		})
	}
}

func TestIngestCanonicalEventWithAppendEffectStopsOnEffectError(t *testing.T) {
	ports := &fakePorts{
		issueID:      issueID(t),
		issueCreated: true,
	}
	transaction := fakeTransaction{ports: ports}
	command := NewIngestCommand(issueEvent(t))
	expectedErr := errors.New("artifact upload failed")

	effect := func(ctx context.Context, event ingestplan.AcceptedEvent) result.Result[struct{}] {
		return result.Err[struct{}](expectedErr)
	}

	receiptResult := IngestCanonicalEventWithAppendEffect(context.Background(), command, transaction, effect)
	_, receiptErr := receiptResult.Value()
	if !errors.Is(receiptErr, expectedErr) {
		t.Fatalf("expected effect error, got %v", receiptErr)
	}

	if !ports.appended {
		t.Fatal("expected event append before effect")
	}

	if ports.applied || ports.enqueued {
		t.Fatalf("effect failure must stop downstream changes: %#v", ports)
	}
}

type fakeTransaction struct {
	ports *fakePorts
}

func (transaction fakeTransaction) Run(
	ctx context.Context,
	program IngestProgram,
) result.Result[IngestTransactionResult] {
	return program(ctx, transaction.ports)
}

type fakePorts struct {
	exists          bool
	appendDuplicate bool
	appended        bool
	applied         bool
	enqueued        bool
	issueID         domain.IssueID
	issueCreated    bool
	quota           QuotaDecision
}

func (ports *fakePorts) Exists(
	ctx context.Context,
	projectID domain.ProjectID,
	eventID domain.EventID,
) result.Result[bool] {
	return result.Ok(ports.exists)
}

func (ports *fakePorts) Append(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
) result.Result[EventAppendResult] {
	ports.appended = true

	if ports.appendDuplicate {
		return result.Ok(NewDuplicateEvent())
	}

	return result.Ok(NewAppendedEvent())
}

func (ports *fakePorts) CheckQuota(
	ctx context.Context,
	event domain.CanonicalEvent,
) result.Result[QuotaDecision] {
	if ports.quota.Reason() != "" {
		return result.Ok(ports.quota)
	}

	return result.Ok(NewQuotaAllowed())
}

func (ports *fakePorts) Apply(
	ctx context.Context,
	plan ingestplan.IssuePlan,
) result.Result[IssueChange] {
	ports.applied = true

	return result.Ok(NewIssueChange(ports.issueID, ports.issueCreated))
}

func (ports *fakePorts) EnqueueIssueOpened(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
	change IssueChange,
) result.Result[IssueOpenedEnqueueResult] {
	ports.enqueued = true

	return result.Ok(NewIssueOpenedEnqueueResult(1))
}

func issueEvent(t *testing.T) domain.CanonicalEvent {
	t.Helper()

	return canonicalEvent(t, domain.CanonicalEventParams{
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title(t, "TypeError: bad operand"),
		DefaultGroupingParts: []string{"TypeError", "handler.go", "42"},
	})
}

func transactionEvent(t *testing.T) domain.CanonicalEvent {
	t.Helper()

	return canonicalEvent(t, domain.CanonicalEventParams{
		Kind:                 domain.EventKindTransaction,
		Level:                domain.EventLevelInfo,
		Title:                title(t, "GET /checkout"),
		DefaultGroupingParts: []string{"GET /checkout"},
	})
}

func canonicalEvent(t *testing.T, params domain.CanonicalEventParams) domain.CanonicalEvent {
	t.Helper()

	organizationID := mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustID(t, domain.NewProjectID, "2222222222224222a222222222222222")
	eventID := mustID(t, domain.NewEventID, "550e8400e29b41d4a716446655440000")
	occurredAt := timePoint(t, time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
	receivedAt := timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC))

	params.OrganizationID = organizationID
	params.ProjectID = projectID
	params.EventID = eventID
	params.OccurredAt = occurredAt
	params.ReceivedAt = receivedAt

	event, err := domain.NewCanonicalEvent(params)
	if err != nil {
		t.Fatalf("canonical event: %v", err)
	}

	return event
}

func issueID(t *testing.T) domain.IssueID {
	t.Helper()

	id, err := domain.NewIssueID("3333333333334333a333333333333333")
	if err != nil {
		t.Fatalf("issue id: %v", err)
	}

	return id
}

func mustID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	id, err := constructor(input)
	if err != nil {
		t.Fatalf("id: %v", err)
	}

	return id
}

func timePoint(t *testing.T, value time.Time) domain.TimePoint {
	t.Helper()

	point, err := domain.NewTimePoint(value)
	if err != nil {
		t.Fatalf("time point: %v", err)
	}

	return point
}

func title(t *testing.T, input string) domain.EventTitle {
	t.Helper()

	value, err := domain.NewEventTitle(input)
	if err != nil {
		t.Fatalf("title: %v", err)
	}

	return value
}

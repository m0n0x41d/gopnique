package ingest

import (
	"context"
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/grouping"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

type IngestCommand struct {
	event domain.CanonicalEvent
}

type EventAppendResult struct {
	duplicate bool
}

type IssueChange struct {
	issueID domain.IssueID
	created bool
}

type IssueOpenedEnqueueResult struct {
	count int
}

type QuotaDecision struct {
	allowed bool
	reason  string
}

type ReceiptKind string

const (
	ReceiptAcceptedIssueEvent    ReceiptKind = "accepted_issue_event"
	ReceiptAcceptedNonIssueEvent ReceiptKind = "accepted_non_issue_event"
	ReceiptDuplicateEvent        ReceiptKind = "duplicate_event"
	ReceiptQuotaRejected         ReceiptKind = "quota_rejected"
)

type IngestReceipt struct {
	kind        ReceiptKind
	eventID     domain.EventID
	fingerprint domain.Fingerprint
	issueID     domain.IssueID
	hasIssueID  bool
	reason      string
}

type IngestTransactionResult struct {
	receipt IngestReceipt
}

func NewIngestCommand(event domain.CanonicalEvent) IngestCommand {
	return IngestCommand{event: event}
}

func NewAppendedEvent() EventAppendResult {
	return EventAppendResult{}
}

func NewDuplicateEvent() EventAppendResult {
	return EventAppendResult{duplicate: true}
}

func NewIssueChange(issueID domain.IssueID, created bool) IssueChange {
	return IssueChange{issueID: issueID, created: created}
}

func NewIssueOpenedEnqueueResult(count int) IssueOpenedEnqueueResult {
	return IssueOpenedEnqueueResult{count: count}
}

func NewQuotaAllowed() QuotaDecision {
	return QuotaDecision{allowed: true}
}

func NewQuotaRejected(reason string) QuotaDecision {
	if reason == "" {
		reason = "quota_exceeded"
	}

	return QuotaDecision{reason: reason}
}

func IngestCanonicalEvent(
	ctx context.Context,
	command IngestCommand,
	transaction IngestTransaction,
) result.Result[IngestReceipt] {
	fingerprintResult := grouping.ComputeFingerprint(command.event)
	fingerprint, fingerprintErr := fingerprintResult.Value()
	if fingerprintErr != nil {
		return result.Err[IngestReceipt](fingerprintErr)
	}

	acceptedResult := ingestplan.NewAcceptedEvent(command.event, fingerprint)
	accepted, acceptedErr := acceptedResult.Value()
	if acceptedErr != nil {
		return result.Err[IngestReceipt](acceptedErr)
	}

	transactionResult := transaction.Run(ctx, ingestProgram(accepted))
	completed, completedErr := transactionResult.Value()
	if completedErr != nil {
		return result.Err[IngestReceipt](completedErr)
	}

	return result.Ok(completed.receipt)
}

func ingestProgram(accepted ingestplan.AcceptedEvent) IngestProgram {
	return func(ctx context.Context, ports TransactionalIngestPorts) result.Result[IngestTransactionResult] {
		event := accepted.Event()

		existsResult := ports.Exists(ctx, event.ProjectID(), event.EventID())
		exists, existsErr := existsResult.Value()
		if existsErr != nil {
			return result.Err[IngestTransactionResult](existsErr)
		}

		if exists {
			return result.Ok(transactionResult(duplicateReceipt(accepted)))
		}

		quotaResult := ports.CheckQuota(ctx, event)
		quota, quotaErr := quotaResult.Value()
		if quotaErr != nil {
			return result.Err[IngestTransactionResult](quotaErr)
		}

		if !quota.Allowed() {
			return result.Ok(transactionResult(quotaReceipt(accepted, quota)))
		}

		appendResult := ports.Append(ctx, accepted)
		appendState, appendErr := appendResult.Value()
		if appendErr != nil {
			return result.Err[IngestTransactionResult](appendErr)
		}

		if appendState.WasDuplicate() {
			return result.Ok(transactionResult(duplicateReceipt(accepted)))
		}

		if !event.CreatesIssue() {
			return result.Ok(transactionResult(nonIssueReceipt(accepted)))
		}

		issuePlanResult := ingestplan.NewIssuePlan(accepted)
		issuePlan, issuePlanErr := issuePlanResult.Value()
		if issuePlanErr != nil {
			return result.Err[IngestTransactionResult](issuePlanErr)
		}

		issueChangeResult := ports.Apply(ctx, issuePlan)
		issueChange, issueChangeErr := issueChangeResult.Value()
		if issueChangeErr != nil {
			return result.Err[IngestTransactionResult](issueChangeErr)
		}

		if issueChange.issueID.String() == "" {
			return result.Err[IngestTransactionResult](errors.New("issue id is required"))
		}

		if issueChange.Created() {
			enqueueResult := ports.EnqueueIssueOpened(ctx, accepted, issueChange)
			_, enqueueErr := enqueueResult.Value()
			if enqueueErr != nil {
				return result.Err[IngestTransactionResult](enqueueErr)
			}
		}

		return result.Ok(transactionResult(issueReceipt(accepted, issueChange)))
	}
}

func transactionResult(receipt IngestReceipt) IngestTransactionResult {
	return IngestTransactionResult{receipt: receipt}
}

func duplicateReceipt(accepted ingestplan.AcceptedEvent) IngestReceipt {
	return IngestReceipt{
		kind:        ReceiptDuplicateEvent,
		eventID:     accepted.Event().EventID(),
		fingerprint: accepted.Fingerprint(),
	}
}

func quotaReceipt(accepted ingestplan.AcceptedEvent, quota QuotaDecision) IngestReceipt {
	return IngestReceipt{
		kind:        ReceiptQuotaRejected,
		eventID:     accepted.Event().EventID(),
		fingerprint: accepted.Fingerprint(),
		reason:      quota.Reason(),
	}
}

func nonIssueReceipt(accepted ingestplan.AcceptedEvent) IngestReceipt {
	return IngestReceipt{
		kind:        ReceiptAcceptedNonIssueEvent,
		eventID:     accepted.Event().EventID(),
		fingerprint: accepted.Fingerprint(),
	}
}

func issueReceipt(accepted ingestplan.AcceptedEvent, change IssueChange) IngestReceipt {
	return IngestReceipt{
		kind:        ReceiptAcceptedIssueEvent,
		eventID:     accepted.Event().EventID(),
		fingerprint: accepted.Fingerprint(),
		issueID:     change.issueID,
		hasIssueID:  true,
	}
}

func (command IngestCommand) Event() domain.CanonicalEvent {
	return command.event
}

func (result EventAppendResult) WasDuplicate() bool {
	return result.duplicate
}

func (change IssueChange) IssueID() domain.IssueID {
	return change.issueID
}

func (change IssueChange) Created() bool {
	return change.created
}

func (enqueue IssueOpenedEnqueueResult) Count() int {
	return enqueue.count
}

func (decision QuotaDecision) Allowed() bool {
	return decision.allowed
}

func (decision QuotaDecision) Reason() string {
	return decision.reason
}

func (receipt IngestReceipt) Kind() ReceiptKind {
	return receipt.kind
}

func (receipt IngestReceipt) EventID() domain.EventID {
	return receipt.eventID
}

func (receipt IngestReceipt) Fingerprint() domain.Fingerprint {
	return receipt.fingerprint
}

func (receipt IngestReceipt) IssueID() (domain.IssueID, bool) {
	return receipt.issueID, receipt.hasIssueID
}

func (receipt IngestReceipt) Reason() string {
	return receipt.reason
}

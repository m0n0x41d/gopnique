package ingest

import (
	"context"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

type EventJournal interface {
	Exists(ctx context.Context, projectID domain.ProjectID, eventID domain.EventID) result.Result[bool]
	Append(ctx context.Context, event ingestplan.AcceptedEvent) result.Result[EventAppendResult]
}

type ProjectDirectory interface {
	ResolveProjectKey(ctx context.Context, ref domain.ProjectRef, key domain.ProjectPublicKey) result.Result[domain.ProjectAuth]
}

type IssueIndex interface {
	Apply(ctx context.Context, plan ingestplan.IssuePlan) result.Result[IssueChange]
}

type IssueOpenedOutbox interface {
	EnqueueIssueOpened(
		ctx context.Context,
		event ingestplan.AcceptedEvent,
		change IssueChange,
	) result.Result[IssueOpenedEnqueueResult]
}

type QuotaGate interface {
	CheckQuota(
		ctx context.Context,
		event domain.CanonicalEvent,
	) result.Result[QuotaDecision]
}

type TransactionalIngestPorts interface {
	EventJournal
	QuotaGate
	IssueIndex
	IssueOpenedOutbox
}

type IngestProgram func(ctx context.Context, ports TransactionalIngestPorts) result.Result[IngestTransactionResult]

type IngestTransaction interface {
	Run(ctx context.Context, program IngestProgram) result.Result[IngestTransactionResult]
}

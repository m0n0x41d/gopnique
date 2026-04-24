package audit

import (
	"context"
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Reader interface {
	ListAuditEvents(ctx context.Context, query Query) result.Result[View]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Query struct {
	Scope Scope
	Limit int
}

type Action string

const (
	ActionBootstrap           Action = "bootstrap"
	ActionAPITokenCreated     Action = "api_token_created"
	ActionAPITokenRevoked     Action = "api_token_revoked"
	ActionIssueAssigned       Action = "issue_assigned"
	ActionIssueCommentCreated Action = "issue_comment_created"
	ActionIssueStatusChanged  Action = "issue_status_changed"
)

type View struct {
	Events []EventView
}

type EventView struct {
	ID        string
	Action    string
	Actor     string
	Target    string
	Metadata  string
	CreatedAt string
}

func List(ctx context.Context, reader Reader, query Query) result.Result[View] {
	if reader == nil {
		return result.Err[View](errors.New("audit reader is required"))
	}

	if query.Scope.OrganizationID.String() == "" || query.Scope.ProjectID.String() == "" {
		return result.Err[View](errors.New("audit scope is required"))
	}

	if query.Limit <= 0 {
		query.Limit = 50
	}

	return reader.ListAuditEvents(ctx, query)
}

func (action Action) Valid() bool {
	return action == ActionBootstrap ||
		action == ActionAPITokenCreated ||
		action == ActionAPITokenRevoked ||
		action == ActionIssueAssigned ||
		action == ActionIssueCommentCreated ||
		action == ActionIssueStatusChanged
}

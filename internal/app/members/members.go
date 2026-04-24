package members

import (
	"context"
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Reader interface {
	ShowMembers(ctx context.Context, query Query) result.Result[View]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Query struct {
	Scope Scope
}

type View struct {
	Operators []OperatorView
	Teams     []TeamView
}

type OperatorView struct {
	ID          string
	Email       string
	DisplayName string
	OrgRole     string
	ProjectRole string
	Status      string
}

type TeamView struct {
	ID          string
	Name        string
	Slug        string
	MemberCount int
	MemberRole  string
	ProjectRole string
}

func Show(
	ctx context.Context,
	reader Reader,
	query Query,
) result.Result[View] {
	if reader == nil {
		return result.Err[View](errors.New("member reader is required"))
	}

	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[View](scopeErr)
	}

	return reader.ShowMembers(ctx, query)
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("member scope is required")
	}

	return nil
}

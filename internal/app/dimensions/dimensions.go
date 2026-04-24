package dimensions

import (
	"context"
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Reader interface {
	ListEnvironments(ctx context.Context, query Query) result.Result[View]
	ListReleases(ctx context.Context, query Query) result.Result[View]
}

type Kind string

const (
	KindEnvironment Kind = "environment"
	KindRelease     Kind = "release"
)

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Query struct {
	Scope Scope
	Limit int
}

type View struct {
	Kind  Kind
	Items []ItemView
}

type ItemView struct {
	Name            string
	IssueCount      int
	UnresolvedCount int
	ResolvedCount   int
	IgnoredCount    int
	LastSeen        string
}

func ListEnvironments(
	ctx context.Context,
	reader Reader,
	query Query,
) result.Result[View] {
	if reader == nil {
		return result.Err[View](errors.New("dimension reader is required"))
	}

	queryResult := normalizeQuery(query)
	normalizedQuery, queryErr := queryResult.Value()
	if queryErr != nil {
		return result.Err[View](queryErr)
	}

	return reader.ListEnvironments(ctx, normalizedQuery)
}

func ListReleases(
	ctx context.Context,
	reader Reader,
	query Query,
) result.Result[View] {
	if reader == nil {
		return result.Err[View](errors.New("dimension reader is required"))
	}

	queryResult := normalizeQuery(query)
	normalizedQuery, queryErr := queryResult.Value()
	if queryErr != nil {
		return result.Err[View](queryErr)
	}

	return reader.ListReleases(ctx, normalizedQuery)
}

func normalizeQuery(query Query) result.Result[Query] {
	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[Query](scopeErr)
	}

	if query.Limit < 1 {
		return result.Err[Query](errors.New("dimension limit must be positive"))
	}

	if query.Limit > 250 {
		return result.Err[Query](errors.New("dimension limit must be at most 250"))
	}

	return result.Ok(query)
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("dimension scope is required")
	}

	return nil
}

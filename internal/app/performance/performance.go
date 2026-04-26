package performance

import (
	"context"
	"errors"
	"regexp"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const defaultLimit = 50
const maxLimit = 100

var transactionGroupPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Reader interface {
	ListTransactionGroups(ctx context.Context, query Query) result.Result[ListView]
	ShowTransactionGroup(ctx context.Context, query DetailQuery) result.Result[DetailView]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Query struct {
	Scope Scope
	Limit int
}

type DetailQuery struct {
	Scope       Scope
	GroupID     string
	RecentLimit int
}

type ListView struct {
	Groups []GroupSummaryView
}

type GroupSummaryView struct {
	ID              string
	Name            string
	Operation       string
	Count           int
	AverageDuration string
	P95Duration     string
	LatestStatus    string
	LatestSeen      string
}

type DetailView struct {
	ID              string
	Name            string
	Operation       string
	Count           int
	AverageDuration string
	P95Duration     string
	LatestStatus    string
	LatestSeen      string
	RecentEvents    []EventView
}

type EventView struct {
	EventID    string
	Duration   string
	Status     string
	TraceID    string
	SpanID     string
	SpanCount  int
	ReceivedAt string
}

func List(
	ctx context.Context,
	reader Reader,
	query Query,
) result.Result[ListView] {
	if reader == nil {
		return result.Err[ListView](errors.New("performance reader is required"))
	}

	normalized, normalizeErr := normalizeQuery(query)
	if normalizeErr != nil {
		return result.Err[ListView](normalizeErr)
	}

	return reader.ListTransactionGroups(ctx, normalized)
}

func Detail(
	ctx context.Context,
	reader Reader,
	query DetailQuery,
) result.Result[DetailView] {
	if reader == nil {
		return result.Err[DetailView](errors.New("performance reader is required"))
	}

	normalized, normalizeErr := normalizeDetailQuery(query)
	if normalizeErr != nil {
		return result.Err[DetailView](normalizeErr)
	}

	return reader.ShowTransactionGroup(ctx, normalized)
}

func normalizeQuery(query Query) (Query, error) {
	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return Query{}, scopeErr
	}

	query.Limit = normalizeLimit(query.Limit)
	return query, nil
}

func normalizeDetailQuery(query DetailQuery) (DetailQuery, error) {
	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return DetailQuery{}, scopeErr
	}

	if !transactionGroupPattern.MatchString(query.GroupID) {
		return DetailQuery{}, errors.New("transaction group id is invalid")
	}

	query.RecentLimit = normalizeLimit(query.RecentLimit)
	return query, nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}

	if limit > maxLimit {
		return maxLimit
	}

	return limit
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("performance scope is required")
	}

	return nil
}

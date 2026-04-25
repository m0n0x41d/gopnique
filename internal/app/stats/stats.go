package stats

import (
	"context"
	"errors"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Reader interface {
	ShowProjectStats(ctx context.Context, query Query) result.Result[View]
}

type Period string

const (
	Period24h Period = "24h"
	Period14d Period = "14d"
)

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Query struct {
	Scope  Scope
	Period Period
	Now    time.Time
}

type View struct {
	Period            string
	Granularity       string
	TotalEvents       int
	IssueEvents       int
	TransactionEvents int
	UnresolvedIssues  int
	ResolvedIssues    int
	IgnoredIssues     int
	UserReports       int
	MaxBucketEvents   int
	Buckets           []BucketView
}

type BucketView struct {
	Start             string
	Label             string
	EventCount        int
	IssueEvents       int
	TransactionEvents int
}

func ShowProjectStats(
	ctx context.Context,
	reader Reader,
	query Query,
) result.Result[View] {
	if reader == nil {
		return result.Err[View](errors.New("stats reader is required"))
	}

	queryResult := NormalizeQuery(query)
	normalizedQuery, queryErr := queryResult.Value()
	if queryErr != nil {
		return result.Err[View](queryErr)
	}

	return reader.ShowProjectStats(ctx, normalizedQuery)
}

func NormalizeQuery(query Query) result.Result[Query] {
	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[Query](scopeErr)
	}

	if !query.Period.Valid() {
		return result.Err[Query](errors.New("stats period is invalid"))
	}

	if query.Now.IsZero() {
		return result.Err[Query](errors.New("stats reference time is required"))
	}

	query.Now = query.Now.UTC()

	return result.Ok(query)
}

func ParsePeriod(input string) Period {
	value := Period(input)
	if value.Valid() {
		return value
	}

	return Period24h
}

func (period Period) Valid() bool {
	return period == Period24h || period == Period14d
}

func (period Period) Granularity() string {
	if period == Period14d {
		return "day"
	}

	return "hour"
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("stats scope is required")
	}

	return nil
}

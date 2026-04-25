package stats

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestNormalizeQueryRequiresValidPeriod(t *testing.T) {
	query := validQuery()
	query.Period = Period("year")

	result := NormalizeQuery(query)
	_, err := result.Value()
	if err == nil {
		t.Fatal("expected invalid period")
	}
}

func TestShowProjectStatsCallsReaderWithUTCReferenceTime(t *testing.T) {
	reader := &fakeReader{}
	query := validQuery()
	query.Now = time.Date(2026, 4, 25, 13, 30, 0, 0, time.FixedZone("AMT", 4*60*60))

	viewResult := ShowProjectStats(context.Background(), reader, query)
	_, viewErr := viewResult.Value()
	if viewErr != nil {
		t.Fatalf("show stats: %v", viewErr)
	}

	if reader.query.Now.Location() != time.UTC {
		t.Fatalf("expected UTC reference time, got %s", reader.query.Now.Location())
	}

	if reader.query.Period.Granularity() != "hour" {
		t.Fatalf("unexpected granularity: %s", reader.query.Period.Granularity())
	}
}

func validQuery() Query {
	return Query{
		Scope: Scope{
			OrganizationID: mustID(domain.NewOrganizationID, "1111111111114111a111111111111111"),
			ProjectID:      mustID(domain.NewProjectID, "2222222222224222a222222222222222"),
		},
		Period: Period24h,
		Now:    time.Date(2026, 4, 25, 9, 30, 0, 0, time.UTC),
	}
}

type fakeReader struct {
	query Query
}

func (reader *fakeReader) ShowProjectStats(
	_ context.Context,
	query Query,
) result.Result[View] {
	reader.query = query
	return result.Ok(View{Period: string(query.Period)})
}

func mustID[T any](build func(string) (T, error), input string) T {
	value, err := build(input)
	if err != nil {
		panic(err)
	}

	return value
}

func (_ *fakeReader) unused() result.Result[View] {
	return result.Err[View](errors.New("unused"))
}

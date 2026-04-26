package performance

import (
	"context"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestListNormalizesLimitAndRequiresScope(t *testing.T) {
	reader := &fakeReader{}
	query := Query{
		Scope: Scope{
			OrganizationID: mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111"),
			ProjectID:      mustID(t, domain.NewProjectID, "2222222222224222a222222222222222"),
		},
		Limit: 0,
	}

	viewResult := List(context.Background(), reader, query)
	_, viewErr := viewResult.Value()
	if viewErr != nil {
		t.Fatalf("list: %v", viewErr)
	}

	if reader.listQuery.Limit != defaultLimit {
		t.Fatalf("expected default limit, got %d", reader.listQuery.Limit)
	}

	missingScopeResult := List(context.Background(), reader, Query{})
	_, missingScopeErr := missingScopeResult.Value()
	if missingScopeErr == nil {
		t.Fatal("expected missing scope to fail")
	}
}

func TestDetailValidatesTransactionGroupID(t *testing.T) {
	reader := &fakeReader{}
	scope := Scope{
		OrganizationID: mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111"),
		ProjectID:      mustID(t, domain.NewProjectID, "2222222222224222a222222222222222"),
	}

	invalidResult := Detail(context.Background(), reader, DetailQuery{
		Scope:   scope,
		GroupID: "bad",
	})
	_, invalidErr := invalidResult.Value()
	if invalidErr == nil {
		t.Fatal("expected invalid group id to fail")
	}

	validResult := Detail(context.Background(), reader, DetailQuery{
		Scope:       scope,
		GroupID:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RecentLimit: 200,
	})
	_, validErr := validResult.Value()
	if validErr != nil {
		t.Fatalf("detail: %v", validErr)
	}

	if reader.detailQuery.RecentLimit != maxLimit {
		t.Fatalf("expected max limit, got %d", reader.detailQuery.RecentLimit)
	}
}

type fakeReader struct {
	listQuery   Query
	detailQuery DetailQuery
}

func (reader *fakeReader) ListTransactionGroups(
	ctx context.Context,
	query Query,
) result.Result[ListView] {
	reader.listQuery = query
	return result.Ok(ListView{})
}

func (reader *fakeReader) ShowTransactionGroup(
	ctx context.Context,
	query DetailQuery,
) result.Result[DetailView] {
	reader.detailQuery = query
	return result.Ok(DetailView{})
}

func mustID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	value, err := constructor(input)
	if err != nil {
		t.Fatalf("id: %v", err)
	}

	return value
}

package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestPlanProjectPurgeBuildsCutoffsFromPolicy(t *testing.T) {
	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.FixedZone("AMT", 4*60*60))
	planResult := PlanProjectPurge(
		validPolicy(),
		Command{Now: now, BatchSize: 25},
	)
	plan, planErr := planResult.Value()
	if planErr != nil {
		t.Fatalf("plan project purge: %v", planErr)
	}

	if plan.EventCutoff != time.Date(2026, 1, 25, 8, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected event cutoff: %s", plan.EventCutoff)
	}

	if plan.PayloadCutoff != time.Date(2026, 3, 26, 8, 0, 0, 0, time.UTC) {
		t.Fatalf("unexpected payload cutoff: %s", plan.PayloadCutoff)
	}

	if plan.BatchSize != 25 {
		t.Fatalf("unexpected batch size: %d", plan.BatchSize)
	}
}

func TestRunSkipsDisabledPolicies(t *testing.T) {
	store := &fakeStore{
		policies: []Policy{
			validPolicy(),
			disabledPolicy(),
		},
	}

	summaryResult := Run(
		context.Background(),
		store,
		Command{
			Now:       time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC),
			BatchSize: 10,
		},
	)
	summary, summaryErr := summaryResult.Value()
	if summaryErr != nil {
		t.Fatalf("run retention: %v", summaryErr)
	}

	if summary.ProjectsProcessed != 1 || store.purges != 1 {
		t.Fatalf("unexpected summary: %#v purges=%d", summary, store.purges)
	}
}

func TestNormalizePolicyRejectsMissingScope(t *testing.T) {
	policy := validPolicy()
	policy.Scope = Scope{}

	policyResult := NormalizePolicy(policy)
	_, policyErr := policyResult.Value()
	if policyErr == nil {
		t.Fatal("expected missing scope to fail")
	}
}

func validPolicy() Policy {
	return Policy{
		Scope: Scope{
			OrganizationID: mustID(domain.NewOrganizationID, "1111111111114111a111111111111111"),
			ProjectID:      mustID(domain.NewProjectID, "2222222222224222a222222222222222"),
		},
		EventRetentionDays:      90,
		PayloadRetentionDays:    30,
		DeliveryRetentionDays:   30,
		UserReportRetentionDays: 90,
		Enabled:                 true,
	}
}

func disabledPolicy() Policy {
	policy := validPolicy()
	policy.Enabled = false

	return policy
}

type fakeStore struct {
	policies []Policy
	purges   int
}

func (store *fakeStore) ListRetentionPolicies(
	_ context.Context,
) result.Result[[]Policy] {
	return result.Ok(store.policies)
}

func (store *fakeStore) PurgeExpiredProjectData(
	_ context.Context,
	_ ProjectPurgePlan,
) result.Result[PurgeResult] {
	store.purges++
	return result.Ok(PurgeResult{EventsDeleted: 1})
}

func mustID[T any](build func(string) (T, error), input string) T {
	value, err := build(input)
	if err != nil {
		panic(err)
	}

	return value
}

func (_ *fakeStore) unused() result.Result[PurgeResult] {
	return result.Err[PurgeResult](errors.New("unused"))
}

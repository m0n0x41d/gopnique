package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestCheckNormalizesReferenceTime(t *testing.T) {
	checker := &fakeChecker{}
	command := validCommand()
	command.Now = time.Date(2026, 4, 25, 13, 30, 0, 0, time.FixedZone("AMT", 4*60*60))

	decisionResult := Check(context.Background(), checker, command)
	_, decisionErr := decisionResult.Value()
	if decisionErr != nil {
		t.Fatalf("check rate limit: %v", decisionErr)
	}

	if checker.command.Now.Location() != time.UTC {
		t.Fatalf("expected UTC reference time, got %s", checker.command.Now.Location())
	}
}

func TestNormalizeCommandRequiresPublicKey(t *testing.T) {
	command := validCommand()
	command.PublicKey = domain.ProjectPublicKey{}

	commandResult := NormalizeCommand(command)
	_, commandErr := commandResult.Value()
	if commandErr == nil {
		t.Fatal("expected missing public key to fail")
	}
}

func TestRejectedDecisionHasRetryAfterFloor(t *testing.T) {
	decision := NewRejected("", 0)

	if decision.Allowed() {
		t.Fatal("expected rejected decision")
	}

	if decision.RetryAfter() != time.Second {
		t.Fatalf("unexpected retry after: %s", decision.RetryAfter())
	}

	if decision.Reason() != "rate_limited" {
		t.Fatalf("unexpected reason: %s", decision.Reason())
	}
}

func validCommand() Command {
	return Command{
		Scope: Scope{
			OrganizationID: mustID(domain.NewOrganizationID, "1111111111114111a111111111111111"),
			ProjectID:      mustID(domain.NewProjectID, "2222222222224222a222222222222222"),
		},
		PublicKey: mustID(domain.NewProjectPublicKey, "550e8400e29b41d4a716446655440000"),
		Now:       time.Date(2026, 4, 25, 9, 30, 0, 0, time.UTC),
	}
}

type fakeChecker struct {
	command Command
}

func (checker *fakeChecker) CheckRateLimit(
	_ context.Context,
	command Command,
) result.Result[Decision] {
	checker.command = command
	return result.Ok(NewAllowed())
}

func mustID[T any](build func(string) (T, error), input string) T {
	value, err := build(input)
	if err != nil {
		panic(err)
	}

	return value
}

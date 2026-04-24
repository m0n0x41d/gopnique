package userreports

import (
	"context"
	"errors"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestSubmitNormalizesUserReport(t *testing.T) {
	writer := &fakeWriter{}

	command := SubmitCommand{
		Scope: Scope{
			OrganizationID: mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111"),
			ProjectID:      mustID(t, domain.NewProjectID, "2222222222224222a222222222222222"),
		},
		EventID:  mustID(t, domain.NewEventID, "980e8400e29b41d4a716446655440000"),
		Name:     " Jane ",
		Email:    " Jane <jane@example.test> ",
		Comments: " It broke ",
	}

	receiptResult := Submit(context.Background(), writer, command)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("submit: %v", receiptErr)
	}

	if receipt.EventID != "980e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("unexpected receipt event id: %s", receipt.EventID)
	}

	if writer.command.Name != "Jane" ||
		writer.command.Email != "jane@example.test" ||
		writer.command.Comments != "It broke" {
		t.Fatalf("command was not normalized: %+v", writer.command)
	}
}

func TestSubmitRejectsInvalidUserReport(t *testing.T) {
	writer := &fakeWriter{}

	command := SubmitCommand{
		Scope: Scope{
			OrganizationID: mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111"),
			ProjectID:      mustID(t, domain.NewProjectID, "2222222222224222a222222222222222"),
		},
		EventID:  mustID(t, domain.NewEventID, "980e8400e29b41d4a716446655440000"),
		Name:     "Jane",
		Email:    "not-email",
		Comments: "It broke",
	}

	receiptResult := Submit(context.Background(), writer, command)
	_, receiptErr := receiptResult.Value()
	if receiptErr == nil {
		t.Fatal("expected invalid email error")
	}
}

type fakeWriter struct {
	command SubmitCommand
}

func (writer *fakeWriter) SubmitUserReport(
	_ context.Context,
	command SubmitCommand,
) result.Result[SubmitReceipt] {
	writer.command = command
	return result.Ok(SubmitReceipt{
		ReportID: "33333333-3333-4333-a333-333333333333",
		EventID:  command.EventID.String(),
	})
}

func (writer *fakeWriter) ListIssueUserReports(
	context.Context,
	IssueReportsQuery,
) result.Result[IssueReportsView] {
	return result.Err[IssueReportsView](errors.New("not implemented"))
}

func mustID[T any](t *testing.T, build func(string) (T, error), input string) T {
	t.Helper()

	value, err := build(input)
	if err != nil {
		t.Fatalf("id: %v", err)
	}

	return value
}

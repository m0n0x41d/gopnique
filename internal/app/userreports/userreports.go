package userreports

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const (
	maxReporterNameLength  = 128
	maxReporterEmailLength = 254
	maxCommentsLength      = 20_000
)

type Writer interface {
	SubmitUserReport(ctx context.Context, command SubmitCommand) result.Result[SubmitReceipt]
}

type Reader interface {
	ListIssueUserReports(ctx context.Context, query IssueReportsQuery) result.Result[IssueReportsView]
}

type Manager interface {
	Writer
	Reader
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type SubmitCommand struct {
	Scope    Scope
	EventID  domain.EventID
	Name     string
	Email    string
	Comments string
}

type SubmitReceipt struct {
	ReportID string
	EventID  string
}

type IssueReportsQuery struct {
	Scope   Scope
	IssueID domain.IssueID
	Limit   int
}

type IssueReportsView struct {
	Items []IssueReportView
}

type IssueReportView struct {
	ID        string
	EventID   string
	Name      string
	Email     string
	Comments  string
	CreatedAt string
}

func Submit(
	ctx context.Context,
	writer Writer,
	command SubmitCommand,
) result.Result[SubmitReceipt] {
	if writer == nil {
		return result.Err[SubmitReceipt](errors.New("user report writer is required"))
	}

	commandResult := normalizeSubmitCommand(command)
	normalizedCommand, commandErr := commandResult.Value()
	if commandErr != nil {
		return result.Err[SubmitReceipt](commandErr)
	}

	return writer.SubmitUserReport(ctx, normalizedCommand)
}

func PrepareSubmit(command SubmitCommand) result.Result[SubmitCommand] {
	return normalizeSubmitCommand(command)
}

func ListForIssue(
	ctx context.Context,
	reader Reader,
	query IssueReportsQuery,
) result.Result[IssueReportsView] {
	if reader == nil {
		return result.Err[IssueReportsView](errors.New("user report reader is required"))
	}

	queryResult := normalizeIssueReportsQuery(query)
	normalizedQuery, queryErr := queryResult.Value()
	if queryErr != nil {
		return result.Err[IssueReportsView](queryErr)
	}

	return reader.ListIssueUserReports(ctx, normalizedQuery)
}

func normalizeSubmitCommand(command SubmitCommand) result.Result[SubmitCommand] {
	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[SubmitCommand](scopeErr)
	}

	if command.EventID.String() == "" {
		return result.Err[SubmitCommand](errors.New("user report event id is required"))
	}

	name, nameErr := normalizeReporterName(command.Name)
	if nameErr != nil {
		return result.Err[SubmitCommand](nameErr)
	}

	email, emailErr := normalizeReporterEmail(command.Email)
	if emailErr != nil {
		return result.Err[SubmitCommand](emailErr)
	}

	comments, commentsErr := normalizeComments(command.Comments)
	if commentsErr != nil {
		return result.Err[SubmitCommand](commentsErr)
	}

	command.Name = name
	command.Email = email
	command.Comments = comments

	return result.Ok(command)
}

func normalizeIssueReportsQuery(query IssueReportsQuery) result.Result[IssueReportsQuery] {
	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[IssueReportsQuery](scopeErr)
	}

	if query.IssueID.String() == "" {
		return result.Err[IssueReportsQuery](errors.New("issue id is required"))
	}

	if query.Limit < 1 {
		return result.Err[IssueReportsQuery](errors.New("user report limit must be positive"))
	}

	if query.Limit > 250 {
		return result.Err[IssueReportsQuery](errors.New("user report limit must be at most 250"))
	}

	return result.Ok(query)
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("user report scope is required")
	}

	return nil
}

func normalizeReporterName(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("user report name is required")
	}

	if len(value) > maxReporterNameLength {
		return "", errors.New("user report name is too long")
	}

	return value, nil
}

func normalizeReporterEmail(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("user report email is required")
	}

	if len(value) > maxReporterEmailLength {
		return "", errors.New("user report email is too long")
	}

	address, parseErr := mail.ParseAddress(value)
	if parseErr != nil {
		return "", errors.New("user report email is invalid")
	}

	return address.Address, nil
}

func normalizeComments(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("user report comments are required")
	}

	if len(value) > maxCommentsLength {
		return "", errors.New("user report comments are too long")
	}

	return value, nil
}

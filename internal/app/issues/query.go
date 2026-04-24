package issues

import (
	"context"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Reader interface {
	ListIssues(ctx context.Context, query IssueListQuery) result.Result[IssueListView]
	ShowIssue(ctx context.Context, query IssueDetailQuery) result.Result[IssueDetailView]
	ShowEvent(ctx context.Context, query EventDetailQuery) result.Result[EventDetailView]
}

type Manager interface {
	Reader
	TransitionIssueStatus(ctx context.Context, command StatusTransitionCommand) result.Result[StatusTransitionResult]
	AddIssueComment(ctx context.Context, command AddCommentCommand) result.Result[CommentMutationResult]
	AssignIssue(ctx context.Context, command AssignIssueCommand) result.Result[AssignmentMutationResult]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type IssueListQuery struct {
	Scope  Scope
	Limit  int
	Status IssueStatus
	Search IssueSearchFilter
}

type IssueDetailQuery struct {
	Scope   Scope
	IssueID domain.IssueID
}

type EventDetailQuery struct {
	Scope   Scope
	EventID domain.EventID
}

type IssueStatus string

const (
	IssueStatusUnresolved IssueStatus = "unresolved"
	IssueStatusResolved   IssueStatus = "resolved"
	IssueStatusIgnored    IssueStatus = "ignored"
)

type IssueSearchFilter struct {
	Status         IssueStatus
	Text           string
	Environment    string
	Release        string
	Level          string
	TagKey         string
	TagValue       string
	Assignee       AssignmentTarget
	LastSeenAfter  *time.Time
	LastSeenBefore *time.Time
}

type StatusTransitionCommand struct {
	Scope        Scope
	IssueID      domain.IssueID
	ActorID      string
	TargetStatus IssueStatus
	Reason       string
}

type StatusTransitionResult struct {
	IssueID string
	Status  IssueStatus
}

type AddCommentCommand struct {
	Scope   Scope
	IssueID domain.IssueID
	ActorID string
	Body    string
}

type CommentMutationResult struct {
	CommentID string
}

type AssignmentTargetKind string

const (
	AssignmentTargetNone     AssignmentTargetKind = "none"
	AssignmentTargetOperator AssignmentTargetKind = "operator"
	AssignmentTargetTeam     AssignmentTargetKind = "team"
)

type AssignmentTarget struct {
	Kind AssignmentTargetKind
	ID   string
}

type AssignIssueCommand struct {
	Scope   Scope
	IssueID domain.IssueID
	ActorID string
	Target  AssignmentTarget
}

type AssignmentMutationResult struct {
	IssueID string
	Target  AssignmentTarget
}

type IssueListView struct {
	Status string
	Search string
	Items  []IssueSummaryView
}

type IssueSummaryView struct {
	ID            string
	ShortID       int64
	Title         string
	Type          string
	Status        string
	EventCount    int64
	LatestEventID string
	Level         string
	Platform      string
	LastSeen      string
	Environment   string
	Release       string
	Assignee      string
}

type IssueDetailView struct {
	ID             string
	ShortID        int64
	Title          string
	Type           string
	Status         string
	EventCount     int64
	FirstSeen      string
	LastSeen       string
	LatestEventID  string
	LatestLevel    string
	LatestPlatform string
	Fingerprint    string
	Environment    string
	Release        string
	Assignee       string
	Tags           []TagView
	Comments       []CommentView
	Assignees      []AssigneeOptionView
}

type EventDetailView struct {
	ID          string
	EventID     string
	IssueID     string
	Title       string
	Kind        string
	Level       string
	Platform    string
	OccurredAt  string
	ReceivedAt  string
	Fingerprint string
	Environment string
	Release     string
	Tags        []TagView
	PayloadJSON string
}

type TagView struct {
	Key   string
	Value string
}

type CommentView struct {
	ID        string
	Actor     string
	Body      string
	CreatedAt string
}

type AssigneeOptionView struct {
	Value string
	Label string
	Kind  string
}

func List(ctx context.Context, reader Reader, query IssueListQuery) result.Result[IssueListView] {
	if query.Scope.OrganizationID.String() == "" || query.Scope.ProjectID.String() == "" {
		return result.Err[IssueListView](errScopeRequired())
	}

	if query.Limit <= 0 {
		query.Limit = 50
	}

	if query.Status == "" {
		query.Status = IssueStatusUnresolved
	}

	if query.Search.Status != "" {
		query.Status = query.Search.Status
	}

	if !query.Status.Valid() {
		return result.Err[IssueListView](errInvalidStatus())
	}

	query.Search.Status = query.Status

	return reader.ListIssues(ctx, query)
}

func Detail(ctx context.Context, reader Reader, query IssueDetailQuery) result.Result[IssueDetailView] {
	if query.Scope.OrganizationID.String() == "" || query.Scope.ProjectID.String() == "" {
		return result.Err[IssueDetailView](errScopeRequired())
	}

	return reader.ShowIssue(ctx, query)
}

func Event(ctx context.Context, reader Reader, query EventDetailQuery) result.Result[EventDetailView] {
	if query.Scope.OrganizationID.String() == "" || query.Scope.ProjectID.String() == "" {
		return result.Err[EventDetailView](errScopeRequired())
	}

	return reader.ShowEvent(ctx, query)
}

func TransitionStatus(
	ctx context.Context,
	manager Manager,
	command StatusTransitionCommand,
) result.Result[StatusTransitionResult] {
	if manager == nil {
		return result.Err[StatusTransitionResult](errManagerRequired())
	}

	if command.Scope.OrganizationID.String() == "" || command.Scope.ProjectID.String() == "" {
		return result.Err[StatusTransitionResult](errScopeRequired())
	}

	if command.IssueID.String() == "" {
		return result.Err[StatusTransitionResult](errIssueRequired())
	}

	if command.ActorID == "" {
		return result.Err[StatusTransitionResult](errActorRequired())
	}

	if !command.TargetStatus.Valid() {
		return result.Err[StatusTransitionResult](errInvalidStatus())
	}

	return manager.TransitionIssueStatus(ctx, command)
}

func AddComment(
	ctx context.Context,
	manager Manager,
	command AddCommentCommand,
) result.Result[CommentMutationResult] {
	if manager == nil {
		return result.Err[CommentMutationResult](errManagerRequired())
	}

	if command.Scope.OrganizationID.String() == "" || command.Scope.ProjectID.String() == "" {
		return result.Err[CommentMutationResult](errScopeRequired())
	}

	if command.IssueID.String() == "" {
		return result.Err[CommentMutationResult](errIssueRequired())
	}

	if command.ActorID == "" {
		return result.Err[CommentMutationResult](errActorRequired())
	}

	bodyErr := requireCommentBody(command.Body)
	if bodyErr != nil {
		return result.Err[CommentMutationResult](bodyErr)
	}

	return manager.AddIssueComment(ctx, command)
}

func Assign(
	ctx context.Context,
	manager Manager,
	command AssignIssueCommand,
) result.Result[AssignmentMutationResult] {
	if manager == nil {
		return result.Err[AssignmentMutationResult](errManagerRequired())
	}

	if command.Scope.OrganizationID.String() == "" || command.Scope.ProjectID.String() == "" {
		return result.Err[AssignmentMutationResult](errScopeRequired())
	}

	if command.IssueID.String() == "" {
		return result.Err[AssignmentMutationResult](errIssueRequired())
	}

	if command.ActorID == "" {
		return result.Err[AssignmentMutationResult](errActorRequired())
	}

	if !command.Target.Valid() {
		return result.Err[AssignmentMutationResult](errAssignmentTargetInvalid())
	}

	return manager.AssignIssue(ctx, command)
}

func ParseIssueStatus(input string) (IssueStatus, error) {
	status := IssueStatus(input)
	if !status.Valid() {
		return "", errInvalidStatus()
	}

	return status, nil
}

func ParseIssueSearch(input string) (IssueSearchFilter, error) {
	tokens := strings.Fields(strings.TrimSpace(input))
	filter := IssueSearchFilter{}
	text := []string{}

	for _, token := range tokens {
		nextFilter, textToken, tokenErr := parseIssueSearchToken(filter, token)
		if tokenErr != nil {
			return IssueSearchFilter{}, tokenErr
		}

		filter = nextFilter
		if textToken != "" {
			text = append(text, textToken)
		}
	}

	filter.Text = strings.Join(text, " ")
	return filter, nil
}

func parseIssueSearchToken(
	filter IssueSearchFilter,
	token string,
) (IssueSearchFilter, string, error) {
	key, value, ok := strings.Cut(token, ":")
	if !ok {
		return filter, token, nil
	}

	if value == "" {
		return IssueSearchFilter{}, "", errInvalidSearch()
	}

	switch key {
	case "is", "status":
		status, statusErr := ParseIssueStatus(value)
		if statusErr != nil {
			return IssueSearchFilter{}, "", statusErr
		}

		filter.Status = status
		return filter, "", nil
	case "environment", "env":
		filter.Environment = value
		return filter, "", nil
	case "release":
		filter.Release = value
		return filter, "", nil
	case "level":
		filter.Level = value
		return filter, "", nil
	case "tag":
		tagKey, tagValue, tagOK := strings.Cut(value, "=")
		if !tagOK || tagKey == "" || tagValue == "" {
			return IssueSearchFilter{}, "", errInvalidSearch()
		}

		filter.TagKey = tagKey
		filter.TagValue = tagValue
		return filter, "", nil
	case "assignee":
		target, targetErr := ParseAssignmentTarget(value)
		if targetErr != nil {
			return IssueSearchFilter{}, "", targetErr
		}

		filter.Assignee = target
		return filter, "", nil
	case "last_seen_after":
		point, pointErr := parseSearchTime(value)
		if pointErr != nil {
			return IssueSearchFilter{}, "", pointErr
		}

		filter.LastSeenAfter = &point
		return filter, "", nil
	case "last_seen_before":
		point, pointErr := parseSearchTime(value)
		if pointErr != nil {
			return IssueSearchFilter{}, "", pointErr
		}

		filter.LastSeenBefore = &point
		return filter, "", nil
	case "text":
		return filter, value, nil
	default:
		return IssueSearchFilter{}, "", errInvalidSearch()
	}
}

func parseSearchTime(input string) (time.Time, error) {
	value, rfcErr := time.Parse(time.RFC3339, input)
	if rfcErr == nil {
		return value, nil
	}

	date, dateErr := time.Parse("2006-01-02", input)
	if dateErr != nil {
		return time.Time{}, errInvalidSearch()
	}

	return date, nil
}

func (filter IssueSearchFilter) Canonical() string {
	tokens := []string{}
	if filter.Status != "" {
		tokens = append(tokens, "is:"+string(filter.Status))
	}
	if filter.Environment != "" {
		tokens = append(tokens, "environment:"+filter.Environment)
	}
	if filter.Release != "" {
		tokens = append(tokens, "release:"+filter.Release)
	}
	if filter.Level != "" {
		tokens = append(tokens, "level:"+filter.Level)
	}
	if filter.TagKey != "" {
		tokens = append(tokens, "tag:"+filter.TagKey+"="+filter.TagValue)
	}
	if filter.Assignee.Valid() {
		tokens = append(tokens, "assignee:"+filter.Assignee.Value())
	}
	if filter.LastSeenAfter != nil {
		tokens = append(tokens, "last_seen_after:"+filter.LastSeenAfter.UTC().Format("2006-01-02"))
	}
	if filter.LastSeenBefore != nil {
		tokens = append(tokens, "last_seen_before:"+filter.LastSeenBefore.UTC().Format("2006-01-02"))
	}
	if filter.Text != "" {
		tokens = append(tokens, "text:"+filter.Text)
	}

	return strings.Join(tokens, " ")
}

func (status IssueStatus) Valid() bool {
	return status == IssueStatusUnresolved ||
		status == IssueStatusResolved ||
		status == IssueStatusIgnored
}

func ParseAssignmentTarget(input string) (AssignmentTarget, error) {
	if input == "none" {
		return AssignmentTarget{Kind: AssignmentTargetNone}, nil
	}

	parts := strings.SplitN(input, ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return AssignmentTarget{}, errAssignmentTargetInvalid()
	}

	target := AssignmentTarget{
		Kind: AssignmentTargetKind(parts[0]),
		ID:   parts[1],
	}
	if !target.Valid() {
		return AssignmentTarget{}, errAssignmentTargetInvalid()
	}

	return target, nil
}

func (target AssignmentTarget) Valid() bool {
	if target.Kind == AssignmentTargetNone {
		return target.ID == ""
	}

	if target.Kind == AssignmentTargetOperator || target.Kind == AssignmentTargetTeam {
		return target.ID != ""
	}

	return false
}

func (target AssignmentTarget) Value() string {
	if target.Kind == AssignmentTargetNone {
		return "none"
	}

	return string(target.Kind) + ":" + target.ID
}

func CanTransitionIssueStatus(from IssueStatus, to IssueStatus) bool {
	if from == IssueStatusUnresolved {
		return to == IssueStatusResolved ||
			to == IssueStatusIgnored
	}

	if from == IssueStatusResolved || from == IssueStatusIgnored {
		return to == IssueStatusUnresolved
	}

	return false
}

func requireCommentBody(input string) error {
	if input == "" {
		return errCommentBodyRequired()
	}

	if len(input) > 4000 {
		return errCommentBodyTooLong()
	}

	return nil
}

type scopeRequiredError struct{}

func errScopeRequired() scopeRequiredError {
	return scopeRequiredError{}
}

func (scopeRequiredError) Error() string {
	return "issue query scope is required"
}

type managerRequiredError struct{}

func errManagerRequired() managerRequiredError {
	return managerRequiredError{}
}

func (managerRequiredError) Error() string {
	return "issue manager is required"
}

type issueRequiredError struct{}

func errIssueRequired() issueRequiredError {
	return issueRequiredError{}
}

func (issueRequiredError) Error() string {
	return "issue is required"
}

type actorRequiredError struct{}

func errActorRequired() actorRequiredError {
	return actorRequiredError{}
}

func (actorRequiredError) Error() string {
	return "actor is required"
}

type invalidStatusError struct{}

func errInvalidStatus() invalidStatusError {
	return invalidStatusError{}
}

func (invalidStatusError) Error() string {
	return "issue status is invalid"
}

type commentBodyRequiredError struct{}

func errCommentBodyRequired() commentBodyRequiredError {
	return commentBodyRequiredError{}
}

func (commentBodyRequiredError) Error() string {
	return "comment body is required"
}

type commentBodyTooLongError struct{}

func errCommentBodyTooLong() commentBodyTooLongError {
	return commentBodyTooLongError{}
}

func (commentBodyTooLongError) Error() string {
	return "comment body is too long"
}

type assignmentTargetInvalidError struct{}

func errAssignmentTargetInvalid() assignmentTargetInvalidError {
	return assignmentTargetInvalidError{}
}

func (assignmentTargetInvalidError) Error() string {
	return "assignment target is invalid"
}

type invalidSearchError struct{}

func errInvalidSearch() invalidSearchError {
	return invalidSearchError{}
}

func (invalidSearchError) Error() string {
	return "issue search query is invalid"
}

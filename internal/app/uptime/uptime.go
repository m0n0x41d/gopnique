package uptime

import (
	"context"
	"errors"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const maxCheckLimit = 100

type Manager interface {
	ShowUptime(ctx context.Context, query Query) result.Result[View]
	CreateHTTPMonitor(ctx context.Context, command CreateHTTPMonitorCommand) result.Result[MutationResult]
	CreateHeartbeatMonitor(ctx context.Context, command CreateHeartbeatMonitorCommand) result.Result[MutationResult]
	CreateStatusPage(ctx context.Context, command CreateStatusPageCommand) result.Result[StatusPageMutationResult]
	ShowPrivateStatusPage(ctx context.Context, query PrivateStatusPageQuery) result.Result[StatusPageView]
	ShowPublicStatusPage(ctx context.Context, query PublicStatusPageQuery) result.Result[StatusPageView]
	RecordHeartbeatCheckIn(ctx context.Context, command HeartbeatCheckInCommand) result.Result[HeartbeatCheckInResult]
}

type CheckStore interface {
	ClaimDueHTTPMonitors(ctx context.Context, now time.Time, limit int) result.Result[[]HTTPMonitorCheckTarget]
	RecordHTTPMonitorCheck(ctx context.Context, observation HTTPCheckObservation) result.Result[HTTPCheckResult]
	ClaimOverdueHeartbeatMonitors(ctx context.Context, now time.Time, limit int) result.Result[[]HeartbeatTimeoutTarget]
	RecordHeartbeatTimeout(ctx context.Context, observation HeartbeatTimeoutObservation) result.Result[HeartbeatTimeoutResult]
}

type HTTPProbe interface {
	Get(ctx context.Context, target outbound.DestinationURL, timeout time.Duration) result.Result[HTTPProbeResult]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Query struct {
	Scope Scope
	Limit int
}

type CreateHTTPMonitorCommand struct {
	Scope           Scope
	ActorID         string
	Name            string
	URL             string
	IntervalSeconds int
	TimeoutSeconds  int
}

type CreateHeartbeatMonitorCommand struct {
	Scope           Scope
	ActorID         string
	Name            string
	IntervalSeconds int
	GraceSeconds    int
}

type HeartbeatCheckInCommand struct {
	EndpointID string
	CheckedAt  time.Time
}

type CreateStatusPageCommand struct {
	Scope      Scope
	ActorID    string
	Name       string
	Visibility string
}

type PrivateStatusPageQuery struct {
	Scope  Scope
	PageID string
}

type PublicStatusPageQuery struct {
	Token string
}

type CheckDueCommand struct {
	Now   time.Time
	Limit int
}

type MutationResult struct {
	MonitorID  string
	EndpointID string
}

type StatusPageMutationResult struct {
	PageID      string
	Visibility  string
	PrivatePath string
	PublicPath  string
}

type View struct {
	Monitors    []MonitorView
	StatusPages []StatusPageSummaryView
}

type MonitorView struct {
	ID             string
	Kind           string
	Name           string
	URL            string
	EndpointID     string
	CheckInPath    string
	State          string
	Enabled        string
	Interval       string
	Timeout        string
	Grace          string
	LastCheckedAt  string
	LastCheckInAt  string
	NextCheckAt    string
	LastStatusCode string
	LastError      string
	OpenIncidentID string
}

type StatusPageSummaryView struct {
	ID          string
	Name        string
	Visibility  string
	PrivatePath string
	PublicPath  string
	Enabled     string
	CreatedAt   string
}

type StatusPageView struct {
	ID          string
	Name        string
	Visibility  string
	PrivatePath string
	PublicPath  string
	GeneratedAt string
	Monitors    []MonitorView
	Incidents   []StatusPageIncidentView
}

type StatusPageIncidentView struct {
	ID          string
	MonitorName string
	State       string
	Reason      string
	OpenedAt    string
	ResolvedAt  string
}

type HTTPMonitorCheckTarget struct {
	Scope     Scope
	MonitorID domain.MonitorID
	URL       outbound.DestinationURL
	Timeout   time.Duration
}

type HeartbeatTimeoutTarget struct {
	Scope     Scope
	MonitorID domain.MonitorID
}

type HTTPProbeResult struct {
	statusCode int
	duration   time.Duration
}

type HTTPCheckObservation struct {
	Scope      Scope
	MonitorID  domain.MonitorID
	CheckedAt  time.Time
	State      domain.MonitorState
	StatusCode int
	Duration   time.Duration
	Error      string
}

type HTTPCheckResult struct {
	MonitorID      string
	PreviousState  string
	CurrentState   string
	IncidentOpened bool
	IncidentClosed bool
}

type HeartbeatCheckInResult struct {
	MonitorID      string
	PreviousState  string
	CurrentState   string
	IncidentClosed bool
}

type HeartbeatTimeoutObservation struct {
	Scope     Scope
	MonitorID domain.MonitorID
	CheckedAt time.Time
	Error     string
}

type HeartbeatTimeoutResult struct {
	MonitorID       string
	PreviousState   string
	CurrentState    string
	IncidentOpened  bool
	AlreadyDown     bool
	TimeoutRecorded bool
}

type CheckSummary struct {
	Claimed  int
	Checked  int
	Up       int
	Down     int
	Failures int
}

func Show(
	ctx context.Context,
	manager Manager,
	query Query,
) result.Result[View] {
	if manager == nil {
		return result.Err[View](errors.New("uptime manager is required"))
	}

	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[View](scopeErr)
	}

	query.Limit = normalizeLimit(query.Limit)

	return manager.ShowUptime(ctx, query)
}

func CreateHTTPMonitor(
	ctx context.Context,
	resolver outbound.Resolver,
	manager Manager,
	command CreateHTTPMonitorCommand,
) result.Result[MutationResult] {
	if manager == nil {
		return result.Err[MutationResult](errors.New("uptime manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[MutationResult](scopeErr)
	}

	if command.ActorID == "" {
		return result.Err[MutationResult](errors.New("monitor actor is required"))
	}

	destinationResult := outbound.ValidateDestination(ctx, resolver, command.URL)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		return result.Err[MutationResult](destinationErr)
	}

	definitionResult := monitorDefinition(command, destination)
	definition, definitionErr := definitionResult.Value()
	if definitionErr != nil {
		return result.Err[MutationResult](definitionErr)
	}

	normalized := CreateHTTPMonitorCommand{
		Scope:           command.Scope,
		ActorID:         command.ActorID,
		Name:            definition.Name().String(),
		URL:             definition.URL(),
		IntervalSeconds: int(definition.Interval().Duration() / time.Second),
		TimeoutSeconds:  int(definition.Timeout().Duration() / time.Second),
	}

	return manager.CreateHTTPMonitor(ctx, normalized)
}

func CreateHeartbeatMonitor(
	ctx context.Context,
	manager Manager,
	command CreateHeartbeatMonitorCommand,
) result.Result[MutationResult] {
	if manager == nil {
		return result.Err[MutationResult](errors.New("uptime manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[MutationResult](scopeErr)
	}

	if command.ActorID == "" {
		return result.Err[MutationResult](errors.New("monitor actor is required"))
	}

	normalizedResult := heartbeatCommand(command)
	normalized, normalizedErr := normalizedResult.Value()
	if normalizedErr != nil {
		return result.Err[MutationResult](normalizedErr)
	}

	return manager.CreateHeartbeatMonitor(ctx, normalized)
}

func RecordHeartbeatCheckIn(
	ctx context.Context,
	manager Manager,
	command HeartbeatCheckInCommand,
) result.Result[HeartbeatCheckInResult] {
	if manager == nil {
		return result.Err[HeartbeatCheckInResult](errors.New("uptime manager is required"))
	}

	endpointID, endpointErr := domain.NewHeartbeatEndpointID(command.EndpointID)
	if endpointErr != nil {
		return result.Err[HeartbeatCheckInResult](endpointErr)
	}

	if command.CheckedAt.IsZero() {
		return result.Err[HeartbeatCheckInResult](errors.New("heartbeat check-in time is required"))
	}

	normalized := HeartbeatCheckInCommand{
		EndpointID: endpointID.String(),
		CheckedAt:  command.CheckedAt.UTC(),
	}

	return manager.RecordHeartbeatCheckIn(ctx, normalized)
}

func CheckDueHTTPMonitors(
	ctx context.Context,
	store CheckStore,
	resolver outbound.Resolver,
	probe HTTPProbe,
	command CheckDueCommand,
) result.Result[CheckSummary] {
	validateErr := validateCheckInputs(store, resolver, probe, command)
	if validateErr != nil {
		return result.Err[CheckSummary](validateErr)
	}

	targetsResult := store.ClaimDueHTTPMonitors(ctx, command.Now, normalizeLimit(command.Limit))
	targets, targetsErr := targetsResult.Value()
	if targetsErr != nil {
		return result.Err[CheckSummary](targetsErr)
	}

	summary := CheckSummary{Claimed: len(targets)}
	for _, target := range targets {
		observation := checkHTTPMonitor(ctx, resolver, probe, command.Now, target)
		recordResult := store.RecordHTTPMonitorCheck(ctx, observation)
		_, recordErr := recordResult.Value()
		if recordErr != nil {
			return result.Err[CheckSummary](recordErr)
		}

		summary = countObservation(summary, observation)
	}

	return result.Ok(summary)
}

func CheckDueHeartbeatMonitors(
	ctx context.Context,
	store CheckStore,
	command CheckDueCommand,
) result.Result[CheckSummary] {
	validateErr := validateHeartbeatCheckInputs(store, command)
	if validateErr != nil {
		return result.Err[CheckSummary](validateErr)
	}

	targetsResult := store.ClaimOverdueHeartbeatMonitors(ctx, command.Now, normalizeLimit(command.Limit))
	targets, targetsErr := targetsResult.Value()
	if targetsErr != nil {
		return result.Err[CheckSummary](targetsErr)
	}

	summary := CheckSummary{Claimed: len(targets)}
	for _, target := range targets {
		observation := heartbeatTimeoutObservation(command.Now, target)
		recordResult := store.RecordHeartbeatTimeout(ctx, observation)
		_, recordErr := recordResult.Value()
		if recordErr != nil {
			return result.Err[CheckSummary](recordErr)
		}

		summary = countHeartbeatTimeout(summary, observation)
	}

	return result.Ok(summary)
}

func NewHTTPProbeResult(statusCode int, duration time.Duration) result.Result[HTTPProbeResult] {
	if statusCode <= 0 {
		return result.Err[HTTPProbeResult](errors.New("http status code is required"))
	}

	if duration < 0 {
		return result.Err[HTTPProbeResult](errors.New("http probe duration must not be negative"))
	}

	return result.Ok(HTTPProbeResult{
		statusCode: statusCode,
		duration:   duration,
	})
}

func monitorDefinition(
	command CreateHTTPMonitorCommand,
	destination outbound.DestinationURL,
) result.Result[domain.HTTPMonitorDefinition] {
	interval := time.Duration(command.IntervalSeconds) * time.Second
	timeout := time.Duration(command.TimeoutSeconds) * time.Second
	definition, definitionErr := domain.NewHTTPMonitorDefinition(
		command.Name,
		destination.String(),
		interval,
		timeout,
	)
	if definitionErr != nil {
		return result.Err[domain.HTTPMonitorDefinition](definitionErr)
	}

	return result.Ok(definition)
}

func heartbeatCommand(
	command CreateHeartbeatMonitorCommand,
) result.Result[CreateHeartbeatMonitorCommand] {
	monitorName, nameErr := domain.NewMonitorName(command.Name)
	if nameErr != nil {
		return result.Err[CreateHeartbeatMonitorCommand](nameErr)
	}

	interval, intervalErr := domain.NewMonitorInterval(time.Duration(command.IntervalSeconds) * time.Second)
	if intervalErr != nil {
		return result.Err[CreateHeartbeatMonitorCommand](intervalErr)
	}

	grace, graceErr := domain.NewMonitorGrace(time.Duration(command.GraceSeconds) * time.Second)
	if graceErr != nil {
		return result.Err[CreateHeartbeatMonitorCommand](graceErr)
	}

	return result.Ok(CreateHeartbeatMonitorCommand{
		Scope:           command.Scope,
		ActorID:         command.ActorID,
		Name:            monitorName.String(),
		IntervalSeconds: int(interval.Duration() / time.Second),
		GraceSeconds:    int(grace.Duration() / time.Second),
	})
}

func checkHTTPMonitor(
	ctx context.Context,
	resolver outbound.Resolver,
	probe HTTPProbe,
	now time.Time,
	target HTTPMonitorCheckTarget,
) HTTPCheckObservation {
	resolvedResult := outbound.ValidateResolvedDestination(ctx, resolver, target.URL)
	_, resolvedErr := resolvedResult.Value()
	if resolvedErr != nil {
		return failedObservation(now, target, resolvedErr.Error())
	}

	probeResult := probe.Get(ctx, target.URL, target.Timeout)
	probeValue, probeErr := probeResult.Value()
	if probeErr != nil {
		return failedObservation(now, target, probeErr.Error())
	}

	return HTTPCheckObservation{
		Scope:      target.Scope,
		MonitorID:  target.MonitorID,
		CheckedAt:  now,
		State:      domain.MonitorStateFromHTTPStatus(probeValue.statusCode),
		StatusCode: probeValue.statusCode,
		Duration:   probeValue.duration,
	}
}

func failedObservation(
	now time.Time,
	target HTTPMonitorCheckTarget,
	message string,
) HTTPCheckObservation {
	return HTTPCheckObservation{
		Scope:     target.Scope,
		MonitorID: target.MonitorID,
		CheckedAt: now,
		State:     domain.MonitorStateDown,
		Error:     message,
	}
}

func countObservation(summary CheckSummary, observation HTTPCheckObservation) CheckSummary {
	summary.Checked++
	if observation.State == domain.MonitorStateUp {
		summary.Up++
	}

	if observation.State == domain.MonitorStateDown {
		summary.Down++
	}

	if observation.Error != "" {
		summary.Failures++
	}

	return summary
}

func heartbeatTimeoutObservation(
	now time.Time,
	target HeartbeatTimeoutTarget,
) HeartbeatTimeoutObservation {
	return HeartbeatTimeoutObservation{
		Scope:     target.Scope,
		MonitorID: target.MonitorID,
		CheckedAt: now,
		Error:     "heartbeat overdue",
	}
}

func countHeartbeatTimeout(summary CheckSummary, observation HeartbeatTimeoutObservation) CheckSummary {
	summary.Checked++
	summary.Down++
	if observation.Error != "" {
		summary.Failures++
	}

	return summary
}

func validateCheckInputs(
	store CheckStore,
	resolver outbound.Resolver,
	probe HTTPProbe,
	command CheckDueCommand,
) error {
	if store == nil {
		return errors.New("uptime check store is required")
	}

	if resolver == nil {
		return errors.New("uptime resolver is required")
	}

	if probe == nil {
		return errors.New("uptime probe is required")
	}

	if command.Now.IsZero() {
		return errors.New("uptime check time is required")
	}

	if command.Limit <= 0 {
		return errors.New("uptime check limit must be positive")
	}

	return nil
}

func validateHeartbeatCheckInputs(
	store CheckStore,
	command CheckDueCommand,
) error {
	if store == nil {
		return errors.New("uptime check store is required")
	}

	if command.Now.IsZero() {
		return errors.New("uptime check time is required")
	}

	if command.Limit <= 0 {
		return errors.New("uptime check limit must be positive")
	}

	return nil
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return maxCheckLimit
	}

	if limit > maxCheckLimit {
		return maxCheckLimit
	}

	return limit
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("uptime scope is required")
	}

	return nil
}

func (result HTTPProbeResult) StatusCode() int {
	return result.statusCode
}

func (result HTTPProbeResult) Duration() time.Duration {
	return result.duration
}

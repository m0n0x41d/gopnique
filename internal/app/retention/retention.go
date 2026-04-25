package retention

import (
	"context"
	"errors"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Store interface {
	ListRetentionPolicies(ctx context.Context) result.Result[[]Policy]
	PurgeExpiredProjectData(ctx context.Context, plan ProjectPurgePlan) result.Result[PurgeResult]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Policy struct {
	Scope                   Scope
	EventRetentionDays      int
	PayloadRetentionDays    int
	DeliveryRetentionDays   int
	UserReportRetentionDays int
	Enabled                 bool
}

type Command struct {
	Now       time.Time
	BatchSize int
}

type ProjectPurgePlan struct {
	Scope             Scope
	EventCutoff       time.Time
	PayloadCutoff     time.Time
	DeliveryCutoff    time.Time
	UserReportCutoff  time.Time
	BatchSize         int
	PayloadRetention  int
	EventRetention    int
	DeliveryRetention int
	ReportRetention   int
}

type PurgeResult struct {
	EventsDeleted       int
	PayloadsCleared     int
	DeliveryRowsDeleted int
	UserReportsDeleted  int
	IssuesDeleted       int
	StatsRowsDeleted    int
}

type Summary struct {
	ProjectsProcessed   int
	EventsDeleted       int
	PayloadsCleared     int
	DeliveryRowsDeleted int
	UserReportsDeleted  int
	IssuesDeleted       int
	StatsRowsDeleted    int
}

func Run(
	ctx context.Context,
	store Store,
	command Command,
) result.Result[Summary] {
	if store == nil {
		return result.Err[Summary](errors.New("retention store is required"))
	}

	commandResult := NormalizeCommand(command)
	normalizedCommand, commandErr := commandResult.Value()
	if commandErr != nil {
		return result.Err[Summary](commandErr)
	}

	policiesResult := store.ListRetentionPolicies(ctx)
	policies, policiesErr := policiesResult.Value()
	if policiesErr != nil {
		return result.Err[Summary](policiesErr)
	}

	return RunPolicies(ctx, store, normalizedCommand, policies)
}

func RunPolicies(
	ctx context.Context,
	store Store,
	command Command,
	policies []Policy,
) result.Result[Summary] {
	summary := Summary{}

	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}

		planResult := PlanProjectPurge(policy, command)
		plan, planErr := planResult.Value()
		if planErr != nil {
			return result.Err[Summary](planErr)
		}

		purgeResult := store.PurgeExpiredProjectData(ctx, plan)
		purged, purgeErr := purgeResult.Value()
		if purgeErr != nil {
			return result.Err[Summary](purgeErr)
		}

		summary = AddPurgeResult(summary, purged)
		summary.ProjectsProcessed++
	}

	return result.Ok(summary)
}

func NormalizeCommand(command Command) result.Result[Command] {
	if command.Now.IsZero() {
		return result.Err[Command](errors.New("retention reference time is required"))
	}

	if command.BatchSize < 1 {
		return result.Err[Command](errors.New("retention batch size must be positive"))
	}

	command.Now = command.Now.UTC()

	return result.Ok(command)
}

func PlanProjectPurge(
	policy Policy,
	command Command,
) result.Result[ProjectPurgePlan] {
	policyResult := NormalizePolicy(policy)
	normalizedPolicy, policyErr := policyResult.Value()
	if policyErr != nil {
		return result.Err[ProjectPurgePlan](policyErr)
	}

	commandResult := NormalizeCommand(command)
	normalizedCommand, commandErr := commandResult.Value()
	if commandErr != nil {
		return result.Err[ProjectPurgePlan](commandErr)
	}

	return result.Ok(ProjectPurgePlan{
		Scope:             normalizedPolicy.Scope,
		EventCutoff:       cutoff(normalizedCommand.Now, normalizedPolicy.EventRetentionDays),
		PayloadCutoff:     cutoff(normalizedCommand.Now, normalizedPolicy.PayloadRetentionDays),
		DeliveryCutoff:    cutoff(normalizedCommand.Now, normalizedPolicy.DeliveryRetentionDays),
		UserReportCutoff:  cutoff(normalizedCommand.Now, normalizedPolicy.UserReportRetentionDays),
		BatchSize:         normalizedCommand.BatchSize,
		PayloadRetention:  normalizedPolicy.PayloadRetentionDays,
		EventRetention:    normalizedPolicy.EventRetentionDays,
		DeliveryRetention: normalizedPolicy.DeliveryRetentionDays,
		ReportRetention:   normalizedPolicy.UserReportRetentionDays,
	})
}

func NormalizePolicy(policy Policy) result.Result[Policy] {
	scopeErr := requireScope(policy.Scope)
	if scopeErr != nil {
		return result.Err[Policy](scopeErr)
	}

	if policy.EventRetentionDays < 1 {
		return result.Err[Policy](errors.New("event retention days must be positive"))
	}

	if policy.PayloadRetentionDays < 1 {
		return result.Err[Policy](errors.New("payload retention days must be positive"))
	}

	if policy.DeliveryRetentionDays < 1 {
		return result.Err[Policy](errors.New("delivery retention days must be positive"))
	}

	if policy.UserReportRetentionDays < 1 {
		return result.Err[Policy](errors.New("user report retention days must be positive"))
	}

	if policy.PayloadRetentionDays > policy.EventRetentionDays {
		return result.Err[Policy](errors.New("payload retention cannot exceed event retention"))
	}

	if policy.DeliveryRetentionDays > policy.EventRetentionDays {
		return result.Err[Policy](errors.New("delivery retention cannot exceed event retention"))
	}

	return result.Ok(policy)
}

func AddPurgeResult(summary Summary, purged PurgeResult) Summary {
	summary.EventsDeleted += purged.EventsDeleted
	summary.PayloadsCleared += purged.PayloadsCleared
	summary.DeliveryRowsDeleted += purged.DeliveryRowsDeleted
	summary.UserReportsDeleted += purged.UserReportsDeleted
	summary.IssuesDeleted += purged.IssuesDeleted
	summary.StatsRowsDeleted += purged.StatsRowsDeleted

	return summary
}

func cutoff(now time.Time, days int) time.Time {
	return now.AddDate(0, 0, -days)
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("retention scope is required")
	}

	return nil
}

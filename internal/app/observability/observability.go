package observability

import (
	"context"

	"github.com/ivanzakutnii/error-tracker/internal/app/health"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const (
	ServiceName = "error-tracker"
	APIVersion  = "observability.v1"
)

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type Reader interface {
	Ping(ctx context.Context) error
	MigrationStatus(ctx context.Context) (health.MigrationStatus, error)
	QueueStatus(ctx context.Context, scope Scope) result.Result[QueueStatus]
	AdminMetrics(ctx context.Context, scope Scope) result.Result[AdminMetrics]
}

type SystemInfo struct {
	ServiceName string
	APIVersion  string
}

type ReadinessView struct {
	Ready       bool
	DatabaseOK  bool
	SchemaOK    bool
	Description string
}

type MigrationView struct {
	AppliedCount int
	Ready        bool
}

type QueueStatus struct {
	Groups []QueueGroup
}

type QueueGroup struct {
	Provider            string
	Status              string
	Count               int
	OldestNextAttemptAt string
}

type AdminMetrics struct {
	Events              int
	Issues              int
	Transactions        int
	UptimeMonitors      int
	UptimeIncidents     int
	StatusPages         int
	NotificationIntents int
}

type Snapshot struct {
	System    SystemInfo
	Readiness ReadinessView
	Migration MigrationView
	Queue     QueueStatus
	Metrics   AdminMetrics
}

func System() SystemInfo {
	return SystemInfo{
		ServiceName: ServiceName,
		APIVersion:  APIVersion,
	}
}

func Readiness(ctx context.Context, reader Reader) ReadinessView {
	report := health.CheckReadiness(ctx, reader)

	return ReadinessView{
		Ready:       report.Ready,
		DatabaseOK:  report.DatabaseOK,
		SchemaOK:    report.SchemaOK,
		Description: report.Description,
	}
}

func Migration(ctx context.Context, reader Reader) result.Result[MigrationView] {
	status, statusErr := reader.MigrationStatus(ctx)
	if statusErr != nil {
		return result.Err[MigrationView](statusErr)
	}

	return result.Ok(MigrationView{
		AppliedCount: status.AppliedCount,
		Ready:        status.Ready,
	})
}

func SnapshotForScope(
	ctx context.Context,
	reader Reader,
	scope Scope,
) result.Result[Snapshot] {
	readiness := Readiness(ctx, reader)

	migrationResult := Migration(ctx, reader)
	migration, migrationErr := migrationResult.Value()
	if migrationErr != nil {
		return result.Err[Snapshot](migrationErr)
	}

	queueResult := reader.QueueStatus(ctx, scope)
	queue, queueErr := queueResult.Value()
	if queueErr != nil {
		return result.Err[Snapshot](queueErr)
	}

	metricsResult := reader.AdminMetrics(ctx, scope)
	metrics, metricsErr := metricsResult.Value()
	if metricsErr != nil {
		return result.Err[Snapshot](metricsErr)
	}

	return result.Ok(Snapshot{
		System:    System(),
		Readiness: readiness,
		Migration: migration,
		Queue:     queue,
		Metrics:   metrics,
	})
}

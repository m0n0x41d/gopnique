package health

import "context"

type DatabaseProbe interface {
	Ping(ctx context.Context) error
	MigrationStatus(ctx context.Context) (MigrationStatus, error)
}

type MigrationStatus struct {
	AppliedCount int
	Ready        bool
}

type ReadinessReport struct {
	Ready       bool
	DatabaseOK  bool
	SchemaOK    bool
	Description string
}

func CheckReadiness(ctx context.Context, probe DatabaseProbe) ReadinessReport {
	pingErr := probe.Ping(ctx)
	if pingErr != nil {
		return ReadinessReport{
			Ready:       false,
			DatabaseOK:  false,
			SchemaOK:    false,
			Description: "database unavailable",
		}
	}

	status, statusErr := probe.MigrationStatus(ctx)
	if statusErr != nil {
		return ReadinessReport{
			Ready:       false,
			DatabaseOK:  true,
			SchemaOK:    false,
			Description: "migration status unavailable",
		}
	}

	return ReadinessReport{
		Ready:       status.Ready,
		DatabaseOK:  true,
		SchemaOK:    status.Ready,
		Description: "ready",
	}
}

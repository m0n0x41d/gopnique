package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/app/uptime"
)

type UptimeTaskConfig struct {
	BatchSize int
}

type uptimeTask struct {
	store    uptime.CheckStore
	resolver outbound.Resolver
	probe    uptime.HTTPProbe
	cfg      UptimeTaskConfig
}

func NewUptimeTask(
	store uptime.CheckStore,
	resolver outbound.Resolver,
	probe uptime.HTTPProbe,
	cfg UptimeTaskConfig,
) Task {
	return uptimeTask{
		store:    store,
		resolver: resolver,
		probe:    probe,
		cfg:      cfg,
	}
}

func (task uptimeTask) RunOnce(ctx context.Context) error {
	now := time.Now().UTC()

	httpResult := uptime.CheckDueHTTPMonitors(
		ctx,
		task.store,
		task.resolver,
		task.probe,
		uptime.CheckDueCommand{
			Now:   now,
			Limit: task.cfg.BatchSize,
		},
	)
	_, httpErr := httpResult.Value()
	if httpErr != nil {
		return httpErr
	}

	heartbeatResult := uptime.CheckDueHeartbeatMonitors(
		ctx,
		task.store,
		uptime.CheckDueCommand{
			Now:   now,
			Limit: task.cfg.BatchSize,
		},
	)
	_, heartbeatErr := heartbeatResult.Value()

	return heartbeatErr
}

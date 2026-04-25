package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
)

type TeamsTaskConfig struct {
	PublicURL string
	BatchSize int
}

type teamsTask struct {
	outbox   notifications.TeamsOutbox
	resolver outbound.Resolver
	sender   notifications.TeamsSender
	cfg      TeamsTaskConfig
}

func NewTeamsTask(
	outbox notifications.TeamsOutbox,
	resolver outbound.Resolver,
	sender notifications.TeamsSender,
	cfg TeamsTaskConfig,
) Task {
	return teamsTask{
		outbox:   outbox,
		resolver: resolver,
		sender:   sender,
		cfg:      cfg,
	}
}

func (task teamsTask) RunOnce(ctx context.Context) error {
	commandResult := notifications.NewTeamsBatchCommand(
		time.Now().UTC(),
		task.cfg.BatchSize,
		task.cfg.PublicURL,
	)
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return commandErr
	}

	batchResult := notifications.DeliverTeamsBatch(
		ctx,
		command,
		task.resolver,
		task.outbox,
		task.sender,
	)
	_, batchErr := batchResult.Value()

	return batchErr
}

package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
)

type DiscordTaskConfig struct {
	PublicURL string
	BatchSize int
}

type discordTask struct {
	outbox   notifications.DiscordOutbox
	resolver outbound.Resolver
	sender   notifications.DiscordSender
	cfg      DiscordTaskConfig
}

func NewDiscordTask(
	outbox notifications.DiscordOutbox,
	resolver outbound.Resolver,
	sender notifications.DiscordSender,
	cfg DiscordTaskConfig,
) Task {
	return discordTask{
		outbox:   outbox,
		resolver: resolver,
		sender:   sender,
		cfg:      cfg,
	}
}

func (task discordTask) RunOnce(ctx context.Context) error {
	commandResult := notifications.NewDiscordBatchCommand(
		time.Now().UTC(),
		task.cfg.BatchSize,
		task.cfg.PublicURL,
	)
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return commandErr
	}

	batchResult := notifications.DeliverDiscordBatch(
		ctx,
		command,
		task.resolver,
		task.outbox,
		task.sender,
	)
	_, batchErr := batchResult.Value()

	return batchErr
}

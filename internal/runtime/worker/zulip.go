package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
)

type ZulipTaskConfig struct {
	PublicURL string
	BatchSize int
}

type zulipTask struct {
	outbox   notifications.ZulipOutbox
	resolver outbound.Resolver
	sender   notifications.ZulipSender
	cfg      ZulipTaskConfig
}

func NewZulipTask(
	outbox notifications.ZulipOutbox,
	resolver outbound.Resolver,
	sender notifications.ZulipSender,
	cfg ZulipTaskConfig,
) Task {
	return zulipTask{
		outbox:   outbox,
		resolver: resolver,
		sender:   sender,
		cfg:      cfg,
	}
}

func (task zulipTask) RunOnce(ctx context.Context) error {
	commandResult := notifications.NewZulipBatchCommand(
		time.Now().UTC(),
		task.cfg.BatchSize,
		task.cfg.PublicURL,
	)
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return commandErr
	}

	batchResult := notifications.DeliverZulipBatch(
		ctx,
		command,
		task.resolver,
		task.outbox,
		task.sender,
	)
	_, batchErr := batchResult.Value()

	return batchErr
}

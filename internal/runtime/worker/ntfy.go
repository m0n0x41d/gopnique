package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
)

type NtfyTaskConfig struct {
	PublicURL string
	BatchSize int
}

type ntfyTask struct {
	outbox   notifications.NtfyOutbox
	resolver outbound.Resolver
	sender   notifications.NtfySender
	cfg      NtfyTaskConfig
}

func NewNtfyTask(
	outbox notifications.NtfyOutbox,
	resolver outbound.Resolver,
	sender notifications.NtfySender,
	cfg NtfyTaskConfig,
) Task {
	return ntfyTask{
		outbox:   outbox,
		resolver: resolver,
		sender:   sender,
		cfg:      cfg,
	}
}

func (task ntfyTask) RunOnce(ctx context.Context) error {
	commandResult := notifications.NewNtfyBatchCommand(
		time.Now().UTC(),
		task.cfg.BatchSize,
		task.cfg.PublicURL,
	)
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return commandErr
	}

	batchResult := notifications.DeliverNtfyBatch(
		ctx,
		command,
		task.resolver,
		task.outbox,
		task.sender,
	)
	_, batchErr := batchResult.Value()

	return batchErr
}

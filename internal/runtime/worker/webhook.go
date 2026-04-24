package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
)

type WebhookTaskConfig struct {
	PublicURL string
	BatchSize int
}

type webhookTask struct {
	outbox   notifications.WebhookOutbox
	resolver outbound.Resolver
	sender   notifications.WebhookSender
	cfg      WebhookTaskConfig
}

func NewWebhookTask(
	outbox notifications.WebhookOutbox,
	resolver outbound.Resolver,
	sender notifications.WebhookSender,
	cfg WebhookTaskConfig,
) Task {
	return webhookTask{
		outbox:   outbox,
		resolver: resolver,
		sender:   sender,
		cfg:      cfg,
	}
}

func (task webhookTask) RunOnce(ctx context.Context) error {
	commandResult := notifications.NewWebhookBatchCommand(
		time.Now().UTC(),
		task.cfg.BatchSize,
		task.cfg.PublicURL,
	)
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return commandErr
	}

	batchResult := notifications.DeliverWebhookBatch(
		ctx,
		command,
		task.resolver,
		task.outbox,
		task.sender,
	)
	_, batchErr := batchResult.Value()

	return batchErr
}

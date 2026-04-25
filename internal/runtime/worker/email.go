package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
)

type EmailTaskConfig struct {
	PublicURL string
	BatchSize int
}

type emailTask struct {
	outbox notifications.EmailOutbox
	sender notifications.EmailSender
	cfg    EmailTaskConfig
}

func NewEmailTask(
	outbox notifications.EmailOutbox,
	sender notifications.EmailSender,
	cfg EmailTaskConfig,
) Task {
	return emailTask{
		outbox: outbox,
		sender: sender,
		cfg:    cfg,
	}
}

func (task emailTask) RunOnce(ctx context.Context) error {
	commandResult := notifications.NewEmailBatchCommand(
		time.Now().UTC(),
		task.cfg.BatchSize,
		task.cfg.PublicURL,
	)
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return commandErr
	}

	batchResult := notifications.DeliverEmailBatch(
		ctx,
		command,
		task.outbox,
		task.sender,
	)
	_, batchErr := batchResult.Value()

	return batchErr
}

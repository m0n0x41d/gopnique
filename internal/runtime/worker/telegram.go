package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
)

type TelegramTaskConfig struct {
	PublicURL string
	BatchSize int
}

type telegramTask struct {
	outbox notifications.TelegramOutbox
	sender notifications.TelegramSender
	cfg    TelegramTaskConfig
}

func NewTelegramTask(
	outbox notifications.TelegramOutbox,
	sender notifications.TelegramSender,
	cfg TelegramTaskConfig,
) Task {
	return telegramTask{
		outbox: outbox,
		sender: sender,
		cfg:    cfg,
	}
}

func (task telegramTask) RunOnce(ctx context.Context) error {
	commandResult := notifications.NewTelegramBatchCommand(
		time.Now().UTC(),
		task.cfg.BatchSize,
		task.cfg.PublicURL,
	)
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return commandErr
	}

	batchResult := notifications.DeliverTelegramBatch(
		ctx,
		command,
		task.outbox,
		task.sender,
	)
	_, batchErr := batchResult.Value()

	return batchErr
}

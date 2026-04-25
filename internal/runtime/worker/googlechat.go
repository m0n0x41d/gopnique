package worker

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
)

type GoogleChatTaskConfig struct {
	PublicURL string
	BatchSize int
}

type googleChatTask struct {
	outbox   notifications.GoogleChatOutbox
	resolver outbound.Resolver
	sender   notifications.GoogleChatSender
	cfg      GoogleChatTaskConfig
}

func NewGoogleChatTask(
	outbox notifications.GoogleChatOutbox,
	resolver outbound.Resolver,
	sender notifications.GoogleChatSender,
	cfg GoogleChatTaskConfig,
) Task {
	return googleChatTask{
		outbox:   outbox,
		resolver: resolver,
		sender:   sender,
		cfg:      cfg,
	}
}

func (task googleChatTask) RunOnce(ctx context.Context) error {
	commandResult := notifications.NewGoogleChatBatchCommand(
		time.Now().UTC(),
		task.cfg.BatchSize,
		task.cfg.PublicURL,
	)
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return commandErr
	}

	batchResult := notifications.DeliverGoogleChatBatch(
		ctx,
		command,
		task.resolver,
		task.outbox,
		task.sender,
	)
	_, batchErr := batchResult.Value()

	return batchErr
}

package notifications

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/notifyplan"
)

type TelegramBatchCommand struct {
	now       time.Time
	limit     int
	publicURL string
}

type TelegramBatchReceipt struct {
	claimed   int
	delivered int
	failed    int
}

type WebhookBatchCommand struct {
	now       time.Time
	limit     int
	publicURL string
}

type WebhookBatchReceipt struct {
	claimed   int
	delivered int
	failed    int
}

func NewTelegramBatchCommand(
	now time.Time,
	limit int,
	publicURL string,
) result.Result[TelegramBatchCommand] {
	if now.IsZero() {
		return result.Err[TelegramBatchCommand](errors.New("batch time is required"))
	}

	if limit < 1 {
		return result.Err[TelegramBatchCommand](errors.New("batch limit must be positive"))
	}

	if publicURL == "" {
		return result.Err[TelegramBatchCommand](errors.New("public url is required"))
	}

	return result.Ok(TelegramBatchCommand{
		now:       now.UTC(),
		limit:     limit,
		publicURL: publicURL,
	})
}

func NewWebhookBatchCommand(
	now time.Time,
	limit int,
	publicURL string,
) result.Result[WebhookBatchCommand] {
	if now.IsZero() {
		return result.Err[WebhookBatchCommand](errors.New("batch time is required"))
	}

	if limit < 1 {
		return result.Err[WebhookBatchCommand](errors.New("batch limit must be positive"))
	}

	if publicURL == "" {
		return result.Err[WebhookBatchCommand](errors.New("public url is required"))
	}

	return result.Ok(WebhookBatchCommand{
		now:       now.UTC(),
		limit:     limit,
		publicURL: publicURL,
	})
}

func DeliverTelegramBatch(
	ctx context.Context,
	command TelegramBatchCommand,
	outbox TelegramOutbox,
	sender TelegramSender,
) result.Result[TelegramBatchReceipt] {
	deliveriesResult := outbox.ClaimTelegramDeliveries(ctx, command.now, command.limit)
	deliveries, deliveriesErr := deliveriesResult.Value()
	if deliveriesErr != nil {
		return result.Err[TelegramBatchReceipt](deliveriesErr)
	}

	receipt := TelegramBatchReceipt{claimed: len(deliveries)}
	for _, delivery := range deliveries {
		nextReceipt := deliverTelegramDelivery(ctx, command, delivery, outbox, sender, receipt)
		receipt = nextReceipt
	}

	return result.Ok(receipt)
}

func DeliverWebhookBatch(
	ctx context.Context,
	command WebhookBatchCommand,
	resolver outbound.Resolver,
	outbox WebhookOutbox,
	sender WebhookSender,
) result.Result[WebhookBatchReceipt] {
	deliveriesResult := outbox.ClaimWebhookDeliveries(ctx, command.now, command.limit)
	deliveries, deliveriesErr := deliveriesResult.Value()
	if deliveriesErr != nil {
		return result.Err[WebhookBatchReceipt](deliveriesErr)
	}

	receipt := WebhookBatchReceipt{claimed: len(deliveries)}
	for _, delivery := range deliveries {
		nextReceipt := deliverWebhookDelivery(ctx, command, resolver, delivery, outbox, sender, receipt)
		receipt = nextReceipt
	}

	return result.Ok(receipt)
}

func deliverTelegramDelivery(
	ctx context.Context,
	command TelegramBatchCommand,
	delivery TelegramDelivery,
	outbox TelegramOutbox,
	sender TelegramSender,
	receipt TelegramBatchReceipt,
) TelegramBatchReceipt {
	messageResult := telegramMessage(delivery, command.publicURL)
	message, messageErr := messageResult.Value()
	if messageErr != nil {
		return markTelegramFailure(ctx, outbox, delivery.intentID, command.now, messageErr.Error(), receipt)
	}

	sendResult := sender.SendTelegram(ctx, message)
	sendReceipt, sendErr := sendResult.Value()
	if sendErr != nil {
		return markTelegramFailure(ctx, outbox, delivery.intentID, command.now, sendErr.Error(), receipt)
	}

	markResult := outbox.MarkTelegramDelivered(ctx, delivery.intentID, command.now, sendReceipt)
	_, markErr := markResult.Value()
	if markErr != nil {
		return TelegramBatchReceipt{
			claimed:   receipt.claimed,
			delivered: receipt.delivered,
			failed:    receipt.failed + 1,
		}
	}

	return TelegramBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered + 1,
		failed:    receipt.failed,
	}
}

func deliverWebhookDelivery(
	ctx context.Context,
	command WebhookBatchCommand,
	resolver outbound.Resolver,
	delivery WebhookDelivery,
	outbox WebhookOutbox,
	sender WebhookSender,
	receipt WebhookBatchReceipt,
) WebhookBatchReceipt {
	resolvedResult := outbound.ValidateResolvedDestination(ctx, resolver, delivery.destinationURL)
	_, resolvedErr := resolvedResult.Value()
	if resolvedErr != nil {
		sendReceipt := NewWebhookFailedReceipt(0, resolvedErr.Error())
		return markWebhookFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	messageResult := webhookMessage(delivery, command.publicURL)
	message, messageErr := messageResult.Value()
	if messageErr != nil {
		sendReceipt := NewWebhookFailedReceipt(0, messageErr.Error())
		return markWebhookFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	sendResult := sender.SendWebhook(ctx, message)
	sendReceipt, sendErr := sendResult.Value()
	if sendErr != nil {
		failureReceipt := NewWebhookFailedReceipt(0, sendErr.Error())
		return markWebhookFailure(ctx, outbox, delivery.intentID, command.now, failureReceipt, receipt)
	}

	if !sendReceipt.Delivered() {
		return markWebhookFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	markResult := outbox.MarkWebhookDelivered(ctx, delivery.intentID, command.now, sendReceipt)
	_, markErr := markResult.Value()
	if markErr != nil {
		return WebhookBatchReceipt{
			claimed:   receipt.claimed,
			delivered: receipt.delivered,
			failed:    receipt.failed + 1,
		}
	}

	return WebhookBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered + 1,
		failed:    receipt.failed,
	}
}

func telegramMessage(
	delivery TelegramDelivery,
	publicURL string,
) result.Result[TelegramMessage] {
	openedResult := notifyplan.NewIssueOpened(
		delivery.event,
		delivery.issueID,
		delivery.issueShortID,
	)
	opened, openedErr := openedResult.Value()
	if openedErr != nil {
		return result.Err[TelegramMessage](openedErr)
	}

	textResult := notifyplan.TelegramIssueOpenedText(opened, publicURL)
	text, textErr := textResult.Value()
	if textErr != nil {
		return result.Err[TelegramMessage](textErr)
	}

	return result.Ok(NewTelegramMessage(delivery.intentID, delivery.chatID, text))
}

func webhookMessage(
	delivery WebhookDelivery,
	publicURL string,
) result.Result[WebhookMessage] {
	openedResult := notifyplan.NewIssueOpened(
		delivery.event,
		delivery.issueID,
		delivery.issueShortID,
	)
	opened, openedErr := openedResult.Value()
	if openedErr != nil {
		return result.Err[WebhookMessage](openedErr)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if baseURL == "" {
		return result.Err[WebhookMessage](errors.New("public url is required"))
	}

	payload := WebhookPayload{
		EventID:      opened.Event().EventID().String(),
		IssueID:      opened.IssueID().String(),
		IssueShortID: opened.IssueShortID(),
		Title:        opened.Event().Title().String(),
		Level:        opened.Event().Level().String(),
		Platform:     opened.Event().Platform(),
		IssueURL:     baseURL + "/issues/" + opened.IssueID().String(),
	}

	return result.Ok(NewWebhookMessage(delivery.intentID, delivery.destinationURL, payload))
}

func markTelegramFailure(
	ctx context.Context,
	outbox TelegramOutbox,
	intentID domain.NotificationIntentID,
	now time.Time,
	reason string,
	receipt TelegramBatchReceipt,
) TelegramBatchReceipt {
	markResult := outbox.MarkTelegramFailed(ctx, intentID, now, reason)
	_, _ = markResult.Value()

	return TelegramBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered,
		failed:    receipt.failed + 1,
	}
}

func markWebhookFailure(
	ctx context.Context,
	outbox WebhookOutbox,
	intentID domain.NotificationIntentID,
	now time.Time,
	sendReceipt WebhookSendReceipt,
	receipt WebhookBatchReceipt,
) WebhookBatchReceipt {
	markResult := outbox.MarkWebhookFailed(ctx, intentID, now, sendReceipt)
	_, _ = markResult.Value()

	return WebhookBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered,
		failed:    receipt.failed + 1,
	}
}

func (receipt TelegramBatchReceipt) Claimed() int {
	return receipt.claimed
}

func (receipt TelegramBatchReceipt) Delivered() int {
	return receipt.delivered
}

func (receipt TelegramBatchReceipt) Failed() int {
	return receipt.failed
}

func (receipt WebhookBatchReceipt) Claimed() int {
	return receipt.claimed
}

func (receipt WebhookBatchReceipt) Delivered() int {
	return receipt.delivered
}

func (receipt WebhookBatchReceipt) Failed() int {
	return receipt.failed
}

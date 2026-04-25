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

type EmailBatchCommand struct {
	now       time.Time
	limit     int
	publicURL string
}

type EmailBatchReceipt struct {
	claimed   int
	delivered int
	failed    int
}

type DiscordBatchCommand struct {
	now       time.Time
	limit     int
	publicURL string
}

type DiscordBatchReceipt struct {
	claimed   int
	delivered int
	failed    int
}

type GoogleChatBatchCommand struct {
	now       time.Time
	limit     int
	publicURL string
}

type GoogleChatBatchReceipt struct {
	claimed   int
	delivered int
	failed    int
}

type NtfyBatchCommand struct {
	now       time.Time
	limit     int
	publicURL string
}

type NtfyBatchReceipt struct {
	claimed   int
	delivered int
	failed    int
}

type TeamsBatchCommand struct {
	now       time.Time
	limit     int
	publicURL string
}

type TeamsBatchReceipt struct {
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

func NewEmailBatchCommand(
	now time.Time,
	limit int,
	publicURL string,
) result.Result[EmailBatchCommand] {
	if now.IsZero() {
		return result.Err[EmailBatchCommand](errors.New("batch time is required"))
	}

	if limit < 1 {
		return result.Err[EmailBatchCommand](errors.New("batch limit must be positive"))
	}

	if publicURL == "" {
		return result.Err[EmailBatchCommand](errors.New("public url is required"))
	}

	return result.Ok(EmailBatchCommand{
		now:       now.UTC(),
		limit:     limit,
		publicURL: publicURL,
	})
}

func NewDiscordBatchCommand(
	now time.Time,
	limit int,
	publicURL string,
) result.Result[DiscordBatchCommand] {
	if now.IsZero() {
		return result.Err[DiscordBatchCommand](errors.New("batch time is required"))
	}

	if limit < 1 {
		return result.Err[DiscordBatchCommand](errors.New("batch limit must be positive"))
	}

	if publicURL == "" {
		return result.Err[DiscordBatchCommand](errors.New("public url is required"))
	}

	return result.Ok(DiscordBatchCommand{
		now:       now.UTC(),
		limit:     limit,
		publicURL: publicURL,
	})
}

func NewGoogleChatBatchCommand(
	now time.Time,
	limit int,
	publicURL string,
) result.Result[GoogleChatBatchCommand] {
	if now.IsZero() {
		return result.Err[GoogleChatBatchCommand](errors.New("batch time is required"))
	}

	if limit < 1 {
		return result.Err[GoogleChatBatchCommand](errors.New("batch limit must be positive"))
	}

	if publicURL == "" {
		return result.Err[GoogleChatBatchCommand](errors.New("public url is required"))
	}

	return result.Ok(GoogleChatBatchCommand{
		now:       now.UTC(),
		limit:     limit,
		publicURL: publicURL,
	})
}

func NewNtfyBatchCommand(
	now time.Time,
	limit int,
	publicURL string,
) result.Result[NtfyBatchCommand] {
	if now.IsZero() {
		return result.Err[NtfyBatchCommand](errors.New("batch time is required"))
	}

	if limit < 1 {
		return result.Err[NtfyBatchCommand](errors.New("batch limit must be positive"))
	}

	if publicURL == "" {
		return result.Err[NtfyBatchCommand](errors.New("public url is required"))
	}

	return result.Ok(NtfyBatchCommand{
		now:       now.UTC(),
		limit:     limit,
		publicURL: publicURL,
	})
}

func NewTeamsBatchCommand(
	now time.Time,
	limit int,
	publicURL string,
) result.Result[TeamsBatchCommand] {
	if now.IsZero() {
		return result.Err[TeamsBatchCommand](errors.New("batch time is required"))
	}

	if limit < 1 {
		return result.Err[TeamsBatchCommand](errors.New("batch limit must be positive"))
	}

	if publicURL == "" {
		return result.Err[TeamsBatchCommand](errors.New("public url is required"))
	}

	return result.Ok(TeamsBatchCommand{
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

func DeliverEmailBatch(
	ctx context.Context,
	command EmailBatchCommand,
	outbox EmailOutbox,
	sender EmailSender,
) result.Result[EmailBatchReceipt] {
	deliveriesResult := outbox.ClaimEmailDeliveries(ctx, command.now, command.limit)
	deliveries, deliveriesErr := deliveriesResult.Value()
	if deliveriesErr != nil {
		return result.Err[EmailBatchReceipt](deliveriesErr)
	}

	receipt := EmailBatchReceipt{claimed: len(deliveries)}
	for _, delivery := range deliveries {
		nextReceipt := deliverEmailDelivery(ctx, command, delivery, outbox, sender, receipt)
		receipt = nextReceipt
	}

	return result.Ok(receipt)
}

func DeliverDiscordBatch(
	ctx context.Context,
	command DiscordBatchCommand,
	resolver outbound.Resolver,
	outbox DiscordOutbox,
	sender DiscordSender,
) result.Result[DiscordBatchReceipt] {
	deliveriesResult := outbox.ClaimDiscordDeliveries(ctx, command.now, command.limit)
	deliveries, deliveriesErr := deliveriesResult.Value()
	if deliveriesErr != nil {
		return result.Err[DiscordBatchReceipt](deliveriesErr)
	}

	receipt := DiscordBatchReceipt{claimed: len(deliveries)}
	for _, delivery := range deliveries {
		nextReceipt := deliverDiscordDelivery(ctx, command, resolver, delivery, outbox, sender, receipt)
		receipt = nextReceipt
	}

	return result.Ok(receipt)
}

func DeliverGoogleChatBatch(
	ctx context.Context,
	command GoogleChatBatchCommand,
	resolver outbound.Resolver,
	outbox GoogleChatOutbox,
	sender GoogleChatSender,
) result.Result[GoogleChatBatchReceipt] {
	deliveriesResult := outbox.ClaimGoogleChatDeliveries(ctx, command.now, command.limit)
	deliveries, deliveriesErr := deliveriesResult.Value()
	if deliveriesErr != nil {
		return result.Err[GoogleChatBatchReceipt](deliveriesErr)
	}

	receipt := GoogleChatBatchReceipt{claimed: len(deliveries)}
	for _, delivery := range deliveries {
		nextReceipt := deliverGoogleChatDelivery(ctx, command, resolver, delivery, outbox, sender, receipt)
		receipt = nextReceipt
	}

	return result.Ok(receipt)
}

func DeliverNtfyBatch(
	ctx context.Context,
	command NtfyBatchCommand,
	resolver outbound.Resolver,
	outbox NtfyOutbox,
	sender NtfySender,
) result.Result[NtfyBatchReceipt] {
	deliveriesResult := outbox.ClaimNtfyDeliveries(ctx, command.now, command.limit)
	deliveries, deliveriesErr := deliveriesResult.Value()
	if deliveriesErr != nil {
		return result.Err[NtfyBatchReceipt](deliveriesErr)
	}

	receipt := NtfyBatchReceipt{claimed: len(deliveries)}
	for _, delivery := range deliveries {
		nextReceipt := deliverNtfyDelivery(ctx, command, resolver, delivery, outbox, sender, receipt)
		receipt = nextReceipt
	}

	return result.Ok(receipt)
}

func DeliverTeamsBatch(
	ctx context.Context,
	command TeamsBatchCommand,
	resolver outbound.Resolver,
	outbox TeamsOutbox,
	sender TeamsSender,
) result.Result[TeamsBatchReceipt] {
	deliveriesResult := outbox.ClaimTeamsDeliveries(ctx, command.now, command.limit)
	deliveries, deliveriesErr := deliveriesResult.Value()
	if deliveriesErr != nil {
		return result.Err[TeamsBatchReceipt](deliveriesErr)
	}

	receipt := TeamsBatchReceipt{claimed: len(deliveries)}
	for _, delivery := range deliveries {
		nextReceipt := deliverTeamsDelivery(ctx, command, resolver, delivery, outbox, sender, receipt)
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

func deliverEmailDelivery(
	ctx context.Context,
	command EmailBatchCommand,
	delivery EmailDelivery,
	outbox EmailOutbox,
	sender EmailSender,
	receipt EmailBatchReceipt,
) EmailBatchReceipt {
	messageResult := emailMessage(delivery, command.publicURL)
	message, messageErr := messageResult.Value()
	if messageErr != nil {
		return markEmailFailure(ctx, outbox, delivery.intentID, command.now, messageErr.Error(), receipt)
	}

	sendResult := sender.SendEmail(ctx, message)
	sendReceipt, sendErr := sendResult.Value()
	if sendErr != nil {
		return markEmailFailure(ctx, outbox, delivery.intentID, command.now, sendErr.Error(), receipt)
	}

	markResult := outbox.MarkEmailDelivered(ctx, delivery.intentID, command.now, sendReceipt)
	_, markErr := markResult.Value()
	if markErr != nil {
		return EmailBatchReceipt{
			claimed:   receipt.claimed,
			delivered: receipt.delivered,
			failed:    receipt.failed + 1,
		}
	}

	return EmailBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered + 1,
		failed:    receipt.failed,
	}
}

func deliverDiscordDelivery(
	ctx context.Context,
	command DiscordBatchCommand,
	resolver outbound.Resolver,
	delivery DiscordDelivery,
	outbox DiscordOutbox,
	sender DiscordSender,
	receipt DiscordBatchReceipt,
) DiscordBatchReceipt {
	resolvedResult := outbound.ValidateResolvedDestination(ctx, resolver, delivery.destinationURL)
	_, resolvedErr := resolvedResult.Value()
	if resolvedErr != nil {
		sendReceipt := NewDiscordFailedReceipt(0, resolvedErr.Error())
		return markDiscordFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	messageResult := discordMessage(delivery, command.publicURL)
	message, messageErr := messageResult.Value()
	if messageErr != nil {
		sendReceipt := NewDiscordFailedReceipt(0, messageErr.Error())
		return markDiscordFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	sendResult := sender.SendDiscord(ctx, message)
	sendReceipt, sendErr := sendResult.Value()
	if sendErr != nil {
		failureReceipt := NewDiscordFailedReceipt(0, sendErr.Error())
		return markDiscordFailure(ctx, outbox, delivery.intentID, command.now, failureReceipt, receipt)
	}

	if !sendReceipt.Delivered() {
		return markDiscordFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	markResult := outbox.MarkDiscordDelivered(ctx, delivery.intentID, command.now, sendReceipt)
	_, markErr := markResult.Value()
	if markErr != nil {
		return DiscordBatchReceipt{
			claimed:   receipt.claimed,
			delivered: receipt.delivered,
			failed:    receipt.failed + 1,
		}
	}

	return DiscordBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered + 1,
		failed:    receipt.failed,
	}
}

func deliverGoogleChatDelivery(
	ctx context.Context,
	command GoogleChatBatchCommand,
	resolver outbound.Resolver,
	delivery GoogleChatDelivery,
	outbox GoogleChatOutbox,
	sender GoogleChatSender,
	receipt GoogleChatBatchReceipt,
) GoogleChatBatchReceipt {
	resolvedResult := outbound.ValidateResolvedDestination(ctx, resolver, delivery.destinationURL)
	_, resolvedErr := resolvedResult.Value()
	if resolvedErr != nil {
		sendReceipt := NewGoogleChatFailedReceipt(0, resolvedErr.Error())
		return markGoogleChatFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	messageResult := googleChatMessage(delivery, command.publicURL)
	message, messageErr := messageResult.Value()
	if messageErr != nil {
		sendReceipt := NewGoogleChatFailedReceipt(0, messageErr.Error())
		return markGoogleChatFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	sendResult := sender.SendGoogleChat(ctx, message)
	sendReceipt, sendErr := sendResult.Value()
	if sendErr != nil {
		failureReceipt := NewGoogleChatFailedReceipt(0, sendErr.Error())
		return markGoogleChatFailure(ctx, outbox, delivery.intentID, command.now, failureReceipt, receipt)
	}

	if !sendReceipt.Delivered() {
		return markGoogleChatFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	markResult := outbox.MarkGoogleChatDelivered(ctx, delivery.intentID, command.now, sendReceipt)
	_, markErr := markResult.Value()
	if markErr != nil {
		return GoogleChatBatchReceipt{
			claimed:   receipt.claimed,
			delivered: receipt.delivered,
			failed:    receipt.failed + 1,
		}
	}

	return GoogleChatBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered + 1,
		failed:    receipt.failed,
	}
}

func deliverNtfyDelivery(
	ctx context.Context,
	command NtfyBatchCommand,
	resolver outbound.Resolver,
	delivery NtfyDelivery,
	outbox NtfyOutbox,
	sender NtfySender,
	receipt NtfyBatchReceipt,
) NtfyBatchReceipt {
	resolvedResult := outbound.ValidateResolvedDestination(ctx, resolver, delivery.destinationURL)
	_, resolvedErr := resolvedResult.Value()
	if resolvedErr != nil {
		sendReceipt := NewNtfyFailedReceipt(0, resolvedErr.Error())
		return markNtfyFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	messageResult := ntfyMessage(delivery, command.publicURL)
	message, messageErr := messageResult.Value()
	if messageErr != nil {
		sendReceipt := NewNtfyFailedReceipt(0, messageErr.Error())
		return markNtfyFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	sendResult := sender.SendNtfy(ctx, message)
	sendReceipt, sendErr := sendResult.Value()
	if sendErr != nil {
		failureReceipt := NewNtfyFailedReceipt(0, sendErr.Error())
		return markNtfyFailure(ctx, outbox, delivery.intentID, command.now, failureReceipt, receipt)
	}

	if !sendReceipt.Delivered() {
		return markNtfyFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	markResult := outbox.MarkNtfyDelivered(ctx, delivery.intentID, command.now, sendReceipt)
	_, markErr := markResult.Value()
	if markErr != nil {
		return NtfyBatchReceipt{
			claimed:   receipt.claimed,
			delivered: receipt.delivered,
			failed:    receipt.failed + 1,
		}
	}

	return NtfyBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered + 1,
		failed:    receipt.failed,
	}
}

func deliverTeamsDelivery(
	ctx context.Context,
	command TeamsBatchCommand,
	resolver outbound.Resolver,
	delivery TeamsDelivery,
	outbox TeamsOutbox,
	sender TeamsSender,
	receipt TeamsBatchReceipt,
) TeamsBatchReceipt {
	resolvedResult := outbound.ValidateResolvedDestination(ctx, resolver, delivery.destinationURL)
	_, resolvedErr := resolvedResult.Value()
	if resolvedErr != nil {
		sendReceipt := NewTeamsFailedReceipt(0, resolvedErr.Error())
		return markTeamsFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	messageResult := teamsMessage(delivery, command.publicURL)
	message, messageErr := messageResult.Value()
	if messageErr != nil {
		sendReceipt := NewTeamsFailedReceipt(0, messageErr.Error())
		return markTeamsFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	sendResult := sender.SendTeams(ctx, message)
	sendReceipt, sendErr := sendResult.Value()
	if sendErr != nil {
		failureReceipt := NewTeamsFailedReceipt(0, sendErr.Error())
		return markTeamsFailure(ctx, outbox, delivery.intentID, command.now, failureReceipt, receipt)
	}

	if !sendReceipt.Delivered() {
		return markTeamsFailure(ctx, outbox, delivery.intentID, command.now, sendReceipt, receipt)
	}

	markResult := outbox.MarkTeamsDelivered(ctx, delivery.intentID, command.now, sendReceipt)
	_, markErr := markResult.Value()
	if markErr != nil {
		return TeamsBatchReceipt{
			claimed:   receipt.claimed,
			delivered: receipt.delivered,
			failed:    receipt.failed + 1,
		}
	}

	return TeamsBatchReceipt{
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

func emailMessage(
	delivery EmailDelivery,
	publicURL string,
) result.Result[EmailMessage] {
	openedResult := notifyplan.NewIssueOpened(
		delivery.event,
		delivery.issueID,
		delivery.issueShortID,
	)
	opened, openedErr := openedResult.Value()
	if openedErr != nil {
		return result.Err[EmailMessage](openedErr)
	}

	messageResult := notifyplan.EmailIssueOpenedMessage(opened, publicURL)
	message, messageErr := messageResult.Value()
	if messageErr != nil {
		return result.Err[EmailMessage](messageErr)
	}

	return result.Ok(NewEmailMessage(
		delivery.intentID,
		delivery.to,
		message.Subject(),
		message.Body(),
	))
}

func discordMessage(
	delivery DiscordDelivery,
	publicURL string,
) result.Result[DiscordMessage] {
	openedResult := notifyplan.NewIssueOpened(
		delivery.event,
		delivery.issueID,
		delivery.issueShortID,
	)
	opened, openedErr := openedResult.Value()
	if openedErr != nil {
		return result.Err[DiscordMessage](openedErr)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if baseURL == "" {
		return result.Err[DiscordMessage](errors.New("public url is required"))
	}

	payload := DiscordPayload{
		EventID:      opened.Event().EventID().String(),
		IssueID:      opened.IssueID().String(),
		IssueShortID: opened.IssueShortID(),
		Title:        opened.Event().Title().String(),
		Level:        opened.Event().Level().String(),
		Platform:     opened.Event().Platform(),
		IssueURL:     baseURL + "/issues/" + opened.IssueID().String(),
	}

	return result.Ok(NewDiscordMessage(delivery.intentID, delivery.destinationURL, payload))
}

func googleChatMessage(
	delivery GoogleChatDelivery,
	publicURL string,
) result.Result[GoogleChatMessage] {
	openedResult := notifyplan.NewIssueOpened(
		delivery.event,
		delivery.issueID,
		delivery.issueShortID,
	)
	opened, openedErr := openedResult.Value()
	if openedErr != nil {
		return result.Err[GoogleChatMessage](openedErr)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if baseURL == "" {
		return result.Err[GoogleChatMessage](errors.New("public url is required"))
	}

	payload := GoogleChatPayload{
		EventID:      opened.Event().EventID().String(),
		IssueID:      opened.IssueID().String(),
		IssueShortID: opened.IssueShortID(),
		Title:        opened.Event().Title().String(),
		Level:        opened.Event().Level().String(),
		Platform:     opened.Event().Platform(),
		IssueURL:     baseURL + "/issues/" + opened.IssueID().String(),
	}

	return result.Ok(NewGoogleChatMessage(delivery.intentID, delivery.destinationURL, payload))
}

func ntfyMessage(
	delivery NtfyDelivery,
	publicURL string,
) result.Result[NtfyMessage] {
	openedResult := notifyplan.NewIssueOpened(
		delivery.event,
		delivery.issueID,
		delivery.issueShortID,
	)
	opened, openedErr := openedResult.Value()
	if openedErr != nil {
		return result.Err[NtfyMessage](openedErr)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if baseURL == "" {
		return result.Err[NtfyMessage](errors.New("public url is required"))
	}

	payload := NtfyPayload{
		EventID:      opened.Event().EventID().String(),
		IssueID:      opened.IssueID().String(),
		IssueShortID: opened.IssueShortID(),
		Title:        opened.Event().Title().String(),
		Level:        opened.Event().Level().String(),
		Platform:     opened.Event().Platform(),
		IssueURL:     baseURL + "/issues/" + opened.IssueID().String(),
	}

	return result.Ok(NewNtfyMessage(delivery.intentID, delivery.destinationURL, delivery.topic, payload))
}

func teamsMessage(
	delivery TeamsDelivery,
	publicURL string,
) result.Result[TeamsMessage] {
	openedResult := notifyplan.NewIssueOpened(
		delivery.event,
		delivery.issueID,
		delivery.issueShortID,
	)
	opened, openedErr := openedResult.Value()
	if openedErr != nil {
		return result.Err[TeamsMessage](openedErr)
	}

	baseURL := strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if baseURL == "" {
		return result.Err[TeamsMessage](errors.New("public url is required"))
	}

	payload := TeamsPayload{
		EventID:      opened.Event().EventID().String(),
		IssueID:      opened.IssueID().String(),
		IssueShortID: opened.IssueShortID(),
		Title:        opened.Event().Title().String(),
		Level:        opened.Event().Level().String(),
		Platform:     opened.Event().Platform(),
		IssueURL:     baseURL + "/issues/" + opened.IssueID().String(),
	}

	return result.Ok(NewTeamsMessage(delivery.intentID, delivery.destinationURL, payload))
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

func markEmailFailure(
	ctx context.Context,
	outbox EmailOutbox,
	intentID domain.NotificationIntentID,
	now time.Time,
	reason string,
	receipt EmailBatchReceipt,
) EmailBatchReceipt {
	markResult := outbox.MarkEmailFailed(ctx, intentID, now, reason)
	_, _ = markResult.Value()

	return EmailBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered,
		failed:    receipt.failed + 1,
	}
}

func markDiscordFailure(
	ctx context.Context,
	outbox DiscordOutbox,
	intentID domain.NotificationIntentID,
	now time.Time,
	sendReceipt DiscordSendReceipt,
	receipt DiscordBatchReceipt,
) DiscordBatchReceipt {
	markResult := outbox.MarkDiscordFailed(ctx, intentID, now, sendReceipt)
	_, _ = markResult.Value()

	return DiscordBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered,
		failed:    receipt.failed + 1,
	}
}

func markGoogleChatFailure(
	ctx context.Context,
	outbox GoogleChatOutbox,
	intentID domain.NotificationIntentID,
	now time.Time,
	sendReceipt GoogleChatSendReceipt,
	receipt GoogleChatBatchReceipt,
) GoogleChatBatchReceipt {
	markResult := outbox.MarkGoogleChatFailed(ctx, intentID, now, sendReceipt)
	_, _ = markResult.Value()

	return GoogleChatBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered,
		failed:    receipt.failed + 1,
	}
}

func markNtfyFailure(
	ctx context.Context,
	outbox NtfyOutbox,
	intentID domain.NotificationIntentID,
	now time.Time,
	sendReceipt NtfySendReceipt,
	receipt NtfyBatchReceipt,
) NtfyBatchReceipt {
	markResult := outbox.MarkNtfyFailed(ctx, intentID, now, sendReceipt)
	_, _ = markResult.Value()

	return NtfyBatchReceipt{
		claimed:   receipt.claimed,
		delivered: receipt.delivered,
		failed:    receipt.failed + 1,
	}
}

func markTeamsFailure(
	ctx context.Context,
	outbox TeamsOutbox,
	intentID domain.NotificationIntentID,
	now time.Time,
	sendReceipt TeamsSendReceipt,
	receipt TeamsBatchReceipt,
) TeamsBatchReceipt {
	markResult := outbox.MarkTeamsFailed(ctx, intentID, now, sendReceipt)
	_, _ = markResult.Value()

	return TeamsBatchReceipt{
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

func (receipt EmailBatchReceipt) Claimed() int {
	return receipt.claimed
}

func (receipt EmailBatchReceipt) Delivered() int {
	return receipt.delivered
}

func (receipt EmailBatchReceipt) Failed() int {
	return receipt.failed
}

func (receipt DiscordBatchReceipt) Claimed() int {
	return receipt.claimed
}

func (receipt DiscordBatchReceipt) Delivered() int {
	return receipt.delivered
}

func (receipt DiscordBatchReceipt) Failed() int {
	return receipt.failed
}

func (receipt GoogleChatBatchReceipt) Claimed() int {
	return receipt.claimed
}

func (receipt GoogleChatBatchReceipt) Delivered() int {
	return receipt.delivered
}

func (receipt GoogleChatBatchReceipt) Failed() int {
	return receipt.failed
}

func (receipt NtfyBatchReceipt) Claimed() int {
	return receipt.claimed
}

func (receipt NtfyBatchReceipt) Delivered() int {
	return receipt.delivered
}

func (receipt NtfyBatchReceipt) Failed() int {
	return receipt.failed
}

func (receipt TeamsBatchReceipt) Claimed() int {
	return receipt.claimed
}

func (receipt TeamsBatchReceipt) Delivered() int {
	return receipt.delivered
}

func (receipt TeamsBatchReceipt) Failed() int {
	return receipt.failed
}

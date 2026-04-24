package notifications

import (
	"context"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type TelegramDelivery struct {
	intentID     domain.NotificationIntentID
	chatID       domain.TelegramChatID
	event        domain.CanonicalEvent
	issueID      domain.IssueID
	issueShortID int64
}

type TelegramMessage struct {
	intentID domain.NotificationIntentID
	chatID   domain.TelegramChatID
	text     domain.NotificationText
}

type TelegramSendReceipt struct {
	providerMessageID string
}

type WebhookDelivery struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	event          domain.CanonicalEvent
	issueID        domain.IssueID
	issueShortID   int64
}

type WebhookPayload struct {
	EventID      string
	IssueID      string
	IssueShortID int64
	Title        string
	Level        string
	Platform     string
	IssueURL     string
}

type WebhookMessage struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	payload        WebhookPayload
}

type WebhookSendReceipt struct {
	delivered bool
	status    int
	reason    string
}

type TelegramOutbox interface {
	ClaimTelegramDeliveries(
		ctx context.Context,
		now time.Time,
		limit int,
	) result.Result[[]TelegramDelivery]
	MarkTelegramDelivered(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt TelegramSendReceipt,
	) result.Result[struct{}]
	MarkTelegramFailed(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		reason string,
	) result.Result[struct{}]
}

type TelegramSender interface {
	SendTelegram(ctx context.Context, message TelegramMessage) result.Result[TelegramSendReceipt]
}

type WebhookOutbox interface {
	ClaimWebhookDeliveries(
		ctx context.Context,
		now time.Time,
		limit int,
	) result.Result[[]WebhookDelivery]
	MarkWebhookDelivered(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt WebhookSendReceipt,
	) result.Result[struct{}]
	MarkWebhookFailed(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt WebhookSendReceipt,
	) result.Result[struct{}]
}

type WebhookSender interface {
	SendWebhook(ctx context.Context, message WebhookMessage) result.Result[WebhookSendReceipt]
}

func NewTelegramDelivery(
	intentID domain.NotificationIntentID,
	chatID domain.TelegramChatID,
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) TelegramDelivery {
	return TelegramDelivery{
		intentID:     intentID,
		chatID:       chatID,
		event:        event,
		issueID:      issueID,
		issueShortID: issueShortID,
	}
}

func NewTelegramMessage(
	intentID domain.NotificationIntentID,
	chatID domain.TelegramChatID,
	text domain.NotificationText,
) TelegramMessage {
	return TelegramMessage{
		intentID: intentID,
		chatID:   chatID,
		text:     text,
	}
}

func NewTelegramSendReceipt(providerMessageID string) TelegramSendReceipt {
	return TelegramSendReceipt{providerMessageID: providerMessageID}
}

func NewWebhookDelivery(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) WebhookDelivery {
	return WebhookDelivery{
		intentID:       intentID,
		destinationURL: destinationURL,
		event:          event,
		issueID:        issueID,
		issueShortID:   issueShortID,
	}
}

func NewWebhookMessage(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	payload WebhookPayload,
) WebhookMessage {
	return WebhookMessage{
		intentID:       intentID,
		destinationURL: destinationURL,
		payload:        payload,
	}
}

func NewWebhookDeliveredReceipt(status int) WebhookSendReceipt {
	return WebhookSendReceipt{delivered: true, status: status}
}

func NewWebhookFailedReceipt(status int, reason string) WebhookSendReceipt {
	return WebhookSendReceipt{
		delivered: false,
		status:    status,
		reason:    reason,
	}
}

func (delivery TelegramDelivery) IntentID() domain.NotificationIntentID {
	return delivery.intentID
}

func (delivery TelegramDelivery) ChatID() domain.TelegramChatID {
	return delivery.chatID
}

func (delivery TelegramDelivery) Event() domain.CanonicalEvent {
	return delivery.event
}

func (delivery TelegramDelivery) IssueID() domain.IssueID {
	return delivery.issueID
}

func (delivery TelegramDelivery) IssueShortID() int64 {
	return delivery.issueShortID
}

func (message TelegramMessage) IntentID() domain.NotificationIntentID {
	return message.intentID
}

func (message TelegramMessage) ChatID() domain.TelegramChatID {
	return message.chatID
}

func (message TelegramMessage) Text() domain.NotificationText {
	return message.text
}

func (receipt TelegramSendReceipt) ProviderMessageID() string {
	return receipt.providerMessageID
}

func (delivery WebhookDelivery) IntentID() domain.NotificationIntentID {
	return delivery.intentID
}

func (delivery WebhookDelivery) DestinationURL() outbound.DestinationURL {
	return delivery.destinationURL
}

func (delivery WebhookDelivery) Event() domain.CanonicalEvent {
	return delivery.event
}

func (delivery WebhookDelivery) IssueID() domain.IssueID {
	return delivery.issueID
}

func (delivery WebhookDelivery) IssueShortID() int64 {
	return delivery.issueShortID
}

func (message WebhookMessage) IntentID() domain.NotificationIntentID {
	return message.intentID
}

func (message WebhookMessage) DestinationURL() outbound.DestinationURL {
	return message.destinationURL
}

func (message WebhookMessage) Payload() WebhookPayload {
	return message.payload
}

func (receipt WebhookSendReceipt) Delivered() bool {
	return receipt.delivered
}

func (receipt WebhookSendReceipt) Status() int {
	return receipt.status
}

func (receipt WebhookSendReceipt) Reason() string {
	return receipt.reason
}

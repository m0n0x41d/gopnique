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

type EmailDelivery struct {
	intentID     domain.NotificationIntentID
	to           domain.EmailAddress
	event        domain.CanonicalEvent
	issueID      domain.IssueID
	issueShortID int64
}

type EmailMessage struct {
	intentID domain.NotificationIntentID
	to       domain.EmailAddress
	subject  domain.NotificationSubject
	body     domain.NotificationText
}

type EmailSendReceipt struct {
	providerMessageID string
}

type DiscordDelivery struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	event          domain.CanonicalEvent
	issueID        domain.IssueID
	issueShortID   int64
}

type DiscordPayload struct {
	EventID      string
	IssueID      string
	IssueShortID int64
	Title        string
	Level        string
	Platform     string
	IssueURL     string
}

type DiscordMessage struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	payload        DiscordPayload
}

type DiscordSendReceipt struct {
	delivered bool
	status    int
	reason    string
}

type GoogleChatDelivery struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	event          domain.CanonicalEvent
	issueID        domain.IssueID
	issueShortID   int64
}

type GoogleChatPayload struct {
	EventID      string
	IssueID      string
	IssueShortID int64
	Title        string
	Level        string
	Platform     string
	IssueURL     string
}

type GoogleChatMessage struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	payload        GoogleChatPayload
}

type GoogleChatSendReceipt struct {
	delivered bool
	status    int
	reason    string
}

type NtfyDelivery struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	topic          domain.NtfyTopic
	event          domain.CanonicalEvent
	issueID        domain.IssueID
	issueShortID   int64
}

type NtfyPayload struct {
	EventID      string
	IssueID      string
	IssueShortID int64
	Title        string
	Level        string
	Platform     string
	IssueURL     string
}

type NtfyMessage struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	topic          domain.NtfyTopic
	payload        NtfyPayload
}

type NtfySendReceipt struct {
	delivered bool
	status    int
	reason    string
}

type TeamsDelivery struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	event          domain.CanonicalEvent
	issueID        domain.IssueID
	issueShortID   int64
}

type TeamsPayload struct {
	EventID      string
	IssueID      string
	IssueShortID int64
	Title        string
	Level        string
	Platform     string
	IssueURL     string
}

type TeamsMessage struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	payload        TeamsPayload
}

type TeamsSendReceipt struct {
	delivered bool
	status    int
	reason    string
}

type ZulipDelivery struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	botEmail       domain.ZulipBotEmail
	apiKey         domain.ZulipAPIKey
	stream         domain.ZulipStreamName
	topic          domain.ZulipTopicName
	event          domain.CanonicalEvent
	issueID        domain.IssueID
	issueShortID   int64
}

type ZulipPayload struct {
	EventID      string
	IssueID      string
	IssueShortID int64
	Title        string
	Level        string
	Platform     string
	IssueURL     string
}

type ZulipMessage struct {
	intentID       domain.NotificationIntentID
	destinationURL outbound.DestinationURL
	botEmail       domain.ZulipBotEmail
	apiKey         domain.ZulipAPIKey
	stream         domain.ZulipStreamName
	topic          domain.ZulipTopicName
	payload        ZulipPayload
}

type ZulipSendReceipt struct {
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

type EmailOutbox interface {
	ClaimEmailDeliveries(
		ctx context.Context,
		now time.Time,
		limit int,
	) result.Result[[]EmailDelivery]
	MarkEmailDelivered(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt EmailSendReceipt,
	) result.Result[struct{}]
	MarkEmailFailed(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		reason string,
	) result.Result[struct{}]
}

type EmailSender interface {
	SendEmail(ctx context.Context, message EmailMessage) result.Result[EmailSendReceipt]
}

type DiscordOutbox interface {
	ClaimDiscordDeliveries(
		ctx context.Context,
		now time.Time,
		limit int,
	) result.Result[[]DiscordDelivery]
	MarkDiscordDelivered(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt DiscordSendReceipt,
	) result.Result[struct{}]
	MarkDiscordFailed(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt DiscordSendReceipt,
	) result.Result[struct{}]
}

type DiscordSender interface {
	SendDiscord(ctx context.Context, message DiscordMessage) result.Result[DiscordSendReceipt]
}

type GoogleChatOutbox interface {
	ClaimGoogleChatDeliveries(
		ctx context.Context,
		now time.Time,
		limit int,
	) result.Result[[]GoogleChatDelivery]
	MarkGoogleChatDelivered(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt GoogleChatSendReceipt,
	) result.Result[struct{}]
	MarkGoogleChatFailed(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt GoogleChatSendReceipt,
	) result.Result[struct{}]
}

type GoogleChatSender interface {
	SendGoogleChat(ctx context.Context, message GoogleChatMessage) result.Result[GoogleChatSendReceipt]
}

type NtfyOutbox interface {
	ClaimNtfyDeliveries(
		ctx context.Context,
		now time.Time,
		limit int,
	) result.Result[[]NtfyDelivery]
	MarkNtfyDelivered(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt NtfySendReceipt,
	) result.Result[struct{}]
	MarkNtfyFailed(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt NtfySendReceipt,
	) result.Result[struct{}]
}

type NtfySender interface {
	SendNtfy(ctx context.Context, message NtfyMessage) result.Result[NtfySendReceipt]
}

type TeamsOutbox interface {
	ClaimTeamsDeliveries(
		ctx context.Context,
		now time.Time,
		limit int,
	) result.Result[[]TeamsDelivery]
	MarkTeamsDelivered(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt TeamsSendReceipt,
	) result.Result[struct{}]
	MarkTeamsFailed(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt TeamsSendReceipt,
	) result.Result[struct{}]
}

type TeamsSender interface {
	SendTeams(ctx context.Context, message TeamsMessage) result.Result[TeamsSendReceipt]
}

type ZulipOutbox interface {
	ClaimZulipDeliveries(
		ctx context.Context,
		now time.Time,
		limit int,
	) result.Result[[]ZulipDelivery]
	MarkZulipDelivered(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt ZulipSendReceipt,
	) result.Result[struct{}]
	MarkZulipFailed(
		ctx context.Context,
		intentID domain.NotificationIntentID,
		now time.Time,
		receipt ZulipSendReceipt,
	) result.Result[struct{}]
}

type ZulipSender interface {
	SendZulip(ctx context.Context, message ZulipMessage) result.Result[ZulipSendReceipt]
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

func NewEmailDelivery(
	intentID domain.NotificationIntentID,
	to domain.EmailAddress,
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) EmailDelivery {
	return EmailDelivery{
		intentID:     intentID,
		to:           to,
		event:        event,
		issueID:      issueID,
		issueShortID: issueShortID,
	}
}

func NewEmailMessage(
	intentID domain.NotificationIntentID,
	to domain.EmailAddress,
	subject domain.NotificationSubject,
	body domain.NotificationText,
) EmailMessage {
	return EmailMessage{
		intentID: intentID,
		to:       to,
		subject:  subject,
		body:     body,
	}
}

func NewEmailSendReceipt(providerMessageID string) EmailSendReceipt {
	return EmailSendReceipt{providerMessageID: providerMessageID}
}

func NewDiscordDelivery(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) DiscordDelivery {
	return DiscordDelivery{
		intentID:       intentID,
		destinationURL: destinationURL,
		event:          event,
		issueID:        issueID,
		issueShortID:   issueShortID,
	}
}

func NewDiscordMessage(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	payload DiscordPayload,
) DiscordMessage {
	return DiscordMessage{
		intentID:       intentID,
		destinationURL: destinationURL,
		payload:        payload,
	}
}

func NewDiscordDeliveredReceipt(status int) DiscordSendReceipt {
	return DiscordSendReceipt{delivered: true, status: status}
}

func NewDiscordFailedReceipt(status int, reason string) DiscordSendReceipt {
	return DiscordSendReceipt{
		delivered: false,
		status:    status,
		reason:    reason,
	}
}

func NewGoogleChatDelivery(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) GoogleChatDelivery {
	return GoogleChatDelivery{
		intentID:       intentID,
		destinationURL: destinationURL,
		event:          event,
		issueID:        issueID,
		issueShortID:   issueShortID,
	}
}

func NewGoogleChatMessage(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	payload GoogleChatPayload,
) GoogleChatMessage {
	return GoogleChatMessage{
		intentID:       intentID,
		destinationURL: destinationURL,
		payload:        payload,
	}
}

func NewGoogleChatDeliveredReceipt(status int) GoogleChatSendReceipt {
	return GoogleChatSendReceipt{delivered: true, status: status}
}

func NewGoogleChatFailedReceipt(status int, reason string) GoogleChatSendReceipt {
	return GoogleChatSendReceipt{
		delivered: false,
		status:    status,
		reason:    reason,
	}
}

func NewNtfyDelivery(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	topic domain.NtfyTopic,
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) NtfyDelivery {
	return NtfyDelivery{
		intentID:       intentID,
		destinationURL: destinationURL,
		topic:          topic,
		event:          event,
		issueID:        issueID,
		issueShortID:   issueShortID,
	}
}

func NewNtfyMessage(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	topic domain.NtfyTopic,
	payload NtfyPayload,
) NtfyMessage {
	return NtfyMessage{
		intentID:       intentID,
		destinationURL: destinationURL,
		topic:          topic,
		payload:        payload,
	}
}

func NewNtfyDeliveredReceipt(status int) NtfySendReceipt {
	return NtfySendReceipt{delivered: true, status: status}
}

func NewNtfyFailedReceipt(status int, reason string) NtfySendReceipt {
	return NtfySendReceipt{
		delivered: false,
		status:    status,
		reason:    reason,
	}
}

func NewTeamsDelivery(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) TeamsDelivery {
	return TeamsDelivery{
		intentID:       intentID,
		destinationURL: destinationURL,
		event:          event,
		issueID:        issueID,
		issueShortID:   issueShortID,
	}
}

func NewTeamsMessage(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	payload TeamsPayload,
) TeamsMessage {
	return TeamsMessage{
		intentID:       intentID,
		destinationURL: destinationURL,
		payload:        payload,
	}
}

func NewTeamsDeliveredReceipt(status int) TeamsSendReceipt {
	return TeamsSendReceipt{delivered: true, status: status}
}

func NewTeamsFailedReceipt(status int, reason string) TeamsSendReceipt {
	return TeamsSendReceipt{
		delivered: false,
		status:    status,
		reason:    reason,
	}
}

func NewZulipDelivery(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	botEmail domain.ZulipBotEmail,
	apiKey domain.ZulipAPIKey,
	stream domain.ZulipStreamName,
	topic domain.ZulipTopicName,
	event domain.CanonicalEvent,
	issueID domain.IssueID,
	issueShortID int64,
) ZulipDelivery {
	return ZulipDelivery{
		intentID:       intentID,
		destinationURL: destinationURL,
		botEmail:       botEmail,
		apiKey:         apiKey,
		stream:         stream,
		topic:          topic,
		event:          event,
		issueID:        issueID,
		issueShortID:   issueShortID,
	}
}

func NewZulipMessage(
	intentID domain.NotificationIntentID,
	destinationURL outbound.DestinationURL,
	botEmail domain.ZulipBotEmail,
	apiKey domain.ZulipAPIKey,
	stream domain.ZulipStreamName,
	topic domain.ZulipTopicName,
	payload ZulipPayload,
) ZulipMessage {
	return ZulipMessage{
		intentID:       intentID,
		destinationURL: destinationURL,
		botEmail:       botEmail,
		apiKey:         apiKey,
		stream:         stream,
		topic:          topic,
		payload:        payload,
	}
}

func NewZulipDeliveredReceipt(status int) ZulipSendReceipt {
	return ZulipSendReceipt{delivered: true, status: status}
}

func NewZulipFailedReceipt(status int, reason string) ZulipSendReceipt {
	return ZulipSendReceipt{
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

func (delivery EmailDelivery) IntentID() domain.NotificationIntentID {
	return delivery.intentID
}

func (delivery EmailDelivery) To() domain.EmailAddress {
	return delivery.to
}

func (delivery EmailDelivery) Event() domain.CanonicalEvent {
	return delivery.event
}

func (delivery EmailDelivery) IssueID() domain.IssueID {
	return delivery.issueID
}

func (delivery EmailDelivery) IssueShortID() int64 {
	return delivery.issueShortID
}

func (message EmailMessage) IntentID() domain.NotificationIntentID {
	return message.intentID
}

func (message EmailMessage) To() domain.EmailAddress {
	return message.to
}

func (message EmailMessage) Subject() domain.NotificationSubject {
	return message.subject
}

func (message EmailMessage) Body() domain.NotificationText {
	return message.body
}

func (receipt EmailSendReceipt) ProviderMessageID() string {
	return receipt.providerMessageID
}

func (delivery DiscordDelivery) IntentID() domain.NotificationIntentID {
	return delivery.intentID
}

func (delivery DiscordDelivery) DestinationURL() outbound.DestinationURL {
	return delivery.destinationURL
}

func (delivery DiscordDelivery) Event() domain.CanonicalEvent {
	return delivery.event
}

func (delivery DiscordDelivery) IssueID() domain.IssueID {
	return delivery.issueID
}

func (delivery DiscordDelivery) IssueShortID() int64 {
	return delivery.issueShortID
}

func (message DiscordMessage) IntentID() domain.NotificationIntentID {
	return message.intentID
}

func (message DiscordMessage) DestinationURL() outbound.DestinationURL {
	return message.destinationURL
}

func (message DiscordMessage) Payload() DiscordPayload {
	return message.payload
}

func (receipt DiscordSendReceipt) Delivered() bool {
	return receipt.delivered
}

func (receipt DiscordSendReceipt) Status() int {
	return receipt.status
}

func (receipt DiscordSendReceipt) Reason() string {
	return receipt.reason
}

func (delivery GoogleChatDelivery) IntentID() domain.NotificationIntentID {
	return delivery.intentID
}

func (delivery GoogleChatDelivery) DestinationURL() outbound.DestinationURL {
	return delivery.destinationURL
}

func (delivery GoogleChatDelivery) Event() domain.CanonicalEvent {
	return delivery.event
}

func (delivery GoogleChatDelivery) IssueID() domain.IssueID {
	return delivery.issueID
}

func (delivery GoogleChatDelivery) IssueShortID() int64 {
	return delivery.issueShortID
}

func (message GoogleChatMessage) IntentID() domain.NotificationIntentID {
	return message.intentID
}

func (message GoogleChatMessage) DestinationURL() outbound.DestinationURL {
	return message.destinationURL
}

func (message GoogleChatMessage) Payload() GoogleChatPayload {
	return message.payload
}

func (receipt GoogleChatSendReceipt) Delivered() bool {
	return receipt.delivered
}

func (receipt GoogleChatSendReceipt) Status() int {
	return receipt.status
}

func (receipt GoogleChatSendReceipt) Reason() string {
	return receipt.reason
}

func (delivery NtfyDelivery) IntentID() domain.NotificationIntentID {
	return delivery.intentID
}

func (delivery NtfyDelivery) DestinationURL() outbound.DestinationURL {
	return delivery.destinationURL
}

func (delivery NtfyDelivery) Topic() domain.NtfyTopic {
	return delivery.topic
}

func (delivery NtfyDelivery) Event() domain.CanonicalEvent {
	return delivery.event
}

func (delivery NtfyDelivery) IssueID() domain.IssueID {
	return delivery.issueID
}

func (delivery NtfyDelivery) IssueShortID() int64 {
	return delivery.issueShortID
}

func (message NtfyMessage) IntentID() domain.NotificationIntentID {
	return message.intentID
}

func (message NtfyMessage) DestinationURL() outbound.DestinationURL {
	return message.destinationURL
}

func (message NtfyMessage) Topic() domain.NtfyTopic {
	return message.topic
}

func (message NtfyMessage) Payload() NtfyPayload {
	return message.payload
}

func (receipt NtfySendReceipt) Delivered() bool {
	return receipt.delivered
}

func (receipt NtfySendReceipt) Status() int {
	return receipt.status
}

func (receipt NtfySendReceipt) Reason() string {
	return receipt.reason
}

func (delivery TeamsDelivery) IntentID() domain.NotificationIntentID {
	return delivery.intentID
}

func (delivery TeamsDelivery) DestinationURL() outbound.DestinationURL {
	return delivery.destinationURL
}

func (delivery TeamsDelivery) Event() domain.CanonicalEvent {
	return delivery.event
}

func (delivery TeamsDelivery) IssueID() domain.IssueID {
	return delivery.issueID
}

func (delivery TeamsDelivery) IssueShortID() int64 {
	return delivery.issueShortID
}

func (message TeamsMessage) IntentID() domain.NotificationIntentID {
	return message.intentID
}

func (message TeamsMessage) DestinationURL() outbound.DestinationURL {
	return message.destinationURL
}

func (message TeamsMessage) Payload() TeamsPayload {
	return message.payload
}

func (receipt TeamsSendReceipt) Delivered() bool {
	return receipt.delivered
}

func (receipt TeamsSendReceipt) Status() int {
	return receipt.status
}

func (receipt TeamsSendReceipt) Reason() string {
	return receipt.reason
}

func (delivery ZulipDelivery) IntentID() domain.NotificationIntentID {
	return delivery.intentID
}

func (delivery ZulipDelivery) DestinationURL() outbound.DestinationURL {
	return delivery.destinationURL
}

func (delivery ZulipDelivery) BotEmail() domain.ZulipBotEmail {
	return delivery.botEmail
}

func (delivery ZulipDelivery) APIKey() domain.ZulipAPIKey {
	return delivery.apiKey
}

func (delivery ZulipDelivery) Stream() domain.ZulipStreamName {
	return delivery.stream
}

func (delivery ZulipDelivery) Topic() domain.ZulipTopicName {
	return delivery.topic
}

func (delivery ZulipDelivery) Event() domain.CanonicalEvent {
	return delivery.event
}

func (delivery ZulipDelivery) IssueID() domain.IssueID {
	return delivery.issueID
}

func (delivery ZulipDelivery) IssueShortID() int64 {
	return delivery.issueShortID
}

func (message ZulipMessage) IntentID() domain.NotificationIntentID {
	return message.intentID
}

func (message ZulipMessage) DestinationURL() outbound.DestinationURL {
	return message.destinationURL
}

func (message ZulipMessage) BotEmail() domain.ZulipBotEmail {
	return message.botEmail
}

func (message ZulipMessage) APIKey() domain.ZulipAPIKey {
	return message.apiKey
}

func (message ZulipMessage) Stream() domain.ZulipStreamName {
	return message.stream
}

func (message ZulipMessage) Topic() domain.ZulipTopicName {
	return message.topic
}

func (message ZulipMessage) Payload() ZulipPayload {
	return message.payload
}

func (receipt ZulipSendReceipt) Delivered() bool {
	return receipt.delivered
}

func (receipt ZulipSendReceipt) Status() int {
	return receipt.status
}

func (receipt ZulipSendReceipt) Reason() string {
	return receipt.reason
}

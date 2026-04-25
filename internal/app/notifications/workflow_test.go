package notifications

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestDeliverTelegramBatchSendsAndMarksDelivered(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeOutbox{deliveries: []TelegramDelivery{telegramDelivery(t)}}
	sender := &fakeSender{}
	command := telegramBatchCommand(t, now)

	batchResult := DeliverTelegramBatch(context.Background(), command, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 1 || receipt.Failed() != 0 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.delivered != 1 {
		t.Fatalf("expected delivered mark, got %d", outbox.delivered)
	}

	if sender.sentText == "" {
		t.Fatal("expected telegram message")
	}
}

func TestDeliverTelegramBatchMarksFailedAfterSendError(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeOutbox{deliveries: []TelegramDelivery{telegramDelivery(t)}}
	sender := &fakeSender{sendErr: errors.New("telegram unavailable")}
	command := telegramBatchCommand(t, now)

	batchResult := DeliverTelegramBatch(context.Background(), command, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 0 || receipt.Failed() != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.failed != 1 {
		t.Fatalf("expected failed mark, got %d", outbox.failed)
	}
}

func TestDeliverWebhookBatchSendsAndMarksDelivered(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeWebhookOutbox{deliveries: []WebhookDelivery{webhookDelivery(t, "https://hooks.example.com/issues")}}
	resolver := fakeResolver{"hooks.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	sender := &fakeWebhookSender{receipt: NewWebhookDeliveredReceipt(204)}
	command := webhookBatchCommand(t, now)

	batchResult := DeliverWebhookBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 1 || receipt.Failed() != 0 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.deliveredStatus != 204 {
		t.Fatalf("expected delivered status 204, got %d", outbox.deliveredStatus)
	}

	if sender.payload.EventID == "" || sender.payload.IssueURL == "" {
		t.Fatalf("expected webhook payload, got %#v", sender.payload)
	}
}

func TestDeliverWebhookBatchRejectsUnsafeSendTimeResolution(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeWebhookOutbox{deliveries: []WebhookDelivery{webhookDelivery(t, "https://hooks.example.com/issues")}}
	resolver := fakeResolver{"hooks.example.com": []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	sender := &fakeWebhookSender{receipt: NewWebhookDeliveredReceipt(204)}
	command := webhookBatchCommand(t, now)

	batchResult := DeliverWebhookBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 0 || receipt.Failed() != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if sender.sent != 0 {
		t.Fatalf("expected no webhook send, got %d", sender.sent)
	}

	if outbox.failedReason == "" {
		t.Fatal("expected webhook failure reason")
	}
}

func TestDeliverWebhookBatchRecordsProviderFailureStatus(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeWebhookOutbox{deliveries: []WebhookDelivery{webhookDelivery(t, "https://hooks.example.com/issues")}}
	resolver := fakeResolver{"hooks.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	sender := &fakeWebhookSender{receipt: NewWebhookFailedReceipt(500, "webhook returned HTTP 500")}
	command := webhookBatchCommand(t, now)

	batchResult := DeliverWebhookBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 0 || receipt.Failed() != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.failedStatus != 500 {
		t.Fatalf("expected failed status 500, got %d", outbox.failedStatus)
	}
}

func TestDeliverEmailBatchSendsAndMarksDelivered(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeEmailOutbox{deliveries: []EmailDelivery{emailDelivery(t)}}
	sender := &fakeEmailSender{receipt: NewEmailSendReceipt("<intent@example.test>")}
	command := emailBatchCommand(t, now)

	batchResult := DeliverEmailBatch(context.Background(), command, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 1 || receipt.Failed() != 0 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.deliveredMessageID != "<intent@example.test>" {
		t.Fatalf("expected delivered message id, got %q", outbox.deliveredMessageID)
	}

	if sender.subject == "" || sender.body == "" {
		t.Fatalf("expected email message, got subject=%q body=%q", sender.subject, sender.body)
	}
}

func TestDeliverEmailBatchMarksFailedAfterSendError(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeEmailOutbox{deliveries: []EmailDelivery{emailDelivery(t)}}
	sender := &fakeEmailSender{sendErr: errors.New("smtp unavailable")}
	command := emailBatchCommand(t, now)

	batchResult := DeliverEmailBatch(context.Background(), command, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 0 || receipt.Failed() != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.failedReason != "smtp unavailable" {
		t.Fatalf("expected failure reason, got %q", outbox.failedReason)
	}
}

func TestDeliverDiscordBatchSendsAndMarksDelivered(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeDiscordOutbox{deliveries: []DiscordDelivery{discordDelivery(t, "https://discord.example.com/webhook")}}
	resolver := fakeResolver{"discord.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	sender := &fakeDiscordSender{receipt: NewDiscordDeliveredReceipt(204)}
	command := discordBatchCommand(t, now)

	batchResult := DeliverDiscordBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 1 || receipt.Failed() != 0 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.deliveredStatus != 204 {
		t.Fatalf("expected delivered status 204, got %d", outbox.deliveredStatus)
	}

	if sender.payload.EventID == "" || sender.payload.IssueURL == "" {
		t.Fatalf("expected discord payload, got %#v", sender.payload)
	}
}

func TestDeliverDiscordBatchRejectsUnsafeSendTimeResolution(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeDiscordOutbox{deliveries: []DiscordDelivery{discordDelivery(t, "https://discord.example.com/webhook")}}
	resolver := fakeResolver{"discord.example.com": []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	sender := &fakeDiscordSender{receipt: NewDiscordDeliveredReceipt(204)}
	command := discordBatchCommand(t, now)

	batchResult := DeliverDiscordBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 0 || receipt.Failed() != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if sender.sent != 0 {
		t.Fatalf("expected no discord send, got %d", sender.sent)
	}

	if outbox.failedReason == "" {
		t.Fatal("expected discord failure reason")
	}
}

func TestDeliverGoogleChatBatchSendsAndMarksDelivered(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeGoogleChatOutbox{deliveries: []GoogleChatDelivery{googleChatDelivery(t, "https://chat.example.com/webhook")}}
	resolver := fakeResolver{"chat.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	sender := &fakeGoogleChatSender{receipt: NewGoogleChatDeliveredReceipt(200)}
	command := googleChatBatchCommand(t, now)

	batchResult := DeliverGoogleChatBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 1 || receipt.Failed() != 0 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.deliveredStatus != 200 {
		t.Fatalf("expected delivered status 200, got %d", outbox.deliveredStatus)
	}

	if sender.payload.EventID == "" || sender.payload.IssueURL == "" {
		t.Fatalf("expected google chat payload, got %#v", sender.payload)
	}
}

func TestDeliverGoogleChatBatchRejectsUnsafeSendTimeResolution(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeGoogleChatOutbox{deliveries: []GoogleChatDelivery{googleChatDelivery(t, "https://chat.example.com/webhook")}}
	resolver := fakeResolver{"chat.example.com": []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	sender := &fakeGoogleChatSender{receipt: NewGoogleChatDeliveredReceipt(200)}
	command := googleChatBatchCommand(t, now)

	batchResult := DeliverGoogleChatBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 0 || receipt.Failed() != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if sender.sent != 0 {
		t.Fatalf("expected no google chat send, got %d", sender.sent)
	}

	if outbox.failedReason == "" {
		t.Fatal("expected google chat failure reason")
	}
}

func TestDeliverNtfyBatchSendsAndMarksDelivered(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeNtfyOutbox{deliveries: []NtfyDelivery{ntfyDelivery(t, "https://ntfy.example.com")}}
	resolver := fakeResolver{"ntfy.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	sender := &fakeNtfySender{receipt: NewNtfyDeliveredReceipt(200)}
	command := ntfyBatchCommand(t, now)

	batchResult := DeliverNtfyBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 1 || receipt.Failed() != 0 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.deliveredStatus != 200 {
		t.Fatalf("expected delivered status 200, got %d", outbox.deliveredStatus)
	}

	if sender.payload.EventID == "" || sender.payload.IssueURL == "" || sender.topic == "" {
		t.Fatalf("expected ntfy payload, got payload=%#v topic=%q", sender.payload, sender.topic)
	}
}

func TestDeliverNtfyBatchRejectsUnsafeSendTimeResolution(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeNtfyOutbox{deliveries: []NtfyDelivery{ntfyDelivery(t, "https://ntfy.example.com")}}
	resolver := fakeResolver{"ntfy.example.com": []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	sender := &fakeNtfySender{receipt: NewNtfyDeliveredReceipt(200)}
	command := ntfyBatchCommand(t, now)

	batchResult := DeliverNtfyBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 0 || receipt.Failed() != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if sender.sent != 0 {
		t.Fatalf("expected no ntfy send, got %d", sender.sent)
	}

	if outbox.failedReason == "" {
		t.Fatal("expected ntfy failure reason")
	}
}

func TestDeliverTeamsBatchSendsAndMarksDelivered(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeTeamsOutbox{deliveries: []TeamsDelivery{teamsDelivery(t, "https://teams.example.com/webhook")}}
	resolver := fakeResolver{"teams.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	sender := &fakeTeamsSender{receipt: NewTeamsDeliveredReceipt(200)}
	command := teamsBatchCommand(t, now)

	batchResult := DeliverTeamsBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 1 || receipt.Failed() != 0 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if outbox.deliveredStatus != 200 {
		t.Fatalf("expected delivered status 200, got %d", outbox.deliveredStatus)
	}

	if sender.payload.EventID == "" || sender.payload.IssueURL == "" {
		t.Fatalf("expected teams payload, got %#v", sender.payload)
	}
}

func TestDeliverTeamsBatchRejectsUnsafeSendTimeResolution(t *testing.T) {
	now := time.Date(2026, 4, 24, 14, 0, 0, 0, time.UTC)
	outbox := &fakeTeamsOutbox{deliveries: []TeamsDelivery{teamsDelivery(t, "https://teams.example.com/webhook")}}
	resolver := fakeResolver{"teams.example.com": []netip.Addr{netip.MustParseAddr("10.0.0.10")}}
	sender := &fakeTeamsSender{receipt: NewTeamsDeliveredReceipt(200)}
	command := teamsBatchCommand(t, now)

	batchResult := DeliverTeamsBatch(context.Background(), command, resolver, outbox, sender)
	receipt, receiptErr := batchResult.Value()
	if receiptErr != nil {
		t.Fatalf("batch: %v", receiptErr)
	}

	if receipt.Claimed() != 1 || receipt.Delivered() != 0 || receipt.Failed() != 1 {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if sender.sent != 0 {
		t.Fatalf("expected no teams send, got %d", sender.sent)
	}

	if outbox.failedReason == "" {
		t.Fatal("expected teams failure reason")
	}
}

type fakeOutbox struct {
	deliveries []TelegramDelivery
	delivered  int
	failed     int
}

func (outbox *fakeOutbox) ClaimTelegramDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]TelegramDelivery] {
	return result.Ok(outbox.deliveries)
}

func (outbox *fakeOutbox) MarkTelegramDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt TelegramSendReceipt,
) result.Result[struct{}] {
	outbox.delivered++

	return result.Ok(struct{}{})
}

func (outbox *fakeOutbox) MarkTelegramFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	reason string,
) result.Result[struct{}] {
	outbox.failed++

	return result.Ok(struct{}{})
}

type fakeWebhookOutbox struct {
	deliveries      []WebhookDelivery
	deliveredStatus int
	failedStatus    int
	failedReason    string
}

func (outbox *fakeWebhookOutbox) ClaimWebhookDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]WebhookDelivery] {
	return result.Ok(outbox.deliveries)
}

func (outbox *fakeWebhookOutbox) MarkWebhookDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt WebhookSendReceipt,
) result.Result[struct{}] {
	outbox.deliveredStatus = receipt.Status()

	return result.Ok(struct{}{})
}

func (outbox *fakeWebhookOutbox) MarkWebhookFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt WebhookSendReceipt,
) result.Result[struct{}] {
	outbox.failedStatus = receipt.Status()
	outbox.failedReason = receipt.Reason()

	return result.Ok(struct{}{})
}

type fakeEmailOutbox struct {
	deliveries         []EmailDelivery
	deliveredMessageID string
	failedReason       string
}

func (outbox *fakeEmailOutbox) ClaimEmailDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]EmailDelivery] {
	return result.Ok(outbox.deliveries)
}

func (outbox *fakeEmailOutbox) MarkEmailDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt EmailSendReceipt,
) result.Result[struct{}] {
	outbox.deliveredMessageID = receipt.ProviderMessageID()

	return result.Ok(struct{}{})
}

func (outbox *fakeEmailOutbox) MarkEmailFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	reason string,
) result.Result[struct{}] {
	outbox.failedReason = reason

	return result.Ok(struct{}{})
}

type fakeDiscordOutbox struct {
	deliveries      []DiscordDelivery
	deliveredStatus int
	failedStatus    int
	failedReason    string
}

func (outbox *fakeDiscordOutbox) ClaimDiscordDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]DiscordDelivery] {
	return result.Ok(outbox.deliveries)
}

func (outbox *fakeDiscordOutbox) MarkDiscordDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt DiscordSendReceipt,
) result.Result[struct{}] {
	outbox.deliveredStatus = receipt.Status()

	return result.Ok(struct{}{})
}

func (outbox *fakeDiscordOutbox) MarkDiscordFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt DiscordSendReceipt,
) result.Result[struct{}] {
	outbox.failedStatus = receipt.Status()
	outbox.failedReason = receipt.Reason()

	return result.Ok(struct{}{})
}

type fakeGoogleChatOutbox struct {
	deliveries      []GoogleChatDelivery
	deliveredStatus int
	failedStatus    int
	failedReason    string
}

func (outbox *fakeGoogleChatOutbox) ClaimGoogleChatDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]GoogleChatDelivery] {
	return result.Ok(outbox.deliveries)
}

func (outbox *fakeGoogleChatOutbox) MarkGoogleChatDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt GoogleChatSendReceipt,
) result.Result[struct{}] {
	outbox.deliveredStatus = receipt.Status()

	return result.Ok(struct{}{})
}

func (outbox *fakeGoogleChatOutbox) MarkGoogleChatFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt GoogleChatSendReceipt,
) result.Result[struct{}] {
	outbox.failedStatus = receipt.Status()
	outbox.failedReason = receipt.Reason()

	return result.Ok(struct{}{})
}

type fakeNtfyOutbox struct {
	deliveries      []NtfyDelivery
	deliveredStatus int
	failedStatus    int
	failedReason    string
}

func (outbox *fakeNtfyOutbox) ClaimNtfyDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]NtfyDelivery] {
	return result.Ok(outbox.deliveries)
}

func (outbox *fakeNtfyOutbox) MarkNtfyDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt NtfySendReceipt,
) result.Result[struct{}] {
	outbox.deliveredStatus = receipt.Status()

	return result.Ok(struct{}{})
}

func (outbox *fakeNtfyOutbox) MarkNtfyFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt NtfySendReceipt,
) result.Result[struct{}] {
	outbox.failedStatus = receipt.Status()
	outbox.failedReason = receipt.Reason()

	return result.Ok(struct{}{})
}

type fakeTeamsOutbox struct {
	deliveries      []TeamsDelivery
	deliveredStatus int
	failedStatus    int
	failedReason    string
}

func (outbox *fakeTeamsOutbox) ClaimTeamsDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]TeamsDelivery] {
	return result.Ok(outbox.deliveries)
}

func (outbox *fakeTeamsOutbox) MarkTeamsDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt TeamsSendReceipt,
) result.Result[struct{}] {
	outbox.deliveredStatus = receipt.Status()

	return result.Ok(struct{}{})
}

func (outbox *fakeTeamsOutbox) MarkTeamsFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt TeamsSendReceipt,
) result.Result[struct{}] {
	outbox.failedStatus = receipt.Status()
	outbox.failedReason = receipt.Reason()

	return result.Ok(struct{}{})
}

type fakeSender struct {
	sendErr  error
	sentText string
}

func (sender *fakeSender) SendTelegram(
	ctx context.Context,
	message TelegramMessage,
) result.Result[TelegramSendReceipt] {
	sender.sentText = message.Text().String()

	if sender.sendErr != nil {
		return result.Err[TelegramSendReceipt](sender.sendErr)
	}

	return result.Ok(NewTelegramSendReceipt("telegram-message-1"))
}

type fakeWebhookSender struct {
	receipt WebhookSendReceipt
	sent    int
	payload WebhookPayload
}

func (sender *fakeWebhookSender) SendWebhook(
	ctx context.Context,
	message WebhookMessage,
) result.Result[WebhookSendReceipt] {
	sender.sent++
	sender.payload = message.Payload()

	return result.Ok(sender.receipt)
}

type fakeEmailSender struct {
	receipt EmailSendReceipt
	sendErr error
	subject string
	body    string
}

func (sender *fakeEmailSender) SendEmail(
	ctx context.Context,
	message EmailMessage,
) result.Result[EmailSendReceipt] {
	sender.subject = message.Subject().String()
	sender.body = message.Body().String()

	if sender.sendErr != nil {
		return result.Err[EmailSendReceipt](sender.sendErr)
	}

	return result.Ok(sender.receipt)
}

type fakeDiscordSender struct {
	receipt DiscordSendReceipt
	sent    int
	payload DiscordPayload
}

func (sender *fakeDiscordSender) SendDiscord(
	ctx context.Context,
	message DiscordMessage,
) result.Result[DiscordSendReceipt] {
	sender.sent++
	sender.payload = message.Payload()

	return result.Ok(sender.receipt)
}

type fakeGoogleChatSender struct {
	receipt GoogleChatSendReceipt
	sent    int
	payload GoogleChatPayload
}

func (sender *fakeGoogleChatSender) SendGoogleChat(
	ctx context.Context,
	message GoogleChatMessage,
) result.Result[GoogleChatSendReceipt] {
	sender.sent++
	sender.payload = message.Payload()

	return result.Ok(sender.receipt)
}

type fakeNtfySender struct {
	receipt NtfySendReceipt
	sent    int
	topic   string
	payload NtfyPayload
}

func (sender *fakeNtfySender) SendNtfy(
	ctx context.Context,
	message NtfyMessage,
) result.Result[NtfySendReceipt] {
	sender.sent++
	sender.topic = message.Topic().String()
	sender.payload = message.Payload()

	return result.Ok(sender.receipt)
}

type fakeTeamsSender struct {
	receipt TeamsSendReceipt
	sent    int
	payload TeamsPayload
}

func (sender *fakeTeamsSender) SendTeams(
	ctx context.Context,
	message TeamsMessage,
) result.Result[TeamsSendReceipt] {
	sender.sent++
	sender.payload = message.Payload()

	return result.Ok(sender.receipt)
}

type fakeResolver map[string][]netip.Addr

func (resolver fakeResolver) LookupHost(
	ctx context.Context,
	host string,
) result.Result[[]netip.Addr] {
	addresses, ok := resolver[host]
	if !ok {
		return result.Err[[]netip.Addr](errors.New("not found"))
	}

	return result.Ok(addresses)
}

func telegramBatchCommand(t *testing.T, now time.Time) TelegramBatchCommand {
	t.Helper()

	commandResult := NewTelegramBatchCommand(now, 10, "http://127.0.0.1:8085")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("command: %v", commandErr)
	}

	return command
}

func webhookBatchCommand(t *testing.T, now time.Time) WebhookBatchCommand {
	t.Helper()

	commandResult := NewWebhookBatchCommand(now, 10, "http://127.0.0.1:8085")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("command: %v", commandErr)
	}

	return command
}

func emailBatchCommand(t *testing.T, now time.Time) EmailBatchCommand {
	t.Helper()

	commandResult := NewEmailBatchCommand(now, 10, "http://127.0.0.1:8085")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("command: %v", commandErr)
	}

	return command
}

func discordBatchCommand(t *testing.T, now time.Time) DiscordBatchCommand {
	t.Helper()

	commandResult := NewDiscordBatchCommand(now, 10, "http://127.0.0.1:8085")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("command: %v", commandErr)
	}

	return command
}

func googleChatBatchCommand(t *testing.T, now time.Time) GoogleChatBatchCommand {
	t.Helper()

	commandResult := NewGoogleChatBatchCommand(now, 10, "http://127.0.0.1:8085")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("command: %v", commandErr)
	}

	return command
}

func ntfyBatchCommand(t *testing.T, now time.Time) NtfyBatchCommand {
	t.Helper()

	commandResult := NewNtfyBatchCommand(now, 10, "http://127.0.0.1:8085")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("command: %v", commandErr)
	}

	return command
}

func teamsBatchCommand(t *testing.T, now time.Time) TeamsBatchCommand {
	t.Helper()

	commandResult := NewTeamsBatchCommand(now, 10, "http://127.0.0.1:8085")
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		t.Fatalf("command: %v", commandErr)
	}

	return command
}

func telegramDelivery(t *testing.T) TelegramDelivery {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	chatID := telegramChatID(t, "123456")

	return NewTelegramDelivery(
		intentID,
		chatID,
		issueEvent(t),
		issueID(t),
		7,
	)
}

func webhookDelivery(t *testing.T, destination string) WebhookDelivery {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination url: %v", destinationErr)
	}

	return NewWebhookDelivery(
		intentID,
		destinationURL,
		issueEvent(t),
		issueID(t),
		7,
	)
}

func emailDelivery(t *testing.T) EmailDelivery {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	address := emailAddress(t, "alerts@example.test")

	return NewEmailDelivery(
		intentID,
		address,
		issueEvent(t),
		issueID(t),
		7,
	)
}

func discordDelivery(t *testing.T, destination string) DiscordDelivery {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination url: %v", destinationErr)
	}

	return NewDiscordDelivery(
		intentID,
		destinationURL,
		issueEvent(t),
		issueID(t),
		7,
	)
}

func googleChatDelivery(t *testing.T, destination string) GoogleChatDelivery {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination url: %v", destinationErr)
	}

	return NewGoogleChatDelivery(
		intentID,
		destinationURL,
		issueEvent(t),
		issueID(t),
		7,
	)
}

func ntfyDelivery(t *testing.T, destination string) NtfyDelivery {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination url: %v", destinationErr)
	}
	topic := ntfyTopic(t, "ops-alerts")

	return NewNtfyDelivery(
		intentID,
		destinationURL,
		topic,
		issueEvent(t),
		issueID(t),
		7,
	)
}

func teamsDelivery(t *testing.T, destination string) TeamsDelivery {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination url: %v", destinationErr)
	}

	return NewTeamsDelivery(
		intentID,
		destinationURL,
		issueEvent(t),
		issueID(t),
		7,
	)
}

func issueEvent(t *testing.T) domain.CanonicalEvent {
	t.Helper()

	organizationID := mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustID(t, domain.NewProjectID, "2222222222224222a222222222222222")
	eventID := mustID(t, domain.NewEventID, "950e8400e29b41d4a716446655440000")
	occurredAt := timePoint(t, time.Date(2026, 4, 24, 12, 30, 0, 0, time.UTC))
	receivedAt := timePoint(t, time.Date(2026, 4, 24, 12, 30, 1, 0, time.UTC))
	title := eventTitle(t, "panic: broken pipe")

	event, eventErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		EventID:              eventID,
		OccurredAt:           occurredAt,
		ReceivedAt:           receivedAt,
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title,
		Platform:             "go",
		DefaultGroupingParts: []string{"panic", "worker.go", "12"},
	})
	if eventErr != nil {
		t.Fatalf("event: %v", eventErr)
	}

	return event
}

func issueID(t *testing.T) domain.IssueID {
	t.Helper()

	id, idErr := domain.NewIssueID("3333333333334333a333333333333333")
	if idErr != nil {
		t.Fatalf("issue id: %v", idErr)
	}

	return id
}

func telegramChatID(t *testing.T, input string) domain.TelegramChatID {
	t.Helper()

	id, idErr := domain.NewTelegramChatID(input)
	if idErr != nil {
		t.Fatalf("chat id: %v", idErr)
	}

	return id
}

func emailAddress(t *testing.T, input string) domain.EmailAddress {
	t.Helper()

	address, addressErr := domain.NewEmailAddress(input)
	if addressErr != nil {
		t.Fatalf("email address: %v", addressErr)
	}

	return address
}

func ntfyTopic(t *testing.T, input string) domain.NtfyTopic {
	t.Helper()

	topic, topicErr := domain.NewNtfyTopic(input)
	if topicErr != nil {
		t.Fatalf("ntfy topic: %v", topicErr)
	}

	return topic
}

func mustID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	id, idErr := constructor(input)
	if idErr != nil {
		t.Fatalf("id: %v", idErr)
	}

	return id
}

func timePoint(t *testing.T, value time.Time) domain.TimePoint {
	t.Helper()

	point, pointErr := domain.NewTimePoint(value)
	if pointErr != nil {
		t.Fatalf("time point: %v", pointErr)
	}

	return point
}

func eventTitle(t *testing.T, input string) domain.EventTitle {
	t.Helper()

	title, titleErr := domain.NewEventTitle(input)
	if titleErr != nil {
		t.Fatalf("title: %v", titleErr)
	}

	return title
}

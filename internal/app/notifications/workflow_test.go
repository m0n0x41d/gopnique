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

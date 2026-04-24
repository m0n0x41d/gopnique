package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSenderPostsSendMessage(t *testing.T) {
	var requestPath string
	var requestPayload sendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		decodeErr := json.NewDecoder(r.Body).Decode(&requestPayload)
		if decodeErr != nil {
			t.Fatalf("decode request: %v", decodeErr)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77}}`))
	}))
	defer server.Close()

	sender, senderErr := NewSender(server.Client(), server.URL, "test-token")
	if senderErr != nil {
		t.Fatalf("sender: %v", senderErr)
	}

	sendResult := sender.SendTelegram(context.Background(), telegramMessage(t))
	receipt, receiptErr := sendResult.Value()
	if receiptErr != nil {
		t.Fatalf("send telegram: %v", receiptErr)
	}

	if requestPath != "/bottest-token/sendMessage" {
		t.Fatalf("unexpected path: %s", requestPath)
	}

	if requestPayload.ChatID != "123456" {
		t.Fatalf("unexpected chat id: %s", requestPayload.ChatID)
	}

	if requestPayload.Text != "hello telegram" {
		t.Fatalf("unexpected text: %s", requestPayload.Text)
	}

	if receipt.ProviderMessageID() != "77" {
		t.Fatalf("unexpected provider message id: %s", receipt.ProviderMessageID())
	}
}

func telegramMessage(t *testing.T) notifications.TelegramMessage {
	t.Helper()

	intentID, intentErr := domain.NewNotificationIntentID("44444444-4444-4444-a444-444444444444")
	if intentErr != nil {
		t.Fatalf("intent id: %v", intentErr)
	}

	chatID, chatErr := domain.NewTelegramChatID("123456")
	if chatErr != nil {
		t.Fatalf("chat id: %v", chatErr)
	}

	text, textErr := domain.NewNotificationText("hello telegram")
	if textErr != nil {
		t.Fatalf("text: %v", textErr)
	}

	return notifications.NewTelegramMessage(intentID, chatID, text)
}

package webhook

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSenderPostsIssueOpenedPayload(t *testing.T) {
	var requestPayload issueOpenedPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content type: %s", r.Header.Get("Content-Type"))
		}

		decodeErr := json.NewDecoder(r.Body).Decode(&requestPayload)
		if decodeErr != nil {
			t.Fatalf("decode payload: %v", decodeErr)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	publicURL := webhookPublicURL(t, server.URL)
	sender := NewSender(&http.Client{Transport: rewriteTransport(t, publicURL)})
	sendResult := sender.SendWebhook(t.Context(), webhookMessage(t, publicURL))
	receipt, receiptErr := sendResult.Value()
	if receiptErr != nil {
		t.Fatalf("send webhook: %v", receiptErr)
	}

	if !receipt.Delivered() || receipt.Status() != http.StatusNoContent {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if requestPayload.EventID != "950e8400-e29b-41d4-a716-446655440000" {
		t.Fatalf("unexpected event id: %s", requestPayload.EventID)
	}
}

func TestSenderReturnsFailedReceiptForNonSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()

	publicURL := webhookPublicURL(t, server.URL)
	sender := NewSender(&http.Client{Transport: rewriteTransport(t, publicURL)})
	sendResult := sender.SendWebhook(t.Context(), webhookMessage(t, publicURL))
	receipt, receiptErr := sendResult.Value()
	if receiptErr != nil {
		t.Fatalf("send webhook: %v", receiptErr)
	}

	if receipt.Delivered() || receipt.Status() != http.StatusBadGateway {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}
}

func webhookPublicURL(t *testing.T, serverURL string) string {
	t.Helper()

	parsed, parseErr := url.Parse(serverURL)
	if parseErr != nil {
		t.Fatalf("parse server url: %v", parseErr)
	}

	return parsed.Scheme + "://hooks.example.test:" + parsed.Port() + "/hook"
}

func rewriteTransport(t *testing.T, destination string) http.RoundTripper {
	t.Helper()

	parsed, parseErr := url.Parse(destination)
	if parseErr != nil {
		t.Fatalf("parse destination: %v", parseErr)
	}

	targetAddress := net.JoinHostPort("127.0.0.1", parsed.Port())
	dialer := &net.Dialer{Timeout: 5 * time.Second}

	return &http.Transport{
		DialContext: func(ctx context.Context, network string, address string) (net.Conn, error) {
			if address == parsed.Host {
				return dialer.DialContext(ctx, network, targetAddress)
			}

			return dialer.DialContext(ctx, network, address)
		},
	}
}

func webhookMessage(t *testing.T, destination string) notifications.WebhookMessage {
	t.Helper()

	intentID, intentErr := domain.NewNotificationIntentID("44444444-4444-4444-a444-444444444444")
	if intentErr != nil {
		t.Fatalf("intent id: %v", intentErr)
	}

	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination url: %v", destinationErr)
	}

	return notifications.NewWebhookMessage(
		intentID,
		destinationURL,
		notifications.WebhookPayload{
			EventID:      "950e8400-e29b-41d4-a716-446655440000",
			IssueID:      "33333333-3333-4333-a333-333333333333",
			IssueShortID: 42,
			Title:        "panic: broken pipe",
			Level:        "error",
			Platform:     "go",
			IssueURL:     "http://127.0.0.1:8085/issues/33333333-3333-4333-a333-333333333333",
		},
	)
}

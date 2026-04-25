package ntfy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSenderPostsNtfyRequest(t *testing.T) {
	var capturedPath string
	var capturedTitle string
	var capturedTags string
	var capturedClick string
	var capturedBody string
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		capturedPath = r.URL.Path
		capturedTitle = r.Header.Get("Title")
		capturedTags = r.Header.Get("Tags")
		capturedClick = r.Header.Get("Click")

		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Fatalf("read body: %v", readErr)
		}
		capturedBody = string(body)

		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     http.Header{},
			Request:    r,
		}

		return response, nil
	})
	client := &http.Client{Transport: transport}
	message := ntfyMessage(t, "https://ntfy.example.com", "ops-alerts")
	sender := NewSender(client)

	sendResult := sender.SendNtfy(context.Background(), message)
	receipt, sendErr := sendResult.Value()
	if sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}

	if !receipt.Delivered() || receipt.Status() != http.StatusOK {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if capturedPath != "/ops-alerts" || capturedTitle != "New issue #7" || capturedTags != "rotating_light" {
		t.Fatalf("unexpected ntfy headers: path=%s title=%s tags=%s", capturedPath, capturedTitle, capturedTags)
	}

	if capturedClick != "http://example.test/issues/issue-1" {
		t.Fatalf("unexpected click: %s", capturedClick)
	}

	for _, expected := range []string{"panic", "Level: error", "Platform: go", "Event: event-1"} {
		if !strings.Contains(capturedBody, expected) {
			t.Fatalf("expected body to contain %q: %s", expected, capturedBody)
		}
	}
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func ntfyMessage(t *testing.T, destination string, topicText string) notifications.NtfyMessage {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination: %v", destinationErr)
	}
	topic, topicErr := domain.NewNtfyTopic(topicText)
	if topicErr != nil {
		t.Fatalf("topic: %v", topicErr)
	}

	return notifications.NewNtfyMessage(
		intentID,
		destinationURL,
		topic,
		notifications.NtfyPayload{
			EventID:      "event-1",
			IssueID:      "issue-1",
			IssueShortID: 7,
			Title:        "panic",
			Level:        "error",
			Platform:     "go",
			IssueURL:     "http://example.test/issues/issue-1",
		},
	)
}

func mustID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	id, idErr := constructor(input)
	if idErr != nil {
		t.Fatalf("id: %v", idErr)
	}

	return id
}

package googlechat

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSenderPostsGoogleChatCard(t *testing.T) {
	var captured cardMessage
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/chat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		decodeErr := json.NewDecoder(r.Body).Decode(&captured)
		if decodeErr != nil {
			t.Fatalf("decode body: %v", decodeErr)
		}

		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     http.Header{},
			Request:    r,
		}

		return response, nil
	})
	client := &http.Client{Transport: transport}
	message := googleChatMessage(t, "https://chat.example.com/chat")
	sender := NewSender(client)

	sendResult := sender.SendGoogleChat(context.Background(), message)
	receipt, sendErr := sendResult.Value()
	if sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}

	if !receipt.Delivered() || receipt.Status() != http.StatusOK {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if len(captured.CardsV2) != 1 {
		t.Fatalf("unexpected cards: %#v", captured.CardsV2)
	}

	header := captured.CardsV2[0].Card.Header
	if header.Title != "New issue #7" || header.Subtitle != "panic" {
		t.Fatalf("unexpected header: %#v", header)
	}
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func googleChatMessage(t *testing.T, destination string) notifications.GoogleChatMessage {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination: %v", destinationErr)
	}

	return notifications.NewGoogleChatMessage(
		intentID,
		destinationURL,
		notifications.GoogleChatPayload{
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

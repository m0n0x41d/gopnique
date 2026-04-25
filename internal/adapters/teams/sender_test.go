package teams

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSenderPostsTeamsAdaptiveCard(t *testing.T) {
	var captured adaptiveCardMessage
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/teams" {
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
	message := teamsMessage(t, "https://teams.example.com/teams")
	sender := NewSender(client)

	sendResult := sender.SendTeams(context.Background(), message)
	receipt, sendErr := sendResult.Value()
	if sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}

	if !receipt.Delivered() || receipt.Status() != http.StatusOK {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if len(captured.Attachments) != 1 {
		t.Fatalf("unexpected attachments: %#v", captured.Attachments)
	}

	card := captured.Attachments[0].Content
	if card.Type != "AdaptiveCard" || card.Body[0].Text != "New issue #7" {
		t.Fatalf("unexpected card: %#v", card)
	}
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func teamsMessage(t *testing.T, destination string) notifications.TeamsMessage {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination: %v", destinationErr)
	}

	return notifications.NewTeamsMessage(
		intentID,
		destinationURL,
		notifications.TeamsPayload{
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

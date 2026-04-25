package discord

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSenderPostsDiscordIssueOpenedPayload(t *testing.T) {
	var captured requestBody
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/discord" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		decodeErr := json.NewDecoder(r.Body).Decode(&captured)
		if decodeErr != nil {
			t.Fatalf("decode body: %v", decodeErr)
		}

		response := &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       http.NoBody,
			Header:     http.Header{},
			Request:    r,
		}

		return response, nil
	})
	client := &http.Client{Transport: transport}
	message := discordMessage(t, "https://discord.example.com/discord")
	sender := NewSender(client)

	sendResult := sender.SendDiscord(context.Background(), message)
	receipt, sendErr := sendResult.Value()
	if sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}

	if !receipt.Delivered() || receipt.Status() != http.StatusNoContent {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if captured.Content != "New issue #7" {
		t.Fatalf("unexpected content: %s", captured.Content)
	}

	if len(captured.Embeds) != 1 {
		t.Fatalf("unexpected embeds: %#v", captured.Embeds)
	}

	embed := captured.Embeds[0]
	if embed.Title != "panic" || embed.URL != "http://example.test/issues/issue-1" || embed.Color != 15158332 {
		t.Fatalf("unexpected embed: %#v", embed)
	}

	if len(embed.Fields) != 3 || embed.Fields[0].Value != "error" || embed.Fields[2].Value != "event-1" {
		t.Fatalf("unexpected fields: %#v", embed.Fields)
	}
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func discordMessage(t *testing.T, destination string) notifications.DiscordMessage {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination: %v", destinationErr)
	}

	return notifications.NewDiscordMessage(
		intentID,
		destinationURL,
		notifications.DiscordPayload{
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

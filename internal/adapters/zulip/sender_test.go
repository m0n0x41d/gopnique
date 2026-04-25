package zulip

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestSenderPostsZulipMessage(t *testing.T) {
	var capturedPath string
	var capturedUser string
	var capturedPassword string
	var capturedForm url.Values
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		capturedPath = r.URL.Path
		user, password, ok := r.BasicAuth()
		if !ok {
			t.Fatal("expected basic auth")
		}
		capturedUser = user
		capturedPassword = password

		parseErr := r.ParseForm()
		if parseErr != nil {
			t.Fatalf("parse form: %v", parseErr)
		}
		capturedForm = r.PostForm

		response := &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     http.Header{},
			Request:    r,
		}

		return response, nil
	})
	client := &http.Client{Transport: transport}
	message := zulipMessage(t, "https://zulip.example.com")
	sender := NewSender(client)

	sendResult := sender.SendZulip(context.Background(), message)
	receipt, sendErr := sendResult.Value()
	if sendErr != nil {
		t.Fatalf("send: %v", sendErr)
	}

	if !receipt.Delivered() || receipt.Status() != http.StatusOK {
		t.Fatalf("unexpected receipt: %#v", receipt)
	}

	if capturedPath != "/api/v1/messages" {
		t.Fatalf("unexpected path: %s", capturedPath)
	}

	if capturedUser != "bot@example.test" || capturedPassword != "zulip-key" {
		t.Fatalf("unexpected auth: %s %s", capturedUser, capturedPassword)
	}

	if capturedForm.Get("type") != "stream" ||
		capturedForm.Get("to") != "ops" ||
		capturedForm.Get("topic") != "alerts" {
		t.Fatalf("unexpected form: %#v", capturedForm)
	}

	for _, expected := range []string{"panic", "Level: error", "Platform: go", "Event: event-1"} {
		if !strings.Contains(capturedForm.Get("content"), expected) {
			t.Fatalf("expected content to contain %q: %s", expected, capturedForm.Get("content"))
		}
	}
}

type roundTripFunc func(r *http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func zulipMessage(t *testing.T, destination string) notifications.ZulipMessage {
	t.Helper()

	intentID := mustID(t, domain.NewNotificationIntentID, "44444444-4444-4444-a444-444444444444")
	destinationResult := outbound.ParseDestinationURL(destination)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination: %v", destinationErr)
	}

	return notifications.NewZulipMessage(
		intentID,
		destinationURL,
		zulipBotEmail(t, "bot@example.test"),
		zulipAPIKey(t, "zulip-key"),
		zulipStream(t, "ops"),
		zulipTopic(t, "alerts"),
		notifications.ZulipPayload{
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

func zulipBotEmail(t *testing.T, input string) domain.ZulipBotEmail {
	t.Helper()

	email, emailErr := domain.NewZulipBotEmail(input)
	if emailErr != nil {
		t.Fatalf("bot email: %v", emailErr)
	}

	return email
}

func zulipAPIKey(t *testing.T, input string) domain.ZulipAPIKey {
	t.Helper()

	key, keyErr := domain.NewZulipAPIKey(input)
	if keyErr != nil {
		t.Fatalf("api key: %v", keyErr)
	}

	return key
}

func zulipStream(t *testing.T, input string) domain.ZulipStreamName {
	t.Helper()

	stream, streamErr := domain.NewZulipStreamName(input)
	if streamErr != nil {
		t.Fatalf("stream: %v", streamErr)
	}

	return stream
}

func zulipTopic(t *testing.T, input string) domain.ZulipTopicName {
	t.Helper()

	topic, topicErr := domain.NewZulipTopicName(input)
	if topicErr != nil {
		t.Fatalf("topic: %v", topicErr)
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

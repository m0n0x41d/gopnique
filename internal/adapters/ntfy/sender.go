package ntfy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Sender struct {
	client *http.Client
}

func NewSender(client *http.Client) Sender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return Sender{client: client}
}

func (sender Sender) SendNtfy(
	ctx context.Context,
	message notifications.NtfyMessage,
) result.Result[notifications.NtfySendReceipt] {
	requestResult := ntfyRequest(ctx, message)
	request, requestErr := requestResult.Value()
	if requestErr != nil {
		return result.Err[notifications.NtfySendReceipt](requestErr)
	}

	response, responseErr := sender.client.Do(request)
	if responseErr != nil {
		return result.Err[notifications.NtfySendReceipt](responseErr)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result.Ok(notifications.NewNtfyFailedReceipt(
			response.StatusCode,
			fmt.Sprintf("ntfy returned HTTP %d", response.StatusCode),
		))
	}

	return result.Ok(notifications.NewNtfyDeliveredReceipt(response.StatusCode))
}

func ntfyRequest(
	ctx context.Context,
	message notifications.NtfyMessage,
) result.Result[*http.Request] {
	targetResult := ntfyTopicURL(message.DestinationURL().String(), message.Topic().String())
	target, targetErr := targetResult.Value()
	if targetErr != nil {
		return result.Err[*http.Request](targetErr)
	}

	payload := message.Payload()
	body := strings.Join([]string{
		"New issue #" + fmt.Sprintf("%d", payload.IssueShortID),
		payload.Title,
		"Level: " + payload.Level,
		"Platform: " + payload.Platform,
		"Event: " + payload.EventID,
		payload.IssueURL,
	}, "\n")

	request, requestErr := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		target,
		bytes.NewBufferString(body),
	)
	if requestErr != nil {
		return result.Err[*http.Request](requestErr)
	}

	request.Header.Set("Title", "New issue #"+fmt.Sprintf("%d", payload.IssueShortID))
	request.Header.Set("Tags", tagForLevel(payload.Level))
	request.Header.Set("Click", payload.IssueURL)
	request.Header.Set("Priority", priorityForLevel(payload.Level))
	request.Header.Set("User-Agent", "error-tracker-ntfy/0.1")

	return result.Ok(request)
}

func ntfyTopicURL(base string, topic string) result.Result[string] {
	if base == "" {
		return result.Err[string](errors.New("ntfy destination url is required"))
	}

	if topic == "" {
		return result.Err[string](errors.New("ntfy topic is required"))
	}

	parsed, parseErr := url.Parse(base)
	if parseErr != nil {
		return result.Err[string](parseErr)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + url.PathEscape(topic)
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return result.Ok(parsed.String())
}

func tagForLevel(level string) string {
	if level == "fatal" || level == "error" {
		return "rotating_light"
	}

	if level == "warning" {
		return "warning"
	}

	return "information_source"
}

func priorityForLevel(level string) string {
	if level == "fatal" || level == "error" {
		return "high"
	}

	if level == "warning" {
		return "default"
	}

	return "low"
}

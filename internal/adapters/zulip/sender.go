package zulip

import (
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

func (sender Sender) SendZulip(
	ctx context.Context,
	message notifications.ZulipMessage,
) result.Result[notifications.ZulipSendReceipt] {
	requestResult := zulipRequest(ctx, message)
	request, requestErr := requestResult.Value()
	if requestErr != nil {
		return result.Err[notifications.ZulipSendReceipt](requestErr)
	}

	response, responseErr := sender.client.Do(request)
	if responseErr != nil {
		return result.Err[notifications.ZulipSendReceipt](responseErr)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result.Ok(notifications.NewZulipFailedReceipt(
			response.StatusCode,
			fmt.Sprintf("zulip returned HTTP %d", response.StatusCode),
		))
	}

	return result.Ok(notifications.NewZulipDeliveredReceipt(response.StatusCode))
}

func zulipRequest(
	ctx context.Context,
	message notifications.ZulipMessage,
) result.Result[*http.Request] {
	targetResult := zulipMessagesURL(message.DestinationURL().String())
	target, targetErr := targetResult.Value()
	if targetErr != nil {
		return result.Err[*http.Request](targetErr)
	}

	payload := message.Payload()
	content := strings.Join([]string{
		"**New issue #" + fmt.Sprintf("%d", payload.IssueShortID) + "**",
		payload.Title,
		"Level: " + payload.Level,
		"Platform: " + payload.Platform,
		"Event: " + payload.EventID,
		payload.IssueURL,
	}, "\n")

	form := url.Values{}
	form.Set("type", "stream")
	form.Set("to", message.Stream().String())
	form.Set("topic", message.Topic().String())
	form.Set("content", content)

	request, requestErr := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		target,
		strings.NewReader(form.Encode()),
	)
	if requestErr != nil {
		return result.Err[*http.Request](requestErr)
	}

	request.SetBasicAuth(message.BotEmail().String(), message.APIKey().String())
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", "error-tracker-zulip/0.1")

	return result.Ok(request)
}

func zulipMessagesURL(base string) result.Result[string] {
	if base == "" {
		return result.Err[string](errors.New("zulip destination url is required"))
	}

	parsed, parseErr := url.Parse(base)
	if parseErr != nil {
		return result.Err[string](parseErr)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/api/v1/messages"
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return result.Ok(parsed.String())
}

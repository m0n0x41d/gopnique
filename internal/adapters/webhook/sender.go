package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Sender struct {
	client *http.Client
}

type issueOpenedPayload struct {
	EventID      string `json:"event_id"`
	IssueID      string `json:"issue_id"`
	IssueShortID int64  `json:"issue_short_id"`
	Title        string `json:"title"`
	Level        string `json:"level"`
	Platform     string `json:"platform"`
	IssueURL     string `json:"issue_url"`
}

func NewSender(client *http.Client) Sender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return Sender{client: client}
}

func (sender Sender) SendWebhook(
	ctx context.Context,
	message notifications.WebhookMessage,
) result.Result[notifications.WebhookSendReceipt] {
	bodyResult := webhookBody(message.Payload())
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		return result.Err[notifications.WebhookSendReceipt](bodyErr)
	}

	requestResult := webhookRequest(ctx, message.DestinationURL().String(), body)
	request, requestErr := requestResult.Value()
	if requestErr != nil {
		return result.Err[notifications.WebhookSendReceipt](requestErr)
	}

	response, responseErr := sender.client.Do(request)
	if responseErr != nil {
		return result.Err[notifications.WebhookSendReceipt](responseErr)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result.Ok(notifications.NewWebhookFailedReceipt(
			response.StatusCode,
			fmt.Sprintf("webhook returned HTTP %d", response.StatusCode),
		))
	}

	return result.Ok(notifications.NewWebhookDeliveredReceipt(response.StatusCode))
}

func webhookBody(payload notifications.WebhookPayload) result.Result[[]byte] {
	body, bodyErr := json.Marshal(issueOpenedPayload{
		EventID:      payload.EventID,
		IssueID:      payload.IssueID,
		IssueShortID: payload.IssueShortID,
		Title:        payload.Title,
		Level:        payload.Level,
		Platform:     payload.Platform,
		IssueURL:     payload.IssueURL,
	})
	if bodyErr != nil {
		return result.Err[[]byte](bodyErr)
	}

	return result.Ok(body)
}

func webhookRequest(
	ctx context.Context,
	destinationURL string,
	body []byte,
) result.Result[*http.Request] {
	if destinationURL == "" {
		return result.Err[*http.Request](errors.New("webhook destination url is required"))
	}

	request, requestErr := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		destinationURL,
		bytes.NewReader(body),
	)
	if requestErr != nil {
		return result.Err[*http.Request](requestErr)
	}

	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "error-tracker-webhook/0.1")

	return result.Ok(request)
}

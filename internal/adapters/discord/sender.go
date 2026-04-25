package discord

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

type requestBody struct {
	Content string  `json:"content"`
	Embeds  []embed `json:"embeds"`
}

type embed struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Description string  `json:"description"`
	Color       int     `json:"color"`
	Fields      []field `json:"fields"`
}

type field struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

func NewSender(client *http.Client) Sender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return Sender{client: client}
}

func (sender Sender) SendDiscord(
	ctx context.Context,
	message notifications.DiscordMessage,
) result.Result[notifications.DiscordSendReceipt] {
	bodyResult := discordBody(message.Payload())
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		return result.Err[notifications.DiscordSendReceipt](bodyErr)
	}

	requestResult := discordRequest(ctx, message.DestinationURL().String(), body)
	request, requestErr := requestResult.Value()
	if requestErr != nil {
		return result.Err[notifications.DiscordSendReceipt](requestErr)
	}

	response, responseErr := sender.client.Do(request)
	if responseErr != nil {
		return result.Err[notifications.DiscordSendReceipt](responseErr)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result.Ok(notifications.NewDiscordFailedReceipt(
			response.StatusCode,
			fmt.Sprintf("discord returned HTTP %d", response.StatusCode),
		))
	}

	return result.Ok(notifications.NewDiscordDeliveredReceipt(response.StatusCode))
}

func discordBody(payload notifications.DiscordPayload) result.Result[[]byte] {
	body, bodyErr := json.Marshal(requestBody{
		Content: "New issue #" + fmt.Sprintf("%d", payload.IssueShortID),
		Embeds: []embed{
			{
				Title:       payload.Title,
				URL:         payload.IssueURL,
				Description: "A new issue was created.",
				Color:       colorForLevel(payload.Level),
				Fields: []field{
					{Name: "Level", Value: payload.Level, Inline: true},
					{Name: "Platform", Value: payload.Platform, Inline: true},
					{Name: "Event", Value: payload.EventID, Inline: false},
				},
			},
		},
	})
	if bodyErr != nil {
		return result.Err[[]byte](bodyErr)
	}

	return result.Ok(body)
}

func discordRequest(
	ctx context.Context,
	destinationURL string,
	body []byte,
) result.Result[*http.Request] {
	if destinationURL == "" {
		return result.Err[*http.Request](errors.New("discord destination url is required"))
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
	request.Header.Set("User-Agent", "error-tracker-discord/0.1")

	return result.Ok(request)
}

func colorForLevel(level string) int {
	if level == "fatal" || level == "error" {
		return 15158332
	}

	if level == "warning" {
		return 16776960
	}

	return 3447003
}

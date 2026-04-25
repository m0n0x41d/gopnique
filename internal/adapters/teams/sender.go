package teams

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

type adaptiveCardMessage struct {
	Type        string       `json:"type"`
	Attachments []attachment `json:"attachments"`
}

type attachment struct {
	ContentType string       `json:"contentType"`
	Content     adaptiveCard `json:"content"`
}

type adaptiveCard struct {
	Schema  string       `json:"$schema"`
	Type    string       `json:"type"`
	Version string       `json:"version"`
	Body    []cardBody   `json:"body"`
	Actions []cardAction `json:"actions"`
}

type cardBody struct {
	Type   string `json:"type"`
	Text   string `json:"text,omitempty"`
	Weight string `json:"weight,omitempty"`
	Size   string `json:"size,omitempty"`
	Wrap   bool   `json:"wrap,omitempty"`
	Facts  []fact `json:"facts,omitempty"`
}

type fact struct {
	Title string `json:"title"`
	Value string `json:"value"`
}

type cardAction struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func NewSender(client *http.Client) Sender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return Sender{client: client}
}

func (sender Sender) SendTeams(
	ctx context.Context,
	message notifications.TeamsMessage,
) result.Result[notifications.TeamsSendReceipt] {
	bodyResult := teamsBody(message.Payload())
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		return result.Err[notifications.TeamsSendReceipt](bodyErr)
	}

	requestResult := teamsRequest(ctx, message.DestinationURL().String(), body)
	request, requestErr := requestResult.Value()
	if requestErr != nil {
		return result.Err[notifications.TeamsSendReceipt](requestErr)
	}

	response, responseErr := sender.client.Do(request)
	if responseErr != nil {
		return result.Err[notifications.TeamsSendReceipt](responseErr)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result.Ok(notifications.NewTeamsFailedReceipt(
			response.StatusCode,
			fmt.Sprintf("microsoft teams returned HTTP %d", response.StatusCode),
		))
	}

	return result.Ok(notifications.NewTeamsDeliveredReceipt(response.StatusCode))
}

func teamsBody(payload notifications.TeamsPayload) result.Result[[]byte] {
	body, bodyErr := json.Marshal(adaptiveCardMessage{
		Type: "message",
		Attachments: []attachment{
			{
				ContentType: "application/vnd.microsoft.card.adaptive",
				Content: adaptiveCard{
					Schema:  "http://adaptivecards.io/schemas/adaptive-card.json",
					Type:    "AdaptiveCard",
					Version: "1.4",
					Body: []cardBody{
						{
							Type:   "TextBlock",
							Text:   "New issue #" + fmt.Sprintf("%d", payload.IssueShortID),
							Weight: "Bolder",
							Size:   "Medium",
							Wrap:   true,
						},
						{
							Type: "TextBlock",
							Text: payload.Title,
							Wrap: true,
						},
						{
							Type: "FactSet",
							Facts: []fact{
								{Title: "Level", Value: payload.Level},
								{Title: "Platform", Value: payload.Platform},
								{Title: "Event", Value: payload.EventID},
							},
						},
					},
					Actions: []cardAction{
						{
							Type:  "Action.OpenUrl",
							Title: "Open issue",
							URL:   payload.IssueURL,
						},
					},
				},
			},
		},
	})
	if bodyErr != nil {
		return result.Err[[]byte](bodyErr)
	}

	return result.Ok(body)
}

func teamsRequest(
	ctx context.Context,
	destinationURL string,
	body []byte,
) result.Result[*http.Request] {
	if destinationURL == "" {
		return result.Err[*http.Request](errors.New("microsoft teams destination url is required"))
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
	request.Header.Set("User-Agent", "error-tracker-microsoft-teams/0.1")

	return result.Ok(request)
}

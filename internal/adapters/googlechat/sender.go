package googlechat

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

type cardMessage struct {
	CardsV2 []cardV2 `json:"cardsV2"`
}

type cardV2 struct {
	Card card `json:"card"`
}

type card struct {
	Header   cardHeader    `json:"header"`
	Sections []cardSection `json:"sections"`
}

type cardHeader struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

type cardSection struct {
	Widgets []widget `json:"widgets"`
}

type widget struct {
	DecoratedText *decoratedText `json:"decoratedText,omitempty"`
	ButtonList    *buttonList    `json:"buttonList,omitempty"`
}

type decoratedText struct {
	TopLabel string `json:"topLabel"`
	Text     string `json:"text"`
}

type buttonList struct {
	Buttons []button `json:"buttons"`
}

type button struct {
	Text    string  `json:"text"`
	OnClick onClick `json:"onClick"`
}

type onClick struct {
	OpenLink openLink `json:"openLink"`
}

type openLink struct {
	URL string `json:"url"`
}

func NewSender(client *http.Client) Sender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	return Sender{client: client}
}

func (sender Sender) SendGoogleChat(
	ctx context.Context,
	message notifications.GoogleChatMessage,
) result.Result[notifications.GoogleChatSendReceipt] {
	bodyResult := googleChatBody(message.Payload())
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		return result.Err[notifications.GoogleChatSendReceipt](bodyErr)
	}

	requestResult := googleChatRequest(ctx, message.DestinationURL().String(), body)
	request, requestErr := requestResult.Value()
	if requestErr != nil {
		return result.Err[notifications.GoogleChatSendReceipt](requestErr)
	}

	response, responseErr := sender.client.Do(request)
	if responseErr != nil {
		return result.Err[notifications.GoogleChatSendReceipt](responseErr)
	}
	defer response.Body.Close()

	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return result.Ok(notifications.NewGoogleChatFailedReceipt(
			response.StatusCode,
			fmt.Sprintf("google chat returned HTTP %d", response.StatusCode),
		))
	}

	return result.Ok(notifications.NewGoogleChatDeliveredReceipt(response.StatusCode))
}

func googleChatBody(payload notifications.GoogleChatPayload) result.Result[[]byte] {
	body, bodyErr := json.Marshal(cardMessage{
		CardsV2: []cardV2{
			{
				Card: card{
					Header: cardHeader{
						Title:    "New issue #" + fmt.Sprintf("%d", payload.IssueShortID),
						Subtitle: payload.Title,
					},
					Sections: []cardSection{
						{
							Widgets: []widget{
								{DecoratedText: &decoratedText{TopLabel: "Level", Text: payload.Level}},
								{DecoratedText: &decoratedText{TopLabel: "Platform", Text: payload.Platform}},
								{DecoratedText: &decoratedText{TopLabel: "Event", Text: payload.EventID}},
								{ButtonList: &buttonList{Buttons: []button{
									{
										Text: "Open issue",
										OnClick: onClick{
											OpenLink: openLink{URL: payload.IssueURL},
										},
									},
								}}},
							},
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

func googleChatRequest(
	ctx context.Context,
	destinationURL string,
	body []byte,
) result.Result[*http.Request] {
	if destinationURL == "" {
		return result.Err[*http.Request](errors.New("google chat destination url is required"))
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
	request.Header.Set("User-Agent", "error-tracker-google-chat/0.1")

	return result.Ok(request)
}

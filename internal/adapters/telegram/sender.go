package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Sender struct {
	client   *http.Client
	baseURL  string
	botToken string
}

type sendMessageRequest struct {
	ChatID                string `json:"chat_id"`
	Text                  string `json:"text"`
	DisableWebPagePreview bool   `json:"disable_web_page_preview"`
}

type sendMessageResponse struct {
	OK     bool `json:"ok"`
	Result struct {
		MessageID int64 `json:"message_id"`
	} `json:"result"`
	Description string `json:"description"`
}

func NewSender(client *http.Client, baseURL string, botToken string) (Sender, error) {
	if client == nil {
		return Sender{}, errors.New("http client is required")
	}

	apiBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if apiBaseURL == "" {
		return Sender{}, errors.New("telegram api base url is required")
	}

	token := strings.TrimSpace(botToken)
	if token == "" {
		return Sender{}, errors.New("telegram bot token is required")
	}

	return Sender{
		client:   client,
		baseURL:  apiBaseURL,
		botToken: token,
	}, nil
}

func (sender Sender) SendTelegram(
	ctx context.Context,
	message notifications.TelegramMessage,
) result.Result[notifications.TelegramSendReceipt] {
	bodyResult := sender.requestBody(message)
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		return result.Err[notifications.TelegramSendReceipt](bodyErr)
	}

	request, requestErr := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		sender.sendMessageURL(),
		bytes.NewReader(body),
	)
	if requestErr != nil {
		return result.Err[notifications.TelegramSendReceipt](requestErr)
	}

	request.Header.Set("Content-Type", "application/json")

	response, responseErr := sender.client.Do(request)
	if responseErr != nil {
		return result.Err[notifications.TelegramSendReceipt](responseErr)
	}
	defer response.Body.Close()

	return sender.receipt(response)
}

func (sender Sender) requestBody(
	message notifications.TelegramMessage,
) result.Result[[]byte] {
	payload := sendMessageRequest{
		ChatID:                message.ChatID().String(),
		Text:                  message.Text().String(),
		DisableWebPagePreview: true,
	}

	body, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return result.Err[[]byte](marshalErr)
	}

	return result.Ok(body)
}

func (sender Sender) receipt(
	response *http.Response,
) result.Result[notifications.TelegramSendReceipt] {
	body, readErr := io.ReadAll(response.Body)
	if readErr != nil {
		return result.Err[notifications.TelegramSendReceipt](readErr)
	}

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return result.Err[notifications.TelegramSendReceipt](errors.New("telegram send failed"))
	}

	var decoded sendMessageResponse
	decodeErr := json.Unmarshal(body, &decoded)
	if decodeErr != nil {
		return result.Err[notifications.TelegramSendReceipt](decodeErr)
	}

	if !decoded.OK {
		return result.Err[notifications.TelegramSendReceipt](errors.New("telegram send rejected"))
	}

	return result.Ok(notifications.NewTelegramSendReceipt(messageID(decoded.Result.MessageID)))
}

func (sender Sender) sendMessageURL() string {
	return sender.baseURL + "/bot" + sender.botToken + "/sendMessage"
}

func messageID(value int64) string {
	return strconv.FormatInt(value, 10)
}

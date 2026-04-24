package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

type TelegramChatID struct {
	value string
}

type TelegramDestinationLabel struct {
	value string
}

type WebhookDestinationLabel struct {
	value string
}

type NotificationText struct {
	value string
}

func NewTelegramChatID(input string) (TelegramChatID, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return TelegramChatID{}, errors.New("telegram chat id is required")
	}

	if strings.ContainsAny(value, "\r\n\t ") {
		return TelegramChatID{}, errors.New("telegram chat id cannot contain whitespace")
	}

	if utf8.RuneCountInString(value) > 128 {
		return TelegramChatID{}, errors.New("telegram chat id is too long")
	}

	return TelegramChatID{value: value}, nil
}

func NewTelegramDestinationLabel(input string) (TelegramDestinationLabel, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return TelegramDestinationLabel{}, errors.New("telegram label is required")
	}

	if strings.ContainsAny(value, "\r\n\t") {
		return TelegramDestinationLabel{}, errors.New("telegram label cannot contain control whitespace")
	}

	if utf8.RuneCountInString(value) > 80 {
		return TelegramDestinationLabel{}, errors.New("telegram label is too long")
	}

	return TelegramDestinationLabel{value: value}, nil
}

func NewWebhookDestinationLabel(input string) (WebhookDestinationLabel, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return WebhookDestinationLabel{}, errors.New("webhook label is required")
	}

	if strings.ContainsAny(value, "\r\n\t") {
		return WebhookDestinationLabel{}, errors.New("webhook label cannot contain control whitespace")
	}

	if utf8.RuneCountInString(value) > 80 {
		return WebhookDestinationLabel{}, errors.New("webhook label is too long")
	}

	return WebhookDestinationLabel{value: value}, nil
}

func NewNotificationText(input string) (NotificationText, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return NotificationText{}, errors.New("notification text is required")
	}

	if utf8.RuneCountInString(value) > 4096 {
		return NotificationText{}, errors.New("notification text is too long")
	}

	return NotificationText{value: value}, nil
}

func (id TelegramChatID) String() string {
	return id.value
}

func (label TelegramDestinationLabel) String() string {
	return label.value
}

func (label WebhookDestinationLabel) String() string {
	return label.value
}

func (text NotificationText) String() string {
	return text.value
}

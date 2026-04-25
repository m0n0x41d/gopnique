package domain

import (
	"errors"
	"net/mail"
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

type EmailAddress struct {
	value string
}

type EmailDestinationLabel struct {
	value string
}

type DiscordDestinationLabel struct {
	value string
}

type GoogleChatDestinationLabel struct {
	value string
}

type NtfyDestinationLabel struct {
	value string
}

type TeamsDestinationLabel struct {
	value string
}

type NtfyTopic struct {
	value string
}

type NotificationSubject struct {
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

func NewEmailAddress(input string) (EmailAddress, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return EmailAddress{}, errors.New("email address is required")
	}

	if strings.ContainsAny(value, "\r\n\t ") {
		return EmailAddress{}, errors.New("email address cannot contain whitespace")
	}

	parsed, parseErr := mail.ParseAddress(value)
	if parseErr != nil {
		return EmailAddress{}, errors.New("email address is invalid")
	}

	if parsed.Address != value {
		return EmailAddress{}, errors.New("email address must not include a display name")
	}

	if utf8.RuneCountInString(value) > 254 {
		return EmailAddress{}, errors.New("email address is too long")
	}

	return EmailAddress{value: value}, nil
}

func NewEmailDestinationLabel(input string) (EmailDestinationLabel, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return EmailDestinationLabel{}, errors.New("email label is required")
	}

	if strings.ContainsAny(value, "\r\n\t") {
		return EmailDestinationLabel{}, errors.New("email label cannot contain control whitespace")
	}

	if utf8.RuneCountInString(value) > 80 {
		return EmailDestinationLabel{}, errors.New("email label is too long")
	}

	return EmailDestinationLabel{value: value}, nil
}

func NewDiscordDestinationLabel(input string) (DiscordDestinationLabel, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return DiscordDestinationLabel{}, errors.New("discord label is required")
	}

	if strings.ContainsAny(value, "\r\n\t") {
		return DiscordDestinationLabel{}, errors.New("discord label cannot contain control whitespace")
	}

	if utf8.RuneCountInString(value) > 80 {
		return DiscordDestinationLabel{}, errors.New("discord label is too long")
	}

	return DiscordDestinationLabel{value: value}, nil
}

func NewGoogleChatDestinationLabel(input string) (GoogleChatDestinationLabel, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return GoogleChatDestinationLabel{}, errors.New("google chat label is required")
	}

	if strings.ContainsAny(value, "\r\n\t") {
		return GoogleChatDestinationLabel{}, errors.New("google chat label cannot contain control whitespace")
	}

	if utf8.RuneCountInString(value) > 80 {
		return GoogleChatDestinationLabel{}, errors.New("google chat label is too long")
	}

	return GoogleChatDestinationLabel{value: value}, nil
}

func NewNtfyDestinationLabel(input string) (NtfyDestinationLabel, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return NtfyDestinationLabel{}, errors.New("ntfy label is required")
	}

	if strings.ContainsAny(value, "\r\n\t") {
		return NtfyDestinationLabel{}, errors.New("ntfy label cannot contain control whitespace")
	}

	if utf8.RuneCountInString(value) > 80 {
		return NtfyDestinationLabel{}, errors.New("ntfy label is too long")
	}

	return NtfyDestinationLabel{value: value}, nil
}

func NewTeamsDestinationLabel(input string) (TeamsDestinationLabel, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return TeamsDestinationLabel{}, errors.New("microsoft teams label is required")
	}

	if strings.ContainsAny(value, "\r\n\t") {
		return TeamsDestinationLabel{}, errors.New("microsoft teams label cannot contain control whitespace")
	}

	if utf8.RuneCountInString(value) > 80 {
		return TeamsDestinationLabel{}, errors.New("microsoft teams label is too long")
	}

	return TeamsDestinationLabel{value: value}, nil
}

func NewNtfyTopic(input string) (NtfyTopic, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return NtfyTopic{}, errors.New("ntfy topic is required")
	}

	if strings.ContainsAny(value, "\r\n\t /?#") {
		return NtfyTopic{}, errors.New("ntfy topic is invalid")
	}

	if utf8.RuneCountInString(value) > 128 {
		return NtfyTopic{}, errors.New("ntfy topic is too long")
	}

	return NtfyTopic{value: value}, nil
}

func NewNotificationSubject(input string) (NotificationSubject, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return NotificationSubject{}, errors.New("notification subject is required")
	}

	if strings.ContainsAny(value, "\r\n") {
		return NotificationSubject{}, errors.New("notification subject cannot contain line breaks")
	}

	if utf8.RuneCountInString(value) > 240 {
		return NotificationSubject{}, errors.New("notification subject is too long")
	}

	return NotificationSubject{value: value}, nil
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

func (address EmailAddress) String() string {
	return address.value
}

func (label EmailDestinationLabel) String() string {
	return label.value
}

func (label DiscordDestinationLabel) String() string {
	return label.value
}

func (label GoogleChatDestinationLabel) String() string {
	return label.value
}

func (label NtfyDestinationLabel) String() string {
	return label.value
}

func (label TeamsDestinationLabel) String() string {
	return label.value
}

func (topic NtfyTopic) String() string {
	return topic.value
}

func (subject NotificationSubject) String() string {
	return subject.value
}

func (text NotificationText) String() string {
	return text.value
}

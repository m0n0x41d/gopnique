package domain

import (
	"errors"
	"regexp"
	"strings"
)

var uuidHexPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type OrganizationID struct {
	value string
}

type ProjectID struct {
	value string
}

type ProjectKeyID struct {
	value string
}

type EventID struct {
	value string
}

type IssueID struct {
	value string
}

type TelegramDestinationID struct {
	value string
}

type WebhookDestinationID struct {
	value string
}

type EmailDestinationID struct {
	value string
}

type DiscordDestinationID struct {
	value string
}

type GoogleChatDestinationID struct {
	value string
}

type NtfyDestinationID struct {
	value string
}

type TeamsDestinationID struct {
	value string
}

type ZulipDestinationID struct {
	value string
}

type AlertRuleID struct {
	value string
}

type APITokenID struct {
	value string
}

type TeamID struct {
	value string
}

type NotificationIntentID struct {
	value string
}

func NewOrganizationID(input string) (OrganizationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return OrganizationID{}, err
	}

	return OrganizationID{value: value}, nil
}

func NewProjectID(input string) (ProjectID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return ProjectID{}, err
	}

	return ProjectID{value: value}, nil
}

func NewProjectKeyID(input string) (ProjectKeyID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return ProjectKeyID{}, err
	}

	return ProjectKeyID{value: value}, nil
}

func NewEventID(input string) (EventID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return EventID{}, err
	}

	return EventID{value: value}, nil
}

func NewIssueID(input string) (IssueID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return IssueID{}, err
	}

	return IssueID{value: value}, nil
}

func NewTelegramDestinationID(input string) (TelegramDestinationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return TelegramDestinationID{}, err
	}

	return TelegramDestinationID{value: value}, nil
}

func NewWebhookDestinationID(input string) (WebhookDestinationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return WebhookDestinationID{}, err
	}

	return WebhookDestinationID{value: value}, nil
}

func NewEmailDestinationID(input string) (EmailDestinationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return EmailDestinationID{}, err
	}

	return EmailDestinationID{value: value}, nil
}

func NewDiscordDestinationID(input string) (DiscordDestinationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return DiscordDestinationID{}, err
	}

	return DiscordDestinationID{value: value}, nil
}

func NewGoogleChatDestinationID(input string) (GoogleChatDestinationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return GoogleChatDestinationID{}, err
	}

	return GoogleChatDestinationID{value: value}, nil
}

func NewNtfyDestinationID(input string) (NtfyDestinationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return NtfyDestinationID{}, err
	}

	return NtfyDestinationID{value: value}, nil
}

func NewTeamsDestinationID(input string) (TeamsDestinationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return TeamsDestinationID{}, err
	}

	return TeamsDestinationID{value: value}, nil
}

func NewZulipDestinationID(input string) (ZulipDestinationID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return ZulipDestinationID{}, err
	}

	return ZulipDestinationID{value: value}, nil
}

func NewAlertRuleID(input string) (AlertRuleID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return AlertRuleID{}, err
	}

	return AlertRuleID{value: value}, nil
}

func NewAPITokenID(input string) (APITokenID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return APITokenID{}, err
	}

	return APITokenID{value: value}, nil
}

func NewTeamID(input string) (TeamID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return TeamID{}, err
	}

	return TeamID{value: value}, nil
}

func NewNotificationIntentID(input string) (NotificationIntentID, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return NotificationIntentID{}, err
	}

	return NotificationIntentID{value: value}, nil
}

func (id OrganizationID) String() string {
	return dashedUUID(id.value)
}

func (id ProjectID) String() string {
	return dashedUUID(id.value)
}

func (id ProjectKeyID) String() string {
	return dashedUUID(id.value)
}

func (id EventID) String() string {
	return dashedUUID(id.value)
}

func (id IssueID) String() string {
	return dashedUUID(id.value)
}

func (id TelegramDestinationID) String() string {
	return dashedUUID(id.value)
}

func (id WebhookDestinationID) String() string {
	return dashedUUID(id.value)
}

func (id EmailDestinationID) String() string {
	return dashedUUID(id.value)
}

func (id DiscordDestinationID) String() string {
	return dashedUUID(id.value)
}

func (id GoogleChatDestinationID) String() string {
	return dashedUUID(id.value)
}

func (id NtfyDestinationID) String() string {
	return dashedUUID(id.value)
}

func (id TeamsDestinationID) String() string {
	return dashedUUID(id.value)
}

func (id ZulipDestinationID) String() string {
	return dashedUUID(id.value)
}

func (id AlertRuleID) String() string {
	return dashedUUID(id.value)
}

func (id APITokenID) String() string {
	return dashedUUID(id.value)
}

func (id TeamID) String() string {
	return dashedUUID(id.value)
}

func (id NotificationIntentID) String() string {
	return dashedUUID(id.value)
}

func (id EventID) Hex() string {
	return id.value
}

func normalizeUUID(input string) (string, error) {
	value := strings.TrimSpace(input)
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "-", "")

	if !uuidHexPattern.MatchString(value) {
		return "", errors.New("invalid uuid")
	}

	return value, nil
}

func dashedUUID(value string) string {
	if value == "" {
		return ""
	}

	parts := []string{
		value[0:8],
		value[8:12],
		value[12:16],
		value[16:20],
		value[20:32],
	}

	return strings.Join(parts, "-")
}

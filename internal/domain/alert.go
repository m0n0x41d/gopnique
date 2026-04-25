package domain

import (
	"errors"
	"strings"
)

type AlertTrigger string
type AlertActionProvider string

const (
	AlertTriggerIssueOpened AlertTrigger = "issue_opened"

	AlertActionProviderTelegram   AlertActionProvider = "telegram"
	AlertActionProviderWebhook    AlertActionProvider = "webhook"
	AlertActionProviderEmail      AlertActionProvider = "email"
	AlertActionProviderDiscord    AlertActionProvider = "discord"
	AlertActionProviderGoogleChat AlertActionProvider = "google_chat"
	AlertActionProviderNtfy       AlertActionProvider = "ntfy"
	AlertActionProviderTeams      AlertActionProvider = "microsoft_teams"
	AlertActionProviderZulip      AlertActionProvider = "zulip"
)

type AlertRuleName struct {
	value string
}

func NewAlertRuleName(input string) (AlertRuleName, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return AlertRuleName{}, errors.New("alert rule name is required")
	}

	return AlertRuleName{value: value}, nil
}

func (trigger AlertTrigger) Valid() bool {
	return trigger == AlertTriggerIssueOpened
}

func (provider AlertActionProvider) Valid() bool {
	return provider == AlertActionProviderTelegram ||
		provider == AlertActionProviderWebhook ||
		provider == AlertActionProviderEmail ||
		provider == AlertActionProviderDiscord ||
		provider == AlertActionProviderGoogleChat ||
		provider == AlertActionProviderNtfy ||
		provider == AlertActionProviderTeams ||
		provider == AlertActionProviderZulip
}

func (trigger AlertTrigger) String() string {
	return string(trigger)
}

func (provider AlertActionProvider) String() string {
	return string(provider)
}

func (name AlertRuleName) String() string {
	return name.value
}

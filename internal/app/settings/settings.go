package settings

import (
	"context"
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Manager interface {
	ShowProjectSettings(ctx context.Context, query ProjectSettingsQuery) result.Result[ProjectSettingsView]
	CreateTelegramDestination(ctx context.Context, command AddTelegramDestinationCommand) result.Result[SettingsMutationResult]
	CreateWebhookDestination(ctx context.Context, command AddWebhookDestinationCommand) result.Result[SettingsMutationResult]
	CreateIssueOpenedAlert(ctx context.Context, command AddIssueOpenedAlertCommand) result.Result[SettingsMutationResult]
	SetIssueOpenedAlertStatus(ctx context.Context, command SetIssueOpenedAlertStatusCommand) result.Result[SettingsMutationResult]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type ProjectSettingsQuery struct {
	Scope Scope
}

type AddTelegramDestinationCommand struct {
	Scope  Scope
	ChatID string
	Label  string
}

type AddWebhookDestinationCommand struct {
	Scope Scope
	URL   string
	Label string
}

type AddIssueOpenedAlertCommand struct {
	Scope         Scope
	Provider      domain.AlertActionProvider
	DestinationID string
	Name          string
}

type AddIssueOpenedTelegramAlertCommand = AddIssueOpenedAlertCommand

type SetIssueOpenedAlertStatusCommand struct {
	Scope   Scope
	RuleID  string
	Enabled bool
}

type SettingsMutationResult struct {
	DestinationID string
	RuleID        string
	ActionID      string
}

type ProjectSettingsView struct {
	TelegramDestinations []TelegramDestinationView
	WebhookDestinations  []WebhookDestinationView
	IssueOpenedAlerts    []IssueOpenedAlertView
	DeliveryIntents      []DeliveryIntentView
}

type TelegramDestinationView struct {
	ID     string
	Label  string
	ChatID string
	Status string
}

type WebhookDestinationView struct {
	ID     string
	Label  string
	URL    string
	Status string
}

type IssueOpenedAlertView struct {
	ID               string
	Name             string
	Provider         string
	DestinationID    string
	DestinationLabel string
	Status           string
}

type DeliveryIntentView struct {
	ID               string
	Provider         string
	DestinationLabel string
	Status           string
	Attempts         int
	ResponseCode     string
	LastError        string
	EventID          string
	IssueID          string
	CreatedAt        string
	DeliveredAt      string
}

func ShowProjectSettings(
	ctx context.Context,
	manager Manager,
	query ProjectSettingsQuery,
) result.Result[ProjectSettingsView] {
	if manager == nil {
		return result.Err[ProjectSettingsView](errors.New("settings manager is required"))
	}

	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[ProjectSettingsView](scopeErr)
	}

	return manager.ShowProjectSettings(ctx, query)
}

func AddTelegramDestination(
	ctx context.Context,
	manager Manager,
	command AddTelegramDestinationCommand,
) result.Result[SettingsMutationResult] {
	if manager == nil {
		return result.Err[SettingsMutationResult](errors.New("settings manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[SettingsMutationResult](scopeErr)
	}

	return manager.CreateTelegramDestination(ctx, command)
}

func AddWebhookDestination(
	ctx context.Context,
	resolver outbound.Resolver,
	manager Manager,
	command AddWebhookDestinationCommand,
) result.Result[SettingsMutationResult] {
	if manager == nil {
		return result.Err[SettingsMutationResult](errors.New("settings manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[SettingsMutationResult](scopeErr)
	}

	destinationResult := outbound.ValidateDestination(ctx, resolver, command.URL)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		return result.Err[SettingsMutationResult](destinationErr)
	}

	return manager.CreateWebhookDestination(
		ctx,
		AddWebhookDestinationCommand{
			Scope: command.Scope,
			URL:   destination.String(),
			Label: command.Label,
		},
	)
}

func AddIssueOpenedTelegramAlert(
	ctx context.Context,
	manager Manager,
	command AddIssueOpenedAlertCommand,
) result.Result[SettingsMutationResult] {
	command.Provider = domain.AlertActionProviderTelegram

	return AddIssueOpenedAlert(ctx, manager, command)
}

func AddIssueOpenedAlert(
	ctx context.Context,
	manager Manager,
	command AddIssueOpenedAlertCommand,
) result.Result[SettingsMutationResult] {
	if manager == nil {
		return result.Err[SettingsMutationResult](errors.New("settings manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[SettingsMutationResult](scopeErr)
	}

	if !command.Provider.Valid() {
		return result.Err[SettingsMutationResult](errors.New("alert provider is invalid"))
	}

	return manager.CreateIssueOpenedAlert(ctx, command)
}

func SetIssueOpenedAlertStatus(
	ctx context.Context,
	manager Manager,
	command SetIssueOpenedAlertStatusCommand,
) result.Result[SettingsMutationResult] {
	if manager == nil {
		return result.Err[SettingsMutationResult](errors.New("settings manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[SettingsMutationResult](scopeErr)
	}

	_, ruleIDErr := domain.NewAlertRuleID(command.RuleID)
	if ruleIDErr != nil {
		return result.Err[SettingsMutationResult](ruleIDErr)
	}

	return manager.SetIssueOpenedAlertStatus(ctx, command)
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("settings scope is required")
	}

	return nil
}

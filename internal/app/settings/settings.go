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
	CreateEmailDestination(ctx context.Context, command AddEmailDestinationCommand) result.Result[SettingsMutationResult]
	CreateDiscordDestination(ctx context.Context, command AddDiscordDestinationCommand) result.Result[SettingsMutationResult]
	CreateGoogleChatDestination(ctx context.Context, command AddGoogleChatDestinationCommand) result.Result[SettingsMutationResult]
	CreateNtfyDestination(ctx context.Context, command AddNtfyDestinationCommand) result.Result[SettingsMutationResult]
	CreateTeamsDestination(ctx context.Context, command AddTeamsDestinationCommand) result.Result[SettingsMutationResult]
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

type AddEmailDestinationCommand struct {
	Scope   Scope
	Address string
	Label   string
}

type AddDiscordDestinationCommand struct {
	Scope Scope
	URL   string
	Label string
}

type AddGoogleChatDestinationCommand struct {
	Scope Scope
	URL   string
	Label string
}

type AddNtfyDestinationCommand struct {
	Scope Scope
	URL   string
	Topic string
	Label string
}

type AddTeamsDestinationCommand struct {
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
	TelegramDestinations   []TelegramDestinationView
	WebhookDestinations    []WebhookDestinationView
	EmailDestinations      []EmailDestinationView
	DiscordDestinations    []DiscordDestinationView
	GoogleChatDestinations []GoogleChatDestinationView
	NtfyDestinations       []NtfyDestinationView
	TeamsDestinations      []TeamsDestinationView
	IssueOpenedAlerts      []IssueOpenedAlertView
	DeliveryIntents        []DeliveryIntentView
	RetentionPolicy        RetentionPolicyView
	QuotaPolicy            QuotaPolicyView
	RateLimitPolicy        RateLimitPolicyView
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

type EmailDestinationView struct {
	ID      string
	Label   string
	Address string
	Status  string
}

type DiscordDestinationView struct {
	ID     string
	Label  string
	URL    string
	Status string
}

type GoogleChatDestinationView struct {
	ID     string
	Label  string
	URL    string
	Status string
}

type NtfyDestinationView struct {
	ID     string
	Label  string
	URL    string
	Topic  string
	Status string
}

type TeamsDestinationView struct {
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

type RetentionPolicyView struct {
	EventRetentionDays      int
	PayloadRetentionDays    int
	DeliveryRetentionDays   int
	UserReportRetentionDays int
	Status                  string
}

type QuotaPolicyView struct {
	OrganizationEnabled    bool
	OrganizationDailyLimit int
	ProjectEnabled         bool
	ProjectDailyLimit      int
}

type RateLimitPolicyView struct {
	PublicKey      string
	Enabled        bool
	WindowSeconds  int
	EventLimit     int
	RequestsPerMin int
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

func AddEmailDestination(
	ctx context.Context,
	manager Manager,
	command AddEmailDestinationCommand,
) result.Result[SettingsMutationResult] {
	if manager == nil {
		return result.Err[SettingsMutationResult](errors.New("settings manager is required"))
	}

	scopeErr := requireScope(command.Scope)
	if scopeErr != nil {
		return result.Err[SettingsMutationResult](scopeErr)
	}

	return manager.CreateEmailDestination(ctx, command)
}

func AddDiscordDestination(
	ctx context.Context,
	resolver outbound.Resolver,
	manager Manager,
	command AddDiscordDestinationCommand,
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

	return manager.CreateDiscordDestination(
		ctx,
		AddDiscordDestinationCommand{
			Scope: command.Scope,
			URL:   destination.String(),
			Label: command.Label,
		},
	)
}

func AddGoogleChatDestination(
	ctx context.Context,
	resolver outbound.Resolver,
	manager Manager,
	command AddGoogleChatDestinationCommand,
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

	return manager.CreateGoogleChatDestination(
		ctx,
		AddGoogleChatDestinationCommand{
			Scope: command.Scope,
			URL:   destination.String(),
			Label: command.Label,
		},
	)
}

func AddNtfyDestination(
	ctx context.Context,
	resolver outbound.Resolver,
	manager Manager,
	command AddNtfyDestinationCommand,
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

	return manager.CreateNtfyDestination(
		ctx,
		AddNtfyDestinationCommand{
			Scope: command.Scope,
			URL:   destination.String(),
			Topic: command.Topic,
			Label: command.Label,
		},
	)
}

func AddTeamsDestination(
	ctx context.Context,
	resolver outbound.Resolver,
	manager Manager,
	command AddTeamsDestinationCommand,
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

	return manager.CreateTeamsDestination(
		ctx,
		AddTeamsDestinationCommand{
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

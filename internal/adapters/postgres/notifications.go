package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	"github.com/ivanzakutnii/error-tracker/internal/app/notifications"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

type TelegramDestinationInput struct {
	ProjectRef string
	ChatID     string
	Label      string
}

type TelegramDestinationResult struct {
	DestinationID string
	ProjectID     string
	ChatID        string
	Label         string
}

type IssueOpenedTelegramAlertInput struct {
	ProjectRef    string
	DestinationID string
	Name          string
}

type IssueOpenedTelegramAlertResult struct {
	RuleID        string
	ActionID      string
	DestinationID string
	ProjectID     string
	Name          string
}

type WebhookDestinationInput struct {
	ProjectRef string
	URL        string
	Label      string
}

type WebhookDestinationResult struct {
	DestinationID string
	ProjectID     string
	URL           string
	Label         string
}

func (store *Store) AddTelegramDestination(
	ctx context.Context,
	input TelegramDestinationInput,
) (TelegramDestinationResult, error) {
	projectRef, refErr := domain.NewProjectRef(input.ProjectRef)
	if refErr != nil {
		return TelegramDestinationResult{}, refErr
	}

	chatID, chatErr := domain.NewTelegramChatID(input.ChatID)
	if chatErr != nil {
		return TelegramDestinationResult{}, chatErr
	}

	label, labelErr := domain.NewTelegramDestinationLabel(input.Label)
	if labelErr != nil {
		return TelegramDestinationResult{}, labelErr
	}

	projectResult, projectErr := store.findProjectByRef(ctx, projectRef)
	if projectErr != nil {
		return TelegramDestinationResult{}, projectErr
	}

	return store.addTelegramDestinationForProject(ctx, projectResult, chatID, label)
}

func (store *Store) CreateTelegramDestination(
	ctx context.Context,
	command settingsapp.AddTelegramDestinationCommand,
) result.Result[settingsapp.SettingsMutationResult] {
	chatID, chatErr := domain.NewTelegramChatID(command.ChatID)
	if chatErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](chatErr)
	}

	label, labelErr := domain.NewTelegramDestinationLabel(command.Label)
	if labelErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](labelErr)
	}

	project := projectRefResult{
		OrganizationID: command.Scope.OrganizationID,
		ProjectID:      command.Scope.ProjectID,
	}
	destination, destinationErr := store.addTelegramDestinationForProject(ctx, project, chatID, label)
	if destinationErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](destinationErr)
	}

	return result.Ok(settingsapp.SettingsMutationResult{DestinationID: destination.DestinationID})
}

func (store *Store) AddWebhookDestination(
	ctx context.Context,
	input WebhookDestinationInput,
) (WebhookDestinationResult, error) {
	projectRef, refErr := domain.NewProjectRef(input.ProjectRef)
	if refErr != nil {
		return WebhookDestinationResult{}, refErr
	}

	destinationResult := outbound.ParseDestinationURL(input.URL)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		return WebhookDestinationResult{}, destinationErr
	}

	label, labelErr := domain.NewWebhookDestinationLabel(input.Label)
	if labelErr != nil {
		return WebhookDestinationResult{}, labelErr
	}

	projectResult, projectErr := store.findProjectByRef(ctx, projectRef)
	if projectErr != nil {
		return WebhookDestinationResult{}, projectErr
	}

	return store.addWebhookDestinationForProject(ctx, projectResult, destinationURL, label)
}

func (store *Store) CreateWebhookDestination(
	ctx context.Context,
	command settingsapp.AddWebhookDestinationCommand,
) result.Result[settingsapp.SettingsMutationResult] {
	destinationResult := outbound.ParseDestinationURL(command.URL)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](destinationErr)
	}

	label, labelErr := domain.NewWebhookDestinationLabel(command.Label)
	if labelErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](labelErr)
	}

	project := projectRefResult{
		OrganizationID: command.Scope.OrganizationID,
		ProjectID:      command.Scope.ProjectID,
	}
	destination, addErr := store.addWebhookDestinationForProject(ctx, project, destinationURL, label)
	if addErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](addErr)
	}

	return result.Ok(settingsapp.SettingsMutationResult{DestinationID: destination.DestinationID})
}

func (store *Store) CreateIssueOpenedAlert(
	ctx context.Context,
	command settingsapp.AddIssueOpenedAlertCommand,
) result.Result[settingsapp.SettingsMutationResult] {
	if command.Provider == domain.AlertActionProviderTelegram {
		_, destinationErr := domain.NewTelegramDestinationID(command.DestinationID)
		if destinationErr != nil {
			return result.Err[settingsapp.SettingsMutationResult](destinationErr)
		}
	}

	if command.Provider == domain.AlertActionProviderWebhook {
		_, destinationErr := domain.NewWebhookDestinationID(command.DestinationID)
		if destinationErr != nil {
			return result.Err[settingsapp.SettingsMutationResult](destinationErr)
		}
	}

	name, nameErr := domain.NewAlertRuleName(command.Name)
	if nameErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](nameErr)
	}

	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](beginErr)
	}

	project := projectRefResult{
		OrganizationID: command.Scope.OrganizationID,
		ProjectID:      command.Scope.ProjectID,
	}
	alert, alertErr := store.addIssueOpenedAlertInTx(ctx, tx, project, command.Provider, command.DestinationID, name)
	if alertErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[settingsapp.SettingsMutationResult](alertErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](commitErr)
	}

	return result.Ok(settingsapp.SettingsMutationResult{
		DestinationID: alert.DestinationID,
		RuleID:        alert.RuleID,
		ActionID:      alert.ActionID,
	})
}

func (store *Store) SetIssueOpenedAlertStatus(
	ctx context.Context,
	command settingsapp.SetIssueOpenedAlertStatusCommand,
) result.Result[settingsapp.SettingsMutationResult] {
	ruleID, ruleIDErr := domain.NewAlertRuleID(command.RuleID)
	if ruleIDErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](ruleIDErr)
	}

	query := `
update alert_rules
set enabled = $4
where organization_id = $1
  and project_id = $2
  and id = $3
  and trigger = 'issue_opened'
`
	tag, execErr := store.pool.Exec(
		ctx,
		query,
		command.Scope.OrganizationID.String(),
		command.Scope.ProjectID.String(),
		ruleID.String(),
		command.Enabled,
	)
	if execErr != nil {
		return result.Err[settingsapp.SettingsMutationResult](execErr)
	}

	if tag.RowsAffected() != 1 {
		return result.Err[settingsapp.SettingsMutationResult](errors.New("issue-opened alert not found"))
	}

	return result.Ok(settingsapp.SettingsMutationResult{RuleID: ruleID.String()})
}

func (store *Store) ShowProjectSettings(
	ctx context.Context,
	query settingsapp.ProjectSettingsQuery,
) result.Result[settingsapp.ProjectSettingsView] {
	destinationsResult := store.listTelegramDestinations(ctx, query.Scope)
	destinations, destinationsErr := destinationsResult.Value()
	if destinationsErr != nil {
		return result.Err[settingsapp.ProjectSettingsView](destinationsErr)
	}

	webhooksResult := store.listWebhookDestinations(ctx, query.Scope)
	webhooks, webhooksErr := webhooksResult.Value()
	if webhooksErr != nil {
		return result.Err[settingsapp.ProjectSettingsView](webhooksErr)
	}

	alertsResult := store.listIssueOpenedAlerts(ctx, query.Scope)
	alerts, alertsErr := alertsResult.Value()
	if alertsErr != nil {
		return result.Err[settingsapp.ProjectSettingsView](alertsErr)
	}

	deliveriesResult := store.listDeliveryIntents(ctx, query.Scope)
	deliveries, deliveriesErr := deliveriesResult.Value()
	if deliveriesErr != nil {
		return result.Err[settingsapp.ProjectSettingsView](deliveriesErr)
	}

	return result.Ok(settingsapp.ProjectSettingsView{
		TelegramDestinations: destinations,
		WebhookDestinations:  webhooks,
		IssueOpenedAlerts:    alerts,
		DeliveryIntents:      deliveries,
	})
}

func (store *Store) addTelegramDestinationForProject(
	ctx context.Context,
	project projectRefResult,
	chatID domain.TelegramChatID,
	label domain.TelegramDestinationLabel,
) (TelegramDestinationResult, error) {
	destinationID, destinationErr := randomUUID()
	if destinationErr != nil {
		return TelegramDestinationResult{}, destinationErr
	}

	query := `
insert into telegram_destinations (
  id,
  organization_id,
  project_id,
  label,
  chat_id,
  enabled,
  created_at
) values (
  $1, $2, $3, $4, $5, true, $6
)
on conflict (project_id, chat_id) do update
set label = excluded.label,
    enabled = true
returning id
`
	var storedID string
	scanErr := store.pool.QueryRow(
		ctx,
		query,
		destinationID,
		project.OrganizationID.String(),
		project.ProjectID.String(),
		label.String(),
		chatID.String(),
		time.Now().UTC(),
	).Scan(&storedID)
	if scanErr != nil {
		return TelegramDestinationResult{}, scanErr
	}

	return TelegramDestinationResult{
		DestinationID: storedID,
		ProjectID:     project.ProjectID.String(),
		ChatID:        chatID.String(),
		Label:         label.String(),
	}, nil
}

func (store *Store) listTelegramDestinations(
	ctx context.Context,
	scope settingsapp.Scope,
) result.Result[[]settingsapp.TelegramDestinationView] {
	query := `
select id, label, chat_id, enabled
from telegram_destinations
where organization_id = $1
  and project_id = $2
order by created_at asc, label asc
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	)
	if queryErr != nil {
		return result.Err[[]settingsapp.TelegramDestinationView](queryErr)
	}
	defer rows.Close()

	destinations := []settingsapp.TelegramDestinationView{}
	for rows.Next() {
		var destination settingsapp.TelegramDestinationView
		var enabled bool
		scanErr := rows.Scan(
			&destination.ID,
			&destination.Label,
			&destination.ChatID,
			&enabled,
		)
		if scanErr != nil {
			return result.Err[[]settingsapp.TelegramDestinationView](scanErr)
		}

		destination.Status = statusFromEnabled(enabled)
		destinations = append(destinations, destination)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]settingsapp.TelegramDestinationView](rowsErr)
	}

	return result.Ok(destinations)
}

func (store *Store) addWebhookDestinationForProject(
	ctx context.Context,
	project projectRefResult,
	destinationURL outbound.DestinationURL,
	label domain.WebhookDestinationLabel,
) (WebhookDestinationResult, error) {
	destinationID, destinationErr := randomUUID()
	if destinationErr != nil {
		return WebhookDestinationResult{}, destinationErr
	}

	query := `
insert into webhook_destinations (
  id,
  organization_id,
  project_id,
  label,
  url,
  enabled,
  created_at
) values (
  $1, $2, $3, $4, $5, true, $6
)
on conflict (project_id, url) do update
set label = excluded.label,
    enabled = true
returning id
`
	var storedID string
	scanErr := store.pool.QueryRow(
		ctx,
		query,
		destinationID,
		project.OrganizationID.String(),
		project.ProjectID.String(),
		label.String(),
		destinationURL.String(),
		time.Now().UTC(),
	).Scan(&storedID)
	if scanErr != nil {
		return WebhookDestinationResult{}, scanErr
	}

	return WebhookDestinationResult{
		DestinationID: storedID,
		ProjectID:     project.ProjectID.String(),
		URL:           destinationURL.String(),
		Label:         label.String(),
	}, nil
}

func (store *Store) listWebhookDestinations(
	ctx context.Context,
	scope settingsapp.Scope,
) result.Result[[]settingsapp.WebhookDestinationView] {
	query := `
select id, label, url, enabled
from webhook_destinations
where organization_id = $1
  and project_id = $2
order by created_at asc, label asc
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	)
	if queryErr != nil {
		return result.Err[[]settingsapp.WebhookDestinationView](queryErr)
	}
	defer rows.Close()

	destinations := []settingsapp.WebhookDestinationView{}
	for rows.Next() {
		var destination settingsapp.WebhookDestinationView
		var enabled bool
		scanErr := rows.Scan(
			&destination.ID,
			&destination.Label,
			&destination.URL,
			&enabled,
		)
		if scanErr != nil {
			return result.Err[[]settingsapp.WebhookDestinationView](scanErr)
		}

		destination.Status = statusFromEnabled(enabled)
		destinations = append(destinations, destination)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]settingsapp.WebhookDestinationView](rowsErr)
	}

	return result.Ok(destinations)
}

func (store *Store) listIssueOpenedAlerts(
	ctx context.Context,
	scope settingsapp.Scope,
) result.Result[[]settingsapp.IssueOpenedAlertView] {
	query := `
select
  ar.id,
  ar.name,
  ara.provider,
  ara.destination_id,
  coalesce(td.label, wd.label),
  (ar.enabled and ara.enabled and coalesce(td.enabled, wd.enabled, false))
from alert_rules ar
join alert_rule_actions ara on ara.rule_id = ar.id
left join telegram_destinations td on ara.provider = 'telegram' and td.id = ara.destination_id
left join webhook_destinations wd on ara.provider = 'webhook' and wd.id = ara.destination_id
where ar.organization_id = $1
  and ar.project_id = $2
  and ar.trigger = 'issue_opened'
order by ar.created_at asc, coalesce(td.label, wd.label) asc
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	)
	if queryErr != nil {
		return result.Err[[]settingsapp.IssueOpenedAlertView](queryErr)
	}
	defer rows.Close()

	alerts := []settingsapp.IssueOpenedAlertView{}
	for rows.Next() {
		var alert settingsapp.IssueOpenedAlertView
		var enabled bool
		scanErr := rows.Scan(
			&alert.ID,
			&alert.Name,
			&alert.Provider,
			&alert.DestinationID,
			&alert.DestinationLabel,
			&enabled,
		)
		if scanErr != nil {
			return result.Err[[]settingsapp.IssueOpenedAlertView](scanErr)
		}

		alert.Status = statusFromEnabled(enabled)
		alerts = append(alerts, alert)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]settingsapp.IssueOpenedAlertView](rowsErr)
	}

	return result.Ok(alerts)
}

func statusFromEnabled(enabled bool) string {
	if enabled {
		return "enabled"
	}

	return "disabled"
}

func (store *Store) listDeliveryIntents(
	ctx context.Context,
	scope settingsapp.Scope,
) result.Result[[]settingsapp.DeliveryIntentView] {
	query := `
select
  ni.id,
  ni.provider,
  coalesce(td.label, wd.label, ni.destination_id::text),
  ni.status,
  ni.attempts,
  ni.provider_status_code,
  ni.last_error,
  e.event_id,
  ni.issue_id,
  ni.created_at,
  ni.delivered_at
from notification_intents ni
join events e on e.id = ni.event_id
left join telegram_destinations td on ni.provider = 'telegram' and td.id = ni.destination_id
left join webhook_destinations wd on ni.provider = 'webhook' and wd.id = ni.destination_id
where ni.organization_id = $1
  and ni.project_id = $2
order by ni.created_at desc
limit 25
`
	rows, queryErr := store.pool.Query(
		ctx,
		query,
		scope.OrganizationID.String(),
		scope.ProjectID.String(),
	)
	if queryErr != nil {
		return result.Err[[]settingsapp.DeliveryIntentView](queryErr)
	}
	defer rows.Close()

	deliveries := []settingsapp.DeliveryIntentView{}
	for rows.Next() {
		delivery, deliveryErr := scanDeliveryIntentView(rows)
		if deliveryErr != nil {
			return result.Err[[]settingsapp.DeliveryIntentView](deliveryErr)
		}

		deliveries = append(deliveries, delivery)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]settingsapp.DeliveryIntentView](rowsErr)
	}

	return result.Ok(deliveries)
}

func scanDeliveryIntentView(rows pgx.Rows) (settingsapp.DeliveryIntentView, error) {
	var delivery settingsapp.DeliveryIntentView
	var responseCode sql.NullInt64
	var lastError sql.NullString
	var createdAt time.Time
	var deliveredAt sql.NullTime

	scanErr := rows.Scan(
		&delivery.ID,
		&delivery.Provider,
		&delivery.DestinationLabel,
		&delivery.Status,
		&delivery.Attempts,
		&responseCode,
		&lastError,
		&delivery.EventID,
		&delivery.IssueID,
		&createdAt,
		&deliveredAt,
	)
	if scanErr != nil {
		return settingsapp.DeliveryIntentView{}, scanErr
	}

	if responseCode.Valid {
		delivery.ResponseCode = int64Text(responseCode.Int64)
	}

	if lastError.Valid {
		delivery.LastError = lastError.String
	}

	delivery.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	if deliveredAt.Valid {
		delivery.DeliveredAt = deliveredAt.Time.UTC().Format(time.RFC3339)
	}

	return delivery, nil
}

func int64Text(value int64) string {
	return strconv.FormatInt(value, 10)
}

func (store *Store) AddIssueOpenedTelegramAlert(
	ctx context.Context,
	input IssueOpenedTelegramAlertInput,
) (IssueOpenedTelegramAlertResult, error) {
	projectRef, refErr := domain.NewProjectRef(input.ProjectRef)
	if refErr != nil {
		return IssueOpenedTelegramAlertResult{}, refErr
	}

	name, nameErr := domain.NewAlertRuleName(input.Name)
	if nameErr != nil {
		return IssueOpenedTelegramAlertResult{}, nameErr
	}

	destinationID, destinationErr := domain.NewTelegramDestinationID(input.DestinationID)
	if destinationErr != nil {
		return IssueOpenedTelegramAlertResult{}, destinationErr
	}

	projectResult, projectErr := store.findProjectByRef(ctx, projectRef)
	if projectErr != nil {
		return IssueOpenedTelegramAlertResult{}, projectErr
	}

	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return IssueOpenedTelegramAlertResult{}, beginErr
	}

	alert, alertErr := store.addIssueOpenedAlertInTx(
		ctx,
		tx,
		projectResult,
		domain.AlertActionProviderTelegram,
		destinationID.String(),
		name,
	)
	if alertErr != nil {
		_ = tx.Rollback(ctx)
		return IssueOpenedTelegramAlertResult{}, alertErr
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return IssueOpenedTelegramAlertResult{}, commitErr
	}

	return alert, nil
}

func (store txStore) EnqueueIssueOpened(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
	change ingest.IssueChange,
) result.Result[ingest.IssueOpenedEnqueueResult] {
	if !change.Created() {
		return result.Ok(ingest.NewIssueOpenedEnqueueResult(0))
	}

	eventRowIDResult := store.eventRowID(ctx, event.Event().ProjectID(), event.Event().EventID())
	eventRowID, eventRowIDErr := eventRowIDResult.Value()
	if eventRowIDErr != nil {
		return result.Err[ingest.IssueOpenedEnqueueResult](eventRowIDErr)
	}

	destinationsResult := store.destinationActionsForIssueOpenedRules(ctx, event.Event().ProjectID())
	destinations, destinationsErr := destinationsResult.Value()
	if destinationsErr != nil {
		return result.Err[ingest.IssueOpenedEnqueueResult](destinationsErr)
	}

	count := 0
	now := time.Now().UTC()
	for _, destination := range destinations {
		intentID, intentErr := randomUUID()
		if intentErr != nil {
			return result.Err[ingest.IssueOpenedEnqueueResult](intentErr)
		}

		insertResult := store.insertNotificationIntent(ctx, intentID, destination, eventRowID, event, change, now)
		inserted, insertErr := insertResult.Value()
		if insertErr != nil {
			return result.Err[ingest.IssueOpenedEnqueueResult](insertErr)
		}

		count += inserted
	}

	return result.Ok(ingest.NewIssueOpenedEnqueueResult(count))
}

type destinationAction struct {
	provider      domain.AlertActionProvider
	destinationID string
}

func (store *Store) ClaimTelegramDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]notifications.TelegramDelivery] {
	query := `
with claimable as (
  select ni.id
  from notification_intents ni
  join telegram_destinations td on td.id = ni.destination_id
  where ni.provider = 'telegram'
    and td.enabled = true
    and ni.status in ('pending', 'failed')
    and ni.next_attempt_at <= $1
    and (ni.locked_until is null or ni.locked_until < $1)
  order by ni.created_at asc
  limit $2
  for update of ni skip locked
),
updated as (
  update notification_intents ni
  set status = 'delivering',
      attempts = attempts + 1,
      locked_until = $1::timestamptz + interval '60 seconds',
      last_error = null
  from claimable
  where ni.id = claimable.id
  returning ni.id, ni.issue_id, ni.event_id, ni.destination_id
)
select
  u.id,
  td.chat_id,
  e.organization_id,
  e.project_id,
  e.event_id,
  e.kind,
  e.level,
  e.title,
  e.platform,
  e.occurred_at,
  e.received_at,
  i.id,
  i.short_id
from updated u
join telegram_destinations td on td.id = u.destination_id
join events e on e.id = u.event_id
join issues i on i.id = u.issue_id
order by i.last_seen_at desc
`
	rows, queryErr := store.pool.Query(ctx, query, now.UTC(), limit)
	if queryErr != nil {
		return result.Err[[]notifications.TelegramDelivery](queryErr)
	}
	defer rows.Close()

	deliveries := []notifications.TelegramDelivery{}
	for rows.Next() {
		delivery, deliveryErr := scanTelegramDelivery(rows)
		if deliveryErr != nil {
			return result.Err[[]notifications.TelegramDelivery](deliveryErr)
		}

		deliveries = append(deliveries, delivery)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]notifications.TelegramDelivery](rowsErr)
	}

	return result.Ok(deliveries)
}

func (store *Store) ClaimWebhookDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]notifications.WebhookDelivery] {
	query := `
with claimable as (
  select ni.id
  from notification_intents ni
  join webhook_destinations wd on wd.id = ni.destination_id
  where ni.provider = 'webhook'
    and wd.enabled = true
    and ni.status in ('pending', 'failed')
    and ni.next_attempt_at <= $1
    and (ni.locked_until is null or ni.locked_until < $1)
  order by ni.created_at asc
  limit $2
  for update of ni skip locked
),
updated as (
  update notification_intents ni
  set status = 'delivering',
      attempts = attempts + 1,
      locked_until = $1::timestamptz + interval '60 seconds',
      provider_status_code = null,
      last_error = null
  from claimable
  where ni.id = claimable.id
  returning ni.id, ni.issue_id, ni.event_id, ni.destination_id
)
select
  u.id,
  wd.url,
  e.organization_id,
  e.project_id,
  e.event_id,
  e.kind,
  e.level,
  e.title,
  e.platform,
  e.occurred_at,
  e.received_at,
  i.id,
  i.short_id
from updated u
join webhook_destinations wd on wd.id = u.destination_id
join events e on e.id = u.event_id
join issues i on i.id = u.issue_id
order by i.last_seen_at desc
`
	rows, queryErr := store.pool.Query(ctx, query, now.UTC(), limit)
	if queryErr != nil {
		return result.Err[[]notifications.WebhookDelivery](queryErr)
	}
	defer rows.Close()

	deliveries := []notifications.WebhookDelivery{}
	for rows.Next() {
		delivery, deliveryErr := scanWebhookDelivery(rows)
		if deliveryErr != nil {
			return result.Err[[]notifications.WebhookDelivery](deliveryErr)
		}

		deliveries = append(deliveries, delivery)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]notifications.WebhookDelivery](rowsErr)
	}

	return result.Ok(deliveries)
}

func (store *Store) MarkTelegramDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt notifications.TelegramSendReceipt,
) result.Result[struct{}] {
	query := `
update notification_intents
set status = 'delivered',
    locked_until = null,
    delivered_at = $2,
    provider_message_id = $3,
    last_error = null
where id = $1
`
	_, execErr := store.pool.Exec(
		ctx,
		query,
		intentID.String(),
		now.UTC(),
		receipt.ProviderMessageID(),
	)
	if execErr != nil {
		return result.Err[struct{}](execErr)
	}

	return result.Ok(struct{}{})
}

func (store *Store) MarkWebhookDelivered(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt notifications.WebhookSendReceipt,
) result.Result[struct{}] {
	query := `
update notification_intents
set status = 'delivered',
    locked_until = null,
    delivered_at = $2,
    provider_status_code = $3,
    provider_message_id = null,
    last_error = null
where id = $1
`
	_, execErr := store.pool.Exec(
		ctx,
		query,
		intentID.String(),
		now.UTC(),
		receipt.Status(),
	)
	if execErr != nil {
		return result.Err[struct{}](execErr)
	}

	return result.Ok(struct{}{})
}

func (store *Store) MarkTelegramFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	reason string,
) result.Result[struct{}] {
	query := `
update notification_intents
set status = 'failed',
    locked_until = null,
    next_attempt_at = $2 + make_interval(mins => least(60, (1 << least(greatest(attempts - 1, 0), 6)))),
    last_error = $3
where id = $1
`
	_, execErr := store.pool.Exec(
		ctx,
		query,
		intentID.String(),
		now.UTC().Add(time.Minute),
		reason,
	)
	if execErr != nil {
		return result.Err[struct{}](execErr)
	}

	return result.Ok(struct{}{})
}

func (store *Store) MarkWebhookFailed(
	ctx context.Context,
	intentID domain.NotificationIntentID,
	now time.Time,
	receipt notifications.WebhookSendReceipt,
) result.Result[struct{}] {
	query := `
update notification_intents
set status = 'failed',
    locked_until = null,
    next_attempt_at = $2 + make_interval(mins => least(60, (1 << least(greatest(attempts - 1, 0), 6)))),
    provider_status_code = nullif($3, 0),
    last_error = $4
where id = $1
`
	_, execErr := store.pool.Exec(
		ctx,
		query,
		intentID.String(),
		now.UTC().Add(time.Minute),
		receipt.Status(),
		receipt.Reason(),
	)
	if execErr != nil {
		return result.Err[struct{}](execErr)
	}

	return result.Ok(struct{}{})
}

type projectRefResult struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

func (store *Store) findProjectByRef(
	ctx context.Context,
	ref domain.ProjectRef,
) (projectRefResult, error) {
	query := `select organization_id, id from projects where ingest_ref = $1`

	var organizationIDText string
	var projectIDText string
	scanErr := store.pool.QueryRow(ctx, query, ref.String()).Scan(&organizationIDText, &projectIDText)
	if scanErr != nil {
		return projectRefResult{}, scanErr
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return projectRefResult{}, organizationErr
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return projectRefResult{}, projectErr
	}

	return projectRefResult{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	}, nil
}

func (store txStore) destinationActionsForIssueOpenedRules(
	ctx context.Context,
	projectID domain.ProjectID,
) result.Result[[]destinationAction] {
	query := `
select distinct ara.provider, ara.destination_id
from alert_rules ar
join alert_rule_actions ara on ara.rule_id = ar.id
left join telegram_destinations td on ara.provider = 'telegram' and td.id = ara.destination_id
left join webhook_destinations wd on ara.provider = 'webhook' and wd.id = ara.destination_id
where ar.project_id = $1
  and ar.trigger = 'issue_opened'
  and ar.enabled = true
  and ara.enabled = true
  and (
    (ara.provider = 'telegram' and td.enabled = true)
    or (ara.provider = 'webhook' and wd.enabled = true)
  )
order by ara.provider asc, ara.destination_id asc
`
	rows, queryErr := store.tx.Query(ctx, query, projectID.String())
	if queryErr != nil {
		return result.Err[[]destinationAction](queryErr)
	}
	defer rows.Close()

	destinations := []destinationAction{}
	for rows.Next() {
		var providerText string
		var destinationID string
		scanErr := rows.Scan(&providerText, &destinationID)
		if scanErr != nil {
			return result.Err[[]destinationAction](scanErr)
		}

		provider := domain.AlertActionProvider(providerText)
		if !provider.Valid() {
			return result.Err[[]destinationAction](errors.New("alert provider is invalid"))
		}

		destinations = append(destinations, destinationAction{
			provider:      provider,
			destinationID: destinationID,
		})
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[[]destinationAction](rowsErr)
	}

	return result.Ok(destinations)
}

func (store *Store) addIssueOpenedAlertInTx(
	ctx context.Context,
	tx pgx.Tx,
	project projectRefResult,
	provider domain.AlertActionProvider,
	destinationID string,
	name domain.AlertRuleName,
) (IssueOpenedTelegramAlertResult, error) {
	destinationErr := ensureDestinationForProject(ctx, tx, project.ProjectID.String(), provider, destinationID)
	if destinationErr != nil {
		return IssueOpenedTelegramAlertResult{}, destinationErr
	}

	ruleID, ruleIDErr := randomUUID()
	if ruleIDErr != nil {
		return IssueOpenedTelegramAlertResult{}, ruleIDErr
	}

	actionID, actionIDErr := randomUUID()
	if actionIDErr != nil {
		return IssueOpenedTelegramAlertResult{}, actionIDErr
	}

	now := time.Now().UTC()
	storedRuleID, ruleErr := upsertIssueOpenedRule(ctx, tx, ruleID, project, name, now)
	if ruleErr != nil {
		return IssueOpenedTelegramAlertResult{}, ruleErr
	}

	storedActionID, actionErr := upsertAlertAction(ctx, tx, actionID, storedRuleID, project, provider, destinationID, now)
	if actionErr != nil {
		return IssueOpenedTelegramAlertResult{}, actionErr
	}

	return IssueOpenedTelegramAlertResult{
		RuleID:        storedRuleID,
		ActionID:      storedActionID,
		DestinationID: destinationID,
		ProjectID:     project.ProjectID.String(),
		Name:          name.String(),
	}, nil
}

func ensureDestinationForProject(
	ctx context.Context,
	tx pgx.Tx,
	projectID string,
	provider domain.AlertActionProvider,
	destinationID string,
) error {
	if provider == domain.AlertActionProviderTelegram {
		return ensureDestinationExists(ctx, tx, "telegram_destinations", "telegram destination is not enabled for project", projectID, destinationID)
	}

	if provider == domain.AlertActionProviderWebhook {
		return ensureDestinationExists(ctx, tx, "webhook_destinations", "webhook destination is not enabled for project", projectID, destinationID)
	}

	return errors.New("alert provider is invalid")
}

func ensureDestinationExists(
	ctx context.Context,
	tx pgx.Tx,
	table string,
	message string,
	projectID string,
	destinationID string,
) error {
	query := `
select exists(
  select 1
  from ` + table + `
  where id = $1 and project_id = $2 and enabled = true
)
`
	var exists bool
	scanErr := tx.QueryRow(ctx, query, destinationID, projectID).Scan(&exists)
	if scanErr != nil {
		return scanErr
	}

	if !exists {
		return errors.New("telegram destination is not enabled for project")
	}

	return nil
}

func upsertIssueOpenedRule(
	ctx context.Context,
	tx pgx.Tx,
	ruleID string,
	project projectRefResult,
	name domain.AlertRuleName,
	now time.Time,
) (string, error) {
	query := `
insert into alert_rules (
  id,
  organization_id,
  project_id,
  trigger,
  name,
  enabled,
  created_at
) values (
  $1, $2, $3, 'issue_opened', $4, true, $5
)
on conflict (project_id, trigger, name) do update
set enabled = true
returning id
`
	var storedRuleID string
	scanErr := tx.QueryRow(
		ctx,
		query,
		ruleID,
		project.OrganizationID.String(),
		project.ProjectID.String(),
		name.String(),
		now,
	).Scan(&storedRuleID)

	return storedRuleID, scanErr
}

func upsertAlertAction(
	ctx context.Context,
	tx pgx.Tx,
	actionID string,
	ruleID string,
	project projectRefResult,
	provider domain.AlertActionProvider,
	destinationID string,
	now time.Time,
) (string, error) {
	query := `
insert into alert_rule_actions (
  id,
  organization_id,
  project_id,
  rule_id,
  provider,
  destination_id,
  enabled,
  created_at
) values (
  $1, $2, $3, $4, $5, $6, true, $7
)
on conflict (rule_id, provider, destination_id) do update
set enabled = true
returning id
`
	var storedActionID string
	scanErr := tx.QueryRow(
		ctx,
		query,
		actionID,
		project.OrganizationID.String(),
		project.ProjectID.String(),
		ruleID,
		provider.String(),
		destinationID,
		now,
	).Scan(&storedActionID)

	return storedActionID, scanErr
}

func (store txStore) insertNotificationIntent(
	ctx context.Context,
	intentID string,
	destination destinationAction,
	eventRowID string,
	event ingestplan.AcceptedEvent,
	change ingest.IssueChange,
	now time.Time,
) result.Result[int] {
	query := `
insert into notification_intents (
  id,
  organization_id,
  project_id,
  issue_id,
  event_id,
  provider,
  destination_id,
  status,
  dedupe_key,
  attempts,
  next_attempt_at,
  created_at
) values (
  $1, $2, $3, $4, $5, $6, $7, 'pending', $8, 0, $9, $9
)
on conflict (dedupe_key) do nothing
`
	dedupeKey := destination.provider.String() + ":issue-opened:" + destination.destinationID + ":" + change.IssueID().String()
	tag, execErr := store.tx.Exec(
		ctx,
		query,
		intentID,
		event.Event().OrganizationID().String(),
		event.Event().ProjectID().String(),
		change.IssueID().String(),
		eventRowID,
		destination.provider.String(),
		destination.destinationID,
		dedupeKey,
		now,
	)
	if execErr != nil {
		return result.Err[int](execErr)
	}

	return result.Ok(int(tag.RowsAffected()))
}

func scanTelegramDelivery(rows pgx.Rows) (notifications.TelegramDelivery, error) {
	var intentIDText string
	var chatIDText string
	var organizationIDText string
	var projectIDText string
	var eventIDText string
	var kindText string
	var levelText string
	var titleText string
	var platform string
	var occurredAt time.Time
	var receivedAt time.Time
	var issueIDText string
	var issueShortID int64

	scanErr := rows.Scan(
		&intentIDText,
		&chatIDText,
		&organizationIDText,
		&projectIDText,
		&eventIDText,
		&kindText,
		&levelText,
		&titleText,
		&platform,
		&occurredAt,
		&receivedAt,
		&issueIDText,
		&issueShortID,
	)
	if scanErr != nil {
		return notifications.TelegramDelivery{}, scanErr
	}

	return newTelegramDelivery(
		intentIDText,
		chatIDText,
		organizationIDText,
		projectIDText,
		eventIDText,
		kindText,
		levelText,
		titleText,
		platform,
		occurredAt,
		receivedAt,
		issueIDText,
		issueShortID,
	)
}

func scanWebhookDelivery(rows pgx.Rows) (notifications.WebhookDelivery, error) {
	var intentIDText string
	var destinationURLText string
	var organizationIDText string
	var projectIDText string
	var eventIDText string
	var kindText string
	var levelText string
	var titleText string
	var platform string
	var occurredAt time.Time
	var receivedAt time.Time
	var issueIDText string
	var issueShortID int64

	scanErr := rows.Scan(
		&intentIDText,
		&destinationURLText,
		&organizationIDText,
		&projectIDText,
		&eventIDText,
		&kindText,
		&levelText,
		&titleText,
		&platform,
		&occurredAt,
		&receivedAt,
		&issueIDText,
		&issueShortID,
	)
	if scanErr != nil {
		return notifications.WebhookDelivery{}, scanErr
	}

	return newWebhookDelivery(
		intentIDText,
		destinationURLText,
		organizationIDText,
		projectIDText,
		eventIDText,
		kindText,
		levelText,
		titleText,
		platform,
		occurredAt,
		receivedAt,
		issueIDText,
		issueShortID,
	)
}

func newTelegramDelivery(
	intentIDText string,
	chatIDText string,
	organizationIDText string,
	projectIDText string,
	eventIDText string,
	kindText string,
	levelText string,
	titleText string,
	platform string,
	occurredAt time.Time,
	receivedAt time.Time,
	issueIDText string,
	issueShortID int64,
) (notifications.TelegramDelivery, error) {
	intentID, intentErr := domain.NewNotificationIntentID(intentIDText)
	if intentErr != nil {
		return notifications.TelegramDelivery{}, intentErr
	}

	chatID, chatErr := domain.NewTelegramChatID(chatIDText)
	if chatErr != nil {
		return notifications.TelegramDelivery{}, chatErr
	}

	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return notifications.TelegramDelivery{}, organizationErr
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return notifications.TelegramDelivery{}, projectErr
	}

	eventID, eventErr := domain.NewEventID(eventIDText)
	if eventErr != nil {
		return notifications.TelegramDelivery{}, eventErr
	}

	level, levelErr := domain.NewEventLevel(levelText)
	if levelErr != nil {
		return notifications.TelegramDelivery{}, levelErr
	}

	title, titleErr := domain.NewEventTitle(titleText)
	if titleErr != nil {
		return notifications.TelegramDelivery{}, titleErr
	}

	occurredPoint, occurredErr := domain.NewTimePoint(occurredAt)
	if occurredErr != nil {
		return notifications.TelegramDelivery{}, occurredErr
	}

	receivedPoint, receivedErr := domain.NewTimePoint(receivedAt)
	if receivedErr != nil {
		return notifications.TelegramDelivery{}, receivedErr
	}

	event, canonicalErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		EventID:              eventID,
		OccurredAt:           occurredPoint,
		ReceivedAt:           receivedPoint,
		Kind:                 domain.EventKind(kindText),
		Level:                level,
		Title:                title,
		Platform:             platform,
		DefaultGroupingParts: []string{title.String()},
	})
	if canonicalErr != nil {
		return notifications.TelegramDelivery{}, canonicalErr
	}

	issueID, issueErr := domain.NewIssueID(issueIDText)
	if issueErr != nil {
		return notifications.TelegramDelivery{}, issueErr
	}

	return notifications.NewTelegramDelivery(
		intentID,
		chatID,
		event,
		issueID,
		issueShortID,
	), nil
}

func newWebhookDelivery(
	intentIDText string,
	destinationURLText string,
	organizationIDText string,
	projectIDText string,
	eventIDText string,
	kindText string,
	levelText string,
	titleText string,
	platform string,
	occurredAt time.Time,
	receivedAt time.Time,
	issueIDText string,
	issueShortID int64,
) (notifications.WebhookDelivery, error) {
	intentID, intentErr := domain.NewNotificationIntentID(intentIDText)
	if intentErr != nil {
		return notifications.WebhookDelivery{}, intentErr
	}

	destinationResult := outbound.ParseDestinationURL(destinationURLText)
	destinationURL, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		return notifications.WebhookDelivery{}, destinationErr
	}

	event, eventErr := newNotificationEvent(
		organizationIDText,
		projectIDText,
		eventIDText,
		kindText,
		levelText,
		titleText,
		platform,
		occurredAt,
		receivedAt,
	)
	if eventErr != nil {
		return notifications.WebhookDelivery{}, eventErr
	}

	issueID, issueErr := domain.NewIssueID(issueIDText)
	if issueErr != nil {
		return notifications.WebhookDelivery{}, issueErr
	}

	return notifications.NewWebhookDelivery(
		intentID,
		destinationURL,
		event,
		issueID,
		issueShortID,
	), nil
}

func newNotificationEvent(
	organizationIDText string,
	projectIDText string,
	eventIDText string,
	kindText string,
	levelText string,
	titleText string,
	platform string,
	occurredAt time.Time,
	receivedAt time.Time,
) (domain.CanonicalEvent, error) {
	organizationID, organizationErr := domain.NewOrganizationID(organizationIDText)
	if organizationErr != nil {
		return domain.CanonicalEvent{}, organizationErr
	}

	projectID, projectErr := domain.NewProjectID(projectIDText)
	if projectErr != nil {
		return domain.CanonicalEvent{}, projectErr
	}

	eventID, eventErr := domain.NewEventID(eventIDText)
	if eventErr != nil {
		return domain.CanonicalEvent{}, eventErr
	}

	level, levelErr := domain.NewEventLevel(levelText)
	if levelErr != nil {
		return domain.CanonicalEvent{}, levelErr
	}

	title, titleErr := domain.NewEventTitle(titleText)
	if titleErr != nil {
		return domain.CanonicalEvent{}, titleErr
	}

	occurredPoint, occurredErr := domain.NewTimePoint(occurredAt)
	if occurredErr != nil {
		return domain.CanonicalEvent{}, occurredErr
	}

	receivedPoint, receivedErr := domain.NewTimePoint(receivedAt)
	if receivedErr != nil {
		return domain.CanonicalEvent{}, receivedErr
	}

	return domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		EventID:              eventID,
		OccurredAt:           occurredPoint,
		ReceivedAt:           receivedPoint,
		Kind:                 domain.EventKind(kindText),
		Level:                level,
		Title:                title,
		Platform:             platform,
		DefaultGroupingParts: []string{title.String()},
	})
}

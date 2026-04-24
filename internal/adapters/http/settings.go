package httpadapter

import (
	"errors"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func notificationSettingsHandler(
	manager settingsapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageAlerts,
		)
		if !sessionOK {
			return
		}

		message := settingsMessageFromNotice(r.URL.Query().Get("saved"))
		renderNotificationSettings(w, r, manager, session, message, false, http.StatusOK)
	}
}

func createTelegramDestinationHandler(
	manager settingsapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageAlerts,
		)
		if !sessionOK {
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			message := templates.NotificationSettingsMessage{
				Text: "Invalid form",
				Kind: "error",
			}
			renderNotificationSettings(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		commandResult := settingsapp.AddTelegramDestination(
			r.Context(),
			manager,
			settingsapp.AddTelegramDestinationCommand{
				Scope:  settingsScopeFromSession(session),
				ChatID: r.PostFormValue("chat_id"),
				Label:  r.PostFormValue("label"),
			},
		)
		_, commandErr := commandResult.Value()
		if commandErr != nil {
			message := templates.NotificationSettingsMessage{
				Text: commandErr.Error(),
				Kind: "error",
			}
			renderNotificationSettings(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		if !isHTMX(r) {
			http.Redirect(w, r, "/settings/notifications?saved=telegram-destination", http.StatusSeeOther)
			return
		}

		message := templates.NotificationSettingsMessage{
			Text: "Telegram destination saved",
			Kind: "success",
		}
		renderNotificationSettings(w, r, manager, session, message, true, http.StatusOK)
	}
}

func createWebhookDestinationHandler(
	manager settingsapp.Manager,
	resolver outbound.Resolver,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageAlerts,
		)
		if !sessionOK {
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			message := templates.NotificationSettingsMessage{
				Text: "Invalid form",
				Kind: "error",
			}
			renderNotificationSettings(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		commandResult := settingsapp.AddWebhookDestination(
			r.Context(),
			resolver,
			manager,
			settingsapp.AddWebhookDestinationCommand{
				Scope: settingsScopeFromSession(session),
				URL:   r.PostFormValue("url"),
				Label: r.PostFormValue("label"),
			},
		)
		_, commandErr := commandResult.Value()
		if commandErr != nil {
			message := templates.NotificationSettingsMessage{
				Text: commandErr.Error(),
				Kind: "error",
			}
			renderNotificationSettings(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		if !isHTMX(r) {
			http.Redirect(w, r, "/settings/notifications?saved=webhook-destination", http.StatusSeeOther)
			return
		}

		message := templates.NotificationSettingsMessage{
			Text: "Webhook destination saved",
			Kind: "success",
		}
		renderNotificationSettings(w, r, manager, session, message, true, http.StatusOK)
	}
}

func createIssueOpenedAlertHandler(
	manager settingsapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageAlerts,
		)
		if !sessionOK {
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			message := templates.NotificationSettingsMessage{
				Text: "Invalid form",
				Kind: "error",
			}
			renderNotificationSettings(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		destination, destinationErr := alertDestinationFromForm(r.PostFormValue("destination"))
		if destinationErr != nil {
			message := templates.NotificationSettingsMessage{
				Text: destinationErr.Error(),
				Kind: "error",
			}
			renderNotificationSettings(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		commandResult := settingsapp.AddIssueOpenedAlert(
			r.Context(),
			manager,
			settingsapp.AddIssueOpenedAlertCommand{
				Scope:         settingsScopeFromSession(session),
				Provider:      destination.provider,
				DestinationID: destination.id,
				Name:          r.PostFormValue("name"),
			},
		)
		_, commandErr := commandResult.Value()
		if commandErr != nil {
			message := templates.NotificationSettingsMessage{
				Text: commandErr.Error(),
				Kind: "error",
			}
			renderNotificationSettings(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		if !isHTMX(r) {
			http.Redirect(w, r, "/settings/notifications?saved=issue-opened-alert", http.StatusSeeOther)
			return
		}

		message := templates.NotificationSettingsMessage{
			Text: "Issue-opened alert saved",
			Kind: "success",
		}
		renderNotificationSettings(w, r, manager, session, message, true, http.StatusOK)
	}
}

func setIssueOpenedAlertStatusHandler(
	manager settingsapp.Manager,
	access operators.Access,
	sessions SessionCodec,
	enabled bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageAlerts,
		)
		if !sessionOK {
			return
		}

		commandResult := settingsapp.SetIssueOpenedAlertStatus(
			r.Context(),
			manager,
			settingsapp.SetIssueOpenedAlertStatusCommand{
				Scope:   settingsScopeFromSession(session),
				RuleID:  r.PathValue("rule_id"),
				Enabled: enabled,
			},
		)
		_, commandErr := commandResult.Value()
		if commandErr != nil {
			message := templates.NotificationSettingsMessage{
				Text: commandErr.Error(),
				Kind: "error",
			}
			renderNotificationSettings(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		if !isHTMX(r) {
			http.Redirect(w, r, "/settings/notifications?saved=issue-opened-alert", http.StatusSeeOther)
			return
		}

		message := templates.NotificationSettingsMessage{
			Text: alertStatusMessage(enabled),
			Kind: "success",
		}
		renderNotificationSettings(w, r, manager, session, message, true, http.StatusOK)
	}
}

func renderNotificationSettings(
	w http.ResponseWriter,
	r *http.Request,
	manager settingsapp.Manager,
	session operators.OperatorSession,
	message templates.NotificationSettingsMessage,
	fragment bool,
	status int,
) {
	viewResult := settingsapp.ShowProjectSettings(
		r.Context(),
		manager,
		settingsapp.ProjectSettingsQuery{Scope: settingsScopeFromSession(session)},
	)
	view, viewErr := viewResult.Value()
	if viewErr != nil {
		http.Error(w, "notification settings unavailable", http.StatusServiceUnavailable)
		return
	}

	component := notificationSettingsComponent(view, message, fragment)
	renderHTMLStatus(w, r, component, status)
}

func notificationSettingsComponent(
	view settingsapp.ProjectSettingsView,
	message templates.NotificationSettingsMessage,
	fragment bool,
) templ.Component {
	if fragment {
		return templates.NotificationSettingsPanel(view, message)
	}

	return templates.NotificationSettingsPage(view, message)
}

func settingsScopeFromSession(session operators.OperatorSession) settingsapp.Scope {
	return settingsapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

func settingsMessageFromNotice(notice string) templates.NotificationSettingsMessage {
	if notice == "telegram-destination" {
		return templates.NotificationSettingsMessage{
			Text: "Telegram destination saved",
			Kind: "success",
		}
	}

	if notice == "webhook-destination" {
		return templates.NotificationSettingsMessage{
			Text: "Webhook destination saved",
			Kind: "success",
		}
	}

	if notice == "issue-opened-alert" {
		return templates.NotificationSettingsMessage{
			Text: "Issue-opened alert saved",
			Kind: "success",
		}
	}

	return templates.NotificationSettingsMessage{}
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func alertStatusMessage(enabled bool) string {
	if enabled {
		return "Issue-opened alert enabled"
	}

	return "Issue-opened alert disabled"
}

type alertDestination struct {
	provider domain.AlertActionProvider
	id       string
}

func alertDestinationFromForm(value string) (alertDestination, error) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[1]) == "" {
		return alertDestination{}, errors.New("alert destination is required")
	}

	provider := domain.AlertActionProvider(parts[0])
	if !provider.Valid() {
		return alertDestination{}, errors.New("alert destination is invalid")
	}

	return alertDestination{provider: provider, id: parts[1]}, nil
}

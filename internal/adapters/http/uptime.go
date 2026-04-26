package httpadapter

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	uptimeapp "github.com/ivanzakutnii/error-tracker/internal/app/uptime"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func uptimeHandler(
	manager uptimeapp.Manager,
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

		message := uptimeMessageFromNotice(r.URL.Query().Get("saved"))
		renderUptime(w, r, manager, session, message, false, http.StatusOK)
	}
}

func createHTTPMonitorHandler(
	manager uptimeapp.Manager,
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
			message := templates.UptimeMessage{Text: "Invalid form", Kind: "error"}
			renderUptime(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		command, commandErr := uptimeCommandFromForm(session, r)
		if commandErr != nil {
			message := templates.UptimeMessage{Text: commandErr.Error(), Kind: "error"}
			renderUptime(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		createResult := uptimeapp.CreateHTTPMonitor(
			r.Context(),
			resolver,
			manager,
			command,
		)
		_, createErr := createResult.Value()
		if createErr != nil {
			message := templates.UptimeMessage{Text: createErr.Error(), Kind: "error"}
			renderUptime(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		if !isHTMX(r) {
			http.Redirect(w, r, "/uptime?saved=monitor", http.StatusSeeOther)
			return
		}

		message := templates.UptimeMessage{Text: "HTTP monitor saved", Kind: "success"}
		renderUptime(w, r, manager, session, message, true, http.StatusOK)
	}
}

func createHeartbeatMonitorHandler(
	manager uptimeapp.Manager,
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
			message := templates.UptimeMessage{Text: "Invalid form", Kind: "error"}
			renderUptime(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		command, commandErr := heartbeatCommandFromForm(session, r)
		if commandErr != nil {
			message := templates.UptimeMessage{Text: commandErr.Error(), Kind: "error"}
			renderUptime(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		createResult := uptimeapp.CreateHeartbeatMonitor(
			r.Context(),
			manager,
			command,
		)
		_, createErr := createResult.Value()
		if createErr != nil {
			message := templates.UptimeMessage{Text: createErr.Error(), Kind: "error"}
			renderUptime(w, r, manager, session, message, isHTMX(r), http.StatusBadRequest)
			return
		}

		if !isHTMX(r) {
			http.Redirect(w, r, "/uptime?saved=heartbeat", http.StatusSeeOther)
			return
		}

		message := templates.UptimeMessage{Text: "Heartbeat monitor saved", Kind: "success"}
		renderUptime(w, r, manager, session, message, true, http.StatusOK)
	}
}

func heartbeatCheckInHandler(manager uptimeapp.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if manager == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "uptime_manager_not_configured"})
			return
		}

		checkInResult := uptimeapp.RecordHeartbeatCheckIn(
			r.Context(),
			manager,
			uptimeapp.HeartbeatCheckInCommand{
				EndpointID: r.PathValue("endpoint_id"),
				CheckedAt:  time.Now().UTC(),
			},
		)
		checkIn, checkInErr := checkInResult.Value()
		if checkInErr != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"detail": "heartbeat_not_found"})
			return
		}

		writeJSON(w, http.StatusAccepted, map[string]string{
			"monitor_id": checkIn.MonitorID,
			"status":     checkIn.CurrentState,
		})
	}
}

func renderUptime(
	w http.ResponseWriter,
	r *http.Request,
	manager uptimeapp.Manager,
	session operators.OperatorSession,
	message templates.UptimeMessage,
	fragment bool,
	status int,
) {
	viewResult := uptimeapp.Show(
		r.Context(),
		manager,
		uptimeapp.Query{
			Scope: uptimeScopeFromSession(session),
			Limit: 100,
		},
	)
	view, viewErr := viewResult.Value()
	if viewErr != nil {
		http.Error(w, "uptime unavailable", http.StatusServiceUnavailable)
		return
	}

	component := uptimeComponent(view, message, fragment)
	renderHTMLStatus(w, r, component, status)
}

func uptimeComponent(
	view uptimeapp.View,
	message templates.UptimeMessage,
	fragment bool,
) templ.Component {
	if fragment {
		return templates.UptimePanel(view, message)
	}

	return templates.UptimePage(view, message)
}

func uptimeCommandFromForm(
	session operators.OperatorSession,
	r *http.Request,
) (uptimeapp.CreateHTTPMonitorCommand, error) {
	intervalSeconds, intervalErr := strconv.Atoi(r.PostFormValue("interval_seconds"))
	if intervalErr != nil {
		return uptimeapp.CreateHTTPMonitorCommand{}, errors.New("monitor interval must be a number")
	}

	timeoutSeconds, timeoutErr := strconv.Atoi(r.PostFormValue("timeout_seconds"))
	if timeoutErr != nil {
		return uptimeapp.CreateHTTPMonitorCommand{}, errors.New("monitor timeout must be a number")
	}

	return uptimeapp.CreateHTTPMonitorCommand{
		Scope:           uptimeScopeFromSession(session),
		ActorID:         session.OperatorID,
		Name:            r.PostFormValue("name"),
		URL:             r.PostFormValue("url"),
		IntervalSeconds: intervalSeconds,
		TimeoutSeconds:  timeoutSeconds,
	}, nil
}

func heartbeatCommandFromForm(
	session operators.OperatorSession,
	r *http.Request,
) (uptimeapp.CreateHeartbeatMonitorCommand, error) {
	intervalSeconds, intervalErr := strconv.Atoi(r.PostFormValue("interval_seconds"))
	if intervalErr != nil {
		return uptimeapp.CreateHeartbeatMonitorCommand{}, errors.New("heartbeat interval must be a number")
	}

	graceSeconds, graceErr := strconv.Atoi(r.PostFormValue("grace_seconds"))
	if graceErr != nil {
		return uptimeapp.CreateHeartbeatMonitorCommand{}, errors.New("heartbeat grace must be a number")
	}

	return uptimeapp.CreateHeartbeatMonitorCommand{
		Scope:           uptimeScopeFromSession(session),
		ActorID:         session.OperatorID,
		Name:            r.PostFormValue("name"),
		IntervalSeconds: intervalSeconds,
		GraceSeconds:    graceSeconds,
	}, nil
}

func uptimeScopeFromSession(session operators.OperatorSession) uptimeapp.Scope {
	return uptimeapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

func uptimeMessageFromNotice(notice string) templates.UptimeMessage {
	if notice == "monitor" {
		return templates.UptimeMessage{Text: "HTTP monitor saved", Kind: "success"}
	}

	if notice == "heartbeat" {
		return templates.UptimeMessage{Text: "Heartbeat monitor saved", Kind: "success"}
	}

	if notice == "status-page" {
		return templates.UptimeMessage{Text: "Status page saved", Kind: "success"}
	}

	return templates.UptimeMessage{}
}

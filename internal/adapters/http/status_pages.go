package httpadapter

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	uptimeapp "github.com/ivanzakutnii/error-tracker/internal/app/uptime"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

const publicStatusPageCacheControl = "public, max-age=30"
const privateStatusPageCacheControl = "private, no-store"

func createStatusPageHandler(
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

		command := statusPageCommandFromForm(session, r)
		createResult := uptimeapp.CreateStatusPage(
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
			http.Redirect(w, r, "/uptime?saved=status-page", http.StatusSeeOther)
			return
		}

		message := templates.UptimeMessage{Text: "Status page saved", Kind: "success"}
		renderUptime(w, r, manager, session, message, true, http.StatusOK)
	}
}

func privateStatusPageHandler(
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
			operators.PermissionReadProject,
		)
		if !sessionOK {
			return
		}

		viewResult := uptimeapp.ShowPrivateStatusPage(
			r.Context(),
			manager,
			uptimeapp.PrivateStatusPageQuery{
				Scope:  uptimeScopeFromSession(session),
				PageID: r.PathValue("page_id"),
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			writeStatusPageError(w, viewErr)
			return
		}

		w.Header().Set("Cache-Control", privateStatusPageCacheControl)
		renderHTML(w, r, templates.PrivateStatusPage(view))
	}
}

func publicStatusPageHandler(
	manager uptimeapp.Manager,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		viewResult := uptimeapp.ShowPublicStatusPage(
			r.Context(),
			manager,
			uptimeapp.PublicStatusPageQuery{Token: r.PathValue("token")},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			writeStatusPageError(w, viewErr)
			return
		}

		w.Header().Set("Cache-Control", publicStatusPageCacheControl)
		renderHTML(w, r, templates.PublicStatusPage(view))
	}
}

func statusPageCommandFromForm(
	session operators.OperatorSession,
	r *http.Request,
) uptimeapp.CreateStatusPageCommand {
	return uptimeapp.CreateStatusPageCommand{
		Scope:      uptimeScopeFromSession(session),
		ActorID:    session.OperatorID,
		Name:       r.PostFormValue("name"),
		Visibility: r.PostFormValue("visibility"),
	}
}

func writeStatusPageError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "status page not found", http.StatusNotFound)
		return
	}

	http.Error(w, "status page unavailable", http.StatusServiceUnavailable)
}

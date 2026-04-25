package httpadapter

import (
	"net/http"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	statsapp "github.com/ivanzakutnii/error-tracker/internal/app/stats"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func projectStatsHandler(
	reader statsapp.Reader,
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

		if reader == nil {
			http.Error(w, "stats reader not configured", http.StatusServiceUnavailable)
			return
		}

		period := statsapp.ParsePeriod(r.URL.Query().Get("period"))
		viewResult := statsapp.ShowProjectStats(
			r.Context(),
			reader,
			statsapp.Query{
				Scope:  statsScopeFromSession(session),
				Period: period,
				Now:    time.Now().UTC(),
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "stats unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.ProjectStatsPage(view))
	}
}

func statsScopeFromSession(session operators.OperatorSession) statsapp.Scope {
	return statsapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

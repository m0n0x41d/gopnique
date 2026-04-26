package httpadapter

import (
	"net/http"

	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	performanceapp "github.com/ivanzakutnii/error-tracker/internal/app/performance"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func performanceListHandler(
	reader performanceapp.Reader,
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
			http.Error(w, "performance reader not configured", http.StatusServiceUnavailable)
			return
		}

		viewResult := performanceapp.List(
			r.Context(),
			reader,
			performanceapp.Query{
				Scope: performanceScopeFromSession(session),
				Limit: 100,
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "performance unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.PerformanceList(view))
	}
}

func performanceDetailHandler(
	reader performanceapp.Reader,
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
			http.Error(w, "performance reader not configured", http.StatusServiceUnavailable)
			return
		}

		viewResult := performanceapp.Detail(
			r.Context(),
			reader,
			performanceapp.DetailQuery{
				Scope:       performanceScopeFromSession(session),
				GroupID:     r.PathValue("group_id"),
				RecentLimit: 100,
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.NotFound(w, r)
			return
		}

		renderHTML(w, r, templates.PerformanceDetail(view))
	}
}

func performanceScopeFromSession(session operators.OperatorSession) performanceapp.Scope {
	return performanceapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

package httpadapter

import (
	"net/http"

	dimensionapp "github.com/ivanzakutnii/error-tracker/internal/app/dimensions"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func environmentsHandler(
	reader dimensionapp.Reader,
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
			http.Error(w, "dimension reader not configured", http.StatusServiceUnavailable)
			return
		}

		viewResult := dimensionapp.ListEnvironments(
			r.Context(),
			reader,
			dimensionapp.Query{Scope: dimensionScopeFromSession(session), Limit: 100},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "environments unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.EnvironmentsPage(view))
	}
}

func releasesHandler(
	reader dimensionapp.Reader,
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
			http.Error(w, "dimension reader not configured", http.StatusServiceUnavailable)
			return
		}

		viewResult := dimensionapp.ListReleases(
			r.Context(),
			reader,
			dimensionapp.Query{Scope: dimensionScopeFromSession(session), Limit: 100},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "releases unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.ReleasesPage(view))
	}
}

func dimensionScopeFromSession(session operators.OperatorSession) dimensionapp.Scope {
	return dimensionapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

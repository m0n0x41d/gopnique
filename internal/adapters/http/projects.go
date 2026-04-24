package httpadapter

import (
	"net/http"

	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	projectapp "github.com/ivanzakutnii/error-tracker/internal/app/projects"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func projectDetailHandler(
	reader projectapp.Reader,
	access operators.Access,
	sessions SessionCodec,
	auth AuthSettings,
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
			http.Error(w, "project reader not configured", http.StatusServiceUnavailable)
			return
		}

		viewResult := projectapp.ShowCurrentProject(
			r.Context(),
			reader,
			projectapp.ProjectQuery{
				Scope: projectapp.Scope{
					OrganizationID: session.OrganizationID,
					ProjectID:      session.ProjectID,
				},
				PublicURL: auth.PublicURL,
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "project unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.ProjectDetail(view))
	}
}

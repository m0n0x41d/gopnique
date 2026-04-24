package httpadapter

import (
	"net/http"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func auditHandler(
	reader auditapp.Reader,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionViewAudit,
		)
		if !sessionOK {
			return
		}

		viewResult := auditapp.List(
			r.Context(),
			reader,
			auditapp.Query{
				Scope: auditapp.Scope{
					OrganizationID: session.OrganizationID,
					ProjectID:      session.ProjectID,
				},
				Limit: 100,
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "audit unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.AuditPage(view))
	}
}

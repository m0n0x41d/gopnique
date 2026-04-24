package httpadapter

import (
	"net/http"

	memberapp "github.com/ivanzakutnii/error-tracker/internal/app/members"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func membersHandler(
	reader memberapp.Reader,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageMembers,
		)
		if !sessionOK {
			return
		}

		viewResult := memberapp.Show(
			r.Context(),
			reader,
			memberapp.Query{Scope: memberScopeFromSession(session)},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "members unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.MembersPage(view))
	}
}

func memberScopeFromSession(session operators.OperatorSession) memberapp.Scope {
	return memberapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

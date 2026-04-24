package httpadapter

import (
	"net/http"
	"strings"

	issueapp "github.com/ivanzakutnii/error-tracker/internal/app/issues"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	userreportapp "github.com/ivanzakutnii/error-tracker/internal/app/userreports"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func issueListHandler(
	reader issueapp.Reader,
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
			http.Error(w, "issue reader not configured", http.StatusServiceUnavailable)
			return
		}

		search, searchErr := issueSearchFromRequest(r)
		if searchErr != nil {
			http.Error(w, searchErr.Error(), http.StatusBadRequest)
			return
		}

		viewResult := issueapp.List(r.Context(), reader, issueapp.IssueListQuery{
			Scope: issueapp.Scope{
				OrganizationID: session.OrganizationID,
				ProjectID:      session.ProjectID,
			},
			Limit:  100,
			Status: search.Status,
			Search: search,
		})
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "issue list unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.IssueList(view))
	}
}

func issueDetailHandler(
	reader issueapp.Reader,
	reportReader userreportapp.Reader,
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
			http.Error(w, "issue reader not configured", http.StatusServiceUnavailable)
			return
		}

		issueID, issueIDErr := domain.NewIssueID(r.PathValue("issue_id"))
		if issueIDErr != nil {
			http.NotFound(w, r)
			return
		}

		viewResult := issueapp.Detail(r.Context(), reader, issueapp.IssueDetailQuery{
			Scope: issueapp.Scope{
				OrganizationID: session.OrganizationID,
				ProjectID:      session.ProjectID,
			},
			IssueID: issueID,
		})
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.NotFound(w, r)
			return
		}

		reportsResult := userreportapp.ListForIssue(
			r.Context(),
			reportReader,
			userreportapp.IssueReportsQuery{
				Scope: userreportapp.Scope{
					OrganizationID: session.OrganizationID,
					ProjectID:      session.ProjectID,
				},
				IssueID: issueID,
				Limit:   100,
			},
		)
		reports, reportsErr := reportsResult.Value()
		if reportsErr != nil {
			http.Error(w, "user reports unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.IssueDetail(view, reports))
	}
}

func issueStatusHandler(
	manager issueapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionTriageIssues,
		)
		if !sessionOK {
			return
		}

		if manager == nil {
			http.Error(w, "issue manager not configured", http.StatusServiceUnavailable)
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		issueID, issueIDErr := domain.NewIssueID(r.PathValue("issue_id"))
		if issueIDErr != nil {
			http.NotFound(w, r)
			return
		}

		status, statusErr := issueapp.ParseIssueStatus(strings.TrimSpace(r.PostFormValue("status")))
		if statusErr != nil {
			http.Error(w, "issue status is invalid", http.StatusBadRequest)
			return
		}

		commandResult := issueapp.TransitionStatus(
			r.Context(),
			manager,
			issueapp.StatusTransitionCommand{
				Scope: issueapp.Scope{
					OrganizationID: session.OrganizationID,
					ProjectID:      session.ProjectID,
				},
				IssueID:      issueID,
				ActorID:      session.OperatorID,
				TargetStatus: status,
				Reason:       strings.TrimSpace(r.PostFormValue("reason")),
			},
		)
		_, commandErr := commandResult.Value()
		if commandErr != nil {
			http.Error(w, commandErr.Error(), http.StatusBadRequest)
			return
		}

		http.Redirect(w, r, "/issues/"+issueID.String(), http.StatusSeeOther)
	}
}

func issueCommentHandler(
	manager issueapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionTriageIssues,
		)
		if !sessionOK {
			return
		}

		if manager == nil {
			http.Error(w, "issue manager not configured", http.StatusServiceUnavailable)
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		issueID, issueIDErr := domain.NewIssueID(r.PathValue("issue_id"))
		if issueIDErr != nil {
			http.NotFound(w, r)
			return
		}

		commandResult := issueapp.AddComment(
			r.Context(),
			manager,
			issueapp.AddCommentCommand{
				Scope: issueapp.Scope{
					OrganizationID: session.OrganizationID,
					ProjectID:      session.ProjectID,
				},
				IssueID: issueID,
				ActorID: session.OperatorID,
				Body:    strings.TrimSpace(r.PostFormValue("body")),
			},
		)
		_, commandErr := commandResult.Value()
		if commandErr != nil {
			http.Error(w, commandErr.Error(), http.StatusBadRequest)
			return
		}

		http.Redirect(w, r, "/issues/"+issueID.String(), http.StatusSeeOther)
	}
}

func issueAssignmentHandler(
	manager issueapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionTriageIssues,
		)
		if !sessionOK {
			return
		}

		if manager == nil {
			http.Error(w, "issue manager not configured", http.StatusServiceUnavailable)
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}

		issueID, issueIDErr := domain.NewIssueID(r.PathValue("issue_id"))
		if issueIDErr != nil {
			http.NotFound(w, r)
			return
		}

		target, targetErr := issueapp.ParseAssignmentTarget(strings.TrimSpace(r.PostFormValue("assignee")))
		if targetErr != nil {
			http.Error(w, targetErr.Error(), http.StatusBadRequest)
			return
		}

		commandResult := issueapp.Assign(
			r.Context(),
			manager,
			issueapp.AssignIssueCommand{
				Scope: issueapp.Scope{
					OrganizationID: session.OrganizationID,
					ProjectID:      session.ProjectID,
				},
				IssueID: issueID,
				ActorID: session.OperatorID,
				Target:  target,
			},
		)
		_, commandErr := commandResult.Value()
		if commandErr != nil {
			http.Error(w, commandErr.Error(), http.StatusBadRequest)
			return
		}

		http.Redirect(w, r, "/issues/"+issueID.String(), http.StatusSeeOther)
	}
}

func issueSearchFromRequest(r *http.Request) (issueapp.IssueSearchFilter, error) {
	search, searchErr := issueapp.ParseIssueSearch(r.URL.Query().Get("q"))
	if searchErr != nil {
		return issueapp.IssueSearchFilter{}, searchErr
	}

	statusValue := strings.TrimSpace(r.URL.Query().Get("status"))
	if statusValue != "" && search.Status == "" {
		status, statusErr := issueapp.ParseIssueStatus(statusValue)
		if statusErr != nil {
			return issueapp.IssueSearchFilter{}, statusErr
		}

		search.Status = status
	}

	if search.Status == "" {
		search.Status = issueapp.IssueStatusUnresolved
	}

	return search, nil
}

func eventDetailHandler(
	reader issueapp.Reader,
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
			http.Error(w, "issue reader not configured", http.StatusServiceUnavailable)
			return
		}

		eventID, eventIDErr := domain.NewEventID(r.PathValue("event_id"))
		if eventIDErr != nil {
			http.NotFound(w, r)
			return
		}

		viewResult := issueapp.Event(r.Context(), reader, issueapp.EventDetailQuery{
			Scope: issueapp.Scope{
				OrganizationID: session.OrganizationID,
				ProjectID:      session.ProjectID,
			},
			EventID: eventID,
		})
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.NotFound(w, r)
			return
		}

		renderHTML(w, r, templates.EventDetail(view))
	}
}

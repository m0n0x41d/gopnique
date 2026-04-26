package httpadapter

import (
	"net/http"
	"strconv"

	logapp "github.com/ivanzakutnii/error-tracker/internal/app/logs"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func logListHandler(
	reader logapp.Reader,
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
			http.Error(w, "log reader not configured", http.StatusServiceUnavailable)
			return
		}

		viewResult := logapp.List(
			r.Context(),
			reader,
			logapp.Query{
				Scope:             logScopeFromSession(session),
				Limit:             logLimitFromRequest(r),
				Severity:          r.URL.Query().Get("severity"),
				Logger:            r.URL.Query().Get("logger"),
				Environment:       r.URL.Query().Get("environment"),
				Release:           r.URL.Query().Get("release"),
				ResourceAttribute: logAttributeFilter(r, "resource_key", "resource_value"),
				LogAttribute:      logAttributeFilter(r, "attribute_key", "attribute_value"),
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "logs unavailable", http.StatusServiceUnavailable)
			return
		}

		renderHTML(w, r, templates.LogList(view))
	}
}

func logDetailHandler(
	reader logapp.Reader,
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
			http.Error(w, "log reader not configured", http.StatusServiceUnavailable)
			return
		}

		viewResult := logapp.Detail(
			r.Context(),
			reader,
			logapp.DetailQuery{
				Scope: logScopeFromSession(session),
				ID:    r.PathValue("log_id"),
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.NotFound(w, r)
			return
		}

		renderHTML(w, r, templates.LogDetail(view))
	}
}

func logScopeFromSession(session operators.OperatorSession) logapp.Scope {
	return logapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

func logLimitFromRequest(r *http.Request) int {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return 100
	}

	limit, limitErr := strconv.Atoi(value)
	if limitErr != nil {
		return 100
	}

	return limit
}

func logAttributeFilter(r *http.Request, keyName string, valueName string) logapp.AttributeFilter {
	return logapp.AttributeFilter{
		Key:   r.URL.Query().Get(keyName),
		Value: r.URL.Query().Get(valueName),
	}
}

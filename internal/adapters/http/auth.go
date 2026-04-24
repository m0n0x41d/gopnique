package httpadapter

import (
	"net/http"

	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func setupGetHandler(access operators.Access, sessions SessionCodec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if access == nil {
			http.Error(w, "operator access not configured", http.StatusServiceUnavailable)
			return
		}

		if hasValidSession(r, access, sessions) {
			http.Redirect(w, r, "/issues", http.StatusSeeOther)
			return
		}

		bootstrappedResult := access.IsBootstrapped(r.Context())
		bootstrapped, bootstrappedErr := bootstrappedResult.Value()
		if bootstrappedErr != nil {
			http.Error(w, "setup unavailable", http.StatusServiceUnavailable)
			return
		}

		if bootstrapped {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		renderHTML(w, r, templates.Setup(""))
	}
}

func setupPostHandler(
	access operators.Access,
	sessions SessionCodec,
	auth AuthSettings,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if access == nil {
			http.Error(w, "operator access not configured", http.StatusServiceUnavailable)
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			renderHTML(w, r, templates.Setup("Invalid form"))
			return
		}

		result := access.BootstrapOperator(r.Context(), operators.BootstrapCommand{
			PublicURL:        auth.PublicURL,
			OrganizationName: r.FormValue("organization_name"),
			ProjectName:      r.FormValue("project_name"),
			Email:            r.FormValue("email"),
			Password:         r.FormValue("password"),
		})
		bootstrap, bootstrapErr := result.Value()
		if bootstrapErr != nil {
			renderHTML(w, r, templates.Setup("Setup failed"))
			return
		}

		setSessionCookie(w, sessions, bootstrap.Session)
		renderHTML(w, r, templates.SetupDone(bootstrap.DSN))
	}
}

func loginGetHandler(access operators.Access, sessions SessionCodec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if access == nil {
			http.Error(w, "operator access not configured", http.StatusServiceUnavailable)
			return
		}

		if hasValidSession(r, access, sessions) {
			http.Redirect(w, r, "/issues", http.StatusSeeOther)
			return
		}

		renderHTML(w, r, templates.Login(""))
	}
}

func loginPostHandler(access operators.Access, sessions SessionCodec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if access == nil {
			http.Error(w, "operator access not configured", http.StatusServiceUnavailable)
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			renderHTML(w, r, templates.Login("Invalid form"))
			return
		}

		result := access.Login(r.Context(), operators.LoginCommand{
			Email:    r.FormValue("email"),
			Password: r.FormValue("password"),
		})
		login, loginErr := result.Value()
		if loginErr != nil {
			renderHTML(w, r, templates.Login("Invalid credentials"))
			return
		}

		setSessionCookie(w, sessions, login.Session)
		http.Redirect(w, r, "/issues", http.StatusSeeOther)
	}
}

func logoutPostHandler(access operators.Access, sessions SessionCodec) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := tokenFromRequest(r, sessions)
		if ok && access != nil {
			_ = access.DeleteSession(r.Context(), token)
		}

		clearSessionCookie(w)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func requireSession(
	w http.ResponseWriter,
	r *http.Request,
	access operators.Access,
	sessions SessionCodec,
) bool {
	_, ok := requireOperatorSession(w, r, access, sessions)

	return ok
}

func requireOperatorSession(
	w http.ResponseWriter,
	r *http.Request,
	access operators.Access,
	sessions SessionCodec,
) (operators.OperatorSession, bool) {
	if access == nil {
		http.Error(w, "operator access not configured", http.StatusServiceUnavailable)
		return operators.OperatorSession{}, false
	}

	session, sessionOK := validSession(r, access, sessions)
	if sessionOK {
		return session, true
	}

	bootstrappedResult := access.IsBootstrapped(r.Context())
	bootstrapped, bootstrappedErr := bootstrappedResult.Value()
	if bootstrappedErr != nil {
		http.Error(w, "operator access unavailable", http.StatusServiceUnavailable)
		return operators.OperatorSession{}, false
	}

	if bootstrapped {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return operators.OperatorSession{}, false
	}

	http.Redirect(w, r, "/setup", http.StatusSeeOther)
	return operators.OperatorSession{}, false
}

func requireOperatorPermission(
	w http.ResponseWriter,
	r *http.Request,
	access operators.Access,
	sessions SessionCodec,
	permission operators.Permission,
) (operators.OperatorSession, bool) {
	session, sessionOK := requireOperatorSession(w, r, access, sessions)
	if !sessionOK {
		return operators.OperatorSession{}, false
	}

	allowedResult := operators.RequirePermission(session, permission)
	allowedSession, allowedErr := allowedResult.Value()
	if allowedErr != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return operators.OperatorSession{}, false
	}

	return allowedSession, true
}

func hasValidSession(r *http.Request, access operators.Access, sessions SessionCodec) bool {
	_, ok := validSession(r, access, sessions)

	return ok
}

func validSession(
	r *http.Request,
	access operators.Access,
	sessions SessionCodec,
) (operators.OperatorSession, bool) {
	token, ok := tokenFromRequest(r, sessions)
	if !ok {
		return operators.OperatorSession{}, false
	}

	result := access.ResolveSession(r.Context(), token)
	session, err := result.Value()

	return session, err == nil
}

func tokenFromRequest(r *http.Request, sessions SessionCodec) (operators.SessionToken, bool) {
	cookie, cookieErr := r.Cookie(sessionCookieName)
	if cookieErr != nil {
		return operators.SessionToken{}, false
	}

	return sessions.Decode(cookie.Value)
}

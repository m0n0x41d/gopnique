package httpadapter

import (
	"net/http"

	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

func tokenSettingsHandler(
	manager tokenapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageTokens,
		)
		if !sessionOK {
			return
		}

		message := tokenMessageFromNotice(r.URL.Query().Get("saved"))
		renderTokenSettings(w, r, manager, session, message, "", http.StatusOK)
	}
}

func createProjectTokenHandler(
	manager tokenapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageTokens,
		)
		if !sessionOK {
			return
		}

		parseErr := r.ParseForm()
		if parseErr != nil {
			message := templates.TokenSettingsMessage{
				Text: "Invalid form",
				Kind: "error",
			}
			renderTokenSettings(w, r, manager, session, message, "", http.StatusBadRequest)
			return
		}

		scope, scopeErr := tokenapp.ParseProjectTokenScope(r.PostFormValue("scope"))
		if scopeErr != nil {
			message := templates.TokenSettingsMessage{
				Text: scopeErr.Error(),
				Kind: "error",
			}
			renderTokenSettings(w, r, manager, session, message, "", http.StatusBadRequest)
			return
		}

		commandResult := tokenapp.CreateProjectToken(
			r.Context(),
			manager,
			tokenapp.CreateProjectTokenCommand{
				Scope:      tokenScopeFromSession(session),
				ActorID:    session.OperatorID,
				Name:       r.PostFormValue("name"),
				TokenScope: scope,
			},
		)
		token, commandErr := commandResult.Value()
		if commandErr != nil {
			message := templates.TokenSettingsMessage{
				Text: commandErr.Error(),
				Kind: "error",
			}
			renderTokenSettings(w, r, manager, session, message, "", http.StatusBadRequest)
			return
		}

		message := templates.TokenSettingsMessage{
			Text: "API token saved",
			Kind: "success",
		}
		renderTokenSettings(w, r, manager, session, message, token.OneTimeToken, http.StatusOK)
	}
}

func revokeProjectTokenHandler(
	manager tokenapp.Manager,
	access operators.Access,
	sessions SessionCodec,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		session, sessionOK := requireOperatorPermission(
			w,
			r,
			access,
			sessions,
			operators.PermissionManageTokens,
		)
		if !sessionOK {
			return
		}

		tokenID, tokenIDErr := domain.NewAPITokenID(r.PathValue("token_id"))
		if tokenIDErr != nil {
			http.NotFound(w, r)
			return
		}

		commandResult := tokenapp.RevokeProjectToken(
			r.Context(),
			manager,
			tokenapp.RevokeProjectTokenCommand{
				Scope:   tokenScopeFromSession(session),
				ActorID: session.OperatorID,
				TokenID: tokenID,
			},
		)
		_, commandErr := commandResult.Value()
		if commandErr != nil {
			message := templates.TokenSettingsMessage{
				Text: commandErr.Error(),
				Kind: "error",
			}
			renderTokenSettings(w, r, manager, session, message, "", http.StatusBadRequest)
			return
		}

		http.Redirect(w, r, "/settings/tokens?saved=revoked", http.StatusSeeOther)
	}
}

func renderTokenSettings(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	session operators.OperatorSession,
	message templates.TokenSettingsMessage,
	oneTimeToken string,
	status int,
) {
	viewResult := tokenapp.ShowProjectTokens(
		r.Context(),
		manager,
		tokenapp.ProjectTokenQuery{Scope: tokenScopeFromSession(session)},
	)
	view, viewErr := viewResult.Value()
	if viewErr != nil {
		http.Error(w, "api token settings unavailable", http.StatusServiceUnavailable)
		return
	}

	renderHTMLStatus(w, r, templates.TokenSettingsPage(view, message, oneTimeToken), status)
}

func tokenScopeFromSession(session operators.OperatorSession) tokenapp.Scope {
	return tokenapp.Scope{
		OrganizationID: session.OrganizationID,
		ProjectID:      session.ProjectID,
	}
}

func tokenMessageFromNotice(notice string) templates.TokenSettingsMessage {
	if notice == "revoked" {
		return templates.TokenSettingsMessage{
			Text: "API token revoked",
			Kind: "success",
		}
	}

	return templates.TokenSettingsMessage{}
}

package httpadapter

import (
	"errors"
	"net/http"
	"strings"

	projectapp "github.com/ivanzakutnii/error-tracker/internal/app/projects"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
)

func currentProjectAPIHandler(
	reader projectapp.Reader,
	manager tokenapp.Manager,
	auth AuthSettings,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenAuth, tokenOK := requireProjectAPIToken(
			w,
			r,
			manager,
			tokenapp.ProjectTokenScopeRead,
		)
		if !tokenOK {
			return
		}

		viewResult := projectapp.ShowCurrentProject(
			r.Context(),
			reader,
			projectapp.ProjectQuery{
				Scope: projectapp.Scope{
					OrganizationID: tokenAuth.OrganizationID,
					ProjectID:      tokenAuth.ProjectID,
				},
				PublicURL: auth.PublicURL,
			},
		)
		view, viewErr := viewResult.Value()
		if viewErr != nil {
			http.Error(w, "project unavailable", http.StatusServiceUnavailable)
			return
		}

		writeJSON(w, http.StatusOK, currentProjectAPIResponse{
			OrganizationName: view.OrganizationName,
			ProjectID:        view.ProjectID,
			Name:             view.Name,
			Slug:             view.Slug,
			IngestRef:        view.IngestRef,
			DSN:              view.DSN,
			StoreEndpoint:    view.StoreEndpoint,
			EnvelopeEndpoint: view.EnvelopeEndpoint,
		})
	}
}

type currentProjectAPIResponse struct {
	OrganizationName string `json:"organization_name"`
	ProjectID        string `json:"project_id"`
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	IngestRef        string `json:"ingest_ref"`
	DSN              string `json:"dsn"`
	StoreEndpoint    string `json:"store_endpoint"`
	EnvelopeEndpoint string `json:"envelope_endpoint"`
}

func requireProjectAPIToken(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	required tokenapp.ProjectTokenScope,
) (tokenapp.ProjectTokenAuth, bool) {
	secret, secretErr := bearerProjectToken(r)
	if secretErr != nil {
		http.Error(w, "api token is required", http.StatusUnauthorized)
		return tokenapp.ProjectTokenAuth{}, false
	}

	authResult := tokenapp.ResolveProjectToken(r.Context(), manager, secret, required)
	auth, authErr := authResult.Value()
	if authErr != nil {
		http.Error(w, "forbidden", http.StatusForbidden)
		return tokenapp.ProjectTokenAuth{}, false
	}

	return auth, true
}

func bearerProjectToken(r *http.Request) (tokenapp.ProjectTokenSecret, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return tokenapp.ProjectTokenSecret{}, errors.New("authorization header is required")
	}

	prefix := "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return tokenapp.ProjectTokenSecret{}, errors.New("bearer token is required")
	}

	return tokenapp.NewProjectTokenSecret(strings.TrimSpace(strings.TrimPrefix(header, prefix)))
}

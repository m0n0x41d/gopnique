package httpadapter

import (
	"context"
	"net/http"
	"time"

	"github.com/a-h/templ"
	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	dimensionapp "github.com/ivanzakutnii/error-tracker/internal/app/dimensions"
	"github.com/ivanzakutnii/error-tracker/internal/app/health"
	issueapp "github.com/ivanzakutnii/error-tracker/internal/app/issues"
	memberapp "github.com/ivanzakutnii/error-tracker/internal/app/members"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	projectapp "github.com/ivanzakutnii/error-tracker/internal/app/projects"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	statsapp "github.com/ivanzakutnii/error-tracker/internal/app/stats"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
	userreportapp "github.com/ivanzakutnii/error-tracker/internal/app/userreports"
	"github.com/ivanzakutnii/error-tracker/web/templates"
)

type Server struct {
	httpServer *http.Server
}

type AuthSettings struct {
	PublicURL string
	SecretKey string
}

func New(
	addr string,
	probe health.DatabaseProbe,
	ingestBackend SentryIngestBackend,
	issueManager issueapp.Manager,
	userReportReader userreportapp.Reader,
	dimensionReader dimensionapp.Reader,
	statsReader statsapp.Reader,
	projectReader projectapp.Reader,
	memberReader memberapp.Reader,
	settingsManager settingsapp.Manager,
	tokenManager tokenapp.Manager,
	auditReader auditapp.Reader,
	resolver outbound.Resolver,
	operatorAccess operators.Access,
	auth AuthSettings,
) *Server {
	server := &http.Server{
		Addr:              addr,
		Handler:           NewHandler(probe, ingestBackend, issueManager, userReportReader, dimensionReader, statsReader, projectReader, memberReader, settingsManager, tokenManager, auditReader, resolver, operatorAccess, auth),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &Server{httpServer: server}
}

func NewHandler(
	probe health.DatabaseProbe,
	ingestBackend SentryIngestBackend,
	issueManager issueapp.Manager,
	userReportReader userreportapp.Reader,
	dimensionReader dimensionapp.Reader,
	statsReader statsapp.Reader,
	projectReader projectapp.Reader,
	memberReader memberapp.Reader,
	settingsManager settingsapp.Manager,
	tokenManager tokenapp.Manager,
	auditReader auditapp.Reader,
	resolver outbound.Resolver,
	operatorAccess operators.Access,
	auth AuthSettings,
) http.Handler {
	return newMux(probe, ingestBackend, issueManager, userReportReader, dimensionReader, statsReader, projectReader, memberReader, settingsManager, tokenManager, auditReader, resolver, operatorAccess, NewSessionCodec(auth.SecretKey), auth)
}

func newMux(
	probe health.DatabaseProbe,
	ingestBackend SentryIngestBackend,
	issueManager issueapp.Manager,
	userReportReader userreportapp.Reader,
	dimensionReader dimensionapp.Reader,
	statsReader statsapp.Reader,
	projectReader projectapp.Reader,
	memberReader memberapp.Reader,
	settingsManager settingsapp.Manager,
	tokenManager tokenapp.Manager,
	auditReader auditapp.Reader,
	resolver outbound.Resolver,
	operatorAccess operators.Access,
	sessions SessionCodec,
	auth AuthSettings,
) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/{project_ref}/store/", sentryStoreHandler(ingestBackend))
	mux.HandleFunc("POST /api/{project_ref}/envelope/", sentryEnvelopeHandler(ingestBackend))
	mux.HandleFunc("POST /api/{project_ref}/security/", sentrySecurityHandler(ingestBackend))
	mux.HandleFunc("POST /api/{project_ref}/csp-report/", sentrySecurityHandler(ingestBackend))
	mux.HandleFunc("POST /api/{project_ref}/user-feedback/", sentryUserFeedbackHandler(ingestBackend))
	mux.HandleFunc("GET /api/v1/project", currentProjectAPIHandler(projectReader, tokenManager, auth))
	mux.HandleFunc("GET /setup", setupGetHandler(operatorAccess, sessions))
	mux.HandleFunc("POST /setup", setupPostHandler(operatorAccess, sessions, auth))
	mux.HandleFunc("GET /login", loginGetHandler(operatorAccess, sessions))
	mux.HandleFunc("POST /login", loginPostHandler(operatorAccess, sessions))
	mux.HandleFunc("POST /logout", logoutPostHandler(operatorAccess, sessions))
	mux.HandleFunc("GET /stats", projectStatsHandler(statsReader, operatorAccess, sessions))
	mux.HandleFunc("GET /issues", issueListHandler(issueManager, operatorAccess, sessions))
	mux.HandleFunc("GET /issues/{issue_id}", issueDetailHandler(issueManager, userReportReader, operatorAccess, sessions))
	mux.HandleFunc("POST /issues/{issue_id}/status", issueStatusHandler(issueManager, operatorAccess, sessions))
	mux.HandleFunc("POST /issues/{issue_id}/comments", issueCommentHandler(issueManager, operatorAccess, sessions))
	mux.HandleFunc("POST /issues/{issue_id}/assignment", issueAssignmentHandler(issueManager, operatorAccess, sessions))
	mux.HandleFunc("GET /events/{event_id}", eventDetailHandler(issueManager, operatorAccess, sessions))
	mux.HandleFunc("GET /environments", environmentsHandler(dimensionReader, operatorAccess, sessions))
	mux.HandleFunc("GET /releases", releasesHandler(dimensionReader, operatorAccess, sessions))
	mux.HandleFunc("GET /projects", projectDetailHandler(projectReader, operatorAccess, sessions, auth))
	mux.HandleFunc("GET /settings/members", membersHandler(memberReader, operatorAccess, sessions))
	mux.HandleFunc("GET /settings/tokens", tokenSettingsHandler(tokenManager, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/tokens", createProjectTokenHandler(tokenManager, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/tokens/{token_id}/revoke", revokeProjectTokenHandler(tokenManager, operatorAccess, sessions))
	mux.HandleFunc("GET /settings/audit", auditHandler(auditReader, operatorAccess, sessions))
	mux.HandleFunc("GET /settings/notifications", notificationSettingsHandler(settingsManager, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/telegram-destinations", createTelegramDestinationHandler(settingsManager, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/webhook-destinations", createWebhookDestinationHandler(settingsManager, resolver, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/email-destinations", createEmailDestinationHandler(settingsManager, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/discord-destinations", createDiscordDestinationHandler(settingsManager, resolver, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/google-chat-destinations", createGoogleChatDestinationHandler(settingsManager, resolver, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/ntfy-destinations", createNtfyDestinationHandler(settingsManager, resolver, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/teams-destinations", createTeamsDestinationHandler(settingsManager, resolver, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/issue-opened-alerts", createIssueOpenedAlertHandler(settingsManager, operatorAccess, sessions))
	mux.HandleFunc("POST /settings/notifications/issue-opened-alerts/{rule_id}/enable", setIssueOpenedAlertStatusHandler(settingsManager, operatorAccess, sessions, true))
	mux.HandleFunc("POST /settings/notifications/issue-opened-alerts/{rule_id}/disable", setIssueOpenedAlertStatusHandler(settingsManager, operatorAccess, sessions, false))
	mux.HandleFunc("/health/live", liveHandler)
	mux.HandleFunc("/health/ready", readyHandler(probe))
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(StaticFiles()))))
	mux.HandleFunc("/", rootHandler)

	return mux
}

func (server *Server) ListenAndServe() error {
	return server.httpServer.ListenAndServe()
}

func (server *Server) Shutdown(ctx context.Context) error {
	return server.httpServer.Shutdown(ctx)
}

func liveHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("live\n"))
}

func readyHandler(probe health.DatabaseProbe) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := health.CheckReadiness(r.Context(), probe)
		status := http.StatusOK
		if !report.Ready {
			status = http.StatusServiceUnavailable
		}

		w.WriteHeader(status)
		_, _ = w.Write([]byte(report.Description + "\n"))
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	renderHTML(w, r, templates.Home())
}

func renderHTML(w http.ResponseWriter, r *http.Request, component templ.Component) {
	renderHTMLStatus(w, r, component, http.StatusOK)
}

func renderHTMLStatus(w http.ResponseWriter, r *http.Request, component templ.Component, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := component.Render(r.Context(), w)
	if err != nil {
		_, _ = w.Write([]byte("render failed"))
	}
}

package httpadapter

import (
	"context"
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	dimensionapp "github.com/ivanzakutnii/error-tracker/internal/app/dimensions"
	"github.com/ivanzakutnii/error-tracker/internal/app/health"
	issueapp "github.com/ivanzakutnii/error-tracker/internal/app/issues"
	logapp "github.com/ivanzakutnii/error-tracker/internal/app/logs"
	memberapp "github.com/ivanzakutnii/error-tracker/internal/app/members"
	"github.com/ivanzakutnii/error-tracker/internal/app/minidumps"
	observabilityapp "github.com/ivanzakutnii/error-tracker/internal/app/observability"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	performanceapp "github.com/ivanzakutnii/error-tracker/internal/app/performance"
	projectapp "github.com/ivanzakutnii/error-tracker/internal/app/projects"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	"github.com/ivanzakutnii/error-tracker/internal/app/sourcemaps"
	statsapp "github.com/ivanzakutnii/error-tracker/internal/app/stats"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
	uptimeapp "github.com/ivanzakutnii/error-tracker/internal/app/uptime"
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

type IngestEnrichments struct {
	ArtifactVault     artifacts.ArtifactVault
	SourceMapResolver *sourcemaps.Service
	DebugFileStore    *debugfiles.Service
	MinidumpStore     *minidumps.Service
}

func New(
	addr string,
	probe observabilityapp.Reader,
	ingestBackend SentryIngestBackend,
	issueManager issueapp.Manager,
	userReportReader userreportapp.Reader,
	dimensionReader dimensionapp.Reader,
	statsReader statsapp.Reader,
	performanceReader performanceapp.Reader,
	logReader logapp.Reader,
	uptimeManager uptimeapp.Manager,
	projectReader projectapp.Reader,
	memberReader memberapp.Reader,
	settingsManager settingsapp.Manager,
	tokenManager tokenapp.Manager,
	auditReader auditapp.Reader,
	resolver outbound.Resolver,
	operatorAccess operators.Access,
	enrichments IngestEnrichments,
	auth AuthSettings,
) *Server {
	server := &http.Server{
		Addr:              addr,
		Handler:           NewHandler(probe, ingestBackend, issueManager, userReportReader, dimensionReader, statsReader, performanceReader, logReader, uptimeManager, projectReader, memberReader, settingsManager, tokenManager, auditReader, resolver, operatorAccess, enrichments, auth),
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &Server{httpServer: server}
}

func NewHandler(
	probe observabilityapp.Reader,
	ingestBackend SentryIngestBackend,
	issueManager issueapp.Manager,
	userReportReader userreportapp.Reader,
	dimensionReader dimensionapp.Reader,
	statsReader statsapp.Reader,
	performanceReader performanceapp.Reader,
	logReader logapp.Reader,
	uptimeManager uptimeapp.Manager,
	projectReader projectapp.Reader,
	memberReader memberapp.Reader,
	settingsManager settingsapp.Manager,
	tokenManager tokenapp.Manager,
	auditReader auditapp.Reader,
	resolver outbound.Resolver,
	operatorAccess operators.Access,
	enrichments IngestEnrichments,
	auth AuthSettings,
) http.Handler {
	return newMuxWithUptime(probe, ingestBackend, issueManager, userReportReader, dimensionReader, statsReader, performanceReader, logReader, uptimeManager, projectReader, memberReader, settingsManager, tokenManager, auditReader, resolver, operatorAccess, enrichments, NewSessionCodec(auth.SecretKey), auth)
}

func newMux(
	probe observabilityapp.Reader,
	ingestBackend SentryIngestBackend,
	issueManager issueapp.Manager,
	userReportReader userreportapp.Reader,
	dimensionReader dimensionapp.Reader,
	statsReader statsapp.Reader,
	performanceReader performanceapp.Reader,
	projectReader projectapp.Reader,
	memberReader memberapp.Reader,
	settingsManager settingsapp.Manager,
	tokenManager tokenapp.Manager,
	auditReader auditapp.Reader,
	resolver outbound.Resolver,
	operatorAccess operators.Access,
	enrichments IngestEnrichments,
	sessions SessionCodec,
	auth AuthSettings,
) *http.ServeMux {
	return newMuxWithUptime(probe, ingestBackend, issueManager, userReportReader, dimensionReader, statsReader, performanceReader, nil, nil, projectReader, memberReader, settingsManager, tokenManager, auditReader, resolver, operatorAccess, enrichments, sessions, auth)
}

func newMuxWithUptime(
	probe observabilityapp.Reader,
	ingestBackend SentryIngestBackend,
	issueManager issueapp.Manager,
	userReportReader userreportapp.Reader,
	dimensionReader dimensionapp.Reader,
	statsReader statsapp.Reader,
	performanceReader performanceapp.Reader,
	logReader logapp.Reader,
	uptimeManager uptimeapp.Manager,
	projectReader projectapp.Reader,
	memberReader memberapp.Reader,
	settingsManager settingsapp.Manager,
	tokenManager tokenapp.Manager,
	auditReader auditapp.Reader,
	resolver outbound.Resolver,
	operatorAccess operators.Access,
	enrichments IngestEnrichments,
	sessions SessionCodec,
	auth AuthSettings,
) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/{project_ref}/store/", sentryStoreHandler(ingestBackend, enrichments))
	mux.HandleFunc("POST /api/{project_ref}/envelope/", sentryEnvelopeHandler(ingestBackend, enrichments))
	mux.HandleFunc("POST /api/{project_ref}/minidump/", sentryMinidumpHandler(ingestBackend, enrichments))
	mux.HandleFunc("POST /api/{project_ref}/security/", sentrySecurityHandler(ingestBackend))
	mux.HandleFunc("POST /api/{project_ref}/csp-report/", sentrySecurityHandler(ingestBackend))
	mux.HandleFunc("POST /api/{project_ref}/user-feedback/", sentryUserFeedbackHandler(ingestBackend))
	mux.HandleFunc("POST /api/heartbeat/{endpoint_id}", heartbeatCheckInHandler(uptimeManager))
	mux.HandleFunc("GET /api/0/organizations/{organization_slug}/chunk-upload/", artifactChunkUploadInfoHandler(tokenManager, projectReader, enrichments.ArtifactVault))
	mux.HandleFunc("POST /api/0/organizations/{organization_slug}/chunk-upload/", artifactChunkUploadHandler(tokenManager, projectReader, enrichments.ArtifactVault))
	mux.HandleFunc("POST /api/0/organizations/{organization_slug}/artifactbundle/assemble/", artifactBundleAssembleHandler(tokenManager, projectReader, enrichments.ArtifactVault, enrichments.SourceMapResolver))
	mux.HandleFunc("POST /api/0/projects/{organization_slug}/{project_slug}/releases/{version}/files/", sourceMapUploadHandler(tokenManager, projectReader, enrichments.SourceMapResolver))
	mux.HandleFunc("POST /api/0/projects/{organization_slug}/{project_slug}/files/difs/", debugFileUploadHandler(tokenManager, projectReader, enrichments.DebugFileStore))
	mux.HandleFunc("POST /api/0/projects/{organization_slug}/{project_slug}/files/dsyms/", debugFileUploadHandler(tokenManager, projectReader, enrichments.DebugFileStore))
	mux.HandleFunc("POST /api/0/projects/{organization_slug}/{project_slug}/files/difs/assemble/", debugFileAssembleHandler(tokenManager, projectReader, enrichments.ArtifactVault, enrichments.DebugFileStore))
	mux.HandleFunc("POST /api/0/projects/{organization_slug}/{project_slug}/reprocessing/", debugFileReprocessingHandler(tokenManager, projectReader))
	mux.HandleFunc("GET /api/v1/project", currentProjectAPIHandler(projectReader, tokenManager, auth))
	mux.HandleFunc("GET /api/admin/observability", observabilitySnapshotHandler(probe, operatorAccess, sessions))
	mux.HandleFunc("GET /api/admin/observability/system", observabilitySystemHandler(operatorAccess, sessions))
	mux.HandleFunc("GET /api/admin/observability/readiness", observabilityReadinessHandler(probe, operatorAccess, sessions))
	mux.HandleFunc("GET /api/admin/observability/migrations", observabilityMigrationHandler(probe, operatorAccess, sessions))
	mux.HandleFunc("GET /api/admin/observability/queue", observabilityQueueHandler(probe, operatorAccess, sessions))
	mux.HandleFunc("GET /api/admin/observability/metrics", observabilityMetricsHandler(probe, operatorAccess, sessions))
	mux.HandleFunc("GET /setup", setupGetHandler(operatorAccess, sessions))
	mux.HandleFunc("POST /setup", setupPostHandler(operatorAccess, sessions, auth))
	mux.HandleFunc("GET /login", loginGetHandler(operatorAccess, sessions))
	mux.HandleFunc("POST /login", loginPostHandler(operatorAccess, sessions))
	mux.HandleFunc("GET /auth/oidc/{provider_slug}/start", oidcStartHandler(operatorAccess, sessions, auth))
	mux.HandleFunc("GET /auth/oidc/{provider_slug}/callback", oidcCallbackHandler(operatorAccess, sessions, auth))
	mux.HandleFunc("POST /logout", logoutPostHandler(operatorAccess, sessions))
	mux.HandleFunc("GET /stats", projectStatsHandler(statsReader, operatorAccess, sessions))
	mux.HandleFunc("GET /performance", performanceListHandler(performanceReader, operatorAccess, sessions))
	mux.HandleFunc("GET /performance/{group_id}", performanceDetailHandler(performanceReader, operatorAccess, sessions))
	mux.HandleFunc("GET /logs", logListHandler(logReader, operatorAccess, sessions))
	mux.HandleFunc("GET /logs/{log_id}", logDetailHandler(logReader, operatorAccess, sessions))
	mux.HandleFunc("GET /uptime", uptimeHandler(uptimeManager, operatorAccess, sessions))
	mux.HandleFunc("POST /uptime/monitors", createHTTPMonitorHandler(uptimeManager, resolver, operatorAccess, sessions))
	mux.HandleFunc("POST /uptime/heartbeats", createHeartbeatMonitorHandler(uptimeManager, operatorAccess, sessions))
	mux.HandleFunc("POST /uptime/status-pages", createStatusPageHandler(uptimeManager, operatorAccess, sessions))
	mux.HandleFunc("GET /status-pages/{page_id}", privateStatusPageHandler(uptimeManager, operatorAccess, sessions))
	mux.HandleFunc("GET /status/{token}", publicStatusPageHandler(uptimeManager))
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
	mux.HandleFunc("POST /settings/notifications/zulip-destinations", createZulipDestinationHandler(settingsManager, resolver, operatorAccess, sessions))
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

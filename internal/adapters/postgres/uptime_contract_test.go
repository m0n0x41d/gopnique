//go:build integration

package postgres

import (
	"context"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	"github.com/ivanzakutnii/error-tracker/internal/app/operators"
	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	uptimeapp "github.com/ivanzakutnii/error-tracker/internal/app/uptime"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestPostgresUptimeMonitorWorkflow(t *testing.T) {
	ctx := context.Background()
	adminURL := os.Getenv("ERROR_TRACKER_REPOSITORY_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("ERROR_TRACKER_REPOSITORY_POSTGRES_URL is required")
	}

	databaseURL := createRepositoryTestDatabase(t, ctx, adminURL)
	store, storeErr := NewStore(ctx, databaseURL)
	if storeErr != nil {
		t.Fatalf("store: %v", storeErr)
	}
	defer store.Close()

	migrationResult, migrationErr := store.ApplyMigrations(ctx)
	if migrationErr != nil {
		t.Fatalf("migrate: %v", migrationErr)
	}
	if len(migrationResult.Applied) != 30 {
		t.Fatalf("expected 30 migrations, got %d", len(migrationResult.Applied))
	}

	bootstrap, bootstrapErr := store.Bootstrap(ctx, BootstrapInput{
		PublicURL:        "http://example.test",
		OrganizationName: "Uptime Org",
		ProjectName:      "Uptime API",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	auth := resolveRepositoryAuth(t, ctx, store, bootstrap)
	session := loginRepositoryOperator(t, ctx, store)
	scope := uptimeapp.Scope{
		OrganizationID: auth.OrganizationID(),
		ProjectID:      auth.ProjectID(),
	}
	resolver := repositoryResolver{
		"status.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")},
	}

	createResult := uptimeapp.CreateHTTPMonitor(
		ctx,
		resolver,
		store,
		uptimeapp.CreateHTTPMonitorCommand{
			Scope:           scope,
			ActorID:         session.OperatorID,
			Name:            "Repository API",
			URL:             "https://status.example.com/health",
			IntervalSeconds: 60,
			TimeoutSeconds:  5,
		},
	)
	created, createErr := createResult.Value()
	if createErr != nil {
		t.Fatalf("create monitor: %v", createErr)
	}

	downSummary := runRepositoryUptimeCheck(t, ctx, store, resolver, 500, time.Now().UTC())
	if downSummary.Down != 1 {
		t.Fatalf("expected down check, got %#v", downSummary)
	}

	assertRepositoryOpenMonitorIncident(t, ctx, store, created.MonitorID)

	_, scheduleErr := store.pool.Exec(
		ctx,
		`update uptime_monitors set next_check_at = $2 where id = $1`,
		created.MonitorID,
		time.Now().UTC().Add(-time.Second),
	)
	if scheduleErr != nil {
		t.Fatalf("reschedule monitor: %v", scheduleErr)
	}

	upSummary := runRepositoryUptimeCheck(t, ctx, store, resolver, 204, time.Now().UTC())
	if upSummary.Up != 1 {
		t.Fatalf("expected up check, got %#v", upSummary)
	}

	assertRepositoryClosedMonitorIncident(t, ctx, store, created.MonitorID)
	assertRepositoryUptimeView(t, ctx, store, scope)

	heartbeatResult := uptimeapp.CreateHeartbeatMonitor(
		ctx,
		store,
		uptimeapp.CreateHeartbeatMonitorCommand{
			Scope:           scope,
			ActorID:         session.OperatorID,
			Name:            "Repository heartbeat",
			IntervalSeconds: 300,
			GraceSeconds:    120,
		},
	)
	heartbeat, heartbeatErr := heartbeatResult.Value()
	if heartbeatErr != nil {
		t.Fatalf("create heartbeat monitor: %v", heartbeatErr)
	}

	checkInResult := uptimeapp.RecordHeartbeatCheckIn(
		ctx,
		store,
		uptimeapp.HeartbeatCheckInCommand{
			EndpointID: heartbeat.EndpointID,
			CheckedAt:  time.Now().UTC(),
		},
	)
	checkIn, checkInErr := checkInResult.Value()
	if checkInErr != nil {
		t.Fatalf("record heartbeat check-in: %v", checkInErr)
	}
	if checkIn.CurrentState != "up" {
		t.Fatalf("expected heartbeat up check-in, got %#v", checkIn)
	}

	_, heartbeatScheduleErr := store.pool.Exec(
		ctx,
		`update uptime_monitors set next_check_at = $2 where id = $1`,
		heartbeat.MonitorID,
		time.Now().UTC().Add(-time.Second),
	)
	if heartbeatScheduleErr != nil {
		t.Fatalf("reschedule heartbeat: %v", heartbeatScheduleErr)
	}

	heartbeatTimeoutResult := uptimeapp.CheckDueHeartbeatMonitors(
		ctx,
		store,
		uptimeapp.CheckDueCommand{
			Now:   time.Now().UTC(),
			Limit: 10,
		},
	)
	heartbeatTimeout, heartbeatTimeoutErr := heartbeatTimeoutResult.Value()
	if heartbeatTimeoutErr != nil {
		t.Fatalf("run heartbeat timeout: %v", heartbeatTimeoutErr)
	}
	if heartbeatTimeout.Down != 1 {
		t.Fatalf("expected heartbeat down timeout, got %#v", heartbeatTimeout)
	}

	assertRepositoryOpenMonitorIncident(t, ctx, store, heartbeat.MonitorID)

	recoverResult := uptimeapp.RecordHeartbeatCheckIn(
		ctx,
		store,
		uptimeapp.HeartbeatCheckInCommand{
			EndpointID: heartbeat.EndpointID,
			CheckedAt:  time.Now().UTC(),
		},
	)
	recovered, recoverErr := recoverResult.Value()
	if recoverErr != nil {
		t.Fatalf("recover heartbeat: %v", recoverErr)
	}
	if !recovered.IncidentClosed {
		t.Fatalf("expected heartbeat incident close, got %#v", recovered)
	}

	assertRepositoryClosedMonitorIncident(t, ctx, store, heartbeat.MonitorID)
	assertRepositoryStatusPages(t, ctx, store, scope, session.OperatorID)
	assertRepositoryUptimeAudit(t, ctx, store, scope)
}

func resolveRepositoryAuth(
	t *testing.T,
	ctx context.Context,
	store *Store,
	bootstrap BootstrapResult,
) domain.ProjectAuth {
	t.Helper()

	ref := mustRepositoryValue(t, domain.NewProjectRef, bootstrap.ProjectRef)
	publicKey := mustRepositoryValue(t, domain.NewProjectPublicKey, bootstrap.PublicKey)
	authResult := store.ResolveProjectKey(ctx, ref, publicKey)
	auth, authErr := authResult.Value()
	if authErr != nil {
		t.Fatalf("resolve project key: %v", authErr)
	}

	return auth
}

func loginRepositoryOperator(
	t *testing.T,
	ctx context.Context,
	store *Store,
) operators.OperatorSession {
	t.Helper()

	loginResult := store.Login(ctx, operators.LoginCommand{
		Email:    "operator@example.test",
		Password: "correct-horse-battery-staple",
	})
	login, loginErr := loginResult.Value()
	if loginErr != nil {
		t.Fatalf("login: %v", loginErr)
	}

	sessionResult := store.ResolveSession(ctx, login.Session)
	session, sessionErr := sessionResult.Value()
	if sessionErr != nil {
		t.Fatalf("session: %v", sessionErr)
	}

	return session
}

func runRepositoryUptimeCheck(
	t *testing.T,
	ctx context.Context,
	store *Store,
	resolver repositoryResolver,
	statusCode int,
	now time.Time,
) uptimeapp.CheckSummary {
	t.Helper()

	checkResult := uptimeapp.CheckDueHTTPMonitors(
		ctx,
		store,
		resolver,
		repositoryProbe{statusCode: statusCode},
		uptimeapp.CheckDueCommand{
			Now:   now,
			Limit: 10,
		},
	)
	summary, checkErr := checkResult.Value()
	if checkErr != nil {
		t.Fatalf("run uptime check: %v", checkErr)
	}

	return summary
}

type repositoryProbe struct {
	statusCode int
}

func (probe repositoryProbe) Get(
	ctx context.Context,
	target outbound.DestinationURL,
	timeout time.Duration,
) result.Result[uptimeapp.HTTPProbeResult] {
	return uptimeapp.NewHTTPProbeResult(probe.statusCode, 20*time.Millisecond)
}

func assertRepositoryOpenMonitorIncident(
	t *testing.T,
	ctx context.Context,
	store *Store,
	monitorID string,
) {
	t.Helper()

	query := `
select count(*)
from uptime_monitor_incidents
where monitor_id = $1
  and resolved_at is null
`
	var count int
	scanErr := store.pool.QueryRow(ctx, query, monitorID).Scan(&count)
	if scanErr != nil {
		t.Fatalf("open incident count: %v", scanErr)
	}

	if count != 1 {
		t.Fatalf("expected one open incident, got %d", count)
	}
}

func assertRepositoryClosedMonitorIncident(
	t *testing.T,
	ctx context.Context,
	store *Store,
	monitorID string,
) {
	t.Helper()

	query := `
select count(*)
from uptime_monitor_incidents
where monitor_id = $1
  and resolved_at is not null
`
	var count int
	scanErr := store.pool.QueryRow(ctx, query, monitorID).Scan(&count)
	if scanErr != nil {
		t.Fatalf("closed incident count: %v", scanErr)
	}

	if count != 1 {
		t.Fatalf("expected one closed incident, got %d", count)
	}
}

func assertRepositoryUptimeView(
	t *testing.T,
	ctx context.Context,
	store *Store,
	scope uptimeapp.Scope,
) {
	t.Helper()

	viewResult := uptimeapp.Show(ctx, store, uptimeapp.Query{Scope: scope, Limit: 10})
	view, viewErr := viewResult.Value()
	if viewErr != nil {
		t.Fatalf("uptime view: %v", viewErr)
	}

	if len(view.Monitors) != 1 {
		t.Fatalf("expected one monitor, got %#v", view)
	}

	monitor := view.Monitors[0]
	if monitor.State != "up" || monitor.LastStatusCode != "204" || monitor.OpenIncidentID != "" {
		t.Fatalf("unexpected monitor view: %#v", monitor)
	}
}

func assertRepositoryStatusPages(
	t *testing.T,
	ctx context.Context,
	store *Store,
	scope uptimeapp.Scope,
	operatorID string,
) {
	t.Helper()

	privateResult := uptimeapp.CreateStatusPage(
		ctx,
		store,
		uptimeapp.CreateStatusPageCommand{
			Scope:      scope,
			ActorID:    operatorID,
			Name:       "Internal status",
			Visibility: "private",
		},
	)
	privatePage, privateErr := privateResult.Value()
	if privateErr != nil {
		t.Fatalf("create private status page: %v", privateErr)
	}

	publicResult := uptimeapp.CreateStatusPage(
		ctx,
		store,
		uptimeapp.CreateStatusPageCommand{
			Scope:      scope,
			ActorID:    operatorID,
			Name:       "Public status",
			Visibility: "public",
		},
	)
	publicPage, publicErr := publicResult.Value()
	if publicErr != nil {
		t.Fatalf("create public status page: %v", publicErr)
	}

	viewResult := uptimeapp.Show(
		ctx,
		store,
		uptimeapp.Query{Scope: scope, Limit: 10},
	)
	view, viewErr := viewResult.Value()
	if viewErr != nil {
		t.Fatalf("uptime view with status pages: %v", viewErr)
	}
	if len(view.StatusPages) != 2 {
		t.Fatalf("expected two status pages, got %#v", view.StatusPages)
	}

	privateViewResult := uptimeapp.ShowPrivateStatusPage(
		ctx,
		store,
		uptimeapp.PrivateStatusPageQuery{
			Scope:  scope,
			PageID: privatePage.PageID,
		},
	)
	privateView, privateViewErr := privateViewResult.Value()
	if privateViewErr != nil {
		t.Fatalf("show private status page: %v", privateViewErr)
	}
	if len(privateView.Monitors) != 2 || len(privateView.Incidents) == 0 {
		t.Fatalf("unexpected private status page: %#v", privateView)
	}

	token := strings.TrimPrefix(publicPage.PublicPath, "/status/")
	publicViewResult := uptimeapp.ShowPublicStatusPage(
		ctx,
		store,
		uptimeapp.PublicStatusPageQuery{Token: token},
	)
	publicView, publicViewErr := publicViewResult.Value()
	if publicViewErr != nil {
		t.Fatalf("show public status page: %v", publicViewErr)
	}
	if publicView.Visibility != "public" || len(publicView.Monitors) != 2 {
		t.Fatalf("unexpected public status page: %#v", publicView)
	}
}

func assertRepositoryUptimeAudit(
	t *testing.T,
	ctx context.Context,
	store *Store,
	scope uptimeapp.Scope,
) {
	t.Helper()

	auditResult := auditapp.List(
		ctx,
		store,
		auditapp.Query{
			Scope: auditapp.Scope{
				OrganizationID: scope.OrganizationID,
				ProjectID:      scope.ProjectID,
			},
			Limit: 50,
		},
	)
	auditView, auditErr := auditResult.Value()
	if auditErr != nil {
		t.Fatalf("audit view: %v", auditErr)
	}

	actions := auditActionsByName(auditView)
	if !actions["monitor_created"] || !actions["monitor_state_changed"] {
		t.Fatalf("expected monitor audit actions in %#v", auditView)
	}

	if !actions["status_page_created"] {
		t.Fatalf("expected status page audit action in %#v", auditView)
	}
}

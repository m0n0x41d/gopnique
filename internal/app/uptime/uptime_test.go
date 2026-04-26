package uptime

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/outbound"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestCreateHTTPMonitorValidatesDestinationAndNormalizesCommand(t *testing.T) {
	manager := &fakeManager{}
	resolver := fakeResolver{
		"status.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.34")},
	}

	createResult := CreateHTTPMonitor(
		context.Background(),
		resolver,
		manager,
		CreateHTTPMonitorCommand{
			Scope:           testScope(t),
			ActorID:         "operator-1",
			Name:            "API",
			URL:             "HTTPS://Status.Example.COM/health",
			IntervalSeconds: 60,
			TimeoutSeconds:  5,
		},
	)
	created, createErr := createResult.Value()
	if createErr != nil {
		t.Fatalf("create monitor: %v", createErr)
	}

	if created.MonitorID != "monitor-1" {
		t.Fatalf("unexpected monitor id: %s", created.MonitorID)
	}

	if manager.command.URL != "https://status.example.com/health" {
		t.Fatalf("unexpected url: %s", manager.command.URL)
	}
}

func TestCreateHTTPMonitorRejectsPrivateResolvedAddress(t *testing.T) {
	manager := &fakeManager{}
	resolver := fakeResolver{
		"status.example.com": []netip.Addr{netip.MustParseAddr("10.0.0.2")},
	}

	createResult := CreateHTTPMonitor(
		context.Background(),
		resolver,
		manager,
		CreateHTTPMonitorCommand{
			Scope:           testScope(t),
			ActorID:         "operator-1",
			Name:            "API",
			URL:             "https://status.example.com/health",
			IntervalSeconds: 60,
			TimeoutSeconds:  5,
		},
	)
	_, createErr := createResult.Value()
	if createErr == nil {
		t.Fatal("expected private address rejection")
	}

	if !strings.Contains(createErr.Error(), "private address") {
		t.Fatalf("unexpected error: %v", createErr)
	}
}

func TestCreateHeartbeatMonitorValidatesAndNormalizesCommand(t *testing.T) {
	manager := &fakeManager{}

	createResult := CreateHeartbeatMonitor(
		context.Background(),
		manager,
		CreateHeartbeatMonitorCommand{
			Scope:           testScope(t),
			ActorID:         "operator-1",
			Name:            "  Nightly import  ",
			IntervalSeconds: 300,
			GraceSeconds:    120,
		},
	)
	created, createErr := createResult.Value()
	if createErr != nil {
		t.Fatalf("create heartbeat monitor: %v", createErr)
	}

	if created.EndpointID != "endpoint-1" {
		t.Fatalf("unexpected endpoint id: %s", created.EndpointID)
	}

	if manager.heartbeatCommand.Name != "Nightly import" {
		t.Fatalf("unexpected name: %s", manager.heartbeatCommand.Name)
	}
}

func TestCreateHeartbeatMonitorRejectsInvalidGrace(t *testing.T) {
	createResult := CreateHeartbeatMonitor(
		context.Background(),
		&fakeManager{},
		CreateHeartbeatMonitorCommand{
			Scope:           testScope(t),
			ActorID:         "operator-1",
			Name:            "Nightly import",
			IntervalSeconds: 300,
			GraceSeconds:    10,
		},
	)
	_, createErr := createResult.Value()
	if createErr == nil {
		t.Fatal("expected heartbeat grace rejection")
	}

	if !strings.Contains(createErr.Error(), "grace") {
		t.Fatalf("unexpected error: %v", createErr)
	}
}

func TestRecordHeartbeatCheckInNormalizesEndpointID(t *testing.T) {
	manager := &fakeManager{}
	checkResult := RecordHeartbeatCheckIn(
		context.Background(),
		manager,
		HeartbeatCheckInCommand{
			EndpointID: "1111111111114111a111111111111111",
			CheckedAt:  time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
		},
	)
	checkIn, checkErr := checkResult.Value()
	if checkErr != nil {
		t.Fatalf("heartbeat check-in: %v", checkErr)
	}

	if checkIn.MonitorID != "monitor-1" {
		t.Fatalf("unexpected monitor id: %s", checkIn.MonitorID)
	}

	if manager.checkInCommand.EndpointID != "11111111-1111-4111-a111-111111111111" {
		t.Fatalf("unexpected endpoint id: %s", manager.checkInCommand.EndpointID)
	}
}

func TestCreateStatusPageValidatesAndNormalizesCommand(t *testing.T) {
	manager := &fakeManager{}
	createResult := CreateStatusPage(
		context.Background(),
		manager,
		CreateStatusPageCommand{
			Scope:      testScope(t),
			ActorID:    "operator-1",
			Name:       "  Public status  ",
			Visibility: "PUBLIC",
		},
	)
	created, createErr := createResult.Value()
	if createErr != nil {
		t.Fatalf("create status page: %v", createErr)
	}

	if created.PublicPath != "/status/status-token" {
		t.Fatalf("unexpected public path: %s", created.PublicPath)
	}

	if manager.statusPageCommand.Name != "Public status" {
		t.Fatalf("unexpected status page name: %s", manager.statusPageCommand.Name)
	}

	if manager.statusPageCommand.Visibility != "public" {
		t.Fatalf("unexpected visibility: %s", manager.statusPageCommand.Visibility)
	}
}

func TestCreateStatusPageRejectsInvalidVisibility(t *testing.T) {
	createResult := CreateStatusPage(
		context.Background(),
		&fakeManager{},
		CreateStatusPageCommand{
			Scope:      testScope(t),
			ActorID:    "operator-1",
			Name:       "Public status",
			Visibility: "world",
		},
	)
	_, createErr := createResult.Value()
	if createErr == nil {
		t.Fatal("expected status page visibility rejection")
	}

	if !strings.Contains(createErr.Error(), "visibility") {
		t.Fatalf("unexpected error: %v", createErr)
	}
}

func TestShowStatusPagesNormalizeIdentifiers(t *testing.T) {
	manager := &fakeManager{}
	privateResult := ShowPrivateStatusPage(
		context.Background(),
		manager,
		PrivateStatusPageQuery{
			Scope:  testScope(t),
			PageID: "1111111111114111a111111111111111",
		},
	)
	_, privateErr := privateResult.Value()
	if privateErr != nil {
		t.Fatalf("private status page: %v", privateErr)
	}

	publicResult := ShowPublicStatusPage(
		context.Background(),
		manager,
		PublicStatusPageQuery{Token: "2222222222224222a222222222222222"},
	)
	_, publicErr := publicResult.Value()
	if publicErr != nil {
		t.Fatalf("public status page: %v", publicErr)
	}

	if manager.privateStatusPageQuery.PageID != "11111111-1111-4111-a111-111111111111" {
		t.Fatalf("unexpected private page id: %s", manager.privateStatusPageQuery.PageID)
	}

	if manager.publicStatusPageQuery.Token != "22222222-2222-4222-a222-222222222222" {
		t.Fatalf("unexpected public token: %s", manager.publicStatusPageQuery.Token)
	}
}

func TestCheckDueHTTPMonitorsRecordsUpAndDownObservations(t *testing.T) {
	scope := testScope(t)
	store := &fakeCheckStore{
		targets: []HTTPMonitorCheckTarget{
			testTarget(t, scope, "1111111111114111a111111111111111", "https://up.example.com/health"),
			testTarget(t, scope, "2222222222224222a222222222222222", "https://down.example.com/health"),
		},
	}
	resolver := fakeResolver{
		"up.example.com":   []netip.Addr{netip.MustParseAddr("93.184.216.34")},
		"down.example.com": []netip.Addr{netip.MustParseAddr("93.184.216.35")},
	}
	probe := fakeProbe{
		"https://up.example.com/health":   204,
		"https://down.example.com/health": 500,
	}

	checkResult := CheckDueHTTPMonitors(
		context.Background(),
		store,
		resolver,
		probe,
		CheckDueCommand{
			Now:   time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
			Limit: 10,
		},
	)
	summary, checkErr := checkResult.Value()
	if checkErr != nil {
		t.Fatalf("check monitors: %v", checkErr)
	}

	if summary.Checked != 2 || summary.Up != 1 || summary.Down != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	if store.observations[0].State != domain.MonitorStateUp {
		t.Fatalf("expected first monitor up: %#v", store.observations[0])
	}

	if store.observations[1].State != domain.MonitorStateDown {
		t.Fatalf("expected second monitor down: %#v", store.observations[1])
	}
}

func TestCheckDueHTTPMonitorsRecordsSSRFRejectionAsDown(t *testing.T) {
	scope := testScope(t)
	store := &fakeCheckStore{
		targets: []HTTPMonitorCheckTarget{
			testTarget(t, scope, "1111111111114111a111111111111111", "https://status.example.com/health"),
		},
	}
	resolver := fakeResolver{
		"status.example.com": []netip.Addr{netip.MustParseAddr("10.0.0.2")},
	}

	checkResult := CheckDueHTTPMonitors(
		context.Background(),
		store,
		resolver,
		fakeProbe{},
		CheckDueCommand{
			Now:   time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
			Limit: 10,
		},
	)
	summary, checkErr := checkResult.Value()
	if checkErr != nil {
		t.Fatalf("check monitors: %v", checkErr)
	}

	if summary.Down != 1 || summary.Failures != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	if !strings.Contains(store.observations[0].Error, "private address") {
		t.Fatalf("expected ssrf error, got %#v", store.observations[0])
	}
}

func TestCheckDueHeartbeatMonitorsRecordsOverdueAsDown(t *testing.T) {
	scope := testScope(t)
	store := &fakeCheckStore{
		heartbeatTargets: []HeartbeatTimeoutTarget{
			testHeartbeatTarget(t, scope, "1111111111114111a111111111111111"),
		},
	}

	checkResult := CheckDueHeartbeatMonitors(
		context.Background(),
		store,
		CheckDueCommand{
			Now:   time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC),
			Limit: 10,
		},
	)
	summary, checkErr := checkResult.Value()
	if checkErr != nil {
		t.Fatalf("check heartbeats: %v", checkErr)
	}

	if summary.Checked != 1 || summary.Down != 1 || summary.Failures != 1 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	if store.heartbeatTimeouts[0].Error != "heartbeat overdue" {
		t.Fatalf("expected heartbeat overdue observation: %#v", store.heartbeatTimeouts[0])
	}
}

type fakeManager struct {
	command                CreateHTTPMonitorCommand
	heartbeatCommand       CreateHeartbeatMonitorCommand
	statusPageCommand      CreateStatusPageCommand
	privateStatusPageQuery PrivateStatusPageQuery
	publicStatusPageQuery  PublicStatusPageQuery
	checkInCommand         HeartbeatCheckInCommand
}

func (manager *fakeManager) ShowUptime(context.Context, Query) result.Result[View] {
	return result.Ok(View{})
}

func (manager *fakeManager) CreateHTTPMonitor(
	ctx context.Context,
	command CreateHTTPMonitorCommand,
) result.Result[MutationResult] {
	manager.command = command
	return result.Ok(MutationResult{MonitorID: "monitor-1"})
}

func (manager *fakeManager) CreateHeartbeatMonitor(
	ctx context.Context,
	command CreateHeartbeatMonitorCommand,
) result.Result[MutationResult] {
	manager.heartbeatCommand = command
	return result.Ok(MutationResult{MonitorID: "monitor-1", EndpointID: "endpoint-1"})
}

func (manager *fakeManager) CreateStatusPage(
	ctx context.Context,
	command CreateStatusPageCommand,
) result.Result[StatusPageMutationResult] {
	manager.statusPageCommand = command
	return result.Ok(StatusPageMutationResult{
		PageID:      "status-page-1",
		Visibility:  command.Visibility,
		PrivatePath: "/status-pages/status-page-1",
		PublicPath:  "/status/status-token",
	})
}

func (manager *fakeManager) ShowPrivateStatusPage(
	ctx context.Context,
	query PrivateStatusPageQuery,
) result.Result[StatusPageView] {
	manager.privateStatusPageQuery = query
	return result.Ok(StatusPageView{})
}

func (manager *fakeManager) ShowPublicStatusPage(
	ctx context.Context,
	query PublicStatusPageQuery,
) result.Result[StatusPageView] {
	manager.publicStatusPageQuery = query
	return result.Ok(StatusPageView{})
}

func (manager *fakeManager) RecordHeartbeatCheckIn(
	ctx context.Context,
	command HeartbeatCheckInCommand,
) result.Result[HeartbeatCheckInResult] {
	manager.checkInCommand = command
	return result.Ok(HeartbeatCheckInResult{MonitorID: "monitor-1"})
}

type fakeCheckStore struct {
	targets           []HTTPMonitorCheckTarget
	heartbeatTargets  []HeartbeatTimeoutTarget
	observations      []HTTPCheckObservation
	heartbeatTimeouts []HeartbeatTimeoutObservation
}

func (store *fakeCheckStore) ClaimDueHTTPMonitors(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]HTTPMonitorCheckTarget] {
	return result.Ok(store.targets)
}

func (store *fakeCheckStore) RecordHTTPMonitorCheck(
	ctx context.Context,
	observation HTTPCheckObservation,
) result.Result[HTTPCheckResult] {
	store.observations = append(store.observations, observation)
	return result.Ok(HTTPCheckResult{})
}

func (store *fakeCheckStore) ClaimOverdueHeartbeatMonitors(
	ctx context.Context,
	now time.Time,
	limit int,
) result.Result[[]HeartbeatTimeoutTarget] {
	return result.Ok(store.heartbeatTargets)
}

func (store *fakeCheckStore) RecordHeartbeatTimeout(
	ctx context.Context,
	observation HeartbeatTimeoutObservation,
) result.Result[HeartbeatTimeoutResult] {
	store.heartbeatTimeouts = append(store.heartbeatTimeouts, observation)
	return result.Ok(HeartbeatTimeoutResult{})
}

type fakeResolver map[string][]netip.Addr

func (resolver fakeResolver) LookupHost(
	ctx context.Context,
	host string,
) result.Result[[]netip.Addr] {
	addresses, ok := resolver[host]
	if !ok {
		return result.Err[[]netip.Addr](errors.New("not found"))
	}

	return result.Ok(addresses)
}

type fakeProbe map[string]int

func (probe fakeProbe) Get(
	ctx context.Context,
	target outbound.DestinationURL,
	timeout time.Duration,
) result.Result[HTTPProbeResult] {
	status, ok := probe[target.String()]
	if !ok {
		return result.Err[HTTPProbeResult](errors.New("probe unavailable"))
	}

	return NewHTTPProbeResult(status, 10*time.Millisecond)
}

func testTarget(
	t *testing.T,
	scope Scope,
	monitorIDText string,
	targetURL string,
) HTTPMonitorCheckTarget {
	t.Helper()

	monitorID, monitorIDErr := domain.NewMonitorID(monitorIDText)
	if monitorIDErr != nil {
		t.Fatalf("monitor id: %v", monitorIDErr)
	}

	destinationResult := outbound.ParseDestinationURL(targetURL)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("destination: %v", destinationErr)
	}

	return HTTPMonitorCheckTarget{
		Scope:     scope,
		MonitorID: monitorID,
		URL:       destination,
		Timeout:   5 * time.Second,
	}
}

func testHeartbeatTarget(
	t *testing.T,
	scope Scope,
	monitorIDText string,
) HeartbeatTimeoutTarget {
	t.Helper()

	monitorID, monitorIDErr := domain.NewMonitorID(monitorIDText)
	if monitorIDErr != nil {
		t.Fatalf("monitor id: %v", monitorIDErr)
	}

	return HeartbeatTimeoutTarget{
		Scope:     scope,
		MonitorID: monitorID,
	}
}

func testScope(t *testing.T) Scope {
	t.Helper()

	organizationID, organizationErr := domain.NewOrganizationID("3333333333334333a333333333333333")
	if organizationErr != nil {
		t.Fatalf("organization id: %v", organizationErr)
	}

	projectID, projectErr := domain.NewProjectID("4444444444444444a444444444444444")
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	return Scope{
		OrganizationID: organizationID,
		ProjectID:      projectID,
	}
}

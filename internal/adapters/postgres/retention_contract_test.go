//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	retentionapp "github.com/ivanzakutnii/error-tracker/internal/app/retention"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	userreportapp "github.com/ivanzakutnii/error-tracker/internal/app/userreports"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestPostgresRetentionPurgesScopedProjectData(t *testing.T) {
	ctx := context.Background()
	adminURL := repositoryAdminURL(t)
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
	if len(migrationResult.Applied) != 33 {
		t.Fatalf("expected 33 migrations, got %d", len(migrationResult.Applied))
	}

	bootstrap, bootstrapErr := store.Bootstrap(ctx, BootstrapInput{
		PublicURL:        "http://example.test",
		OrganizationName: "Retention Org",
		ProjectName:      "Retention API",
		OperatorEmail:    "operator@example.test",
		OperatorPassword: "correct-horse-battery-staple",
	})
	if bootstrapErr != nil {
		t.Fatalf("bootstrap: %v", bootstrapErr)
	}

	ref := mustRepositoryValue(t, domain.NewProjectRef, bootstrap.ProjectRef)
	publicKey := mustRepositoryValue(t, domain.NewProjectPublicKey, bootstrap.PublicKey)
	authResult := store.ResolveProjectKey(ctx, ref, publicKey)
	auth, authErr := authResult.Value()
	if authErr != nil {
		t.Fatalf("resolve project key: %v", authErr)
	}

	scope := settingsapp.Scope{
		OrganizationID: auth.OrganizationID(),
		ProjectID:      auth.ProjectID(),
	}
	destinationResult := settingsapp.AddTelegramDestination(
		ctx,
		store,
		settingsapp.AddTelegramDestinationCommand{
			Scope:  scope,
			ChatID: "123456",
			Label:  "retention-telegram",
		},
	)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("telegram destination: %v", destinationErr)
	}

	alertResult := settingsapp.AddIssueOpenedTelegramAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedTelegramAlertCommand{
			Scope:         scope,
			DestinationID: destination.DestinationID,
			Name:          "Retention alert",
		},
	)
	_, alertErr := alertResult.Value()
	if alertErr != nil {
		t.Fatalf("retention alert: %v", alertErr)
	}

	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	oldEvent := retentionEvent(t, auth.OrganizationID(), auth.ProjectID(), "880e8400e29b41d4a716446655440000", now.AddDate(0, 0, -100), "OldRetentionError")
	payloadEvent := retentionEvent(t, auth.OrganizationID(), auth.ProjectID(), "890e8400e29b41d4a716446655440000", now.AddDate(0, 0, -40), "PayloadRetentionError")
	freshEvent := retentionEvent(t, auth.OrganizationID(), auth.ProjectID(), "900e8400e29b41d4a716446655440000", now.AddDate(0, 0, -1), "FreshRetentionError")

	for _, event := range []domain.CanonicalEvent{oldEvent, payloadEvent, freshEvent} {
		receiptResult := ingest.IngestCanonicalEvent(ctx, ingest.NewIngestCommand(event), store)
		_, receiptErr := receiptResult.Value()
		if receiptErr != nil {
			t.Fatalf("ingest retention event: %v", receiptErr)
		}
	}

	reportResult := userreportapp.Submit(
		ctx,
		store,
		userreportapp.SubmitCommand{
			Scope: userreportapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			EventID:  payloadEvent.EventID(),
			Name:     "Retention User",
			Email:    "retention-user@example.test",
			Comments: "old report",
		},
	)
	_, reportErr := reportResult.Value()
	if reportErr != nil {
		t.Fatalf("submit report: %v", reportErr)
	}

	_, updateErr := store.pool.Exec(
		ctx,
		`update user_reports set created_at = $1 where project_id = $2`,
		now.AddDate(0, 0, -100),
		auth.ProjectID().String(),
	)
	if updateErr != nil {
		t.Fatalf("age user report: %v", updateErr)
	}

	_, deliveryAgeErr := store.pool.Exec(
		ctx,
		`
update notification_intents n
set
  created_at = e.received_at,
  next_attempt_at = e.received_at
from events e
where n.event_id = e.id
  and n.project_id = $1
`,
		auth.ProjectID().String(),
	)
	if deliveryAgeErr != nil {
		t.Fatalf("age deliveries: %v", deliveryAgeErr)
	}

	summaryResult := retentionapp.Run(
		ctx,
		store,
		retentionapp.Command{Now: now, BatchSize: 100},
	)
	summary, summaryErr := summaryResult.Value()
	if summaryErr != nil {
		t.Fatalf("run retention: %v", summaryErr)
	}

	if summary.ProjectsProcessed != 1 {
		t.Fatalf("unexpected processed projects: %#v", summary)
	}
	if summary.EventsDeleted != 1 || summary.IssuesDeleted != 1 {
		t.Fatalf("expected old event and issue purge, got %#v", summary)
	}
	if summary.PayloadsCleared < 1 || summary.DeliveryRowsDeleted < 1 || summary.UserReportsDeleted != 1 {
		t.Fatalf("expected payload, delivery, and report purge, got %#v", summary)
	}

	assertEventMissing(t, ctx, store, auth.ProjectID(), oldEvent.EventID())
	assertPayloadCleared(t, ctx, store, auth.ProjectID(), payloadEvent.EventID())
	assertPayloadPresent(t, ctx, store, auth.ProjectID(), freshEvent.EventID())
	assertProjectRowCount(t, ctx, store, "user_reports", auth.ProjectID(), 0)
	assertProjectRowCount(t, ctx, store, "notification_intents", auth.ProjectID(), 1)
}

func retentionEvent(
	t *testing.T,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	eventIDText string,
	receivedAt time.Time,
	titleText string,
) domain.CanonicalEvent {
	t.Helper()

	eventID := mustRepositoryValue(t, domain.NewEventID, eventIDText)
	timePoint := mustRepositoryTimePoint(t, receivedAt)
	title := mustRepositoryTitle(t, titleText+": retention policy")
	event, eventErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       organizationID,
		ProjectID:            projectID,
		EventID:              eventID,
		OccurredAt:           timePoint,
		ReceivedAt:           timePoint,
		Kind:                 domain.EventKindError,
		Level:                domain.EventLevelError,
		Title:                title,
		Platform:             "go",
		Release:              "retention@1.0.0",
		Environment:          "test",
		Tags:                 map[string]string{"suite": "retention"},
		DefaultGroupingParts: []string{titleText, eventIDText},
	})
	if eventErr != nil {
		t.Fatalf("canonical event: %v", eventErr)
	}

	return event
}

func assertEventMissing(
	t *testing.T,
	ctx context.Context,
	store *Store,
	projectID domain.ProjectID,
	eventID domain.EventID,
) {
	t.Helper()

	count := countRows(
		t,
		ctx,
		store,
		`select count(*) from events where project_id = $1 and event_id = $2`,
		projectID.String(),
		eventID.String(),
	)
	if count != 0 {
		t.Fatalf("expected event %s to be purged, got %d", eventID.String(), count)
	}
}

func assertPayloadCleared(
	t *testing.T,
	ctx context.Context,
	store *Store,
	projectID domain.ProjectID,
	eventID domain.EventID,
) {
	t.Helper()

	count := countRows(
		t,
		ctx,
		store,
		`select count(*) from events where project_id = $1 and event_id = $2 and canonical_payload is null`,
		projectID.String(),
		eventID.String(),
	)
	if count != 1 {
		t.Fatalf("expected payload for %s to be cleared, got %d", eventID.String(), count)
	}
}

func assertPayloadPresent(
	t *testing.T,
	ctx context.Context,
	store *Store,
	projectID domain.ProjectID,
	eventID domain.EventID,
) {
	t.Helper()

	count := countRows(
		t,
		ctx,
		store,
		`select count(*) from events where project_id = $1 and event_id = $2 and canonical_payload is not null`,
		projectID.String(),
		eventID.String(),
	)
	if count != 1 {
		t.Fatalf("expected payload for %s to remain, got %d", eventID.String(), count)
	}
}

func assertProjectRowCount(
	t *testing.T,
	ctx context.Context,
	store *Store,
	table string,
	projectID domain.ProjectID,
	expected int,
) {
	t.Helper()

	count := countRows(
		t,
		ctx,
		store,
		"select count(*) from "+table+" where project_id = $1",
		projectID.String(),
	)
	if count != expected {
		t.Fatalf("expected %s count %d, got %d", table, expected, count)
	}
}

func countRows(
	t *testing.T,
	ctx context.Context,
	store *Store,
	query string,
	args ...any,
) int {
	t.Helper()

	var count int
	scanErr := store.pool.QueryRow(ctx, query, args...).Scan(&count)
	if scanErr != nil {
		t.Fatalf("count rows: %v", scanErr)
	}

	return count
}

//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	settingsapp "github.com/ivanzakutnii/error-tracker/internal/app/settings"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestPostgresQuotaRejectsBeforePersistence(t *testing.T) {
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
		OrganizationName: "Quota Org",
		ProjectName:      "Quota API",
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

	enableProjectQuota(t, ctx, store, auth.ProjectID(), 1)
	addQuotaTelegramAlert(t, ctx, store, auth)

	now := time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
	firstEvent := retentionEvent(t, auth.OrganizationID(), auth.ProjectID(), "910e8400e29b41d4a716446655440000", now, "FirstQuotaError")
	secondEvent := retentionEvent(t, auth.OrganizationID(), auth.ProjectID(), "920e8400e29b41d4a716446655440000", now.Add(time.Minute), "SecondQuotaError")

	firstResult := ingest.IngestCanonicalEvent(ctx, ingest.NewIngestCommand(firstEvent), store)
	firstReceipt, firstErr := firstResult.Value()
	if firstErr != nil {
		t.Fatalf("first ingest: %v", firstErr)
	}
	if firstReceipt.Kind() != ingest.ReceiptAcceptedIssueEvent {
		t.Fatalf("unexpected first receipt: %s", firstReceipt.Kind())
	}

	secondResult := ingest.IngestCanonicalEvent(ctx, ingest.NewIngestCommand(secondEvent), store)
	secondReceipt, secondErr := secondResult.Value()
	if secondErr != nil {
		t.Fatalf("second ingest: %v", secondErr)
	}
	if secondReceipt.Kind() != ingest.ReceiptQuotaRejected {
		t.Fatalf("expected quota receipt, got %s", secondReceipt.Kind())
	}
	if secondReceipt.Reason() != "project_quota_exceeded" {
		t.Fatalf("unexpected quota reason: %s", secondReceipt.Reason())
	}

	duplicateResult := ingest.IngestCanonicalEvent(ctx, ingest.NewIngestCommand(firstEvent), store)
	duplicateReceipt, duplicateErr := duplicateResult.Value()
	if duplicateErr != nil {
		t.Fatalf("duplicate ingest: %v", duplicateErr)
	}
	if duplicateReceipt.Kind() != ingest.ReceiptDuplicateEvent {
		t.Fatalf("expected duplicate before quota, got %s", duplicateReceipt.Kind())
	}

	assertEventMissing(t, ctx, store, auth.ProjectID(), secondEvent.EventID())
	assertProjectRowCount(t, ctx, store, "notification_intents", auth.ProjectID(), 1)
	assertProjectRowCount(t, ctx, store, "issues", auth.ProjectID(), 1)
}

func enableProjectQuota(
	t *testing.T,
	ctx context.Context,
	store *Store,
	projectID domain.ProjectID,
	limit int,
) {
	t.Helper()

	_, updateErr := store.pool.Exec(
		ctx,
		`
update project_quota_policies
set enabled = true,
    daily_event_limit = $1,
    updated_at = $2
where project_id = $3
`,
		limit,
		time.Now().UTC(),
		projectID.String(),
	)
	if updateErr != nil {
		t.Fatalf("enable project quota: %v", updateErr)
	}
}

func addQuotaTelegramAlert(
	t *testing.T,
	ctx context.Context,
	store *Store,
	auth domain.ProjectAuth,
) {
	t.Helper()

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
			Label:  "quota-telegram",
		},
	)
	destination, destinationErr := destinationResult.Value()
	if destinationErr != nil {
		t.Fatalf("quota telegram destination: %v", destinationErr)
	}

	alertResult := settingsapp.AddIssueOpenedTelegramAlert(
		ctx,
		store,
		settingsapp.AddIssueOpenedTelegramAlertCommand{
			Scope:         scope,
			DestinationID: destination.DestinationID,
			Name:          "Quota alert",
		},
	)
	_, alertErr := alertResult.Value()
	if alertErr != nil {
		t.Fatalf("quota alert: %v", alertErr)
	}
}

package projects

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestShowCurrentProjectBuildsDSNAndEndpoints(t *testing.T) {
	projectID := mustProjectDomain(t, domain.NewProjectID, "2222222222224222a222222222222222")
	organizationID := mustProjectDomain(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	keyID := mustProjectDomain(t, domain.NewProjectKeyID, "3333333333334333a333333333333333")
	publicKey := mustProjectDomain(t, domain.NewProjectPublicKey, "550e8400-e29b-41d4-a716-446655440000")
	createdAt := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	reader := fakeProjectReader{
		record: ProjectRecord{
			OrganizationName: "Example Org",
			ProjectID:        projectID,
			Name:             "API",
			Slug:             "api",
			IngestRef:        "1",
			AcceptingEvents:  true,
			ScrubIPAddresses: true,
			CreatedAt:        createdAt,
			ActiveKey: ProjectKeyRecord{
				ID:        keyID,
				PublicKey: publicKey,
				Label:     "default",
				CreatedAt: createdAt,
			},
		},
	}

	viewResult := ShowCurrentProject(
		context.Background(),
		reader,
		ProjectQuery{
			Scope: Scope{
				OrganizationID: organizationID,
				ProjectID:      projectID,
			},
			PublicURL: "http://example.test/app/",
		},
	)
	view, viewErr := viewResult.Value()
	if viewErr != nil {
		t.Fatalf("show project: %v", viewErr)
	}

	expectedDSN := "http://550e8400e29b41d4a716446655440000@example.test/app/1"
	if view.DSN != expectedDSN {
		t.Fatalf("unexpected dsn: %s", view.DSN)
	}

	if view.StoreEndpoint != "http://example.test/app/api/1/store/" {
		t.Fatalf("unexpected store endpoint: %s", view.StoreEndpoint)
	}

	if view.EnvelopeEndpoint != "http://example.test/app/api/1/envelope/" {
		t.Fatalf("unexpected envelope endpoint: %s", view.EnvelopeEndpoint)
	}

	if view.AcceptingEvents != "enabled" || view.ScrubIPAddresses != "enabled" {
		t.Fatalf("unexpected statuses: %s %s", view.AcceptingEvents, view.ScrubIPAddresses)
	}

	if len(view.SDKSnippets) != 4 {
		t.Fatalf("expected sdk snippets, got %d", len(view.SDKSnippets))
	}

	if view.SDKSnippets[0].Package != "@sentry/node" || !strings.Contains(view.SDKSnippets[0].Code, expectedDSN) {
		t.Fatalf("unexpected node snippet: %#v", view.SDKSnippets[0])
	}
}

func TestShowCurrentProjectRejectsInvalidBoundaryInput(t *testing.T) {
	projectID := mustProjectDomain(t, domain.NewProjectID, "2222222222224222a222222222222222")
	organizationID := mustProjectDomain(t, domain.NewOrganizationID, "1111111111114111a111111111111111")

	viewResult := ShowCurrentProject(
		context.Background(),
		fakeProjectReader{},
		ProjectQuery{
			Scope: Scope{
				OrganizationID: organizationID,
				ProjectID:      projectID,
			},
			PublicURL: "ftp://example.test",
		},
	)
	_, viewErr := viewResult.Value()
	if viewErr == nil {
		t.Fatal("expected invalid public url to fail")
	}

	if !strings.Contains(viewErr.Error(), "http or https") {
		t.Fatalf("unexpected error: %v", viewErr)
	}
}

type fakeProjectReader struct {
	record ProjectRecord
}

func (reader fakeProjectReader) FindCurrentProject(
	ctx context.Context,
	query ProjectQuery,
) result.Result[ProjectRecord] {
	return result.Ok(reader.record)
}

func mustProjectDomain[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	value, err := constructor(input)
	if err != nil {
		t.Fatalf("domain value: %v", err)
	}

	return value
}

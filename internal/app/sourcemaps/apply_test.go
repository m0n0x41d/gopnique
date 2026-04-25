package sourcemaps

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestApplyToCanonicalEventResolvesUnresolvedFrames(t *testing.T) {
	ctx := context.Background()
	service, organizationID, projectID := newApplyServiceFixture(t)

	uploadServiceFixture(
		t,
		ctx,
		service,
		organizationID,
		projectID,
		"frontend@1.0.0",
		"static/js/app.min.js",
		buildSourceMapPayload(
			[]string{"original.js"},
			[]string{"computeTotal"},
			"AAAAA",
		),
	)

	frame, frameErr := domain.NewUnresolvedJsStacktraceFrame(
		"https://cdn.example.com/static/js/app.min.js",
		"r",
		1,
		0,
	)
	if frameErr != nil {
		t.Fatalf("frame: %v", frameErr)
	}

	event := buildEventFixture(t, organizationID, projectID, "frontend@1.0.0", []domain.JsStacktraceFrame{frame})

	updated := ApplyToCanonicalEvent(ctx, service, event)
	updatedFrames := updated.JsStacktrace()
	if len(updatedFrames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(updatedFrames))
	}

	resolution, hasResolution := updatedFrames[0].Resolution()
	if !hasResolution {
		t.Fatal("expected resolved frame")
	}

	if resolution.Source() != "original.js" {
		t.Fatalf("unexpected source: %q", resolution.Source())
	}

	if resolution.Symbol() != "computeTotal" {
		t.Fatalf("unexpected symbol: %q", resolution.Symbol())
	}

	if resolution.OriginalLine() != 1 {
		t.Fatalf("expected original line 1 (1-based), got %d", resolution.OriginalLine())
	}

	if resolution.OriginalColumn() != 0 {
		t.Fatalf("expected original column 0, got %d", resolution.OriginalColumn())
	}

	if updatedFrames[0].AbsPath() != "https://cdn.example.com/static/js/app.min.js" {
		t.Fatalf("unexpected abs path: %q", updatedFrames[0].AbsPath())
	}

	if updatedFrames[0].Function() != "r" {
		t.Fatalf("expected original minified function preserved, got %q", updatedFrames[0].Function())
	}
}

func TestApplyToCanonicalEventReturnsUnchangedWhenResolverNil(t *testing.T) {
	_, organizationID, projectID := newApplyServiceFixture(t)

	frame, frameErr := domain.NewUnresolvedJsStacktraceFrame(
		"https://cdn.example.com/static/js/app.min.js",
		"r",
		1,
		0,
	)
	if frameErr != nil {
		t.Fatalf("frame: %v", frameErr)
	}

	event := buildEventFixture(t, organizationID, projectID, "frontend@1.0.0", []domain.JsStacktraceFrame{frame})

	updated := ApplyToCanonicalEvent(context.Background(), nil, event)
	if _, hasResolution := updated.JsStacktrace()[0].Resolution(); hasResolution {
		t.Fatal("expected frame to remain unresolved with nil resolver")
	}
}

func TestApplyToCanonicalEventReturnsUnchangedWhenNoFrames(t *testing.T) {
	ctx := context.Background()
	service, organizationID, projectID := newApplyServiceFixture(t)

	event := buildEventFixture(t, organizationID, projectID, "frontend@1.0.0", nil)

	updated := ApplyToCanonicalEvent(ctx, service, event)
	if len(updated.JsStacktrace()) != 0 {
		t.Fatalf("expected no frames, got %d", len(updated.JsStacktrace()))
	}
}

func TestApplyToCanonicalEventReturnsUnchangedWhenNoRelease(t *testing.T) {
	ctx := context.Background()
	service, organizationID, projectID := newApplyServiceFixture(t)

	uploadServiceFixture(
		t,
		ctx,
		service,
		organizationID,
		projectID,
		"frontend@1.0.0",
		"static/js/app.min.js",
		buildSourceMapPayload([]string{"original.js"}, []string{"computeTotal"}, "AAAAA"),
	)

	frame, frameErr := domain.NewUnresolvedJsStacktraceFrame(
		"https://cdn.example.com/static/js/app.min.js",
		"r",
		1,
		0,
	)
	if frameErr != nil {
		t.Fatalf("frame: %v", frameErr)
	}

	event := buildEventFixture(t, organizationID, projectID, "", []domain.JsStacktraceFrame{frame})

	updated := ApplyToCanonicalEvent(ctx, service, event)
	if _, hasResolution := updated.JsStacktrace()[0].Resolution(); hasResolution {
		t.Fatal("expected frame to remain unresolved when event has no release")
	}
}

func TestApplyToCanonicalEventLeavesUnmappedFrames(t *testing.T) {
	ctx := context.Background()
	service, organizationID, projectID := newApplyServiceFixture(t)

	frame, frameErr := domain.NewUnresolvedJsStacktraceFrame(
		"https://cdn.example.com/static/js/missing.min.js",
		"r",
		1,
		0,
	)
	if frameErr != nil {
		t.Fatalf("frame: %v", frameErr)
	}

	event := buildEventFixture(t, organizationID, projectID, "frontend@1.0.0", []domain.JsStacktraceFrame{frame})

	updated := ApplyToCanonicalEvent(ctx, service, event)
	if _, hasResolution := updated.JsStacktrace()[0].Resolution(); hasResolution {
		t.Fatal("expected frame to remain unresolved when source map missing")
	}
}

func TestApplyToCanonicalEventDoesNotReResolveAlreadyResolvedFrame(t *testing.T) {
	ctx := context.Background()
	service, organizationID, projectID := newApplyServiceFixture(t)

	uploadServiceFixture(
		t,
		ctx,
		service,
		organizationID,
		projectID,
		"frontend@1.0.0",
		"static/js/app.min.js",
		buildSourceMapPayload([]string{"original.js"}, []string{"computeTotal"}, "AAAAA"),
	)

	preResolved, preResolvedErr := domain.NewResolvedJsStacktraceFrame(
		"https://cdn.example.com/static/js/app.min.js",
		"r",
		1,
		0,
		"webpack:///./src/preresolved.tsx",
		"preExisting",
		7,
		3,
	)
	if preResolvedErr != nil {
		t.Fatalf("pre-resolved frame: %v", preResolvedErr)
	}

	event := buildEventFixture(t, organizationID, projectID, "frontend@1.0.0", []domain.JsStacktraceFrame{preResolved})

	updated := ApplyToCanonicalEvent(ctx, service, event)
	resolution, hasResolution := updated.JsStacktrace()[0].Resolution()
	if !hasResolution {
		t.Fatal("expected pre-resolved frame to remain resolved")
	}

	if resolution.Source() != "webpack:///./src/preresolved.tsx" {
		t.Fatalf("expected pre-resolution preserved, got source %q", resolution.Source())
	}

	if resolution.Symbol() != "preExisting" {
		t.Fatalf("expected pre-resolution preserved, got symbol %q", resolution.Symbol())
	}
}

func TestApplyToCanonicalEventLeavesNonJsErrorEventsUnchanged(t *testing.T) {
	ctx := context.Background()
	service, organizationID, projectID := newApplyServiceFixture(t)

	event := buildEventFixture(t, organizationID, projectID, "frontend@1.0.0", nil)
	updated := ApplyToCanonicalEvent(ctx, service, event)
	if len(updated.JsStacktrace()) != 0 {
		t.Fatalf("expected no frames, got %d", len(updated.JsStacktrace()))
	}
}

func TestSourceMapFileNameFromAbsPathHandlesURLAndBarePath(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"https://cdn.example.com/static/js/app.min.js", "static/js/app.min.js"},
		{"https://cdn.example.com/static/js/app.min.js?v=1", "static/js/app.min.js"},
		{"file:///opt/app.min.js", "opt/app.min.js"},
		{"static/js/app.min.js", "static/js/app.min.js"},
		{"app.min.js", "app.min.js"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := sourceMapFileNameFromAbsPath(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got.String() != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got.String())
			}
		})
	}
}

func TestSourceMapFileNameFromAbsPathRejectsEmpty(t *testing.T) {
	if _, err := sourceMapFileNameFromAbsPath("   "); err == nil {
		t.Fatal("expected error for empty abs path")
	}
}

func newApplyServiceFixture(t *testing.T) (*Service, domain.OrganizationID, domain.ProjectID) {
	t.Helper()

	vault := newMemoryVault()
	service, serviceErr := NewService(vault)
	if serviceErr != nil {
		t.Fatalf("service: %v", serviceErr)
	}

	organizationID, _ := domain.NewOrganizationID("11111111-1111-1111-1111-111111111111")
	projectID, _ := domain.NewProjectID("22222222-2222-2222-2222-222222222222")

	return service, organizationID, projectID
}

func uploadServiceFixture(
	t *testing.T,
	ctx context.Context,
	service *Service,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	releaseInput string,
	fileNameInput string,
	payload []byte,
) {
	t.Helper()

	release, releaseErr := domain.NewReleaseName(releaseInput)
	if releaseErr != nil {
		t.Fatalf("release: %v", releaseErr)
	}

	dist, _ := domain.NewOptionalDistName("")

	fileName, fileErr := domain.NewSourceMapFileName(fileNameInput)
	if fileErr != nil {
		t.Fatalf("file name: %v", fileErr)
	}

	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		t.Fatalf("identity: %v", identityErr)
	}

	uploadResult := service.Upload(ctx, organizationID, projectID, identity, bytes.NewReader(payload))
	if _, err := uploadResult.Value(); err != nil {
		t.Fatalf("upload: %v", err)
	}
}

func buildEventFixture(
	t *testing.T,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	release string,
	frames []domain.JsStacktraceFrame,
) domain.CanonicalEvent {
	t.Helper()

	eventID, eventErr := domain.NewEventID("550e8400e29b41d4a716446655440000")
	if eventErr != nil {
		t.Fatalf("event id: %v", eventErr)
	}

	occurredAt, _ := domain.NewTimePoint(time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC))
	receivedAt, _ := domain.NewTimePoint(time.Date(2026, 4, 25, 10, 0, 1, 0, time.UTC))

	title, titleErr := domain.NewEventTitle("TypeError: bad operand")
	if titleErr != nil {
		t.Fatalf("title: %v", titleErr)
	}

	event, buildErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		EventID:        eventID,
		OccurredAt:     occurredAt,
		ReceivedAt:     receivedAt,
		Kind:           domain.EventKindError,
		Level:          domain.EventLevelError,
		Title:          title,
		Platform:       "javascript",
		Release:        release,
		JsStacktrace:   frames,
	})
	if buildErr != nil {
		t.Fatalf("event: %v", buildErr)
	}

	return event
}

package debugfiles

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestApplyToCanonicalEventResolvesBreakpadFrame(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)
	body := []byte(
		"MODULE Linux x86_64 deadbeefcafef00ddeadbeefcafef00d libapp.so\n" +
			"FUNC 1000 20 0 render_home\n",
	)

	if _, err := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(body),
	).Value(); err != nil {
		t.Fatalf("upload: %v", err)
	}

	module, moduleErr := domain.NewNativeModule(
		identity.DebugID(),
		"/usr/lib/libapp.so",
		0x10000000,
		0x2000,
	)
	if moduleErr != nil {
		t.Fatalf("module: %v", moduleErr)
	}

	frame, frameErr := domain.NewNativeFrameWithModule(
		0x10001004,
		identity.DebugID(),
		"",
		"",
	)
	if frameErr != nil {
		t.Fatalf("frame: %v", frameErr)
	}

	event := nativeEventFixture(t, organizationID, projectID, []domain.NativeModule{module}, []domain.NativeFrame{frame})
	updated := ApplyToCanonicalEvent(context.Background(), service, event)
	frames := updated.NativeFrames()

	if len(frames) != 1 {
		t.Fatalf("expected one frame, got %d", len(frames))
	}

	if frames[0].Function() != "render_home" {
		t.Fatalf("unexpected symbolicated function: %q", frames[0].Function())
	}

	if frames[0].Package() != "/usr/lib/libapp.so" {
		t.Fatalf("unexpected package: %q", frames[0].Package())
	}
}

func TestApplyToCanonicalEventLeavesUnmatchedFrame(t *testing.T) {
	service, identity, organizationID, projectID := newServiceFixture(t)
	body := []byte(
		"MODULE Linux x86_64 deadbeefcafef00ddeadbeefcafef00d libapp.so\n" +
			"FUNC 1000 20 0 render_home\n",
	)

	if _, err := service.Upload(
		context.Background(),
		organizationID,
		projectID,
		identity,
		bytes.NewReader(body),
	).Value(); err != nil {
		t.Fatalf("upload: %v", err)
	}

	frame, frameErr := domain.NewNativeFrameWithModule(
		0x20002000,
		identity.DebugID(),
		"",
		"",
	)
	if frameErr != nil {
		t.Fatalf("frame: %v", frameErr)
	}

	event := nativeEventFixture(t, organizationID, projectID, nil, []domain.NativeFrame{frame})
	updated := ApplyToCanonicalEvent(context.Background(), service, event)
	frames := updated.NativeFrames()

	if frames[0].Function() != "" {
		t.Fatalf("expected unmatched frame to remain unresolved: %q", frames[0].Function())
	}
}

func TestBreakpadParserAcceptsMozillaMarkerFunction(t *testing.T) {
	body := []byte(
		"MODULE Linux x86_64 deadbeefcafef00ddeadbeefcafef00d libapp.so\n" +
			"FUNC m 2000 10 0 render_marked\n",
	)

	symbols, parseErr := parseBreakpadSymbols(body)
	if parseErr != nil {
		t.Fatalf("parse symbols: %v", parseErr)
	}

	name, found := symbols.lookup(0x2008)
	if !found {
		t.Fatal("expected function lookup")
	}

	if name != "render_marked" {
		t.Fatalf("unexpected function: %q", name)
	}
}

func nativeEventFixture(
	t *testing.T,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	modules []domain.NativeModule,
	frames []domain.NativeFrame,
) domain.CanonicalEvent {
	t.Helper()

	eventID, _ := domain.NewEventID("550e8400e29b41d4a716446655440000")
	now, _ := domain.NewTimePoint(testInstant())
	title, _ := domain.NewEventTitle("native crash")
	event, eventErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		EventID:        eventID,
		OccurredAt:     now,
		ReceivedAt:     now,
		Kind:           domain.EventKindError,
		Level:          domain.EventLevelFatal,
		Title:          title,
		Platform:       "native",
		NativeModules:  modules,
		NativeFrames:   frames,
	})
	if eventErr != nil {
		t.Fatalf("event: %v", eventErr)
	}

	return event
}

func testInstant() time.Time {
	return time.Date(2026, 4, 25, 12, 0, 0, 0, time.UTC)
}

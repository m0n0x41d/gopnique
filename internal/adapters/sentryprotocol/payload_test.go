package sentryprotocol

import (
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestParseStoreEventBuildsCanonicalErrorEvent(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"platform": "python",
			"level": "error",
			"exception": {
				"values": [{
					"type": "TypeError",
					"value": "bad operand",
					"stacktrace": {
						"frames": [
							{"filename": "lib.py", "function": "helper", "lineno": 3, "in_app": false},
							{"filename": "handler.py", "function": "handle", "lineno": 42, "in_app": true}
						]
					}
				}]
			},
			"fingerprint": ["custom", "{{ default }}"]
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	if event.Kind() != domain.EventKindError {
		t.Fatalf("unexpected kind: %s", event.Kind())
	}

	if event.Title().String() != "TypeError: bad operand" {
		t.Fatalf("unexpected title: %s", event.Title().String())
	}

	if event.Platform() != "python" {
		t.Fatalf("unexpected platform: %s", event.Platform())
	}

	parts := event.DefaultGroupingParts()
	if parts[2] != "handler.py" {
		t.Fatalf("expected in-app frame filename, got %s", parts[2])
	}

	fingerprint := event.ExplicitFingerprint()
	if len(fingerprint) != 2 {
		t.Fatalf("expected explicit fingerprint parts, got %d", len(fingerprint))
	}
}

func TestParseStoreEventRejectsMissingEventID(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{"timestamp": "2026-04-24T10:00:00Z"}`),
	)
	_, eventErr := eventResult.Value()
	if eventErr == nil {
		t.Fatal("expected missing event id to fail")
	}
}

func TestParseStoreEventCarriesReleaseEnvironmentAndTags(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"level": "error",
			"message": "hello",
			"release": "api@1.2.3",
			"environment": "production",
			"server_name": "api-1",
			"tags": [["region", "eu"], ["tier", "api"]]
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	if event.Release() != "api@1.2.3" {
		t.Fatalf("unexpected release: %s", event.Release())
	}

	if event.Environment() != "production" {
		t.Fatalf("unexpected environment: %s", event.Environment())
	}

	tags := event.Tags()
	if tags["region"] != "eu" || tags["tier"] != "api" || tags["server_name"] != "api-1" {
		t.Fatalf("unexpected tags: %#v", tags)
	}
}

func TestParseStoreEventScrubsClientIPByDefault(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"level": "error",
			"message": "hello",
			"user": {"ip_address": "8.8.8.8"},
			"request": {"env": {"REMOTE_ADDR": "1.1.1.1"}},
			"tags": {"client_ip": "9.9.9.9", "region": "eu"}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	tags := event.Tags()
	if _, ok := tags["client_ip"]; ok {
		t.Fatalf("expected client ip to be scrubbed: %#v", tags)
	}

	if tags["region"] != "eu" {
		t.Fatalf("expected non-ip tag to remain: %#v", tags)
	}
}

func TestParseStoreEventCarriesPublicClientIPWhenPolicyAllows(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContextWithScrubPolicy(t, false),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"level": "error",
			"message": "hello",
			"user": {"ip_address": "8.8.8.8"},
			"request": {
				"env": {"REMOTE_ADDR": "10.0.0.1"},
				"headers": [["X-Forwarded-For", "192.168.1.10, 1.1.1.1"]]
			},
			"tags": {"client_ip": "9.9.9.9"}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	tags := event.Tags()
	if tags["client_ip"] != "8.8.8.8" {
		t.Fatalf("unexpected client ip tag: %#v", tags)
	}
}

func TestParseStoreEventRejectsPrivateClientIPEvenWhenPolicyAllows(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContextWithScrubPolicy(t, false),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"level": "error",
			"message": "hello",
			"user": {"ip_address": "10.1.1.1"},
			"request": {"env": {"REMOTE_ADDR": "127.0.0.1"}},
			"tags": {"client_ip": "192.168.1.10"}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	tags := event.Tags()
	if _, ok := tags["client_ip"]; ok {
		t.Fatalf("expected private client ip to be omitted: %#v", tags)
	}
}

func TestParseStoreEventAcceptsGoSDKExceptionArray(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"level": "error",
			"platform": "go",
			"exception": [{
				"type": "*errors.errorString",
				"value": "go sdk fixture error",
				"stacktrace": {
					"frames": [{
						"abs_path": "/tmp/main.go",
						"function": "main",
						"lineno": 20,
						"in_app": true
					}]
				}
			}]
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	if event.Title().String() != "*errors.errorString: go sdk fixture error" {
		t.Fatalf("unexpected title: %s", event.Title().String())
	}

	parts := event.DefaultGroupingParts()
	if parts[2] != "/tmp/main.go" {
		t.Fatalf("unexpected frame path: %s", parts[2])
	}
}

func TestParseStoreEventPopulatesJsStacktraceForJavaScriptPlatform(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"platform": "javascript",
			"level": "error",
			"exception": {
				"values": [{
					"type": "TypeError",
					"value": "bad operand",
					"stacktrace": {
						"frames": [
							{
								"abs_path": "https://cdn.example.com/app.min.js",
								"function": "r",
								"lineno": 1,
								"colno": 1024,
								"in_app": true
							},
							{
								"abs_path": "https://cdn.example.com/app.min.js",
								"function": "renderHome",
								"lineno": 1,
								"colno": 2048,
								"in_app": true
							}
						]
					}
				}]
			}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	frames := event.JsStacktrace()
	if len(frames) != 2 {
		t.Fatalf("expected 2 js stacktrace frames, got %d", len(frames))
	}

	if frames[0].AbsPath() != "https://cdn.example.com/app.min.js" {
		t.Fatalf("unexpected abs path: %s", frames[0].AbsPath())
	}

	if frames[0].Function() != "r" {
		t.Fatalf("unexpected function: %s", frames[0].Function())
	}

	if frames[0].GeneratedLine() != 1 || frames[0].GeneratedColumn() != 1024 {
		t.Fatalf("unexpected first frame position: line=%d column=%d",
			frames[0].GeneratedLine(), frames[0].GeneratedColumn())
	}

	if _, hasResolution := frames[0].Resolution(); hasResolution {
		t.Fatal("expected unresolved frame at ingest time")
	}

	if frames[1].GeneratedColumn() != 2048 {
		t.Fatalf("unexpected second frame column: %d", frames[1].GeneratedColumn())
	}
}

func TestParseStoreEventPopulatesJsStacktraceForNodePlatform(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"platform": "node",
			"level": "error",
			"exception": {
				"values": [{
					"type": "Error",
					"value": "boom",
					"stacktrace": {
						"frames": [
							{
								"filename": "app.js",
								"function": "main",
								"lineno": 12,
								"colno": 5
							}
						]
					}
				}]
			}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	frames := event.JsStacktrace()
	if len(frames) != 1 {
		t.Fatalf("expected 1 js stacktrace frame, got %d", len(frames))
	}

	if frames[0].AbsPath() != "app.js" {
		t.Fatalf("expected filename fallback for abs_path, got %s", frames[0].AbsPath())
	}
}

func TestParseStoreEventOmitsJsStacktraceForNonJsPlatform(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"platform": "python",
			"level": "error",
			"exception": {
				"values": [{
					"type": "TypeError",
					"value": "bad operand",
					"stacktrace": {
						"frames": [
							{"filename": "lib.py", "function": "helper", "lineno": 3, "in_app": true}
						]
					}
				}]
			}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	if frames := event.JsStacktrace(); len(frames) != 0 {
		t.Fatalf("expected no js stacktrace frames for non-js platform, got %d", len(frames))
	}
}

func TestParseStoreEventSkipsJsStacktraceFramesWithMissingPath(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"platform": "javascript",
			"level": "error",
			"exception": {
				"values": [{
					"type": "TypeError",
					"value": "bad operand",
					"stacktrace": {
						"frames": [
							{"function": "anonymous", "lineno": 1, "colno": 5},
							{"abs_path": "https://cdn.example.com/app.min.js", "lineno": 0, "colno": 5},
							{"abs_path": "https://cdn.example.com/app.min.js", "function": "ok", "lineno": 7, "colno": 9}
						]
					}
				}]
			}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	frames := event.JsStacktrace()
	if len(frames) != 1 {
		t.Fatalf("expected 1 valid js stacktrace frame, got %d", len(frames))
	}

	if frames[0].Function() != "ok" {
		t.Fatalf("unexpected surviving frame function: %s", frames[0].Function())
	}
}

func TestParseStoreEventOmitsJsStacktraceForTransactionEvent(t *testing.T) {
	eventResult := ParseStoreEvent(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"event_id": "550e8400e29b41d4a716446655440000",
			"timestamp": "2026-04-24T10:00:00Z",
			"platform": "javascript",
			"type": "transaction",
			"transaction": "GET /home",
			"exception": {
				"values": [{
					"type": "TypeError",
					"value": "bad operand",
					"stacktrace": {
						"frames": [
							{"abs_path": "https://cdn.example.com/app.min.js", "function": "r", "lineno": 1, "colno": 5}
						]
					}
				}]
			}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse store event: %v", eventErr)
	}

	if event.Kind() != domain.EventKindTransaction {
		t.Fatalf("expected transaction kind, got %s", event.Kind())
	}

	if frames := event.JsStacktrace(); len(frames) != 0 {
		t.Fatalf("expected transaction event to omit js stacktrace frames, got %d", len(frames))
	}
}

func projectContext(t *testing.T) ProjectContext {
	t.Helper()

	return projectContextWithScrubPolicy(t, true)
}

func projectContextWithScrubPolicy(t *testing.T, scrubIP bool) ProjectContext {
	t.Helper()

	organizationID := mustID(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustID(t, domain.NewProjectID, "2222222222224222a222222222222222")

	return NewProjectContextWithPrivacy(organizationID, projectID, scrubIP)
}

func mustID[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	id, err := constructor(input)
	if err != nil {
		t.Fatalf("id: %v", err)
	}

	return id
}

func timePoint(t *testing.T, value time.Time) domain.TimePoint {
	t.Helper()

	point, err := domain.NewTimePoint(value)
	if err != nil {
		t.Fatalf("time point: %v", err)
	}

	return point
}

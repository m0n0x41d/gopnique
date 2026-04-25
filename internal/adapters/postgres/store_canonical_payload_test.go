package postgres

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

func TestCanonicalPayloadJSONOmitsAttachmentsWhenAbsent(t *testing.T) {
	accepted := mustAcceptedEvent(t, nil)

	payload, err := canonicalPayloadJSON(accepted)
	if err != nil {
		t.Fatalf("canonical payload: %v", err)
	}

	decoded := decodeCanonicalPayload(t, payload)
	if _, present := decoded["attachments"]; present {
		t.Fatalf("expected no attachments key, got %v", decoded["attachments"])
	}

	if _, present := decoded["js_stacktrace"]; present {
		t.Fatalf("expected no js_stacktrace key, got %v", decoded["js_stacktrace"])
	}

	if _, present := decoded["native"]; present {
		t.Fatalf("expected no native key, got %v", decoded["native"])
	}
}

func TestCanonicalPayloadJSONIncludesJsStacktraceWhenPresent(t *testing.T) {
	unresolved, unresolvedErr := domain.NewUnresolvedJsStacktraceFrame(
		"https://cdn.example.com/app.min.js",
		"r",
		1,
		1024,
	)
	if unresolvedErr != nil {
		t.Fatalf("unresolved frame: %v", unresolvedErr)
	}

	resolved, resolvedErr := domain.NewResolvedJsStacktraceFrame(
		"https://cdn.example.com/app.min.js",
		"r",
		1,
		2048,
		"webpack:///./src/home.tsx",
		"renderHome",
		42,
		8,
	)
	if resolvedErr != nil {
		t.Fatalf("resolved frame: %v", resolvedErr)
	}

	accepted := mustAcceptedEventWith(t, domain.CanonicalEventParams{
		JsStacktrace: []domain.JsStacktraceFrame{unresolved, resolved},
	})

	payload, err := canonicalPayloadJSON(accepted)
	if err != nil {
		t.Fatalf("canonical payload: %v", err)
	}

	decoded := decodeCanonicalPayload(t, payload)
	rawFrames, present := decoded["js_stacktrace"]
	if !present {
		t.Fatalf("expected js_stacktrace key in payload")
	}

	frames, ok := rawFrames.([]any)
	if !ok {
		t.Fatalf("expected js_stacktrace array, got %T", rawFrames)
	}

	if len(frames) != 2 {
		t.Fatalf("expected two js frames, got %d", len(frames))
	}

	first, ok := frames[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first frame object, got %T", frames[0])
	}

	if first["abs_path"] != "https://cdn.example.com/app.min.js" {
		t.Fatalf("unexpected first abs path: %v", first["abs_path"])
	}

	firstGenerated, ok := first["generated"].(map[string]any)
	if !ok {
		t.Fatalf("expected first generated object, got %T", first["generated"])
	}

	if firstGenerated["line"].(float64) != 1 {
		t.Fatalf("unexpected first generated line: %v", firstGenerated["line"])
	}

	if firstGenerated["column"].(float64) != 1024 {
		t.Fatalf("unexpected first generated column: %v", firstGenerated["column"])
	}

	if _, hasResolved := first["resolved"]; hasResolved {
		t.Fatalf("expected unresolved frame to omit resolved key")
	}

	second, ok := frames[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second frame object, got %T", frames[1])
	}

	secondResolved, ok := second["resolved"].(map[string]any)
	if !ok {
		t.Fatalf("expected resolved object, got %T", second["resolved"])
	}

	if secondResolved["source"] != "webpack:///./src/home.tsx" {
		t.Fatalf("unexpected resolved source: %v", secondResolved["source"])
	}

	if secondResolved["symbol"] != "renderHome" {
		t.Fatalf("unexpected resolved symbol: %v", secondResolved["symbol"])
	}

	if secondResolved["line"].(float64) != 42 {
		t.Fatalf("unexpected resolved line: %v", secondResolved["line"])
	}

	if secondResolved["column"].(float64) != 8 {
		t.Fatalf("unexpected resolved column: %v", secondResolved["column"])
	}
}

func TestCanonicalPayloadJSONIncludesNativeReferencesWhenPresent(t *testing.T) {
	debugID, debugIDErr := domain.NewDebugIdentifier("0123456789abcdef0123456789abcdef")
	if debugIDErr != nil {
		t.Fatalf("debug id: %v", debugIDErr)
	}

	module, moduleErr := domain.NewNativeModule(debugID, "/usr/lib/libfoo.so", 0x10000000, 0x4000)
	if moduleErr != nil {
		t.Fatalf("native module: %v", moduleErr)
	}

	plainFrame, plainFrameErr := domain.NewNativeFrame(0x10000050, "", "")
	if plainFrameErr != nil {
		t.Fatalf("plain native frame: %v", plainFrameErr)
	}

	moduleFrame, moduleFrameErr := domain.NewNativeFrameWithModule(
		0x10001234,
		debugID,
		"render_home",
		"app",
	)
	if moduleFrameErr != nil {
		t.Fatalf("module native frame: %v", moduleFrameErr)
	}

	accepted := mustAcceptedEventWith(t, domain.CanonicalEventParams{
		NativeModules: []domain.NativeModule{module},
		NativeFrames:  []domain.NativeFrame{plainFrame, moduleFrame},
	})

	payload, err := canonicalPayloadJSON(accepted)
	if err != nil {
		t.Fatalf("canonical payload: %v", err)
	}

	decoded := decodeCanonicalPayload(t, payload)
	rawNative, present := decoded["native"]
	if !present {
		t.Fatalf("expected native key in payload")
	}

	native, ok := rawNative.(map[string]any)
	if !ok {
		t.Fatalf("expected native object, got %T", rawNative)
	}

	rawModules, ok := native["modules"].([]any)
	if !ok {
		t.Fatalf("expected modules array, got %T", native["modules"])
	}

	if len(rawModules) != 1 {
		t.Fatalf("expected one module, got %d", len(rawModules))
	}

	moduleEntry, ok := rawModules[0].(map[string]any)
	if !ok {
		t.Fatalf("expected module object, got %T", rawModules[0])
	}

	if moduleEntry["debug_id"] != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected module debug id: %v", moduleEntry["debug_id"])
	}

	if moduleEntry["code_file"] != "/usr/lib/libfoo.so" {
		t.Fatalf("unexpected module code file: %v", moduleEntry["code_file"])
	}

	if moduleEntry["image_addr"].(float64) != float64(0x10000000) {
		t.Fatalf("unexpected module image addr: %v", moduleEntry["image_addr"])
	}

	if moduleEntry["image_size"].(float64) != float64(0x4000) {
		t.Fatalf("unexpected module image size: %v", moduleEntry["image_size"])
	}

	rawFrames, ok := native["frames"].([]any)
	if !ok {
		t.Fatalf("expected frames array, got %T", native["frames"])
	}

	if len(rawFrames) != 2 {
		t.Fatalf("expected two frames, got %d", len(rawFrames))
	}

	plain, ok := rawFrames[0].(map[string]any)
	if !ok {
		t.Fatalf("expected plain frame object, got %T", rawFrames[0])
	}

	if plain["instruction_addr"].(float64) != float64(0x10000050) {
		t.Fatalf("unexpected plain instruction addr: %v", plain["instruction_addr"])
	}

	if _, hasModule := plain["module_debug_id"]; hasModule {
		t.Fatalf("expected plain frame to omit module_debug_id")
	}

	if _, hasFunction := plain["function"]; hasFunction {
		t.Fatalf("expected plain frame to omit function")
	}

	withModule, ok := rawFrames[1].(map[string]any)
	if !ok {
		t.Fatalf("expected module frame object, got %T", rawFrames[1])
	}

	if withModule["module_debug_id"] != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected module debug id ref: %v", withModule["module_debug_id"])
	}

	if withModule["function"] != "render_home" {
		t.Fatalf("unexpected module frame function: %v", withModule["function"])
	}

	if withModule["package"] != "app" {
		t.Fatalf("unexpected module frame package: %v", withModule["package"])
	}
}

func TestCanonicalPayloadJSONIncludesAttachmentsWhenPresent(t *testing.T) {
	minidumpName := mustArtifactName(t, "crash.dmp")
	mapName := mustArtifactName(t, "app.min.js.map")

	minidump, minidumpErr := domain.NewEventAttachment(
		domain.ArtifactKindMinidump(),
		minidumpName,
		2048,
		"application/octet-stream",
	)
	if minidumpErr != nil {
		t.Fatalf("minidump attachment: %v", minidumpErr)
	}

	sourceMap, sourceMapErr := domain.NewEventAttachment(
		domain.ArtifactKindSourceMap(),
		mapName,
		512,
		"",
	)
	if sourceMapErr != nil {
		t.Fatalf("source map attachment: %v", sourceMapErr)
	}

	accepted := mustAcceptedEvent(t, []domain.EventAttachment{minidump, sourceMap})

	payload, err := canonicalPayloadJSON(accepted)
	if err != nil {
		t.Fatalf("canonical payload: %v", err)
	}

	decoded := decodeCanonicalPayload(t, payload)
	rawAttachments, present := decoded["attachments"]
	if !present {
		t.Fatalf("expected attachments key in payload")
	}

	attachments, ok := rawAttachments.([]any)
	if !ok {
		t.Fatalf("expected attachments array, got %T", rawAttachments)
	}

	if len(attachments) != 2 {
		t.Fatalf("expected two attachments, got %d", len(attachments))
	}

	first, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first attachment object, got %T", attachments[0])
	}

	if first["kind"] != "minidump" {
		t.Fatalf("unexpected first kind: %v", first["kind"])
	}

	if first["name"] != "crash.dmp" {
		t.Fatalf("unexpected first name: %v", first["name"])
	}

	if first["byte_size"].(float64) != 2048 {
		t.Fatalf("unexpected first byte size: %v", first["byte_size"])
	}

	if first["content_type"] != "application/octet-stream" {
		t.Fatalf("unexpected first content type: %v", first["content_type"])
	}

	second, ok := attachments[1].(map[string]any)
	if !ok {
		t.Fatalf("expected second attachment object, got %T", attachments[1])
	}

	if second["kind"] != "source_map" {
		t.Fatalf("unexpected second kind: %v", second["kind"])
	}

	if second["name"] != "app.min.js.map" {
		t.Fatalf("unexpected second name: %v", second["name"])
	}

	if _, hasContentType := second["content_type"]; hasContentType {
		t.Fatalf("expected omitted content_type for second attachment, got %v", second["content_type"])
	}
}

func mustAcceptedEvent(t *testing.T, attachments []domain.EventAttachment) ingestplan.AcceptedEvent {
	t.Helper()

	return mustAcceptedEventWith(t, domain.CanonicalEventParams{
		Attachments: attachments,
	})
}

func mustAcceptedEventWith(t *testing.T, overrides domain.CanonicalEventParams) ingestplan.AcceptedEvent {
	t.Helper()

	organizationID, orgErr := domain.NewOrganizationID("aaaaaaaaaaaa4aaaaaaaaaaaaaaaaaaa")
	if orgErr != nil {
		t.Fatalf("organization id: %v", orgErr)
	}

	projectID, projectErr := domain.NewProjectID("bbbbbbbbbbbb4bbbbbbbbbbbbbbbbbbb")
	if projectErr != nil {
		t.Fatalf("project id: %v", projectErr)
	}

	eventID, eventIDErr := domain.NewEventID("cccccccccccc4ccccccccccccccccccc")
	if eventIDErr != nil {
		t.Fatalf("event id: %v", eventIDErr)
	}

	occurredAt, occurredErr := domain.NewTimePoint(time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC))
	if occurredErr != nil {
		t.Fatalf("occurred at: %v", occurredErr)
	}

	receivedAt, receivedErr := domain.NewTimePoint(time.Date(2026, 4, 25, 10, 0, 1, 0, time.UTC))
	if receivedErr != nil {
		t.Fatalf("received at: %v", receivedErr)
	}

	title, titleErr := domain.NewEventTitle("TypeError: bad operand")
	if titleErr != nil {
		t.Fatalf("event title: %v", titleErr)
	}

	params := domain.CanonicalEventParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		EventID:        eventID,
		OccurredAt:     occurredAt,
		ReceivedAt:     receivedAt,
		Kind:           domain.EventKindError,
		Level:          domain.EventLevelError,
		Title:          title,
		Platform:       "javascript",
		Attachments:    overrides.Attachments,
		JsStacktrace:   overrides.JsStacktrace,
		NativeModules:  overrides.NativeModules,
		NativeFrames:   overrides.NativeFrames,
	}

	canonical, eventErr := domain.NewCanonicalEvent(params)
	if eventErr != nil {
		t.Fatalf("canonical event: %v", eventErr)
	}

	fingerprint, fingerprintErr := domain.NewFingerprint(
		"sha256",
		strings.Repeat("a", 64),
	)
	if fingerprintErr != nil {
		t.Fatalf("fingerprint: %v", fingerprintErr)
	}

	acceptedResult := ingestplan.NewAcceptedEvent(canonical, fingerprint)
	accepted, acceptedErr := acceptedResult.Value()
	if acceptedErr != nil {
		t.Fatalf("accepted event: %v", acceptedErr)
	}

	return accepted
}

func mustArtifactName(t *testing.T, input string) domain.ArtifactName {
	t.Helper()

	name, err := domain.NewArtifactName(input)
	if err != nil {
		t.Fatalf("artifact name %q: %v", input, err)
	}

	return name
}

func decodeCanonicalPayload(t *testing.T, payload []byte) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}

	return decoded
}

package sentryprotocol

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestParseEnvelopeAcceptsEventItem(t *testing.T) {
	envelope := strings.Join([]string{
		`{"event_id":"550e8400e29b41d4a716446655440000"}`,
		`{"type":"event"}`,
		`{"event_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","timestamp":"2026-04-24T10:00:00Z","message":"hello"}`,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	parsed, parseErr := result.Value()
	if parseErr != nil {
		t.Fatalf("parse envelope: %v", parseErr)
	}

	event, ok := parsed.Event()
	if !ok {
		t.Fatal("expected event")
	}

	if parsed.ItemType() != "event" {
		t.Fatalf("unexpected item type: %s", parsed.ItemType())
	}

	if event.EventID().Hex() != "550e8400e29b41d4a716446655440000" {
		t.Fatalf("expected envelope event id override, got %s", event.EventID().Hex())
	}
}

func TestParseEnvelopeAcceptsLengthPrefixedTransactionItem(t *testing.T) {
	payload := `{"event_id":"550e8400e29b41d4a716446655440000","timestamp":"2026-04-24T10:00:00Z","type":"transaction","transaction":"GET /checkout"}`
	envelope := strings.Join([]string{
		`{}`,
		`{"type":"transaction","length":` + intString(len(payload)) + `}`,
		payload,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	parsed, parseErr := result.Value()
	if parseErr != nil {
		t.Fatalf("parse envelope: %v", parseErr)
	}

	event, ok := parsed.Event()
	if !ok {
		t.Fatal("expected transaction event")
	}

	if event.Kind() != domain.EventKindTransaction {
		t.Fatalf("unexpected kind: %s", event.Kind())
	}
}

func TestParseEnvelopeAcceptsSentryLogItem(t *testing.T) {
	payload := `{"items":[{"timestamp":"2026-04-24T10:00:00Z","level":"warning","body":"checkout failed","logger":"web","trace_id":"0123456789abcdef0123456789abcdef","span_id":"1111111111111111","release":"api@1.2.3","environment":"production","resource_attributes":{"service.name":{"value":"checkout","type":"string"}},"attributes":{"http.route":{"value":"/checkout","type":"string"}}}]}`
	envelope := strings.Join([]string{
		`{}`,
		`{"type":"log","length":` + intString(len(payload)) + `}`,
		payload,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	parsed, parseErr := result.Value()
	if parseErr != nil {
		t.Fatalf("parse envelope: %v", parseErr)
	}

	if parsed.HasEvent() {
		t.Fatal("log item must not create a canonical event")
	}

	logs := parsed.Logs()
	if len(logs) != 1 {
		t.Fatalf("expected one log, got %d", len(logs))
	}

	if logs[0].Severity() != domain.LogSeverityWarning {
		t.Fatalf("unexpected severity: %s", logs[0].Severity())
	}

	if logs[0].Body() != "checkout failed" || logs[0].Logger() != "web" {
		t.Fatalf("unexpected log fields: %#v", logs[0])
	}

	if logs[0].ResourceAttributes()["service.name"] != "checkout" {
		t.Fatalf("unexpected resource attributes: %#v", logs[0].ResourceAttributes())
	}
}

func TestParseEnvelopeAcceptsOTelLogItem(t *testing.T) {
	payload := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"checkout-api"}},{"key":"deployment.environment","value":{"stringValue":"production"}},{"key":"service.version","value":{"stringValue":"api@1.2.3"}}]},"scopeLogs":[{"scope":{"name":"checkout.logger"},"logRecords":[{"timeUnixNano":"1776256800000000000","severityText":"ERROR","body":{"stringValue":"otel checkout failed"},"traceId":"0123456789abcdef0123456789abcdef","spanId":"1111111111111111","attributes":[{"key":"http.route","value":{"stringValue":"/checkout"}}]}]}]}]}`
	envelope := strings.Join([]string{
		`{}`,
		`{"type":"otel_log","length":` + intString(len(payload)) + `}`,
		payload,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	parsed, parseErr := result.Value()
	if parseErr != nil {
		t.Fatalf("parse envelope: %v", parseErr)
	}

	logs := parsed.Logs()
	if len(logs) != 1 {
		t.Fatalf("expected one log, got %d", len(logs))
	}

	if logs[0].Severity() != domain.LogSeverityError {
		t.Fatalf("unexpected severity: %s", logs[0].Severity())
	}

	if logs[0].Body() != "otel checkout failed" || logs[0].Logger() != "checkout.logger" {
		t.Fatalf("unexpected otel log fields: %#v", logs[0])
	}

	if logs[0].Release() != "api@1.2.3" || logs[0].Environment() != "production" {
		t.Fatalf("unexpected otel dimensions: %#v", logs[0])
	}
}

func TestParseEnvelopeRejectsMixedEventAndLog(t *testing.T) {
	envelope := strings.Join([]string{
		`{}`,
		`{"type":"event"}`,
		`{"event_id":"550e8400e29b41d4a716446655440000","timestamp":"2026-04-24T10:00:00Z","message":"hello"}`,
		`{"type":"log"}`,
		`{"timestamp":"2026-04-24T10:00:00Z","body":"log"}`,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	_, parseErr := result.Value()
	if parseErr == nil {
		t.Fatal("expected mixed event and log to fail")
	}
}

func TestParseEnvelopeIgnoresUnsupportedItems(t *testing.T) {
	envelope := strings.Join([]string{
		`{}`,
		`{"type":"session"}`,
		`{"started":"2026-04-24T10:00:00Z"}`,
		`{"type":"client_report"}`,
		`{"discarded_events":[]}`,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	parsed, parseErr := result.Value()
	if parseErr != nil {
		t.Fatalf("parse envelope: %v", parseErr)
	}

	if parsed.HasEvent() {
		t.Fatal("did not expect canonical event")
	}

	if len(parsed.IgnoredItems()) != 2 {
		t.Fatalf("expected two ignored items, got %d", len(parsed.IgnoredItems()))
	}
}

func TestParseEnvelopeReadsLegacyUserReport(t *testing.T) {
	envelope := strings.Join([]string{
		`{"event_id":"550e8400e29b41d4a716446655440000"}`,
		`{"type":"user_report"}`,
		`{"event_id":"550e8400e29b41d4a716446655440000","name":"Jane","email":"jane@example.test","comments":"It broke"}`,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	parsed, parseErr := result.Value()
	if parseErr != nil {
		t.Fatalf("parse envelope: %v", parseErr)
	}

	reports := parsed.UserReports()
	if len(reports) != 1 {
		t.Fatalf("expected one report, got %d", len(reports))
	}

	if reports[0].EventID != "550e8400e29b41d4a716446655440000" ||
		reports[0].Name != "Jane" ||
		reports[0].Email != "jane@example.test" ||
		reports[0].Comments != "It broke" {
		t.Fatalf("unexpected report: %+v", reports[0])
	}
}

func TestParseEnvelopeReadsFeedbackAssociatedEvent(t *testing.T) {
	envelope := strings.Join([]string{
		`{"event_id":"650e8400e29b41d4a716446655440000"}`,
		`{"type":"feedback"}`,
		`{"event_id":"650e8400e29b41d4a716446655440000","contexts":{"feedback":{"associated_event_id":"550e8400e29b41d4a716446655440000","name":"John","contact_email":"john@example.test","message":"This error is annoying"}}}`,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	parsed, parseErr := result.Value()
	if parseErr != nil {
		t.Fatalf("parse envelope: %v", parseErr)
	}

	reports := parsed.UserReports()
	if len(reports) != 1 {
		t.Fatalf("expected one report, got %d", len(reports))
	}

	if reports[0].EventID != "550e8400e29b41d4a716446655440000" ||
		reports[0].Name != "John" ||
		reports[0].Email != "john@example.test" ||
		reports[0].Comments != "This error is annoying" {
		t.Fatalf("unexpected report: %+v", reports[0])
	}
}

func TestParseEnvelopeRejectsEventAndTransactionTogether(t *testing.T) {
	envelope := strings.Join([]string{
		`{}`,
		`{"type":"event"}`,
		`{"event_id":"550e8400e29b41d4a716446655440000","timestamp":"2026-04-24T10:00:00Z","message":"hello"}`,
		`{"type":"transaction"}`,
		`{"event_id":"650e8400e29b41d4a716446655440000","timestamp":"2026-04-24T10:00:00Z","type":"transaction","transaction":"GET /checkout"}`,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	_, parseErr := result.Value()
	if parseErr == nil {
		t.Fatal("expected mixed supported items to fail")
	}
}

func TestParseEnvelopeRejectsReservedItem(t *testing.T) {
	envelope := strings.Join([]string{
		`{}`,
		`{"type":"security"}`,
		`{}`,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	_, parseErr := result.Value()
	if parseErr == nil {
		t.Fatal("expected reserved item to fail")
	}
}

func TestEnvelopeHeaderDSNReadsDSN(t *testing.T) {
	envelope := strings.Join([]string{
		`{"dsn":"http://550e8400e29b41d4a716446655440000@example.test/42"}`,
		`{"type":"client_report"}`,
		`{}`,
	}, "\n")

	result := EnvelopeHeaderDSN([]byte(envelope))
	dsn, dsnErr := result.Value()
	if dsnErr != nil {
		t.Fatalf("dsn: %v", dsnErr)
	}

	if dsn != "http://550e8400e29b41d4a716446655440000@example.test/42" {
		t.Fatalf("unexpected dsn: %s", dsn)
	}
}

func TestParseEnvelopeRejectsTooManyItems(t *testing.T) {
	lines := []string{`{}`}
	for index := 0; index < MaxEnvelopeItems+1; index++ {
		lines = append(lines, `{"type":"client_report"}`)
		lines = append(lines, `{}`)
	}

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(strings.Join(lines, "\n")),
	)
	_, parseErr := result.Value()
	if parseErr == nil {
		t.Fatal("expected too many items to fail")
	}
}

func TestParseEnvelopeRejectsOversizedLengthItem(t *testing.T) {
	envelope := strings.Join([]string{
		`{}`,
		`{"type":"event","length":` + intString(MaxEnvelopeItemBytes+1) + `}`,
		`{}`,
	}, "\n")

	result := ParseEnvelope(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(envelope),
	)
	_, parseErr := result.Value()
	if parseErr == nil {
		t.Fatal("expected oversized item to fail")
	}
}

func intString(value int) string {
	return strconv.Itoa(value)
}

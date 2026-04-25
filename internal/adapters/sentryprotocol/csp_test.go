package sentryprotocol

import (
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestParseCSPReportAcceptsLegacyCarrier(t *testing.T) {
	eventResult := ParseCSPReport(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{
			"csp-report": {
				"document-uri": "https://app.example.test/dashboard",
				"violated-directive": "script-src",
				"effective-directive": "script-src-elem",
				"blocked-uri": "https://cdn.bad.test/app.js",
				"source-file": "https://app.example.test/dashboard",
				"disposition": "enforce"
			}
		}`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse csp report: %v", eventErr)
	}

	if event.Kind() != domain.EventKindDefault {
		t.Fatalf("unexpected kind: %s", event.Kind())
	}

	if event.Level() != domain.EventLevelWarning {
		t.Fatalf("unexpected level: %s", event.Level())
	}

	if event.Platform() != "security" {
		t.Fatalf("unexpected platform: %s", event.Platform())
	}

	if event.Title().String() != "CSP violation: script-src-elem blocked https://cdn.bad.test/app.js" {
		t.Fatalf("unexpected title: %s", event.Title().String())
	}

	tags := event.Tags()
	if tags["security.type"] != "csp" ||
		tags["csp.effective_directive"] != "script-src-elem" ||
		tags["csp.blocked_uri"] != "https://cdn.bad.test/app.js" {
		t.Fatalf("unexpected csp tags: %#v", tags)
	}
}

func TestParseCSPReportAcceptsReportingAPICarrier(t *testing.T) {
	eventResult := ParseCSPReport(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`[{
			"type": "csp-violation",
			"url": "https://app.example.test/dashboard",
			"body": {
				"documentURL": "https://app.example.test/dashboard",
				"effectiveDirective": "img-src",
				"blockedURL": "https://images.bad.test/a.png",
				"disposition": "report"
			}
		}]`),
	)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		t.Fatalf("parse reporting api csp report: %v", eventErr)
	}

	if event.Tags()["csp.effective_directive"] != "img-src" {
		t.Fatalf("unexpected tags: %#v", event.Tags())
	}
}

func TestParseCSPReportRejectsUnsupportedPayload(t *testing.T) {
	eventResult := ParseCSPReport(
		projectContext(t),
		timePoint(t, time.Date(2026, 4, 24, 10, 0, 1, 0, time.UTC)),
		[]byte(`{"hello":"world"}`),
	)
	_, eventErr := eventResult.Value()
	if eventErr == nil {
		t.Fatal("expected unsupported csp payload to fail")
	}
}

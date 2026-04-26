package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewLogRecordNormalizesSeverityAndAttributes(t *testing.T) {
	organizationID := mustDomainValue(t, NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustDomainValue(t, NewProjectID, "2222222222224222a222222222222222")
	timestamp := mustDomainTime(t, time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC))
	severity := mustDomainValue(t, NewLogSeverity, "warn")

	record, recordErr := NewLogRecord(LogRecordParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Timestamp:      timestamp,
		ReceivedAt:     timestamp,
		Severity:       severity,
		Body:           " checkout failed ",
		Logger:         " web ",
		TraceID:        "01234567-89ab-cdef-0123-456789abcdef",
		SpanID:         "1111111111111111",
		Release:        "api@1.0.0",
		Environment:    "production",
		ResourceAttributes: map[string]string{
			"service.name": "checkout",
		},
		Attributes: map[string]string{
			"http.route": "/checkout",
		},
	})
	if recordErr != nil {
		t.Fatalf("log record: %v", recordErr)
	}

	if record.Severity() != LogSeverityWarning {
		t.Fatalf("unexpected severity: %s", record.Severity())
	}

	if record.Body() != "checkout failed" {
		t.Fatalf("unexpected body: %q", record.Body())
	}

	if record.Logger() != "web" {
		t.Fatalf("unexpected logger: %q", record.Logger())
	}

	if record.TraceID() != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected trace id: %s", record.TraceID())
	}

	if record.ResourceAttributes()["service.name"] != "checkout" {
		t.Fatalf("unexpected resource attributes: %#v", record.ResourceAttributes())
	}
}

func TestNewLogRecordRejectsInvalidStates(t *testing.T) {
	organizationID := mustDomainValue(t, NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustDomainValue(t, NewProjectID, "2222222222224222a222222222222222")
	timestamp := mustDomainTime(t, time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC))
	severity := mustDomainValue(t, NewLogSeverity, "info")

	cases := []struct {
		name   string
		params LogRecordParams
	}{
		{
			name: "missing body",
			params: LogRecordParams{
				OrganizationID: organizationID,
				ProjectID:      projectID,
				Timestamp:      timestamp,
				ReceivedAt:     timestamp,
				Severity:       severity,
			},
		},
		{
			name: "invalid trace",
			params: LogRecordParams{
				OrganizationID: organizationID,
				ProjectID:      projectID,
				Timestamp:      timestamp,
				ReceivedAt:     timestamp,
				Severity:       severity,
				Body:           "hello",
				TraceID:        "not-a-trace",
			},
		},
		{
			name: "too many attributes",
			params: LogRecordParams{
				OrganizationID: organizationID,
				ProjectID:      projectID,
				Timestamp:      timestamp,
				ReceivedAt:     timestamp,
				Severity:       severity,
				Body:           "hello",
				Attributes:     tooManyLogAttributes(),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, recordErr := NewLogRecord(tc.params)
			if recordErr == nil {
				t.Fatal("expected invalid log record to fail")
			}
		})
	}
}

func tooManyLogAttributes() map[string]string {
	values := map[string]string{}
	for index := 0; index < logAttributeCountMax+1; index++ {
		key := strings.Repeat("k", 1)
		key = key + string(rune('a'+index))
		values[key] = "value"
	}

	return values
}

func mustDomainTime(t *testing.T, value time.Time) TimePoint {
	t.Helper()

	point, pointErr := NewTimePoint(value)
	if pointErr != nil {
		t.Fatalf("time point: %v", pointErr)
	}

	return point
}

func mustDomainValue[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	value, valueErr := constructor(input)
	if valueErr != nil {
		t.Fatalf("domain value: %v", valueErr)
	}

	return value
}

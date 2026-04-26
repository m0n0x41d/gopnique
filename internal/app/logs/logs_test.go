package logs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func TestIngestAppendsLogRecordsWithoutIssueSideEffects(t *testing.T) {
	ctx := context.Background()
	ports := &fakeLogPorts{}
	record := testLogRecord(t, "hello")

	receiptResult := Ingest(ctx, ports, NewIngestCommand([]domain.LogRecord{record}))
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest logs: %v", receiptErr)
	}

	if receipt.Kind() != ReceiptAcceptedLogRecords {
		t.Fatalf("unexpected receipt: %s", receipt.Kind())
	}

	if receipt.Count() != 1 {
		t.Fatalf("unexpected count: %d", receipt.Count())
	}

	if ports.appendedCount != 1 {
		t.Fatalf("expected one append, got %d", ports.appendedCount)
	}
}

func TestIngestRejectsQuotaBeforeAppend(t *testing.T) {
	ctx := context.Background()
	ports := &fakeLogPorts{quota: NewQuotaRejected("project_quota_exceeded")}
	record := testLogRecord(t, "quota")

	receiptResult := Ingest(ctx, ports, NewIngestCommand([]domain.LogRecord{record}))
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		t.Fatalf("ingest logs: %v", receiptErr)
	}

	if receipt.Kind() != ReceiptQuotaRejected {
		t.Fatalf("unexpected receipt: %s", receipt.Kind())
	}

	if ports.appendedCount != 0 {
		t.Fatalf("quota rejection must not append, got %d", ports.appendedCount)
	}
}

func TestNormalizeQueryBoundsFilters(t *testing.T) {
	scope := Scope{
		OrganizationID: mustLogDomainValue(t, domain.NewOrganizationID, "1111111111114111a111111111111111"),
		ProjectID:      mustLogDomainValue(t, domain.NewProjectID, "2222222222224222a222222222222222"),
	}

	query, queryErr := NormalizeQuery(Query{
		Scope:       scope,
		Limit:       1000,
		Severity:    "warn",
		Logger:      " app ",
		Environment: " production ",
		ResourceAttribute: AttributeFilter{
			Key:   " service.name ",
			Value: " checkout ",
		},
	})
	if queryErr != nil {
		t.Fatalf("normalize query: %v", queryErr)
	}

	if query.Limit != maxLimit {
		t.Fatalf("unexpected limit: %d", query.Limit)
	}

	if query.Severity != "warning" {
		t.Fatalf("unexpected severity: %s", query.Severity)
	}

	if query.Logger != "app" || query.Environment != "production" {
		t.Fatalf("unexpected query fields: %#v", query)
	}

	if query.ResourceAttribute.Key != "service.name" || query.ResourceAttribute.Value != "checkout" {
		t.Fatalf("unexpected resource filter: %#v", query.ResourceAttribute)
	}
}

type fakeLogPorts struct {
	quota         QuotaDecision
	appendedCount int
}

func (ports *fakeLogPorts) RunLogIngest(
	ctx context.Context,
	program IngestProgram,
) result.Result[IngestTransactionResult] {
	return program(ctx, ports)
}

func (ports *fakeLogPorts) CheckLogQuota(
	ctx context.Context,
	record domain.LogRecord,
	count int,
) result.Result[QuotaDecision] {
	if ports.quota.Reason() != "" {
		return result.Ok(ports.quota)
	}

	return result.Ok(NewQuotaAllowed())
}

func (ports *fakeLogPorts) AppendLogRecords(
	ctx context.Context,
	records []domain.LogRecord,
) result.Result[AppendResult] {
	if len(records) == 0 {
		return result.Err[AppendResult](errors.New("missing logs"))
	}

	ports.appendedCount = len(records)
	return result.Ok(NewAppendResult(len(records)))
}

func testLogRecord(t *testing.T, body string) domain.LogRecord {
	t.Helper()

	organizationID := mustLogDomainValue(t, domain.NewOrganizationID, "1111111111114111a111111111111111")
	projectID := mustLogDomainValue(t, domain.NewProjectID, "2222222222224222a222222222222222")
	timestamp := mustLogTimePoint(t, time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC))
	severity := mustLogDomainValue(t, domain.NewLogSeverity, "info")
	record, recordErr := domain.NewLogRecord(domain.LogRecordParams{
		OrganizationID: organizationID,
		ProjectID:      projectID,
		Timestamp:      timestamp,
		ReceivedAt:     timestamp,
		Severity:       severity,
		Body:           body,
	})
	if recordErr != nil {
		t.Fatalf("log record: %v", recordErr)
	}

	return record
}

func mustLogTimePoint(t *testing.T, value time.Time) domain.TimePoint {
	t.Helper()

	point, pointErr := domain.NewTimePoint(value)
	if pointErr != nil {
		t.Fatalf("time point: %v", pointErr)
	}

	return point
}

func mustLogDomainValue[T any](t *testing.T, constructor func(string) (T, error), input string) T {
	t.Helper()

	value, valueErr := constructor(input)
	if valueErr != nil {
		t.Fatalf("domain value: %v", valueErr)
	}

	return value
}

package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewTransactionDataCarriesTypedTraceAndSpans(t *testing.T) {
	trace, traceErr := NewTransactionTraceContext(
		"0123456789abcdef0123456789abcdef",
		"1111111111111111",
		"2222222222222222",
	)
	if traceErr != nil {
		t.Fatalf("trace: %v", traceErr)
	}

	span, spanErr := NewTransactionSpan(
		"3333333333333333",
		"1111111111111111",
		"db",
		"select checkout",
		25*time.Millisecond,
		"ok",
	)
	if spanErr != nil {
		t.Fatalf("span: %v", spanErr)
	}

	transaction, transactionErr := NewTransactionDataWithTrace(
		"GET /checkout",
		"http.server",
		1500*time.Millisecond,
		"ok",
		trace,
		[]TransactionSpan{span},
	)
	if transactionErr != nil {
		t.Fatalf("transaction: %v", transactionErr)
	}

	if transaction.Name() != "GET /checkout" {
		t.Fatalf("unexpected name: %s", transaction.Name())
	}

	if transaction.Operation() != "http.server" {
		t.Fatalf("unexpected operation: %s", transaction.Operation())
	}

	if transaction.DurationMilliseconds() != 1500 {
		t.Fatalf("unexpected duration: %f", transaction.DurationMilliseconds())
	}

	if transaction.Status() != "ok" {
		t.Fatalf("unexpected status: %s", transaction.Status())
	}

	carriedTrace, ok := transaction.Trace()
	if !ok {
		t.Fatal("expected trace context")
	}

	if carriedTrace.TraceID() != "0123456789abcdef0123456789abcdef" ||
		carriedTrace.SpanID() != "1111111111111111" ||
		carriedTrace.ParentSpanID() != "2222222222222222" {
		t.Fatalf("unexpected trace: %#v", carriedTrace)
	}

	spans := transaction.Spans()
	if len(spans) != 1 {
		t.Fatalf("expected one span, got %d", len(spans))
	}

	spans[0] = TransactionSpan{}
	if transaction.Spans()[0].SpanID() != "3333333333333333" {
		t.Fatal("expected spans to be copied")
	}
}

func TestTransactionDataRejectsInvalidStates(t *testing.T) {
	tests := []struct {
		name     string
		build    func() error
		contains string
	}{
		{
			name: "empty name",
			build: func() error {
				_, err := NewTransactionData("", "http", 0, "ok", nil)
				return err
			},
			contains: "transaction name is required",
		},
		{
			name: "negative duration",
			build: func() error {
				_, err := NewTransactionData("GET /", "http", -time.Millisecond, "ok", nil)
				return err
			},
			contains: "duration",
		},
		{
			name: "invalid trace id",
			build: func() error {
				_, err := NewTransactionTraceContext("bad", "1111111111111111", "")
				return err
			},
			contains: "trace id",
		},
		{
			name: "invalid span id",
			build: func() error {
				_, err := NewTransactionSpan("bad", "", "db", "", 0, "ok")
				return err
			},
			contains: "span id",
		},
		{
			name: "oversized operation",
			build: func() error {
				_, err := NewTransactionData("GET /", strings.Repeat("a", transactionOperationMaxBytes+1), 0, "ok", nil)
				return err
			},
			contains: "operation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.build()
			if err == nil {
				t.Fatal("expected error")
			}

			if !strings.Contains(err.Error(), tc.contains) {
				t.Fatalf("expected %q in %q", tc.contains, err.Error())
			}
		})
	}
}

func TestCanonicalEventRequiresTransactionKindForTransactionData(t *testing.T) {
	transaction, transactionErr := NewTransactionData("GET /checkout", "http", time.Second, "ok", nil)
	if transactionErr != nil {
		t.Fatalf("transaction: %v", transactionErr)
	}

	_, eventErr := NewCanonicalEvent(CanonicalEventParams{
		OrganizationID: mustOrganizationID(t),
		ProjectID:      mustProjectID(t),
		EventID:        mustEventID(t),
		OccurredAt:     mustTimePoint(t),
		ReceivedAt:     mustTimePoint(t),
		Kind:           EventKindError,
		Level:          EventLevelError,
		Title:          mustTitle(t, "error"),
		Transaction:    transaction,
	})
	if eventErr == nil {
		t.Fatal("expected transaction data on error event to fail")
	}
}

func TestCanonicalTransactionEventCarriesDefaultTransactionData(t *testing.T) {
	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:  EventKindTransaction,
		Level: EventLevelInfo,
		Title: mustTitle(t, "GET /checkout"),
	})

	transaction, ok := event.Transaction()
	if !ok {
		t.Fatal("expected transaction data")
	}

	if transaction.Name() != "GET /checkout" ||
		transaction.Operation() != "default" ||
		transaction.Status() != "unknown" {
		t.Fatalf("unexpected default transaction: %#v", transaction)
	}
}

func mustOrganizationID(t *testing.T) OrganizationID {
	t.Helper()

	id, err := NewOrganizationID("1111111111114111a111111111111111")
	if err != nil {
		t.Fatalf("organization id: %v", err)
	}

	return id
}

func mustProjectID(t *testing.T) ProjectID {
	t.Helper()

	id, err := NewProjectID("2222222222224222a222222222222222")
	if err != nil {
		t.Fatalf("project id: %v", err)
	}

	return id
}

func mustEventID(t *testing.T) EventID {
	t.Helper()

	id, err := NewEventID("550e8400e29b41d4a716446655440000")
	if err != nil {
		t.Fatalf("event id: %v", err)
	}

	return id
}

func mustTimePoint(t *testing.T) TimePoint {
	t.Helper()

	point, err := NewTimePoint(time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("time point: %v", err)
	}

	return point
}

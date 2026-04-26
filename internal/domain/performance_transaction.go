package domain

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	transactionNameMaxBytes        = 512
	transactionOperationMaxBytes   = 128
	transactionStatusMaxBytes      = 64
	transactionDescriptionMaxBytes = 1024
)

var (
	traceIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
	spanIDPattern  = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

type TransactionData struct {
	name      string
	operation string
	duration  time.Duration
	status    string
	trace     TransactionTraceContext
	hasTrace  bool
	spans     []TransactionSpan
}

type TransactionTraceContext struct {
	traceID      string
	spanID       string
	parentSpanID string
}

type TransactionSpan struct {
	spanID       string
	parentSpanID string
	operation    string
	description  string
	duration     time.Duration
	status       string
}

func NewTransactionData(
	name string,
	operation string,
	duration time.Duration,
	status string,
	spans []TransactionSpan,
) (TransactionData, error) {
	return newTransactionData(name, operation, duration, status, TransactionTraceContext{}, false, spans)
}

func NewTransactionDataWithTrace(
	name string,
	operation string,
	duration time.Duration,
	status string,
	trace TransactionTraceContext,
	spans []TransactionSpan,
) (TransactionData, error) {
	if trace.traceID == "" {
		return TransactionData{}, errors.New("transaction trace context is required")
	}

	return newTransactionData(name, operation, duration, status, trace, true, spans)
}

func NewTransactionTraceContext(
	traceID string,
	spanID string,
	parentSpanID string,
) (TransactionTraceContext, error) {
	normalizedTraceID, traceErr := normalizeTraceID(traceID)
	if traceErr != nil {
		return TransactionTraceContext{}, traceErr
	}

	normalizedSpanID, spanErr := normalizeSpanID(spanID)
	if spanErr != nil {
		return TransactionTraceContext{}, spanErr
	}

	normalizedParentSpanID, parentErr := normalizeOptionalSpanID(parentSpanID)
	if parentErr != nil {
		return TransactionTraceContext{}, parentErr
	}

	return TransactionTraceContext{
		traceID:      normalizedTraceID,
		spanID:       normalizedSpanID,
		parentSpanID: normalizedParentSpanID,
	}, nil
}

func NewTransactionSpan(
	spanID string,
	parentSpanID string,
	operation string,
	description string,
	duration time.Duration,
	status string,
) (TransactionSpan, error) {
	normalizedSpanID, spanErr := normalizeSpanID(spanID)
	if spanErr != nil {
		return TransactionSpan{}, spanErr
	}

	normalizedParentSpanID, parentErr := normalizeOptionalSpanID(parentSpanID)
	if parentErr != nil {
		return TransactionSpan{}, parentErr
	}

	normalizedOperation, operationErr := normalizeOptionalPerformanceText(
		operation,
		"default",
		transactionOperationMaxBytes,
		"transaction span operation",
	)
	if operationErr != nil {
		return TransactionSpan{}, operationErr
	}

	normalizedDescription, descriptionErr := normalizeOptionalPerformanceText(
		description,
		"",
		transactionDescriptionMaxBytes,
		"transaction span description",
	)
	if descriptionErr != nil {
		return TransactionSpan{}, descriptionErr
	}

	normalizedStatus, statusErr := normalizeOptionalPerformanceText(
		status,
		"unknown",
		transactionStatusMaxBytes,
		"transaction span status",
	)
	if statusErr != nil {
		return TransactionSpan{}, statusErr
	}

	if duration < 0 {
		return TransactionSpan{}, errors.New("transaction span duration must not be negative")
	}

	return TransactionSpan{
		spanID:       normalizedSpanID,
		parentSpanID: normalizedParentSpanID,
		operation:    normalizedOperation,
		description:  normalizedDescription,
		duration:     duration,
		status:       normalizedStatus,
	}, nil
}

func newTransactionData(
	name string,
	operation string,
	duration time.Duration,
	status string,
	trace TransactionTraceContext,
	hasTrace bool,
	spans []TransactionSpan,
) (TransactionData, error) {
	normalizedName, nameErr := normalizeRequiredPerformanceText(
		name,
		transactionNameMaxBytes,
		"transaction name",
	)
	if nameErr != nil {
		return TransactionData{}, nameErr
	}

	normalizedOperation, operationErr := normalizeOptionalPerformanceText(
		operation,
		"default",
		transactionOperationMaxBytes,
		"transaction operation",
	)
	if operationErr != nil {
		return TransactionData{}, operationErr
	}

	normalizedStatus, statusErr := normalizeOptionalPerformanceText(
		status,
		"unknown",
		transactionStatusMaxBytes,
		"transaction status",
	)
	if statusErr != nil {
		return TransactionData{}, statusErr
	}

	if duration < 0 {
		return TransactionData{}, errors.New("transaction duration must not be negative")
	}

	return TransactionData{
		name:      normalizedName,
		operation: normalizedOperation,
		duration:  duration,
		status:    normalizedStatus,
		trace:     trace,
		hasTrace:  hasTrace,
		spans:     copyTransactionSpans(spans),
	}, nil
}

func normalizeTraceID(input string) (string, error) {
	value := strings.TrimSpace(input)
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "-", "")

	if !traceIDPattern.MatchString(value) {
		return "", errors.New("transaction trace id must be 32 hex characters")
	}

	return value, nil
}

func normalizeSpanID(input string) (string, error) {
	value := strings.TrimSpace(input)
	value = strings.ToLower(value)

	if !spanIDPattern.MatchString(value) {
		return "", errors.New("transaction span id must be 16 hex characters")
	}

	return value, nil
}

func normalizeOptionalSpanID(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	return normalizeSpanID(value)
}

func normalizeRequiredPerformanceText(input string, limit int, label string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New(label + " is required")
	}

	return normalizePerformanceText(value, limit, label)
}

func normalizeOptionalPerformanceText(input string, fallback string, limit int, label string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		value = fallback
	}

	if value == "" {
		return "", nil
	}

	return normalizePerformanceText(value, limit, label)
}

func normalizePerformanceText(input string, limit int, label string) (string, error) {
	if !utf8.ValidString(input) {
		return "", errors.New(label + " must be valid utf-8")
	}

	if len(input) > limit {
		return "", errors.New(label + " is too long")
	}

	for _, char := range input {
		if unicode.IsControl(char) {
			return "", errors.New(label + " must not contain control characters")
		}
	}

	return input, nil
}

func (transaction TransactionData) Name() string {
	return transaction.name
}

func (transaction TransactionData) Operation() string {
	return transaction.operation
}

func (transaction TransactionData) Duration() time.Duration {
	return transaction.duration
}

func (transaction TransactionData) DurationMilliseconds() float64 {
	return float64(transaction.duration) / float64(time.Millisecond)
}

func (transaction TransactionData) Status() string {
	return transaction.status
}

func (transaction TransactionData) Trace() (TransactionTraceContext, bool) {
	return transaction.trace, transaction.hasTrace
}

func (transaction TransactionData) Spans() []TransactionSpan {
	return copyTransactionSpans(transaction.spans)
}

func (transaction TransactionData) SpanCount() int {
	return len(transaction.spans)
}

func (trace TransactionTraceContext) TraceID() string {
	return trace.traceID
}

func (trace TransactionTraceContext) SpanID() string {
	return trace.spanID
}

func (trace TransactionTraceContext) ParentSpanID() string {
	return trace.parentSpanID
}

func (span TransactionSpan) SpanID() string {
	return span.spanID
}

func (span TransactionSpan) ParentSpanID() string {
	return span.parentSpanID
}

func (span TransactionSpan) Operation() string {
	return span.operation
}

func (span TransactionSpan) Description() string {
	return span.description
}

func (span TransactionSpan) Duration() time.Duration {
	return span.duration
}

func (span TransactionSpan) DurationMilliseconds() float64 {
	return float64(span.duration) / float64(time.Millisecond)
}

func (span TransactionSpan) Status() string {
	return span.status
}

func copyTransactionData(transaction TransactionData) TransactionData {
	return TransactionData{
		name:      transaction.name,
		operation: transaction.operation,
		duration:  transaction.duration,
		status:    transaction.status,
		trace:     transaction.trace,
		hasTrace:  transaction.hasTrace,
		spans:     copyTransactionSpans(transaction.spans),
	}
}

func copyTransactionSpans(spans []TransactionSpan) []TransactionSpan {
	if len(spans) == 0 {
		return nil
	}

	copied := make([]TransactionSpan, len(spans))
	copy(copied, spans)

	return copied
}

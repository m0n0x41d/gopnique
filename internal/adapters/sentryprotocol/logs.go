package sentryprotocol

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const MaxLogRecordsPerItem = 100

type rawSentryLog struct {
	Timestamp          json.RawMessage            `json:"timestamp"`
	ObservedTimestamp  json.RawMessage            `json:"observed_timestamp"`
	Level              string                     `json:"level"`
	Severity           string                     `json:"severity"`
	SeverityText       string                     `json:"severity_text"`
	Body               json.RawMessage            `json:"body"`
	Message            json.RawMessage            `json:"message"`
	Logger             string                     `json:"logger"`
	TraceID            string                     `json:"trace_id"`
	SpanID             string                     `json:"span_id"`
	Release            string                     `json:"release"`
	Environment        string                     `json:"environment"`
	Attributes         map[string]json.RawMessage `json:"attributes"`
	Resource           map[string]json.RawMessage `json:"resource"`
	ResourceAttributes map[string]json.RawMessage `json:"resource_attributes"`
	Contexts           rawContexts                `json:"contexts"`
}

type rawSentryLogBatch struct {
	Items []rawSentryLog `json:"items"`
	Logs  []rawSentryLog `json:"logs"`
}

type rawOTLPLogs struct {
	ResourceLogs []rawOTLPResourceLogs `json:"resourceLogs"`
}

type rawOTLPResourceLogs struct {
	Resource  rawOTLPResource    `json:"resource"`
	ScopeLogs []rawOTLPScopeLogs `json:"scopeLogs"`
}

type rawOTLPResource struct {
	Attributes []rawOTLPAttribute `json:"attributes"`
}

type rawOTLPScopeLogs struct {
	Scope      rawOTLPScope       `json:"scope"`
	LogRecords []rawOTLPLogRecord `json:"logRecords"`
}

type rawOTLPScope struct {
	Name string `json:"name"`
}

type rawOTLPLogRecord struct {
	TimeUnixNano         json.RawMessage    `json:"timeUnixNano"`
	ObservedTimeUnixNano json.RawMessage    `json:"observedTimeUnixNano"`
	SeverityText         string             `json:"severityText"`
	SeverityNumber       int                `json:"severityNumber"`
	Body                 json.RawMessage    `json:"body"`
	Attributes           []rawOTLPAttribute `json:"attributes"`
	TraceID              string             `json:"traceId"`
	SpanID               string             `json:"spanId"`
}

type rawOTLPAttribute struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

func parseSentryLogItem(
	project ProjectContext,
	receivedAt domain.TimePoint,
	payload []byte,
) result.Result[[]domain.LogRecord] {
	rawLogsResult := parseSentryLogPayload(payload)
	rawLogs, rawLogsErr := rawLogsResult.Value()
	if rawLogsErr != nil {
		return result.Err[[]domain.LogRecord](rawLogsErr)
	}

	if len(rawLogs) > MaxLogRecordsPerItem {
		return result.Err[[]domain.LogRecord](NewProtocolError(ErrorInvalidEnvelope, "too many log records"))
	}

	records := []domain.LogRecord{}
	for _, raw := range rawLogs {
		recordResult := sentryLogRecord(project, receivedAt, raw)
		record, recordErr := recordResult.Value()
		if recordErr != nil {
			return result.Err[[]domain.LogRecord](recordErr)
		}

		records = append(records, record)
	}

	return result.Ok(records)
}

func parseOTLPLogItem(
	project ProjectContext,
	receivedAt domain.TimePoint,
	payload []byte,
) result.Result[[]domain.LogRecord] {
	var raw rawOTLPLogs
	decodeErr := json.Unmarshal(payload, &raw)
	if decodeErr != nil {
		return result.Err[[]domain.LogRecord](NewProtocolError(ErrorInvalidEnvelope, "invalid otel log json"))
	}

	records := []domain.LogRecord{}
	for _, resourceLogs := range raw.ResourceLogs {
		resourceAttributes := otlpAttributes(resourceLogs.Resource.Attributes)
		for _, scopeLogs := range resourceLogs.ScopeLogs {
			for _, rawRecord := range scopeLogs.LogRecords {
				recordResult := otlpLogRecord(project, receivedAt, resourceAttributes, scopeLogs.Scope, rawRecord)
				record, recordErr := recordResult.Value()
				if recordErr != nil {
					return result.Err[[]domain.LogRecord](recordErr)
				}

				records = append(records, record)
				if len(records) > MaxLogRecordsPerItem {
					return result.Err[[]domain.LogRecord](NewProtocolError(ErrorInvalidEnvelope, "too many log records"))
				}
			}
		}
	}

	if len(records) == 0 {
		return result.Err[[]domain.LogRecord](NewProtocolError(ErrorInvalidEnvelope, "otel log records are required"))
	}

	return result.Ok(records)
}

func parseSentryLogPayload(payload []byte) result.Result[[]rawSentryLog] {
	var batch rawSentryLogBatch
	batchErr := json.Unmarshal(payload, &batch)
	if batchErr == nil {
		if len(batch.Items) > 0 {
			return result.Ok(batch.Items)
		}

		if len(batch.Logs) > 0 {
			return result.Ok(batch.Logs)
		}
	}

	var list []rawSentryLog
	listErr := json.Unmarshal(payload, &list)
	if listErr == nil && len(list) > 0 {
		return result.Ok(list)
	}

	var single rawSentryLog
	singleErr := json.Unmarshal(payload, &single)
	if singleErr != nil {
		return result.Err[[]rawSentryLog](NewProtocolError(ErrorInvalidEnvelope, "invalid log json"))
	}

	return result.Ok([]rawSentryLog{single})
}

func sentryLogRecord(
	project ProjectContext,
	receivedAt domain.TimePoint,
	raw rawSentryLog,
) result.Result[domain.LogRecord] {
	attributes := jsonObjectToAttributes(raw.Attributes)
	resourceAttributes := mergeAttributes(
		jsonObjectToAttributes(raw.Resource),
		jsonObjectToAttributes(raw.ResourceAttributes),
	)

	timestampResult := logTimestamp(raw.Timestamp, raw.ObservedTimestamp, receivedAt)
	timestamp, timestampErr := timestampResult.Value()
	if timestampErr != nil {
		return result.Err[domain.LogRecord](timestampErr)
	}

	severityResult := logSeverity(firstNonEmptyString(raw.Level, raw.Severity, raw.SeverityText, attributes["level"]))
	severity, severityErr := severityResult.Value()
	if severityErr != nil {
		return result.Err[domain.LogRecord](severityErr)
	}

	bodyResult := sentryLogBody(raw, attributes)
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		return result.Err[domain.LogRecord](bodyErr)
	}

	record, recordErr := domain.NewLogRecord(domain.LogRecordParams{
		OrganizationID:     project.OrganizationID(),
		ProjectID:          project.ProjectID(),
		Timestamp:          timestamp,
		ReceivedAt:         receivedAt,
		Severity:           severity,
		Body:               body,
		Logger:             firstNonEmptyString(raw.Logger, attributes["logger"], attributes["logger.name"]),
		TraceID:            firstNonEmptyString(raw.TraceID, raw.Contexts.Trace.TraceID, attributes["trace_id"]),
		SpanID:             firstNonEmptyString(raw.SpanID, raw.Contexts.Trace.SpanID, attributes["span_id"]),
		Release:            firstNonEmptyString(raw.Release, attributes["sentry.release"], attributes["release"]),
		Environment:        firstNonEmptyString(raw.Environment, attributes["sentry.environment"], attributes["environment"]),
		ResourceAttributes: resourceAttributes,
		Attributes:         attributes,
	})
	if recordErr != nil {
		return result.Err[domain.LogRecord](recordErr)
	}

	return result.Ok(record)
}

func otlpLogRecord(
	project ProjectContext,
	receivedAt domain.TimePoint,
	resourceAttributes map[string]string,
	scope rawOTLPScope,
	raw rawOTLPLogRecord,
) result.Result[domain.LogRecord] {
	attributes := otlpAttributes(raw.Attributes)
	timestampResult := otlpLogTimestamp(raw, receivedAt)
	timestamp, timestampErr := timestampResult.Value()
	if timestampErr != nil {
		return result.Err[domain.LogRecord](timestampErr)
	}

	severityResult := otlpLogSeverity(raw)
	severity, severityErr := severityResult.Value()
	if severityErr != nil {
		return result.Err[domain.LogRecord](severityErr)
	}

	body := anyValueString(raw.Body)
	record, recordErr := domain.NewLogRecord(domain.LogRecordParams{
		OrganizationID: project.OrganizationID(),
		ProjectID:      project.ProjectID(),
		Timestamp:      timestamp,
		ReceivedAt:     receivedAt,
		Severity:       severity,
		Body:           body,
		Logger:         firstNonEmptyString(scope.Name, attributes["logger.name"], resourceAttributes["service.name"]),
		TraceID:        raw.TraceID,
		SpanID:         raw.SpanID,
		Release: firstNonEmptyString(
			attributes["sentry.release"],
			resourceAttributes["service.version"],
		),
		Environment: firstNonEmptyString(
			attributes["sentry.environment"],
			resourceAttributes["deployment.environment"],
		),
		ResourceAttributes: resourceAttributes,
		Attributes:         attributes,
	})
	if recordErr != nil {
		return result.Err[domain.LogRecord](recordErr)
	}

	return result.Ok(record)
}

func logTimestamp(
	timestamp json.RawMessage,
	observed json.RawMessage,
	fallback domain.TimePoint,
) result.Result[domain.TimePoint] {
	if len(timestamp) > 0 {
		return parseTimestamp(timestamp)
	}

	if len(observed) > 0 {
		return parseTimestamp(observed)
	}

	return result.Ok(fallback)
}

func otlpLogTimestamp(
	raw rawOTLPLogRecord,
	fallback domain.TimePoint,
) result.Result[domain.TimePoint] {
	if len(raw.TimeUnixNano) > 0 {
		return unixNanoTimePoint(raw.TimeUnixNano)
	}

	if len(raw.ObservedTimeUnixNano) > 0 {
		return unixNanoTimePoint(raw.ObservedTimeUnixNano)
	}

	return result.Ok(fallback)
}

func unixNanoTimePoint(payload json.RawMessage) result.Result[domain.TimePoint] {
	var text string
	textErr := json.Unmarshal(payload, &text)
	if textErr == nil {
		return parseUnixNanoText(text)
	}

	var number int64
	numberErr := json.Unmarshal(payload, &number)
	if numberErr != nil {
		return result.Err[domain.TimePoint](NewProtocolError(ErrorInvalidEnvelope, "invalid otel log timestamp"))
	}

	point, pointErr := domain.NewTimePoint(time.Unix(0, number))
	if pointErr != nil {
		return result.Err[domain.TimePoint](pointErr)
	}

	return result.Ok(point)
}

func parseUnixNanoText(input string) result.Result[domain.TimePoint] {
	value := strings.TrimSpace(input)
	nanos, parseErr := strconv.ParseInt(value, 10, 64)
	if parseErr != nil {
		return result.Err[domain.TimePoint](NewProtocolError(ErrorInvalidEnvelope, "invalid otel log timestamp"))
	}

	point, pointErr := domain.NewTimePoint(time.Unix(0, nanos))
	if pointErr != nil {
		return result.Err[domain.TimePoint](pointErr)
	}

	return result.Ok(point)
}

func logSeverity(input string) result.Result[domain.LogSeverity] {
	severity, severityErr := domain.NewLogSeverity(input)
	if severityErr != nil {
		return result.Err[domain.LogSeverity](NewProtocolError(ErrorInvalidEnvelope, "invalid log severity"))
	}

	return result.Ok(severity)
}

func otlpLogSeverity(raw rawOTLPLogRecord) result.Result[domain.LogSeverity] {
	value := strings.TrimSpace(raw.SeverityText)
	value = strings.TrimRight(value, "0123456789")
	if value != "" {
		return logSeverity(value)
	}

	return logSeverity(severityTextFromNumber(raw.SeverityNumber))
}

func severityTextFromNumber(value int) string {
	if value >= 21 {
		return "fatal"
	}

	if value >= 17 {
		return "error"
	}

	if value >= 13 {
		return "warning"
	}

	if value >= 9 {
		return "info"
	}

	if value >= 5 {
		return "debug"
	}

	if value >= 1 {
		return "trace"
	}

	return "info"
}

func sentryLogBody(raw rawSentryLog, attributes map[string]string) result.Result[string] {
	if text := jsonMessageText(raw.Body); text != "" {
		return result.Ok(text)
	}

	if text := jsonMessageText(raw.Message); text != "" {
		return result.Ok(text)
	}

	if text := firstNonEmptyString(attributes["body"], attributes["message"], attributes["sentry.message"]); text != "" {
		return result.Ok(text)
	}

	return result.Err[string](NewProtocolError(ErrorInvalidEnvelope, "log body is required"))
}

func jsonMessageText(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}

	if text := anyValueString(payload); text != "" {
		return text
	}

	return messageText(payload)
}

func jsonObjectToAttributes(object map[string]json.RawMessage) map[string]string {
	values := map[string]string{}
	for key, raw := range object {
		value := attributeValueString(raw)
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}

		values[strings.TrimSpace(key)] = value
	}

	return values
}

func otlpAttributes(attributes []rawOTLPAttribute) map[string]string {
	values := map[string]string{}
	for _, attribute := range attributes {
		key := strings.TrimSpace(attribute.Key)
		value := anyValueString(attribute.Value)
		if key == "" || value == "" {
			continue
		}

		values[key] = value
	}

	return values
}

func attributeValueString(payload json.RawMessage) string {
	var wrapped struct {
		Value json.RawMessage `json:"value"`
	}
	wrappedErr := json.Unmarshal(payload, &wrapped)
	if wrappedErr == nil && len(wrapped.Value) > 0 {
		return anyValueString(wrapped.Value)
	}

	return anyValueString(payload)
}

func anyValueString(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}

	var text string
	textErr := json.Unmarshal(payload, &text)
	if textErr == nil {
		return strings.TrimSpace(text)
	}

	var number json.Number
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	numberErr := decoder.Decode(&number)
	if numberErr == nil {
		return strings.TrimSpace(number.String())
	}

	var boolean bool
	boolErr := json.Unmarshal(payload, &boolean)
	if boolErr == nil {
		return strconv.FormatBool(boolean)
	}

	var typed map[string]json.RawMessage
	typedErr := json.Unmarshal(payload, &typed)
	if typedErr == nil {
		return typedAnyValueString(typed)
	}

	return ""
}

func typedAnyValueString(values map[string]json.RawMessage) string {
	keys := []string{
		"stringValue",
		"intValue",
		"doubleValue",
		"boolValue",
		"bytesValue",
	}

	for _, key := range keys {
		if raw := values[key]; len(raw) > 0 {
			return anyValueString(raw)
		}
	}

	if raw := values["kvlistValue"]; len(raw) > 0 {
		return compactJSON(raw)
	}

	if raw := values["arrayValue"]; len(raw) > 0 {
		return compactJSON(raw)
	}

	return ""
}

func compactJSON(payload json.RawMessage) string {
	var value any
	decodeErr := json.Unmarshal(payload, &value)
	if decodeErr != nil {
		return ""
	}

	encoded, encodeErr := json.Marshal(value)
	if encodeErr != nil {
		return ""
	}

	return string(encoded)
}

func mergeAttributes(first map[string]string, second map[string]string) map[string]string {
	merged := map[string]string{}
	for key, value := range first {
		merged[key] = value
	}

	for key, value := range second {
		merged[key] = value
	}

	return merged
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}

package domain

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	logBodyMaxBytes      = 8 * 1024
	logLoggerMaxBytes    = 256
	logDimensionMaxBytes = 256
	logAttributeKeyLimit = 128
	logAttributeValueMax = 1024
	logAttributeCountMax = 64
)

type LogSeverity string

const (
	LogSeverityTrace   LogSeverity = "trace"
	LogSeverityDebug   LogSeverity = "debug"
	LogSeverityInfo    LogSeverity = "info"
	LogSeverityWarning LogSeverity = "warning"
	LogSeverityError   LogSeverity = "error"
	LogSeverityFatal   LogSeverity = "fatal"
)

type LogAttributeSet struct {
	values map[string]string
}

type LogRecordParams struct {
	OrganizationID     OrganizationID
	ProjectID          ProjectID
	Timestamp          TimePoint
	ReceivedAt         TimePoint
	Severity           LogSeverity
	Body               string
	Logger             string
	TraceID            string
	SpanID             string
	Release            string
	Environment        string
	ResourceAttributes map[string]string
	Attributes         map[string]string
}

type LogRecord struct {
	organizationID     OrganizationID
	projectID          ProjectID
	timestamp          TimePoint
	receivedAt         TimePoint
	severity           LogSeverity
	body               string
	logger             string
	traceID            string
	spanID             string
	release            string
	environment        string
	resourceAttributes LogAttributeSet
	attributes         LogAttributeSet
}

func NewLogSeverity(input string) (LogSeverity, error) {
	value := strings.TrimSpace(input)
	value = strings.ToLower(value)

	switch value {
	case "", "info", "information":
		return LogSeverityInfo, nil
	case "trace":
		return LogSeverityTrace, nil
	case "debug":
		return LogSeverityDebug, nil
	case "warn", "warning":
		return LogSeverityWarning, nil
	case "error":
		return LogSeverityError, nil
	case "fatal", "critical":
		return LogSeverityFatal, nil
	default:
		return "", errors.New("invalid log severity")
	}
}

func NewLogAttributeSet(input map[string]string) (LogAttributeSet, error) {
	if len(input) > logAttributeCountMax {
		return LogAttributeSet{}, errors.New("too many log attributes")
	}

	values := map[string]string{}
	for key, value := range input {
		normalizedKey, keyErr := normalizeLogText(key, logAttributeKeyLimit, "log attribute key")
		if keyErr != nil {
			return LogAttributeSet{}, keyErr
		}

		if normalizedKey == "" {
			return LogAttributeSet{}, errors.New("log attribute key is required")
		}

		normalizedValue, valueErr := normalizeLogText(value, logAttributeValueMax, "log attribute value")
		if valueErr != nil {
			return LogAttributeSet{}, valueErr
		}

		values[normalizedKey] = normalizedValue
	}

	return LogAttributeSet{values: values}, nil
}

func NewLogRecord(params LogRecordParams) (LogRecord, error) {
	if params.OrganizationID.value == "" {
		return LogRecord{}, errors.New("organization id is required")
	}

	if params.ProjectID.value == "" {
		return LogRecord{}, errors.New("project id is required")
	}

	if params.Timestamp.value.IsZero() {
		return LogRecord{}, errors.New("log timestamp is required")
	}

	if params.ReceivedAt.value.IsZero() {
		return LogRecord{}, errors.New("log received time is required")
	}

	if !params.Severity.valid() {
		return LogRecord{}, errors.New("invalid log severity")
	}

	body, bodyErr := normalizeRequiredLogText(params.Body, logBodyMaxBytes, "log body")
	if bodyErr != nil {
		return LogRecord{}, bodyErr
	}

	logger, loggerErr := normalizeLogText(params.Logger, logLoggerMaxBytes, "log logger")
	if loggerErr != nil {
		return LogRecord{}, loggerErr
	}

	traceID, traceErr := normalizeOptionalLogTraceID(params.TraceID)
	if traceErr != nil {
		return LogRecord{}, traceErr
	}

	spanID, spanErr := normalizeOptionalLogSpanID(params.SpanID)
	if spanErr != nil {
		return LogRecord{}, spanErr
	}

	release, releaseErr := normalizeLogText(params.Release, logDimensionMaxBytes, "log release")
	if releaseErr != nil {
		return LogRecord{}, releaseErr
	}

	environment, environmentErr := normalizeLogText(params.Environment, logDimensionMaxBytes, "log environment")
	if environmentErr != nil {
		return LogRecord{}, environmentErr
	}

	resourceAttributes, resourceErr := NewLogAttributeSet(params.ResourceAttributes)
	if resourceErr != nil {
		return LogRecord{}, resourceErr
	}

	attributes, attributesErr := NewLogAttributeSet(params.Attributes)
	if attributesErr != nil {
		return LogRecord{}, attributesErr
	}

	return LogRecord{
		organizationID:     params.OrganizationID,
		projectID:          params.ProjectID,
		timestamp:          params.Timestamp,
		receivedAt:         params.ReceivedAt,
		severity:           params.Severity,
		body:               body,
		logger:             logger,
		traceID:            traceID,
		spanID:             spanID,
		release:            release,
		environment:        environment,
		resourceAttributes: resourceAttributes,
		attributes:         attributes,
	}, nil
}

func normalizeRequiredLogText(input string, limit int, label string) (string, error) {
	value, valueErr := normalizeLogText(input, limit, label)
	if valueErr != nil {
		return "", valueErr
	}

	if value == "" {
		return "", errors.New(label + " is required")
	}

	return value, nil
}

func normalizeLogText(input string, limit int, label string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	if !utf8.ValidString(value) {
		return "", errors.New(label + " must be valid utf-8")
	}

	if len(value) > limit {
		return "", errors.New(label + " is too long")
	}

	for _, char := range value {
		if char == '\n' || char == '\r' || char == '\t' {
			continue
		}

		if unicode.IsControl(char) {
			return "", errors.New(label + " must not contain control characters")
		}
	}

	return value, nil
}

func normalizeOptionalLogTraceID(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	normalized, traceErr := normalizeTraceID(value)
	if traceErr != nil {
		return "", errors.New("log trace id must be 32 hex characters")
	}

	return normalized, nil
}

func normalizeOptionalLogSpanID(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	normalized, spanErr := normalizeSpanID(value)
	if spanErr != nil {
		return "", errors.New("log span id must be 16 hex characters")
	}

	return normalized, nil
}

func (severity LogSeverity) valid() bool {
	return severity == LogSeverityTrace ||
		severity == LogSeverityDebug ||
		severity == LogSeverityInfo ||
		severity == LogSeverityWarning ||
		severity == LogSeverityError ||
		severity == LogSeverityFatal
}

func (severity LogSeverity) String() string {
	return string(severity)
}

func (set LogAttributeSet) Values() map[string]string {
	values := map[string]string{}
	for key, value := range set.values {
		values[key] = value
	}

	return values
}

func (record LogRecord) OrganizationID() OrganizationID {
	return record.organizationID
}

func (record LogRecord) ProjectID() ProjectID {
	return record.projectID
}

func (record LogRecord) Timestamp() TimePoint {
	return record.timestamp
}

func (record LogRecord) ReceivedAt() TimePoint {
	return record.receivedAt
}

func (record LogRecord) Severity() LogSeverity {
	return record.severity
}

func (record LogRecord) Body() string {
	return record.body
}

func (record LogRecord) Logger() string {
	return record.logger
}

func (record LogRecord) TraceID() string {
	return record.traceID
}

func (record LogRecord) SpanID() string {
	return record.spanID
}

func (record LogRecord) Release() string {
	return record.release
}

func (record LogRecord) Environment() string {
	return record.environment
}

func (record LogRecord) ResourceAttributes() map[string]string {
	return record.resourceAttributes.Values()
}

func (record LogRecord) Attributes() map[string]string {
	return record.attributes.Values()
}

package domain

import (
	"errors"
	"strings"
)

type EventKind string

const (
	EventKindError       EventKind = "error"
	EventKindDefault     EventKind = "default"
	EventKindTransaction EventKind = "transaction"
)

type EventLevel string

const (
	EventLevelFatal   EventLevel = "fatal"
	EventLevelError   EventLevel = "error"
	EventLevelWarning EventLevel = "warning"
	EventLevelInfo    EventLevel = "info"
	EventLevelDebug   EventLevel = "debug"
)

type EventTitle struct {
	value string
}

type CanonicalEventParams struct {
	OrganizationID       OrganizationID
	ProjectID            ProjectID
	EventID              EventID
	OccurredAt           TimePoint
	ReceivedAt           TimePoint
	Kind                 EventKind
	Level                EventLevel
	Title                EventTitle
	Platform             string
	Release              string
	Environment          string
	Tags                 map[string]string
	DefaultGroupingParts []string
	ExplicitFingerprint  []string
	Attachments          []EventAttachment
	JsStacktrace         []JsStacktraceFrame
	NativeModules        []NativeModule
	NativeFrames         []NativeFrame
}

type CanonicalEvent struct {
	organizationID       OrganizationID
	projectID            ProjectID
	eventID              EventID
	occurredAt           TimePoint
	receivedAt           TimePoint
	kind                 EventKind
	level                EventLevel
	title                EventTitle
	platform             string
	release              string
	environment          string
	tags                 map[string]string
	defaultGroupingParts []string
	explicitFingerprint  []string
	attachments          []EventAttachment
	jsStacktrace         []JsStacktraceFrame
	nativeModules        []NativeModule
	nativeFrames         []NativeFrame
}

func NewEventTitle(input string) (EventTitle, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return EventTitle{}, errors.New("event title is required")
	}

	return EventTitle{value: value}, nil
}

func NewEventLevel(input string) (EventLevel, error) {
	value := EventLevel(strings.ToLower(strings.TrimSpace(input)))

	if !value.valid() {
		return "", errors.New("invalid event level")
	}

	return value, nil
}

func NewCanonicalEvent(params CanonicalEventParams) (CanonicalEvent, error) {
	if params.OrganizationID.value == "" {
		return CanonicalEvent{}, errors.New("organization id is required")
	}

	if params.ProjectID.value == "" {
		return CanonicalEvent{}, errors.New("project id is required")
	}

	if params.EventID.value == "" {
		return CanonicalEvent{}, errors.New("event id is required")
	}

	if params.OccurredAt.value.IsZero() {
		return CanonicalEvent{}, errors.New("occurred time is required")
	}

	if params.ReceivedAt.value.IsZero() {
		return CanonicalEvent{}, errors.New("received time is required")
	}

	if !params.Kind.valid() {
		return CanonicalEvent{}, errors.New("invalid event kind")
	}

	if !params.Level.valid() {
		return CanonicalEvent{}, errors.New("invalid event level")
	}

	if params.Title.value == "" {
		return CanonicalEvent{}, errors.New("event title is required")
	}

	platform := strings.TrimSpace(params.Platform)
	if platform == "" {
		platform = "other"
	}

	defaultParts := normalizeParts(params.DefaultGroupingParts)
	if len(defaultParts) == 0 {
		defaultParts = []string{params.Title.value}
	}

	return CanonicalEvent{
		organizationID:       params.OrganizationID,
		projectID:            params.ProjectID,
		eventID:              params.EventID,
		occurredAt:           params.OccurredAt,
		receivedAt:           params.ReceivedAt,
		kind:                 params.Kind,
		level:                params.Level,
		title:                params.Title,
		platform:             platform,
		release:              normalizeDimension(params.Release),
		environment:          normalizeDimension(params.Environment),
		tags:                 normalizeTags(params.Tags),
		defaultGroupingParts: defaultParts,
		explicitFingerprint:  normalizeParts(params.ExplicitFingerprint),
		attachments:          copyEventAttachments(params.Attachments),
		jsStacktrace:         copyJsStacktraceFrames(params.JsStacktrace),
		nativeModules:        copyNativeModules(params.NativeModules),
		nativeFrames:         copyNativeFrames(params.NativeFrames),
	}, nil
}

func (kind EventKind) valid() bool {
	return kind == EventKindError || kind == EventKindDefault || kind == EventKindTransaction
}

func (level EventLevel) valid() bool {
	return level == EventLevelFatal ||
		level == EventLevelError ||
		level == EventLevelWarning ||
		level == EventLevelInfo ||
		level == EventLevelDebug
}

func (kind EventKind) CreatesIssue() bool {
	return kind == EventKindError || kind == EventKindDefault
}

func (title EventTitle) String() string {
	return title.value
}

func (level EventLevel) String() string {
	return string(level)
}

func (event CanonicalEvent) OrganizationID() OrganizationID {
	return event.organizationID
}

func (event CanonicalEvent) ProjectID() ProjectID {
	return event.projectID
}

func (event CanonicalEvent) EventID() EventID {
	return event.eventID
}

func (event CanonicalEvent) OccurredAt() TimePoint {
	return event.occurredAt
}

func (event CanonicalEvent) ReceivedAt() TimePoint {
	return event.receivedAt
}

func (event CanonicalEvent) Kind() EventKind {
	return event.kind
}

func (event CanonicalEvent) Level() EventLevel {
	return event.level
}

func (event CanonicalEvent) Title() EventTitle {
	return event.title
}

func (event CanonicalEvent) Platform() string {
	return event.platform
}

func (event CanonicalEvent) Release() string {
	return event.release
}

func (event CanonicalEvent) Environment() string {
	return event.environment
}

func (event CanonicalEvent) Tags() map[string]string {
	return copyTags(event.tags)
}

func (event CanonicalEvent) CreatesIssue() bool {
	return event.kind.CreatesIssue()
}

func (event CanonicalEvent) DefaultGroupingParts() []string {
	return append([]string{}, event.defaultGroupingParts...)
}

func (event CanonicalEvent) ExplicitFingerprint() []string {
	return append([]string{}, event.explicitFingerprint...)
}

func (event CanonicalEvent) Attachments() []EventAttachment {
	return copyEventAttachments(event.attachments)
}

func (event CanonicalEvent) JsStacktrace() []JsStacktraceFrame {
	return copyJsStacktraceFrames(event.jsStacktrace)
}

func (event CanonicalEvent) WithJsStacktrace(frames []JsStacktraceFrame) CanonicalEvent {
	updated := event
	updated.jsStacktrace = copyJsStacktraceFrames(frames)
	return updated
}

func (event CanonicalEvent) NativeModules() []NativeModule {
	return copyNativeModules(event.nativeModules)
}

func (event CanonicalEvent) NativeFrames() []NativeFrame {
	return copyNativeFrames(event.nativeFrames)
}

func normalizeParts(parts []string) []string {
	result := make([]string, 0, len(parts))

	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}

		result = append(result, value)
	}

	return result
}

func normalizeDimension(input string) string {
	return strings.TrimSpace(input)
}

func normalizeTags(tags map[string]string) map[string]string {
	result := map[string]string{}

	for key, value := range tags {
		normalizedKey := strings.TrimSpace(key)
		normalizedValue := strings.TrimSpace(value)
		if normalizedKey == "" || normalizedValue == "" {
			continue
		}

		result[normalizedKey] = normalizedValue
	}

	return result
}

func copyTags(tags map[string]string) map[string]string {
	result := make(map[string]string, len(tags))

	for key, value := range tags {
		result[key] = value
	}

	return result
}

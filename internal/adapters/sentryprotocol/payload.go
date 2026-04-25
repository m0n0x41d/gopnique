package sentryprotocol

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type rawEventPayload struct {
	EventID     string          `json:"event_id"`
	Timestamp   json.RawMessage `json:"timestamp"`
	Type        string          `json:"type"`
	Level       string          `json:"level"`
	Logger      string          `json:"logger"`
	Platform    string          `json:"platform"`
	Release     string          `json:"release"`
	Environment string          `json:"environment"`
	ServerName  string          `json:"server_name"`
	Message     json.RawMessage `json:"message"`
	Logentry    rawLogEntry     `json:"logentry"`
	Exception   json.RawMessage `json:"exception"`
	Transaction string          `json:"transaction"`
	Culprit     string          `json:"culprit"`
	Fingerprint []any           `json:"fingerprint"`
	Tags        json.RawMessage `json:"tags"`
	User        json.RawMessage `json:"user"`
	Request     json.RawMessage `json:"request"`
	SDK         json.RawMessage `json:"sdk"`
}

type rawLogEntry struct {
	Message string `json:"message"`
}

type rawException struct {
	Values []rawExceptionValue `json:"values"`
}

type rawExceptionValue struct {
	Type       string        `json:"type"`
	Value      string        `json:"value"`
	Stacktrace rawStacktrace `json:"stacktrace"`
}

type rawStacktrace struct {
	Frames []rawFrame `json:"frames"`
}

type rawFrame struct {
	Filename string `json:"filename"`
	AbsPath  string `json:"abs_path"`
	Function string `json:"function"`
	InApp    *bool  `json:"in_app"`
	Lineno   int    `json:"lineno"`
	Colno    int    `json:"colno"`
}

func ParseStoreEvent(
	project ProjectContext,
	receivedAt domain.TimePoint,
	payload []byte,
) result.Result[domain.CanonicalEvent] {
	return parseEventPayload(project, receivedAt, payload, "event", "")
}

func parseEventPayload(
	project ProjectContext,
	receivedAt domain.TimePoint,
	payload []byte,
	itemType string,
	eventIDOverride string,
) result.Result[domain.CanonicalEvent] {
	var raw rawEventPayload

	decodeErr := json.Unmarshal(payload, &raw)
	if decodeErr != nil {
		return result.Err[domain.CanonicalEvent](NewProtocolError(ErrorInvalidEvent, "invalid event json"))
	}

	if eventIDOverride != "" {
		raw.EventID = eventIDOverride
	}

	eventID, eventIDErr := domain.NewEventID(raw.EventID)
	if eventIDErr != nil {
		return result.Err[domain.CanonicalEvent](NewProtocolError(ErrorInvalidEvent, "invalid event id"))
	}

	occurredAtResult := parseTimestamp(raw.Timestamp)
	occurredAt, occurredAtErr := occurredAtResult.Value()
	if occurredAtErr != nil {
		return result.Err[domain.CanonicalEvent](occurredAtErr)
	}

	kind := eventKind(itemType, raw)
	level := eventLevel(kind, raw.Level)
	titleResult := eventTitle(kind, raw)
	title, titleErr := titleResult.Value()
	if titleErr != nil {
		return result.Err[domain.CanonicalEvent](titleErr)
	}

	canonical, canonicalErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       project.OrganizationID(),
		ProjectID:            project.ProjectID(),
		EventID:              eventID,
		OccurredAt:           occurredAt,
		ReceivedAt:           receivedAt,
		Kind:                 kind,
		Level:                level,
		Title:                title,
		Platform:             raw.Platform,
		Release:              raw.Release,
		Environment:          raw.Environment,
		Tags:                 eventTags(project, raw),
		DefaultGroupingParts: defaultGroupingParts(kind, raw, title),
		ExplicitFingerprint:  normalizeFingerprint(raw.Fingerprint),
		JsStacktrace:         jsStacktraceFrames(kind, raw),
	})
	if canonicalErr != nil {
		return result.Err[domain.CanonicalEvent](canonicalErr)
	}

	return result.Ok(canonical)
}

func eventTags(project ProjectContext, payload rawEventPayload) map[string]string {
	tags := parseTags(payload.Tags)
	clientIP, hasClientIP := clientIPTag(project, payload, tags)
	tags = withoutClientIPTags(tags)
	if hasClientIP {
		tags["client_ip"] = clientIP
	}

	serverName := strings.TrimSpace(payload.ServerName)
	if serverName != "" {
		tags["server_name"] = serverName
	}

	return tags
}

func clientIPTag(
	project ProjectContext,
	payload rawEventPayload,
	tags map[string]string,
) (string, bool) {
	if project.ScrubIPAddresses() {
		return "", false
	}

	return firstPublicClientIP(clientIPCandidates(payload, tags))
}

func clientIPCandidates(payload rawEventPayload, tags map[string]string) []string {
	candidates := []string{}
	candidates = append(candidates, userIPCandidates(payload.User)...)
	candidates = append(candidates, requestIPCandidates(payload.Request)...)

	for key, value := range tags {
		if isClientIPTagKey(key) {
			candidates = append(candidates, value)
		}
	}

	return candidates
}

func userIPCandidates(payload json.RawMessage) []string {
	if len(payload) == 0 {
		return []string{}
	}

	var object struct {
		IPAddress string `json:"ip_address"`
	}
	objectErr := json.Unmarshal(payload, &object)
	if objectErr != nil {
		return []string{}
	}

	return []string{object.IPAddress}
}

func requestIPCandidates(payload json.RawMessage) []string {
	if len(payload) == 0 {
		return []string{}
	}

	var object struct {
		Env     map[string]any  `json:"env"`
		Headers json.RawMessage `json:"headers"`
	}
	objectErr := json.Unmarshal(payload, &object)
	if objectErr != nil {
		return []string{}
	}

	candidates := []string{}
	candidates = append(candidates, requestEnvIPCandidates(object.Env)...)
	candidates = append(candidates, requestHeaderIPCandidates(object.Headers)...)

	return candidates
}

func requestEnvIPCandidates(env map[string]any) []string {
	if len(env) == 0 {
		return []string{}
	}

	return []string{
		fmt.Sprint(env["REMOTE_ADDR"]),
		fmt.Sprint(env["HTTP_X_FORWARDED_FOR"]),
	}
}

func requestHeaderIPCandidates(payload json.RawMessage) []string {
	if len(payload) == 0 {
		return []string{}
	}

	var object map[string]any
	objectErr := json.Unmarshal(payload, &object)
	if objectErr == nil {
		return headerMapIPCandidates(object)
	}

	var pairs [][]any
	pairsErr := json.Unmarshal(payload, &pairs)
	if pairsErr == nil {
		return headerPairIPCandidates(pairs)
	}

	return []string{}
}

func headerMapIPCandidates(headers map[string]any) []string {
	candidates := []string{}

	for key, value := range headers {
		if !isForwardedForHeader(key) {
			continue
		}

		candidates = append(candidates, fmt.Sprint(value))
	}

	return candidates
}

func headerPairIPCandidates(headers [][]any) []string {
	candidates := []string{}

	for _, pair := range headers {
		if len(pair) < 2 || !isForwardedForHeader(fmt.Sprint(pair[0])) {
			continue
		}

		candidates = append(candidates, fmt.Sprint(pair[1]))
	}

	return candidates
}

func firstPublicClientIP(candidates []string) (string, bool) {
	for _, candidate := range candidates {
		for _, part := range strings.Split(candidate, ",") {
			ip, ok := publicClientIP(part)
			if ok {
				return ip, true
			}
		}
	}

	return "", false
}

func publicClientIP(input string) (string, bool) {
	value := strings.TrimSpace(input)
	if value == "" || value == "<nil>" {
		return "", false
	}

	addrPort, addrPortErr := netip.ParseAddrPort(value)
	if addrPortErr == nil {
		value = addrPort.Addr().String()
	}

	addr, addrErr := netip.ParseAddr(value)
	if addrErr != nil || !addr.IsValid() {
		return "", false
	}

	if !addr.IsGlobalUnicast() ||
		addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsUnspecified() {
		return "", false
	}

	return addr.String(), true
}

func withoutClientIPTags(tags map[string]string) map[string]string {
	result := make(map[string]string, len(tags))

	for key, value := range tags {
		if isClientIPTagKey(key) {
			continue
		}

		result[key] = value
	}

	return result
}

func isClientIPTagKey(input string) bool {
	key := strings.ToLower(strings.TrimSpace(input))
	key = strings.ReplaceAll(key, "-", "_")
	key = strings.ReplaceAll(key, ".", "_")

	return key == "client_ip" ||
		key == "ip_address" ||
		key == "remote_addr" ||
		key == "user_ip" ||
		key == "user_ip_address"
}

func isForwardedForHeader(input string) bool {
	key := strings.ToLower(strings.TrimSpace(input))
	key = strings.ReplaceAll(key, "-", "_")

	return key == "x_forwarded_for" || key == "x_real_ip"
}

func parseTags(payload json.RawMessage) map[string]string {
	if len(payload) == 0 {
		return map[string]string{}
	}

	var object map[string]any
	objectErr := json.Unmarshal(payload, &object)
	if objectErr == nil {
		return normalizeTagObject(object)
	}

	var pairs [][]any
	pairsErr := json.Unmarshal(payload, &pairs)
	if pairsErr == nil {
		return normalizeTagPairs(pairs)
	}

	return map[string]string{}
}

func normalizeTagObject(object map[string]any) map[string]string {
	tags := make(map[string]string, len(object))

	for key, value := range object {
		tagKey := strings.TrimSpace(key)
		tagValue := strings.TrimSpace(fmt.Sprint(value))
		if tagKey == "" || tagValue == "" || tagValue == "<nil>" {
			continue
		}

		tags[tagKey] = tagValue
	}

	return tags
}

func normalizeTagPairs(pairs [][]any) map[string]string {
	tags := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		if len(pair) < 2 {
			continue
		}

		tagKey := strings.TrimSpace(fmt.Sprint(pair[0]))
		tagValue := strings.TrimSpace(fmt.Sprint(pair[1]))
		if tagKey == "" || tagValue == "" || tagKey == "<nil>" || tagValue == "<nil>" {
			continue
		}

		tags[tagKey] = tagValue
	}

	return tags
}

func parseTimestamp(payload json.RawMessage) result.Result[domain.TimePoint] {
	if len(payload) == 0 {
		return result.Err[domain.TimePoint](NewProtocolError(ErrorInvalidEvent, "timestamp is required"))
	}

	var text string
	textErr := json.Unmarshal(payload, &text)
	if textErr == nil {
		return parseTextTimestamp(text)
	}

	var seconds float64
	numberErr := json.Unmarshal(payload, &seconds)
	if numberErr != nil {
		return result.Err[domain.TimePoint](NewProtocolError(ErrorInvalidEvent, "invalid timestamp"))
	}

	nanos := int64(seconds * 1_000_000_000)
	instant := time.Unix(0, nanos)
	point, pointErr := domain.NewTimePoint(instant)
	if pointErr != nil {
		return result.Err[domain.TimePoint](pointErr)
	}

	return result.Ok(point)
}

func parseTextTimestamp(input string) result.Result[domain.TimePoint] {
	instant, parseErr := time.Parse(time.RFC3339Nano, input)
	if parseErr != nil {
		return result.Err[domain.TimePoint](NewProtocolError(ErrorInvalidEvent, "invalid timestamp"))
	}

	point, pointErr := domain.NewTimePoint(instant)
	if pointErr != nil {
		return result.Err[domain.TimePoint](pointErr)
	}

	return result.Ok(point)
}

func eventKind(itemType string, payload rawEventPayload) domain.EventKind {
	if itemType == "transaction" || payload.Type == "transaction" {
		return domain.EventKindTransaction
	}

	if len(exceptionValues(payload.Exception).Values) > 0 {
		return domain.EventKindError
	}

	return domain.EventKindDefault
}

func eventLevel(kind domain.EventKind, input string) domain.EventLevel {
	if input == "" && kind == domain.EventKindTransaction {
		return domain.EventLevelInfo
	}

	if input == "" {
		return domain.EventLevelError
	}

	level, levelErr := domain.NewEventLevel(input)
	if levelErr != nil {
		return domain.EventLevelError
	}

	return level
}

func eventTitle(kind domain.EventKind, payload rawEventPayload) result.Result[domain.EventTitle] {
	candidates := titleCandidates(kind, payload)

	for _, candidate := range candidates {
		title, titleErr := domain.NewEventTitle(candidate)
		if titleErr == nil {
			return result.Ok(title)
		}
	}

	fallback := "<event>"
	if kind == domain.EventKindError {
		fallback = "<unknown error>"
	}
	if kind == domain.EventKindTransaction {
		fallback = "<transaction>"
	}

	title, titleErr := domain.NewEventTitle(fallback)
	if titleErr != nil {
		return result.Err[domain.EventTitle](titleErr)
	}

	return result.Ok(title)
}

func titleCandidates(kind domain.EventKind, payload rawEventPayload) []string {
	if kind == domain.EventKindError {
		exception := selectedException(exceptionValues(payload.Exception))
		exceptionTitle := strings.TrimSpace(exception.Type + ": " + exception.Value)
		return []string{
			exceptionTitle,
			exception.Type,
			messageText(payload.Message),
			payload.Logentry.Message,
			payload.Transaction,
			payload.Culprit,
		}
	}

	if kind == domain.EventKindTransaction {
		return []string{
			payload.Transaction,
			payload.Culprit,
		}
	}

	return []string{
		messageText(payload.Message),
		payload.Logentry.Message,
		payload.Transaction,
		payload.Culprit,
	}
}

func defaultGroupingParts(
	kind domain.EventKind,
	payload rawEventPayload,
	title domain.EventTitle,
) []string {
	if kind == domain.EventKindError {
		exception := selectedException(exceptionValues(payload.Exception))
		frame := selectedFrame(exception.Stacktrace)
		return []string{
			exception.Type,
			exception.Value,
			frameFilename(frame),
			frame.Function,
			strconv.Itoa(frame.Lineno),
		}
	}

	if kind == domain.EventKindTransaction {
		return []string{payload.Transaction}
	}

	return []string{
		payload.Level,
		payload.Logger,
		messageText(payload.Message),
		payload.Transaction,
		title.String(),
	}
}

func exceptionValues(payload json.RawMessage) rawException {
	if len(payload) == 0 {
		return rawException{}
	}

	var object rawException
	objectErr := json.Unmarshal(payload, &object)
	if objectErr == nil && len(object.Values) > 0 {
		return object
	}

	var values []rawExceptionValue
	arrayErr := json.Unmarshal(payload, &values)
	if arrayErr == nil {
		return rawException{Values: values}
	}

	return rawException{}
}

func selectedException(exception rawException) rawExceptionValue {
	if len(exception.Values) == 0 {
		return rawExceptionValue{}
	}

	return exception.Values[len(exception.Values)-1]
}

func selectedFrame(stacktrace rawStacktrace) rawFrame {
	inAppFrame, ok := selectedInAppFrame(stacktrace.Frames)
	if ok {
		return inAppFrame
	}

	if len(stacktrace.Frames) == 0 {
		return rawFrame{}
	}

	return stacktrace.Frames[len(stacktrace.Frames)-1]
}

func frameFilename(frame rawFrame) string {
	if strings.TrimSpace(frame.Filename) != "" {
		return frame.Filename
	}

	return frame.AbsPath
}

func selectedInAppFrame(frames []rawFrame) (rawFrame, bool) {
	for index := len(frames) - 1; index >= 0; index-- {
		frame := frames[index]
		if frame.InApp == nil || !*frame.InApp {
			continue
		}

		return frame, true
	}

	return rawFrame{}, false
}

func messageText(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}

	var text string
	textErr := json.Unmarshal(payload, &text)
	if textErr == nil {
		return text
	}

	var object struct {
		Message string `json:"message"`
	}
	objectErr := json.Unmarshal(payload, &object)
	if objectErr == nil {
		return object.Message
	}

	return ""
}

func normalizeFingerprint(values []any) []string {
	result := make([]string, 0, len(values))

	for _, value := range values {
		if value == nil {
			continue
		}

		text := fmt.Sprint(value)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		result = append(result, text)
	}

	return result
}

func jsStacktraceFrames(kind domain.EventKind, payload rawEventPayload) []domain.JsStacktraceFrame {
	if kind != domain.EventKindError {
		return nil
	}

	if !isJsPlatform(payload.Platform) {
		return nil
	}

	exception := exceptionValues(payload.Exception)
	frames := []domain.JsStacktraceFrame{}

	for _, value := range exception.Values {
		for _, raw := range value.Stacktrace.Frames {
			built, ok := buildJsStacktraceFrame(raw)
			if !ok {
				continue
			}

			frames = append(frames, built)
		}
	}

	return frames
}

func buildJsStacktraceFrame(frame rawFrame) (domain.JsStacktraceFrame, bool) {
	absPath := strings.TrimSpace(frame.AbsPath)
	if absPath == "" {
		absPath = strings.TrimSpace(frame.Filename)
	}

	if absPath == "" {
		return domain.JsStacktraceFrame{}, false
	}

	if frame.Lineno < 1 {
		return domain.JsStacktraceFrame{}, false
	}

	column := max(frame.Colno, 0)

	built, builtErr := domain.NewUnresolvedJsStacktraceFrame(
		absPath,
		strings.TrimSpace(frame.Function),
		frame.Lineno,
		column,
	)
	if builtErr != nil {
		return domain.JsStacktraceFrame{}, false
	}

	return built, true
}

func isJsPlatform(platform string) bool {
	normalized := strings.ToLower(strings.TrimSpace(platform))

	return normalized == "javascript" || normalized == "node"
}

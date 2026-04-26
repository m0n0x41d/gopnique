package sentryprotocol

import (
	"encoding/json"
	"errors"
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
	StartTime   json.RawMessage `json:"start_timestamp"`
	Type        string          `json:"type"`
	Level       string          `json:"level"`
	Logger      string          `json:"logger"`
	Platform    string          `json:"platform"`
	Release     string          `json:"release"`
	Dist        string          `json:"dist"`
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
	Contexts    rawContexts     `json:"contexts"`
	Spans       []rawSpan       `json:"spans"`
	DebugMeta   rawDebugMeta    `json:"debug_meta"`
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
	Filename        string          `json:"filename"`
	AbsPath         string          `json:"abs_path"`
	Function        string          `json:"function"`
	Package         string          `json:"package"`
	DebugID         string          `json:"debug_id"`
	InstructionAddr json.RawMessage `json:"instruction_addr"`
	InApp           *bool           `json:"in_app"`
	Lineno          int             `json:"lineno"`
	Colno           int             `json:"colno"`
}

type rawContexts struct {
	Trace rawTraceContext `json:"trace"`
}

type rawTraceContext struct {
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"parent_span_id"`
	Operation    string `json:"op"`
	Status       string `json:"status"`
}

type rawSpan struct {
	SpanID       string          `json:"span_id"`
	ParentSpanID string          `json:"parent_span_id"`
	Operation    string          `json:"op"`
	Description  string          `json:"description"`
	Status       string          `json:"status"`
	StartTime    json.RawMessage `json:"start_timestamp"`
	Timestamp    json.RawMessage `json:"timestamp"`
}

type rawDebugMeta struct {
	Images []rawDebugImage `json:"images"`
}

type rawDebugImage struct {
	DebugID   string          `json:"debug_id"`
	CodeFile  string          `json:"code_file"`
	ImageAddr json.RawMessage `json:"image_addr"`
	ImageSize json.RawMessage `json:"image_size"`
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

	tagsResult := eventTags(project, raw)
	tags, tagsErr := tagsResult.Value()
	if tagsErr != nil {
		return result.Err[domain.CanonicalEvent](tagsErr)
	}

	transactionResult := transactionData(kind, raw, title, occurredAt)
	transaction, transactionErr := transactionResult.Value()
	if transactionErr != nil {
		return result.Err[domain.CanonicalEvent](transactionErr)
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
		Tags:                 tags,
		DefaultGroupingParts: defaultGroupingParts(kind, raw, title),
		ExplicitFingerprint:  normalizeFingerprint(raw.Fingerprint),
		JsStacktrace:         jsStacktraceFrames(kind, raw),
		NativeModules:        nativeModules(raw),
		NativeFrames:         nativeStacktraceFrames(kind, raw),
		Transaction:          transaction,
	})
	if canonicalErr != nil {
		return result.Err[domain.CanonicalEvent](canonicalErr)
	}

	return result.Ok(canonical)
}

func eventTags(project ProjectContext, payload rawEventPayload) result.Result[map[string]string] {
	tags := parseTags(payload.Tags)
	clientIP, hasClientIP := clientIPTag(project, payload, tags)
	tags = withoutClientIPTags(tags)

	distTagsResult := withEventDistTag(tags, payload.Dist)
	distTags, distTagsErr := distTagsResult.Value()
	if distTagsErr != nil {
		return result.Err[map[string]string](distTagsErr)
	}

	tags = distTags
	if hasClientIP {
		tags["client_ip"] = clientIP
	}

	serverName := strings.TrimSpace(payload.ServerName)
	if serverName != "" {
		tags["server_name"] = serverName
	}

	return result.Ok(tags)
}

func withEventDistTag(tags map[string]string, input string) result.Result[map[string]string] {
	dist, distErr := domain.NewOptionalDistName(input)
	if distErr != nil {
		return result.Err[map[string]string](NewProtocolError(ErrorInvalidEvent, "invalid dist"))
	}

	if !dist.HasValue() {
		return result.Ok(tags)
	}

	tags["dist"] = dist.String()
	return result.Ok(tags)
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

func transactionData(
	kind domain.EventKind,
	payload rawEventPayload,
	title domain.EventTitle,
	occurredAt domain.TimePoint,
) result.Result[domain.TransactionData] {
	if kind != domain.EventKindTransaction {
		return result.Ok(domain.TransactionData{})
	}

	durationResult := transactionDuration(payload.StartTime, occurredAt)
	duration, durationErr := durationResult.Value()
	if durationErr != nil {
		return result.Err[domain.TransactionData](durationErr)
	}

	traceResult := transactionTrace(payload.Contexts.Trace)
	traceState, traceErr := traceResult.Value()
	if traceErr != nil {
		return result.Err[domain.TransactionData](traceErr)
	}

	spans := transactionSpans(payload.Spans)
	name := transactionName(payload, title)
	operation := payload.Contexts.Trace.Operation
	status := payload.Contexts.Trace.Status

	if traceState.ok {
		transaction, transactionErr := domain.NewTransactionDataWithTrace(
			name,
			operation,
			duration,
			status,
			traceState.trace,
			spans,
		)
		if transactionErr != nil {
			return result.Err[domain.TransactionData](transactionErr)
		}

		return result.Ok(transaction)
	}

	transaction, transactionErr := domain.NewTransactionData(
		name,
		operation,
		duration,
		status,
		spans,
	)
	if transactionErr != nil {
		return result.Err[domain.TransactionData](transactionErr)
	}

	return result.Ok(transaction)
}

type transactionTraceResult struct {
	trace domain.TransactionTraceContext
	ok    bool
}

func transactionTrace(raw rawTraceContext) result.Result[transactionTraceResult] {
	traceID := strings.TrimSpace(raw.TraceID)
	spanID := strings.TrimSpace(raw.SpanID)
	parentSpanID := strings.TrimSpace(raw.ParentSpanID)
	if traceID == "" && spanID == "" && parentSpanID == "" {
		return result.Ok(transactionTraceResult{})
	}

	trace, traceErr := domain.NewTransactionTraceContext(traceID, spanID, parentSpanID)
	if traceErr != nil {
		return result.Err[transactionTraceResult](NewProtocolError(ErrorInvalidEvent, "invalid trace context"))
	}

	return result.Ok(transactionTraceResult{trace: trace, ok: true})
}

func transactionDuration(
	startPayload json.RawMessage,
	end domain.TimePoint,
) result.Result[time.Duration] {
	if len(startPayload) == 0 {
		return result.Ok(time.Duration(0))
	}

	startResult := parseTimestamp(startPayload)
	start, startErr := startResult.Value()
	if startErr != nil {
		return result.Err[time.Duration](NewProtocolError(ErrorInvalidEvent, "invalid start timestamp"))
	}

	duration := end.Time().Sub(start.Time())
	if duration < 0 {
		return result.Err[time.Duration](NewProtocolError(ErrorInvalidEvent, "transaction duration is negative"))
	}

	return result.Ok(duration)
}

func transactionName(payload rawEventPayload, title domain.EventTitle) string {
	if value := strings.TrimSpace(payload.Transaction); value != "" {
		return value
	}

	return title.String()
}

func transactionSpans(rawSpans []rawSpan) []domain.TransactionSpan {
	spans := []domain.TransactionSpan{}

	for _, raw := range rawSpans {
		span, ok := buildTransactionSpan(raw)
		if !ok {
			continue
		}

		spans = append(spans, span)
	}

	return spans
}

func buildTransactionSpan(raw rawSpan) (domain.TransactionSpan, bool) {
	duration, ok := spanDuration(raw)
	if !ok {
		return domain.TransactionSpan{}, false
	}

	span, spanErr := domain.NewTransactionSpan(
		raw.SpanID,
		raw.ParentSpanID,
		raw.Operation,
		raw.Description,
		duration,
		raw.Status,
	)
	if spanErr != nil {
		return domain.TransactionSpan{}, false
	}

	return span, true
}

func spanDuration(raw rawSpan) (time.Duration, bool) {
	if len(raw.StartTime) == 0 || len(raw.Timestamp) == 0 {
		return 0, true
	}

	startResult := parseTimestamp(raw.StartTime)
	start, startErr := startResult.Value()
	if startErr != nil {
		return 0, false
	}

	endResult := parseTimestamp(raw.Timestamp)
	end, endErr := endResult.Value()
	if endErr != nil {
		return 0, false
	}

	duration := end.Time().Sub(start.Time())
	if duration < 0 {
		return 0, false
	}

	return duration, true
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

func nativeModules(payload rawEventPayload) []domain.NativeModule {
	modules := []domain.NativeModule{}

	for _, image := range payload.DebugMeta.Images {
		module, ok := buildNativeModule(image)
		if !ok {
			continue
		}

		modules = append(modules, module)
	}

	return modules
}

func buildNativeModule(image rawDebugImage) (domain.NativeModule, bool) {
	debugID, debugIDErr := domain.NewDebugIdentifier(image.DebugID)
	if debugIDErr != nil {
		return domain.NativeModule{}, false
	}

	imageAddr, imageAddrOK := parseJSONAddress(image.ImageAddr)
	if !imageAddrOK {
		imageAddr = 0
	}

	imageSize, imageSizeOK := parseJSONAddress(image.ImageSize)
	if !imageSizeOK {
		imageSize = 0
	}

	module, moduleErr := domain.NewNativeModule(
		debugID,
		image.CodeFile,
		imageAddr,
		imageSize,
	)
	if moduleErr != nil {
		return domain.NativeModule{}, false
	}

	return module, true
}

func nativeStacktraceFrames(kind domain.EventKind, payload rawEventPayload) []domain.NativeFrame {
	if kind != domain.EventKindError {
		return nil
	}

	modules := nativeModules(payload)
	exception := exceptionValues(payload.Exception)
	frames := []domain.NativeFrame{}

	for _, value := range exception.Values {
		for _, raw := range value.Stacktrace.Frames {
			frame, ok := buildNativeFrame(raw, modules)
			if !ok {
				continue
			}

			frames = append(frames, frame)
		}
	}

	return frames
}

func buildNativeFrame(
	frame rawFrame,
	modules []domain.NativeModule,
) (domain.NativeFrame, bool) {
	instructionAddr, instructionAddrOK := parseJSONAddress(frame.InstructionAddr)
	if !instructionAddrOK {
		return domain.NativeFrame{}, false
	}

	debugID, hasDebugID := nativeFrameDebugID(frame, modules, instructionAddr)
	if hasDebugID {
		built, builtErr := domain.NewNativeFrameWithModule(
			instructionAddr,
			debugID,
			frame.Function,
			frame.Package,
		)
		if builtErr != nil {
			return domain.NativeFrame{}, false
		}

		return built, true
	}

	built, builtErr := domain.NewNativeFrame(
		instructionAddr,
		frame.Function,
		frame.Package,
	)
	if builtErr != nil {
		return domain.NativeFrame{}, false
	}

	return built, true
}

func nativeFrameDebugID(
	frame rawFrame,
	modules []domain.NativeModule,
	instructionAddr uint64,
) (domain.DebugIdentifier, bool) {
	debugID, debugIDErr := domain.NewDebugIdentifier(frame.DebugID)
	if debugIDErr == nil {
		return debugID, true
	}

	module, hasModule := nativeModuleForPackage(frame.Package, modules)
	if hasModule {
		return module.DebugID(), true
	}

	module, hasModule = nativeModuleForInstructionAddr(instructionAddr, modules)
	if hasModule {
		return module.DebugID(), true
	}

	return domain.DebugIdentifier{}, false
}

func nativeModuleForPackage(
	pkg string,
	modules []domain.NativeModule,
) (domain.NativeModule, bool) {
	value := strings.TrimSpace(pkg)
	if value == "" {
		return domain.NativeModule{}, false
	}

	for _, module := range modules {
		if module.CodeFile() != value {
			continue
		}

		return module, true
	}

	return domain.NativeModule{}, false
}

func nativeModuleForInstructionAddr(
	instructionAddr uint64,
	modules []domain.NativeModule,
) (domain.NativeModule, bool) {
	for _, module := range modules {
		if !nativeModuleContainsInstructionAddr(module, instructionAddr) {
			continue
		}

		return module, true
	}

	return domain.NativeModule{}, false
}

func nativeModuleContainsInstructionAddr(
	module domain.NativeModule,
	instructionAddr uint64,
) bool {
	if instructionAddr < module.ImageAddr() {
		return false
	}

	if module.ImageSize() == 0 {
		return instructionAddr == module.ImageAddr()
	}

	return instructionAddr-module.ImageAddr() < module.ImageSize()
}

func parseJSONAddress(payload json.RawMessage) (uint64, bool) {
	if len(payload) == 0 {
		return 0, false
	}

	var text string
	textErr := json.Unmarshal(payload, &text)
	if textErr == nil {
		value, valueErr := parseAddressText(text)
		return value, valueErr == nil
	}

	var number uint64
	numberErr := json.Unmarshal(payload, &number)
	if numberErr != nil {
		return 0, false
	}

	return number, true
}

func parseAddressText(input string) (uint64, error) {
	value := strings.TrimSpace(input)
	value = strings.TrimPrefix(value, "0x")
	value = strings.TrimPrefix(value, "0X")
	if value == "" {
		return 0, errors.New("address is empty")
	}

	return strconv.ParseUint(value, 16, 64)
}

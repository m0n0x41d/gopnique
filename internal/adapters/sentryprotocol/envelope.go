package sentryprotocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const (
	MaxEnvelopeBytes      = 20 * 1024 * 1024
	MaxEnvelopeHeaderSize = 8 * 1024
	MaxEnvelopeItemHeader = 8 * 1024
	MaxEnvelopeItemBytes  = 1 * 1024 * 1024
	MaxEnvelopeItems      = 64
)

type EnvelopeResult struct {
	event       domain.CanonicalEvent
	hasEvent    bool
	itemType    string
	ignored     []string
	userReports []UserReportItem
	duplicate   bool
}

type UserReportItem struct {
	EventID  string
	Name     string
	Email    string
	Comments string
}

type envelopeHeader struct {
	EventID string `json:"event_id"`
	DSN     string `json:"dsn"`
}

type itemHeader struct {
	Type   string `json:"type"`
	Length *int   `json:"length"`
}

func ParseEnvelope(
	project ProjectContext,
	receivedAt domain.TimePoint,
	payload []byte,
) result.Result[EnvelopeResult] {
	reader := bufio.NewReader(bytes.NewReader(payload))
	headerResult := readEnvelopeHeader(reader)
	header, headerErr := headerResult.Value()
	if headerErr != nil {
		return result.Err[EnvelopeResult](headerErr)
	}

	state := envelopeState{}

	itemCount := 0
	for {
		line, lineErr := readLimitedLine(reader, MaxEnvelopeItemHeader, "item header too large")
		if errors.Is(lineErr, io.EOF) && len(line) == 0 {
			break
		}

		if lineErr != nil {
			return result.Err[EnvelopeResult](lineErr)
		}

		itemCount++
		if itemCount > MaxEnvelopeItems {
			return result.Err[EnvelopeResult](NewProtocolError(ErrorInvalidEnvelope, "too many envelope items"))
		}

		line = bytes.TrimSuffix(line, []byte("\n"))
		line = bytes.TrimSuffix(line, []byte("\r"))
		if len(line) == 0 && errors.Is(lineErr, io.EOF) {
			break
		}

		itemResult := parseItemHeader(line)
		item, itemErr := itemResult.Value()
		if itemErr != nil {
			return result.Err[EnvelopeResult](itemErr)
		}

		itemPayloadResult := readItemPayload(reader, item)
		itemPayload, itemPayloadErr := itemPayloadResult.Value()
		if itemPayloadErr != nil {
			return result.Err[EnvelopeResult](itemPayloadErr)
		}

		nextStateResult := applyItem(project, receivedAt, header, state, item, itemPayload)
		nextState, nextStateErr := nextStateResult.Value()
		if nextStateErr != nil {
			return result.Err[EnvelopeResult](nextStateErr)
		}

		state = nextState
	}

	return result.Ok(EnvelopeResult{
		event:       state.event,
		hasEvent:    state.hasEvent,
		itemType:    state.itemType,
		ignored:     append([]string{}, state.ignored...),
		userReports: append([]UserReportItem{}, state.userReports...),
	})
}

func readEnvelopeHeader(reader *bufio.Reader) result.Result[envelopeHeader] {
	line, lineErr := readLimitedLine(reader, MaxEnvelopeHeaderSize, "envelope header too large")
	if errors.Is(lineErr, io.EOF) && len(line) == 0 {
		return result.Err[envelopeHeader](NewProtocolError(ErrorInvalidEnvelope, "empty envelope"))
	}

	if lineErr != nil && !errors.Is(lineErr, io.EOF) {
		return result.Err[envelopeHeader](lineErr)
	}

	line = bytes.TrimSuffix(line, []byte("\n"))
	line = bytes.TrimSuffix(line, []byte("\r"))

	var header envelopeHeader
	decodeErr := json.Unmarshal(line, &header)
	if decodeErr != nil {
		return result.Err[envelopeHeader](NewProtocolError(ErrorInvalidEnvelope, "invalid envelope header"))
	}

	return result.Ok(header)
}

func parseItemHeader(line []byte) result.Result[itemHeader] {
	var header itemHeader
	decodeErr := json.Unmarshal(line, &header)
	if decodeErr != nil {
		return result.Err[itemHeader](NewProtocolError(ErrorInvalidEnvelope, "invalid item header"))
	}

	if header.Type == "" {
		return result.Err[itemHeader](NewProtocolError(ErrorInvalidEnvelope, "item type is required"))
	}

	if header.Length != nil && *header.Length < 0 {
		return result.Err[itemHeader](NewProtocolError(ErrorInvalidEnvelope, "item length is invalid"))
	}

	return result.Ok(header)
}

func readItemPayload(reader *bufio.Reader, header itemHeader) result.Result[[]byte] {
	if header.Length == nil {
		payload, readErr := readLimitedLine(reader, MaxEnvelopeItemBytes, "item payload too large")
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return result.Err[[]byte](readErr)
		}

		payload = bytes.TrimSuffix(payload, []byte("\n"))
		payload = bytes.TrimSuffix(payload, []byte("\r"))
		return result.Ok(payload)
	}

	if *header.Length > MaxEnvelopeItemBytes {
		return result.Err[[]byte](NewProtocolError(ErrorInvalidEnvelope, "item payload too large"))
	}

	payload := make([]byte, *header.Length)
	_, readErr := io.ReadFull(reader, payload)
	if readErr != nil {
		return result.Err[[]byte](NewProtocolError(ErrorInvalidEnvelope, "incomplete item payload"))
	}

	consumeTrailingNewline(reader)

	return result.Ok(payload)
}

func readLimitedLine(
	reader *bufio.Reader,
	limit int,
	message string,
) ([]byte, error) {
	var line []byte

	for {
		chunk, prefix, readErr := reader.ReadLine()
		line = append(line, chunk...)
		if len(line) > limit {
			return nil, NewProtocolError(ErrorInvalidEnvelope, message)
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) && len(line) > 0 {
				return line, io.EOF
			}

			return line, readErr
		}

		if !prefix {
			return line, nil
		}
	}
}

func consumeTrailingNewline(reader *bufio.Reader) {
	next, peekErr := reader.Peek(1)
	if peekErr != nil || len(next) == 0 {
		return
	}

	if next[0] == '\r' {
		_, _ = reader.ReadByte()
	}

	next, peekErr = reader.Peek(1)
	if peekErr != nil || len(next) == 0 || next[0] != '\n' {
		return
	}

	_, _ = reader.ReadByte()
}

type envelopeState struct {
	event       domain.CanonicalEvent
	hasEvent    bool
	itemType    string
	ignored     []string
	userReports []UserReportItem
}

func applyItem(
	project ProjectContext,
	receivedAt domain.TimePoint,
	header envelopeHeader,
	state envelopeState,
	item itemHeader,
	payload []byte,
) result.Result[envelopeState] {
	if isReservedItem(item.Type) {
		return result.Err[envelopeState](NewProtocolError(ErrorInvalidEnvelope, "reserved item type"))
	}

	if isUserReportItem(item.Type) {
		return applyUserReportItem(state, item, payload)
	}

	if isIgnoredItem(item.Type) {
		state.ignored = append(state.ignored, item.Type)
		return result.Ok(state)
	}

	if !isSupportedItem(item.Type) {
		state.ignored = append(state.ignored, item.Type)
		return result.Ok(state)
	}

	if state.hasEvent {
		return result.Err[envelopeState](NewProtocolError(ErrorInvalidEnvelope, "multiple supported event items"))
	}

	eventResult := parseEventPayload(project, receivedAt, payload, item.Type, header.EventID)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		return result.Err[envelopeState](eventErr)
	}

	state.event = event
	state.hasEvent = true
	state.itemType = item.Type

	return result.Ok(state)
}

func applyUserReportItem(
	state envelopeState,
	item itemHeader,
	payload []byte,
) result.Result[envelopeState] {
	reportResult := parseUserReportItem(item.Type, payload)
	report, reportErr := reportResult.Value()
	if reportErr != nil {
		return result.Err[envelopeState](reportErr)
	}

	state.userReports = append(state.userReports, report)

	return result.Ok(state)
}

type legacyUserReportPayload struct {
	EventID  string `json:"event_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Comments string `json:"comments"`
}

type feedbackPayload struct {
	EventID  string                     `json:"event_id"`
	Contexts map[string]json.RawMessage `json:"contexts"`
}

type feedbackContext struct {
	Message           string `json:"message"`
	Name              string `json:"name"`
	ContactEmail      string `json:"contact_email"`
	AssociatedEventID string `json:"associated_event_id"`
}

func parseUserReportItem(itemType string, payload []byte) result.Result[UserReportItem] {
	if itemType == "feedback" {
		return parseFeedbackItem(payload)
	}

	return parseLegacyUserReportItem(payload)
}

func parseLegacyUserReportItem(payload []byte) result.Result[UserReportItem] {
	var legacy legacyUserReportPayload
	decodeErr := json.Unmarshal(payload, &legacy)
	if decodeErr != nil {
		return result.Err[UserReportItem](NewProtocolError(ErrorInvalidEnvelope, "invalid user report"))
	}

	return result.Ok(UserReportItem{
		EventID:  strings.TrimSpace(legacy.EventID),
		Name:     strings.TrimSpace(legacy.Name),
		Email:    strings.TrimSpace(legacy.Email),
		Comments: strings.TrimSpace(legacy.Comments),
	})
}

func parseFeedbackItem(payload []byte) result.Result[UserReportItem] {
	var feedback feedbackPayload
	decodeErr := json.Unmarshal(payload, &feedback)
	if decodeErr != nil {
		return result.Err[UserReportItem](NewProtocolError(ErrorInvalidEnvelope, "invalid feedback"))
	}

	contextPayload := feedback.Contexts["feedback"]
	if len(contextPayload) == 0 {
		return result.Err[UserReportItem](NewProtocolError(ErrorInvalidEnvelope, "feedback context is required"))
	}

	var context feedbackContext
	contextErr := json.Unmarshal(contextPayload, &context)
	if contextErr != nil {
		return result.Err[UserReportItem](NewProtocolError(ErrorInvalidEnvelope, "invalid feedback context"))
	}

	eventID := strings.TrimSpace(context.AssociatedEventID)
	if eventID == "" {
		eventID = strings.TrimSpace(feedback.EventID)
	}

	return result.Ok(UserReportItem{
		EventID:  eventID,
		Name:     strings.TrimSpace(context.Name),
		Email:    strings.TrimSpace(context.ContactEmail),
		Comments: strings.TrimSpace(context.Message),
	})
}

func isSupportedItem(itemType string) bool {
	return itemType == "event" || itemType == "transaction"
}

func isUserReportItem(itemType string) bool {
	return itemType == "user_report" || itemType == "feedback"
}

func isIgnoredItem(itemType string) bool {
	ignored := map[string]struct{}{
		"attachment":       {},
		"check_in":         {},
		"client_report":    {},
		"log":              {},
		"otel_log":         {},
		"profile":          {},
		"profile_chunk":    {},
		"replay_event":     {},
		"replay_recording": {},
		"replay_video":     {},
		"session":          {},
		"sessions":         {},
		"span":             {},
	}

	_, ok := ignored[itemType]

	return ok
}

func isReservedItem(itemType string) bool {
	reserved := map[string]struct{}{
		"form_data":     {},
		"security":      {},
		"unreal_report": {},
	}

	_, ok := reserved[itemType]

	return ok
}

func (envelope EnvelopeResult) Event() (domain.CanonicalEvent, bool) {
	return envelope.event, envelope.hasEvent
}

func (envelope EnvelopeResult) ItemType() string {
	return envelope.itemType
}

func (envelope EnvelopeResult) UserReports() []UserReportItem {
	return append([]UserReportItem{}, envelope.userReports...)
}

func (envelope EnvelopeResult) IgnoredItems() []string {
	return append([]string{}, envelope.ignored...)
}

func (envelope EnvelopeResult) HasEvent() bool {
	return envelope.hasEvent
}

func EnvelopeHeaderDSN(payload []byte) result.Result[string] {
	reader := bufio.NewReader(bytes.NewReader(payload))
	headerResult := readEnvelopeHeader(reader)
	header, headerErr := headerResult.Value()
	if headerErr != nil {
		return result.Err[string](headerErr)
	}

	return result.Ok(strings.TrimSpace(header.DSN))
}

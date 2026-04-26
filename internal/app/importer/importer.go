package importer

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const (
	JSONLRowMaxBytes = 64 * 1024
	defaultPlatform  = "other"
)

type RowError struct {
	Line    int
	Message string
}

type ParseResult struct {
	records []domain.ImportRecord
	errors  []RowError
	total   int
}

type DryRunResult struct {
	TotalRows   int
	ValidRows   int
	InvalidRows int
	Errors      []RowError
}

type ApplyCommand struct {
	Manifest domain.ImportManifest
	Records  []domain.ImportRecord
	ActorID  string
}

type ApplyResult struct {
	RunID         string
	TotalRows     int
	AppliedRows   int
	DuplicateRows int
	SkippedRows   int
	FailedRows    int
}

type Ledger interface {
	ApplyImport(ctx context.Context, command ApplyCommand) result.Result[ApplyResult]
}

type jsonRecord struct {
	Type            string            `json:"type"`
	ExternalID      string            `json:"external_id"`
	IssueExternalID string            `json:"issue_external_id"`
	EventID         string            `json:"event_id"`
	Kind            string            `json:"kind"`
	Level           string            `json:"level"`
	Title           string            `json:"title"`
	Platform        string            `json:"platform"`
	OccurredAt      string            `json:"occurred_at"`
	ReceivedAt      string            `json:"received_at"`
	Release         string            `json:"release"`
	Environment     string            `json:"environment"`
	Tags            map[string]string `json:"tags"`
	GroupingParts   []string          `json:"grouping_parts"`
	Fingerprint     []string          `json:"fingerprint"`
}

func ParseJSONL(
	reader io.Reader,
	manifest domain.ImportManifest,
) result.Result[ParseResult] {
	scopeErr := requireManifestMode(manifest, domain.ImportModeDryRun, domain.ImportModeApply)
	if scopeErr != nil {
		return result.Err[ParseResult](scopeErr)
	}

	scanner := bufio.NewScanner(reader)
	buffer := make([]byte, 0, JSONLRowMaxBytes)
	scanner.Buffer(buffer, JSONLRowMaxBytes)

	outcome := ParseResult{}
	for scanner.Scan() {
		line := scanner.Text()
		outcome.total++

		record, recordErr := parseLine(line, manifest)
		if recordErr != nil {
			outcome.errors = append(outcome.errors, RowError{
				Line:    outcome.total,
				Message: recordErr.Error(),
			})
			continue
		}

		outcome.records = append(outcome.records, record)
	}

	if scanErr := scanner.Err(); scanErr != nil {
		return result.Err[ParseResult](scanErr)
	}

	return result.Ok(outcome)
}

func DryRun(parsed ParseResult) DryRunResult {
	invalidRows := len(parsed.errors)
	validRows := len(parsed.records)
	errorsCopy := append([]RowError{}, parsed.errors...)

	return DryRunResult{
		TotalRows:   parsed.total,
		ValidRows:   validRows,
		InvalidRows: invalidRows,
		Errors:      errorsCopy,
	}
}

func Apply(
	ctx context.Context,
	ledger Ledger,
	command ApplyCommand,
) result.Result[ApplyResult] {
	if ledger == nil {
		return result.Err[ApplyResult](errors.New("import ledger is required"))
	}

	validateErr := validateApplyCommand(command)
	if validateErr != nil {
		return result.Err[ApplyResult](validateErr)
	}

	return ledger.ApplyImport(ctx, command)
}

func (parsed ParseResult) Records() []domain.ImportRecord {
	return append([]domain.ImportRecord{}, parsed.records...)
}

func (parsed ParseResult) Errors() []RowError {
	return append([]RowError{}, parsed.errors...)
}

func (parsed ParseResult) TotalRows() int {
	return parsed.total
}

func parseLine(
	line string,
	manifest domain.ImportManifest,
) (domain.ImportRecord, error) {
	value := strings.TrimSpace(line)
	if value == "" {
		return domain.ImportRecord{}, errors.New("import row is empty")
	}

	var raw jsonRecord
	decodeErr := json.Unmarshal([]byte(value), &raw)
	if decodeErr != nil {
		return domain.ImportRecord{}, decodeErr
	}

	record, recordErr := buildImportRecord(raw, manifest)
	if recordErr != nil {
		return domain.ImportRecord{}, recordErr
	}

	return record, nil
}

func buildImportRecord(
	raw jsonRecord,
	manifest domain.ImportManifest,
) (domain.ImportRecord, error) {
	recordKind, kindErr := domain.NewImportRecordKind(raw.Type)
	if kindErr != nil {
		return domain.ImportRecord{}, kindErr
	}

	externalID, externalErr := domain.NewImportExternalID(raw.ExternalID)
	if externalErr != nil {
		return domain.ImportRecord{}, externalErr
	}

	event, eventErr := buildCanonicalEvent(raw, manifest, recordKind)
	if eventErr != nil {
		return domain.ImportRecord{}, eventErr
	}

	record, recordErr := domain.NewImportRecord(domain.ImportRecordParams{
		SourceSystem: manifest.SourceSystem(),
		ExternalID:   externalID,
		Kind:         recordKind,
		Event:        event,
	})
	if recordErr != nil {
		return domain.ImportRecord{}, recordErr
	}

	return record, nil
}

func buildCanonicalEvent(
	raw jsonRecord,
	manifest domain.ImportManifest,
	recordKind domain.ImportRecordKind,
) (domain.CanonicalEvent, error) {
	eventID, eventIDErr := importEventID(raw, manifest, recordKind)
	if eventIDErr != nil {
		return domain.CanonicalEvent{}, eventIDErr
	}

	occurredAt, occurredErr := parseRequiredTime(raw.OccurredAt, "occurred_at")
	if occurredErr != nil {
		return domain.CanonicalEvent{}, occurredErr
	}

	receivedAt, receivedErr := parseReceivedTime(raw.ReceivedAt, occurredAt)
	if receivedErr != nil {
		return domain.CanonicalEvent{}, receivedErr
	}

	eventKind, eventKindErr := parseEventKind(raw.Kind)
	if eventKindErr != nil {
		return domain.CanonicalEvent{}, eventKindErr
	}

	level, levelErr := domain.NewEventLevel(raw.Level)
	if levelErr != nil {
		return domain.CanonicalEvent{}, levelErr
	}

	title, titleErr := domain.NewEventTitle(raw.Title)
	if titleErr != nil {
		return domain.CanonicalEvent{}, titleErr
	}

	fingerprint, fingerprintErr := explicitFingerprint(raw, manifest, recordKind)
	if fingerprintErr != nil {
		return domain.CanonicalEvent{}, fingerprintErr
	}

	params := domain.CanonicalEventParams{
		OrganizationID:       manifest.OrganizationID(),
		ProjectID:            manifest.ProjectID(),
		EventID:              eventID,
		OccurredAt:           occurredAt,
		ReceivedAt:           receivedAt,
		Kind:                 eventKind,
		Level:                level,
		Title:                title,
		Platform:             platform(raw.Platform),
		Release:              raw.Release,
		Environment:          raw.Environment,
		Tags:                 raw.Tags,
		DefaultGroupingParts: defaultGroupingParts(raw, title),
		ExplicitFingerprint:  fingerprint,
	}

	event, eventErr := domain.NewCanonicalEvent(params)
	if eventErr != nil {
		return domain.CanonicalEvent{}, eventErr
	}

	return event, nil
}

func importEventID(
	raw jsonRecord,
	manifest domain.ImportManifest,
	recordKind domain.ImportRecordKind,
) (domain.EventID, error) {
	if recordKind == domain.ImportRecordKindEvent {
		return domain.NewEventID(raw.EventID)
	}

	derivedID := deterministicEventID(manifest.SourceSystem().String(), raw.ExternalID)
	eventID, eventErr := domain.NewEventID(derivedID)
	if eventErr != nil {
		return domain.EventID{}, eventErr
	}

	return eventID, nil
}

func deterministicEventID(source string, externalID string) string {
	seed := fmt.Sprintf("error-tracker/import/%s/%s", source, strings.TrimSpace(externalID))
	sum := sha256.Sum256([]byte(seed))
	hexValue := hex.EncodeToString(sum[0:16])
	versioned := []byte(hexValue)
	versioned[12] = '5'
	versioned[16] = variantHex(versioned[16])

	return string(versioned)
}

func variantHex(input byte) byte {
	value := input
	if value >= '0' && value <= '9' {
		numeric := value - '0'
		return "89ab"[numeric&3]
	}

	if value >= 'a' && value <= 'f' {
		numeric := value - 'a' + 10
		return "89ab"[numeric&3]
	}

	return '8'
}

func parseRequiredTime(input string, field string) (domain.TimePoint, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return domain.TimePoint{}, errors.New(field + " is required")
	}

	parsed, parseErr := time.Parse(time.RFC3339Nano, value)
	if parseErr != nil {
		return domain.TimePoint{}, parseErr
	}

	point, pointErr := domain.NewTimePoint(parsed)
	if pointErr != nil {
		return domain.TimePoint{}, pointErr
	}

	return point, nil
}

func parseReceivedTime(input string, fallback domain.TimePoint) (domain.TimePoint, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return fallback, nil
	}

	return parseRequiredTime(value, "received_at")
}

func parseEventKind(input string) (domain.EventKind, error) {
	value := domain.EventKind(strings.TrimSpace(input))
	if !value.CreatesIssue() {
		return "", errors.New("import event kind must be error or default")
	}

	return value, nil
}

func platform(input string) string {
	value := strings.TrimSpace(input)
	if value == "" {
		return defaultPlatform
	}

	return value
}

func defaultGroupingParts(raw jsonRecord, title domain.EventTitle) []string {
	if len(raw.GroupingParts) > 0 {
		return append([]string{}, raw.GroupingParts...)
	}

	return []string{title.String()}
}

func explicitFingerprint(
	raw jsonRecord,
	manifest domain.ImportManifest,
	recordKind domain.ImportRecordKind,
) ([]string, error) {
	if len(raw.Fingerprint) > 0 {
		return append([]string{}, raw.Fingerprint...), nil
	}

	issueExternalID := strings.TrimSpace(raw.IssueExternalID)
	if issueExternalID == "" && recordKind == domain.ImportRecordKindIssue {
		issueExternalID = strings.TrimSpace(raw.ExternalID)
	}

	if issueExternalID == "" {
		return nil, nil
	}

	normalized, normalizeErr := domain.NewImportExternalID(issueExternalID)
	if normalizeErr != nil {
		return nil, normalizeErr
	}

	return []string{
		"imported-issue",
		manifest.SourceSystem().String(),
		normalized.String(),
	}, nil
}

func validateApplyCommand(command ApplyCommand) error {
	modeErr := requireManifestMode(command.Manifest, domain.ImportModeApply)
	if modeErr != nil {
		return modeErr
	}

	if len(command.Records) == 0 {
		return errors.New("import records are required")
	}

	for _, record := range command.Records {
		recordErr := requireRecordMatchesManifest(record, command.Manifest)
		if recordErr != nil {
			return recordErr
		}
	}

	return nil
}

func requireManifestMode(manifest domain.ImportManifest, modes ...domain.ImportMode) error {
	if manifest.OrganizationID().String() == "" || manifest.ProjectID().String() == "" {
		return errors.New("import manifest scope is required")
	}

	if manifest.SourceSystem().String() == "" {
		return errors.New("import manifest source system is required")
	}

	for _, mode := range modes {
		if manifest.Mode() == mode {
			return nil
		}
	}

	return errors.New("import manifest mode is invalid")
}

func requireRecordMatchesManifest(
	record domain.ImportRecord,
	manifest domain.ImportManifest,
) error {
	event := record.Event()
	if event.OrganizationID().String() != manifest.OrganizationID().String() {
		return errors.New("import record organization mismatch")
	}

	if event.ProjectID().String() != manifest.ProjectID().String() {
		return errors.New("import record project mismatch")
	}

	if record.SourceSystem().String() != manifest.SourceSystem().String() {
		return errors.New("import record source system mismatch")
	}

	return nil
}

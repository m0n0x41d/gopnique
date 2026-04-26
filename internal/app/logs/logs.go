package logs

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const defaultLimit = 100
const maxLimit = 200

var logRecordIDPattern = regexp.MustCompile(`^[0-9a-f-]{36}$`)

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type IngestCommand struct {
	records []domain.LogRecord
}

type AppendResult struct {
	count int
}

type QuotaDecision struct {
	allowed bool
	reason  string
}

type ReceiptKind string

const (
	ReceiptAcceptedLogRecords ReceiptKind = "accepted_log_records"
	ReceiptQuotaRejected      ReceiptKind = "quota_rejected"
)

type IngestReceipt struct {
	kind   ReceiptKind
	count  int
	reason string
}

type IngestTransactionResult struct {
	receipt IngestReceipt
}

type Query struct {
	Scope             Scope
	Limit             int
	Severity          string
	Logger            string
	Environment       string
	Release           string
	ResourceAttribute AttributeFilter
	LogAttribute      AttributeFilter
}

type DetailQuery struct {
	Scope Scope
	ID    string
}

type AttributeFilter struct {
	Key   string
	Value string
}

type ListView struct {
	Logs    []RecordView
	Filters FilterView
}

type FilterView struct {
	Severity           string
	Logger             string
	Environment        string
	Release            string
	ResourceKey        string
	ResourceValue      string
	AttributeKey       string
	AttributeValue     string
	HasResourceFilter  bool
	HasAttributeFilter bool
}

type DetailView struct {
	Record RecordView
}

type RecordView struct {
	ID                 string
	Timestamp          string
	ReceivedAt         string
	Severity           string
	Body               string
	Logger             string
	TraceID            string
	SpanID             string
	Release            string
	Environment        string
	ResourceAttributes []AttributeView
	Attributes         []AttributeView
}

type AttributeView struct {
	Key   string
	Value string
}

type LogLedger interface {
	AppendLogRecords(ctx context.Context, records []domain.LogRecord) result.Result[AppendResult]
}

type QuotaGate interface {
	CheckLogQuota(ctx context.Context, record domain.LogRecord, count int) result.Result[QuotaDecision]
}

type TransactionalPorts interface {
	LogLedger
	QuotaGate
}

type IngestProgram func(ctx context.Context, ports TransactionalPorts) result.Result[IngestTransactionResult]

type IngestTransaction interface {
	RunLogIngest(ctx context.Context, program IngestProgram) result.Result[IngestTransactionResult]
}

type Reader interface {
	ListLogRecords(ctx context.Context, query Query) result.Result[ListView]
	ShowLogRecord(ctx context.Context, query DetailQuery) result.Result[DetailView]
}

type Manager interface {
	IngestTransaction
	Reader
}

func NewIngestCommand(records []domain.LogRecord) IngestCommand {
	copied := append([]domain.LogRecord{}, records...)

	return IngestCommand{records: copied}
}

func NewAppendResult(count int) AppendResult {
	return AppendResult{count: count}
}

func NewQuotaAllowed() QuotaDecision {
	return QuotaDecision{allowed: true}
}

func NewQuotaRejected(reason string) QuotaDecision {
	if strings.TrimSpace(reason) == "" {
		reason = "quota_exceeded"
	}

	return QuotaDecision{reason: reason}
}

func Ingest(
	ctx context.Context,
	transaction IngestTransaction,
	command IngestCommand,
) result.Result[IngestReceipt] {
	if transaction == nil {
		return result.Err[IngestReceipt](errors.New("log ingest transaction is required"))
	}

	normalized, normalizeErr := normalizeIngestCommand(command)
	if normalizeErr != nil {
		return result.Err[IngestReceipt](normalizeErr)
	}

	transactionResult := transaction.RunLogIngest(ctx, ingestProgram(normalized))
	completed, completedErr := transactionResult.Value()
	if completedErr != nil {
		return result.Err[IngestReceipt](completedErr)
	}

	return result.Ok(completed.receipt)
}

func List(
	ctx context.Context,
	reader Reader,
	query Query,
) result.Result[ListView] {
	if reader == nil {
		return result.Err[ListView](errors.New("log reader is required"))
	}

	normalized, normalizeErr := NormalizeQuery(query)
	if normalizeErr != nil {
		return result.Err[ListView](normalizeErr)
	}

	return reader.ListLogRecords(ctx, normalized)
}

func Detail(
	ctx context.Context,
	reader Reader,
	query DetailQuery,
) result.Result[DetailView] {
	if reader == nil {
		return result.Err[DetailView](errors.New("log reader is required"))
	}

	normalized, normalizeErr := NormalizeDetailQuery(query)
	if normalizeErr != nil {
		return result.Err[DetailView](normalizeErr)
	}

	return reader.ShowLogRecord(ctx, normalized)
}

func normalizeIngestCommand(command IngestCommand) (IngestCommand, error) {
	if len(command.records) == 0 {
		return IngestCommand{}, errors.New("log records are required")
	}

	first := command.records[0]
	scopeErr := requireRecordScope(first)
	if scopeErr != nil {
		return IngestCommand{}, scopeErr
	}

	for _, record := range command.records {
		recordErr := requireSameScope(first, record)
		if recordErr != nil {
			return IngestCommand{}, recordErr
		}
	}

	return command, nil
}

func ingestProgram(command IngestCommand) IngestProgram {
	return func(ctx context.Context, ports TransactionalPorts) result.Result[IngestTransactionResult] {
		first := command.records[0]
		quotaResult := ports.CheckLogQuota(ctx, first, len(command.records))
		quota, quotaErr := quotaResult.Value()
		if quotaErr != nil {
			return result.Err[IngestTransactionResult](quotaErr)
		}

		if !quota.Allowed() {
			return result.Ok(transactionResult(quotaReceipt(quota)))
		}

		appendResult := ports.AppendLogRecords(ctx, command.records)
		appended, appendErr := appendResult.Value()
		if appendErr != nil {
			return result.Err[IngestTransactionResult](appendErr)
		}

		return result.Ok(transactionResult(acceptedReceipt(appended)))
	}
}

func NormalizeQuery(query Query) (Query, error) {
	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return Query{}, scopeErr
	}

	severity, severityErr := normalizeOptionalSeverity(query.Severity)
	if severityErr != nil {
		return Query{}, severityErr
	}

	query.Severity = severity
	query.Logger = strings.TrimSpace(query.Logger)
	query.Environment = strings.TrimSpace(query.Environment)
	query.Release = strings.TrimSpace(query.Release)
	query.ResourceAttribute = normalizeAttributeFilter(query.ResourceAttribute)
	query.LogAttribute = normalizeAttributeFilter(query.LogAttribute)
	query.Limit = normalizeLimit(query.Limit)

	return query, nil
}

func NormalizeDetailQuery(query DetailQuery) (DetailQuery, error) {
	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return DetailQuery{}, scopeErr
	}

	id := strings.TrimSpace(query.ID)
	if !logRecordIDPattern.MatchString(id) {
		return DetailQuery{}, errors.New("log record id is invalid")
	}

	query.ID = id
	return query, nil
}

func normalizeOptionalSeverity(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	severity, severityErr := domain.NewLogSeverity(value)
	if severityErr != nil {
		return "", severityErr
	}

	return severity.String(), nil
}

func normalizeAttributeFilter(filter AttributeFilter) AttributeFilter {
	return AttributeFilter{
		Key:   strings.TrimSpace(filter.Key),
		Value: strings.TrimSpace(filter.Value),
	}
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}

	if limit > maxLimit {
		return maxLimit
	}

	return limit
}

func requireRecordScope(record domain.LogRecord) error {
	if record.OrganizationID().String() == "" || record.ProjectID().String() == "" {
		return errors.New("log record scope is required")
	}

	return nil
}

func requireSameScope(first domain.LogRecord, next domain.LogRecord) error {
	firstScope := Scope{
		OrganizationID: first.OrganizationID(),
		ProjectID:      first.ProjectID(),
	}
	nextScope := Scope{
		OrganizationID: next.OrganizationID(),
		ProjectID:      next.ProjectID(),
	}

	if firstScope.OrganizationID.String() != nextScope.OrganizationID.String() {
		return errors.New("log records must share one organization")
	}

	if firstScope.ProjectID.String() != nextScope.ProjectID.String() {
		return errors.New("log records must share one project")
	}

	return nil
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("log scope is required")
	}

	return nil
}

func transactionResult(receipt IngestReceipt) IngestTransactionResult {
	return IngestTransactionResult{receipt: receipt}
}

func acceptedReceipt(appended AppendResult) IngestReceipt {
	return IngestReceipt{
		kind:  ReceiptAcceptedLogRecords,
		count: appended.Count(),
	}
}

func quotaReceipt(quota QuotaDecision) IngestReceipt {
	return IngestReceipt{
		kind:   ReceiptQuotaRejected,
		reason: quota.Reason(),
	}
}

func (command IngestCommand) Records() []domain.LogRecord {
	return append([]domain.LogRecord{}, command.records...)
}

func (result AppendResult) Count() int {
	return result.count
}

func (decision QuotaDecision) Allowed() bool {
	return decision.allowed
}

func (decision QuotaDecision) Reason() string {
	return decision.reason
}

func (receipt IngestReceipt) Kind() ReceiptKind {
	return receipt.kind
}

func (receipt IngestReceipt) Count() int {
	return receipt.count
}

func (receipt IngestReceipt) Reason() string {
	return receipt.reason
}

package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	"github.com/ivanzakutnii/error-tracker/internal/app/importer"
	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

const (
	importRecordStatusPending   = "pending"
	importRecordStatusApplied   = "applied"
	importRecordStatusDuplicate = "duplicate"
	importRecordStatusFailed    = "failed"
	importRecordStatusSkipped   = "skipped"
)

type importRecordIdentity struct {
	id     string
	status string
}

type importRecordOutcome struct {
	status string
}

type importIngestTransaction struct {
	tx pgx.Tx
}

type importIngestPorts struct {
	txStore
}

func (store *Store) ApplyImport(
	ctx context.Context,
	command importer.ApplyCommand,
) result.Result[importer.ApplyResult] {
	runID, runErr := store.createImportRun(ctx, command)
	if runErr != nil {
		return result.Err[importer.ApplyResult](runErr)
	}

	outcome := importer.ApplyResult{
		RunID:     runID,
		TotalRows: len(command.Records),
	}

	for index, record := range command.Records {
		rowResult := store.applyImportRecord(ctx, runID, index+1, record)
		row, rowErr := rowResult.Value()
		if rowErr != nil {
			_ = store.markImportRunFailed(ctx, runID)
			return result.Err[importer.ApplyResult](rowErr)
		}

		outcome = countImportOutcome(outcome, row)
	}

	finishErr := store.finishImportRun(ctx, command, outcome)
	if finishErr != nil {
		return result.Err[importer.ApplyResult](finishErr)
	}

	return result.Ok(outcome)
}

func (store *Store) createImportRun(
	ctx context.Context,
	command importer.ApplyCommand,
) (string, error) {
	runID, runIDErr := randomUUID()
	if runIDErr != nil {
		return "", runIDErr
	}

	query := `
insert into import_runs (
  id,
  organization_id,
  project_id,
  source_system,
  mode,
  status,
  total_rows,
  started_at
) values (
  $1, $2, $3, $4, 'apply', 'running', $5, $6
)
`
	_, execErr := store.pool.Exec(
		ctx,
		query,
		runID,
		command.Manifest.OrganizationID().String(),
		command.Manifest.ProjectID().String(),
		command.Manifest.SourceSystem().String(),
		len(command.Records),
		time.Now().UTC(),
	)
	if execErr != nil {
		return "", execErr
	}

	return runID, nil
}

func (store *Store) applyImportRecord(
	ctx context.Context,
	runID string,
	rowNumber int,
	record domain.ImportRecord,
) result.Result[importRecordOutcome] {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return result.Err[importRecordOutcome](beginErr)
	}

	identity, identityErr := store.findImportRecordIdentity(ctx, tx, record)
	if identityErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[importRecordOutcome](identityErr)
	}

	if identity.id != "" && identity.status != importRecordStatusFailed {
		commitErr := tx.Commit(ctx)
		if commitErr != nil {
			return result.Err[importRecordOutcome](commitErr)
		}

		return result.Ok(importRecordOutcome{status: importRecordStatusSkipped})
	}

	recordID, recordIDErr := store.prepareImportRecord(ctx, tx, runID, rowNumber, record, identity)
	if recordIDErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[importRecordOutcome](recordIDErr)
	}

	receiptResult := ingest.IngestCanonicalEvent(
		ctx,
		ingest.NewIngestCommand(record.Event()),
		importIngestTransaction{tx: tx},
	)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[importRecordOutcome](receiptErr)
	}

	status, reason := importRecordStatusForReceipt(receipt)
	updateErr := store.updateImportRecordFromReceipt(ctx, tx, recordID, record, receipt, status, reason)
	if updateErr != nil {
		_ = tx.Rollback(ctx)
		return result.Err[importRecordOutcome](updateErr)
	}

	commitErr := tx.Commit(ctx)
	if commitErr != nil {
		return result.Err[importRecordOutcome](commitErr)
	}

	return result.Ok(importRecordOutcome{status: status})
}

func (store *Store) findImportRecordIdentity(
	ctx context.Context,
	tx pgx.Tx,
	record domain.ImportRecord,
) (importRecordIdentity, error) {
	query := `
select id::text, status
from import_records
where project_id = $1
  and source_system = $2
  and external_id = $3
for update
`
	var identity importRecordIdentity
	scanErr := tx.QueryRow(
		ctx,
		query,
		record.Event().ProjectID().String(),
		record.SourceSystem().String(),
		record.ExternalID().String(),
	).Scan(&identity.id, &identity.status)
	if errors.Is(scanErr, pgx.ErrNoRows) {
		return importRecordIdentity{}, nil
	}

	if scanErr != nil {
		return importRecordIdentity{}, scanErr
	}

	return identity, nil
}

func (store *Store) prepareImportRecord(
	ctx context.Context,
	tx pgx.Tx,
	runID string,
	rowNumber int,
	record domain.ImportRecord,
	identity importRecordIdentity,
) (string, error) {
	if identity.id != "" {
		return store.retryFailedImportRecord(ctx, tx, runID, rowNumber, identity.id)
	}

	return store.insertPendingImportRecord(ctx, tx, runID, rowNumber, record)
}

func (store *Store) retryFailedImportRecord(
	ctx context.Context,
	tx pgx.Tx,
	runID string,
	rowNumber int,
	recordID string,
) (string, error) {
	query := `
update import_records
set
  import_run_id = $2,
  row_number = $3,
  status = 'pending',
  error = null,
  updated_at = $4
where id = $1
returning id::text
`
	var updatedID string
	scanErr := tx.QueryRow(
		ctx,
		query,
		recordID,
		runID,
		rowNumber,
		time.Now().UTC(),
	).Scan(&updatedID)
	if scanErr != nil {
		return "", scanErr
	}

	return updatedID, nil
}

func (store *Store) insertPendingImportRecord(
	ctx context.Context,
	tx pgx.Tx,
	runID string,
	rowNumber int,
	record domain.ImportRecord,
) (string, error) {
	recordID, recordIDErr := randomUUID()
	if recordIDErr != nil {
		return "", recordIDErr
	}

	now := time.Now().UTC()
	query := `
insert into import_records (
  id,
  import_run_id,
  organization_id,
  project_id,
  source_system,
  external_id,
  record_kind,
  status,
  row_number,
  created_at,
  updated_at
) values (
  $1, $2, $3, $4, $5, $6, $7, 'pending', $8, $9, $9
)
returning id::text
`
	var insertedID string
	scanErr := tx.QueryRow(
		ctx,
		query,
		recordID,
		runID,
		record.Event().OrganizationID().String(),
		record.Event().ProjectID().String(),
		record.SourceSystem().String(),
		record.ExternalID().String(),
		string(record.Kind()),
		rowNumber,
		now,
	).Scan(&insertedID)
	if scanErr != nil {
		return "", scanErr
	}

	return insertedID, nil
}

func (store *Store) updateImportRecordFromReceipt(
	ctx context.Context,
	tx pgx.Tx,
	recordID string,
	record domain.ImportRecord,
	receipt ingest.IngestReceipt,
	status string,
	reason string,
) error {
	issueID, hasIssueID := receipt.IssueID()
	query := `
update import_records
set
  status = $2,
  event_id = $3,
  issue_id = $4,
  error = $5,
  updated_at = $6
where id = $1
`
	_, execErr := tx.Exec(
		ctx,
		query,
		recordID,
		status,
		record.Event().EventID().String(),
		nullableIssueID(issueID, hasIssueID),
		nullableText(reason),
		time.Now().UTC(),
	)

	return execErr
}

func nullableIssueID(issueID domain.IssueID, ok bool) any {
	if !ok {
		return nil
	}

	return issueID.String()
}

func importRecordStatusForReceipt(receipt ingest.IngestReceipt) (string, string) {
	if receipt.Kind() == ingest.ReceiptAcceptedIssueEvent {
		return importRecordStatusApplied, ""
	}

	if receipt.Kind() == ingest.ReceiptAcceptedNonIssueEvent {
		return importRecordStatusApplied, ""
	}

	if receipt.Kind() == ingest.ReceiptDuplicateEvent {
		return importRecordStatusDuplicate, ""
	}

	if receipt.Kind() == ingest.ReceiptQuotaRejected {
		return importRecordStatusFailed, receipt.Reason()
	}

	return importRecordStatusFailed, "unknown import receipt"
}

func (transaction importIngestTransaction) Run(
	ctx context.Context,
	program ingest.IngestProgram,
) result.Result[ingest.IngestTransactionResult] {
	ports := importIngestPorts{txStore: txStore{tx: transaction.tx}}

	return program(ctx, ports)
}

func (ports importIngestPorts) EnqueueIssueOpened(
	ctx context.Context,
	event ingestplan.AcceptedEvent,
	change ingest.IssueChange,
) result.Result[ingest.IssueOpenedEnqueueResult] {
	return result.Ok(ingest.NewIssueOpenedEnqueueResult(0))
}

func countImportOutcome(
	outcome importer.ApplyResult,
	row importRecordOutcome,
) importer.ApplyResult {
	if row.status == importRecordStatusApplied {
		outcome.AppliedRows++
		return outcome
	}

	if row.status == importRecordStatusDuplicate {
		outcome.DuplicateRows++
		return outcome
	}

	if row.status == importRecordStatusSkipped {
		outcome.SkippedRows++
		return outcome
	}

	if row.status == importRecordStatusFailed {
		outcome.FailedRows++
		return outcome
	}

	return outcome
}

func (store *Store) finishImportRun(
	ctx context.Context,
	command importer.ApplyCommand,
	outcome importer.ApplyResult,
) error {
	tx, beginErr := store.pool.BeginTx(ctx, pgx.TxOptions{})
	if beginErr != nil {
		return beginErr
	}

	status := importRunStatus(outcome)
	updateErr := updateImportRun(ctx, tx, outcome, status)
	if updateErr != nil {
		_ = tx.Rollback(ctx)
		return updateErr
	}

	auditErr := insertAuditEvent(ctx, tx, auditEventInput{
		OrganizationID: command.Manifest.OrganizationID().String(),
		ProjectID:      command.Manifest.ProjectID().String(),
		ActorID:        command.ActorID,
		Action:         auditapp.ActionImportRunCompleted,
		TargetType:     "import_run",
		TargetID:       outcome.RunID,
		Metadata: map[string]string{
			"source_system":  command.Manifest.SourceSystem().String(),
			"status":         status,
			"total_rows":     strconv.Itoa(outcome.TotalRows),
			"applied_rows":   strconv.Itoa(outcome.AppliedRows),
			"duplicate_rows": strconv.Itoa(outcome.DuplicateRows),
			"skipped_rows":   strconv.Itoa(outcome.SkippedRows),
			"failed_rows":    strconv.Itoa(outcome.FailedRows),
		},
	})
	if auditErr != nil {
		_ = tx.Rollback(ctx)
		return auditErr
	}

	return tx.Commit(ctx)
}

func updateImportRun(
	ctx context.Context,
	tx pgx.Tx,
	outcome importer.ApplyResult,
	status string,
) error {
	query := `
update import_runs
set
  status = $2,
  applied_rows = $3,
  duplicate_rows = $4,
  skipped_rows = $5,
  failed_rows = $6,
  finished_at = $7
where id = $1
`
	_, execErr := tx.Exec(
		ctx,
		query,
		outcome.RunID,
		status,
		outcome.AppliedRows,
		outcome.DuplicateRows,
		outcome.SkippedRows,
		outcome.FailedRows,
		time.Now().UTC(),
	)

	return execErr
}

func importRunStatus(outcome importer.ApplyResult) string {
	if outcome.FailedRows > 0 {
		return "completed_with_errors"
	}

	return "completed"
}

func (store *Store) markImportRunFailed(ctx context.Context, runID string) error {
	query := `
update import_runs
set status = 'failed',
    finished_at = $2
where id = $1
`
	_, execErr := store.pool.Exec(ctx, query, runID, time.Now().UTC())

	return execErr
}

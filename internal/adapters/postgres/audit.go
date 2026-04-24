package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	auditapp "github.com/ivanzakutnii/error-tracker/internal/app/audit"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type auditEventInput struct {
	OrganizationID string
	ProjectID      string
	ActorID        string
	Action         auditapp.Action
	TargetType     string
	TargetID       string
	Metadata       map[string]string
}

func (store *Store) ListAuditEvents(
	ctx context.Context,
	query auditapp.Query,
) result.Result[auditapp.View] {
	sqlQuery := `
select
  ae.id,
  ae.action,
  coalesce(o.email, 'system'),
  ae.target_type || ':' || ae.target_id,
  ae.metadata::text,
  ae.created_at
from audit_events ae
left join operators o on o.id = ae.actor_operator_id
where ae.organization_id = $1
  and ae.project_id = $2
order by ae.created_at desc
limit $3
`
	rows, queryErr := store.pool.Query(
		ctx,
		sqlQuery,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
		query.Limit,
	)
	if queryErr != nil {
		return result.Err[auditapp.View](queryErr)
	}
	defer rows.Close()

	view := auditapp.View{}
	for rows.Next() {
		event, scanErr := scanAuditEvent(rows)
		if scanErr != nil {
			return result.Err[auditapp.View](scanErr)
		}

		view.Events = append(view.Events, event)
	}

	if rowsErr := rows.Err(); rowsErr != nil {
		return result.Err[auditapp.View](rowsErr)
	}

	return result.Ok(view)
}

func insertAuditEvent(
	ctx context.Context,
	tx pgx.Tx,
	input auditEventInput,
) error {
	eventID, eventIDErr := randomUUID()
	if eventIDErr != nil {
		return eventIDErr
	}

	metadata, metadataErr := json.Marshal(input.Metadata)
	if metadataErr != nil {
		return metadataErr
	}

	query := `
insert into audit_events (
  id,
  organization_id,
  project_id,
  actor_operator_id,
  action,
  target_type,
  target_id,
  metadata,
  created_at
) values (
  $1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9
)
`
	_, execErr := tx.Exec(
		ctx,
		query,
		eventID,
		input.OrganizationID,
		nullString(input.ProjectID),
		nullString(input.ActorID),
		string(input.Action),
		input.TargetType,
		input.TargetID,
		string(metadata),
		time.Now().UTC(),
	)

	return execErr
}

func scanAuditEvent(rows pgx.Rows) (auditapp.EventView, error) {
	var event auditapp.EventView
	var createdAt time.Time
	scanErr := rows.Scan(
		&event.ID,
		&event.Action,
		&event.Actor,
		&event.Target,
		&event.Metadata,
		&createdAt,
	)
	if scanErr != nil {
		return auditapp.EventView{}, scanErr
	}

	event.CreatedAt = formatTime(createdAt)

	return event, nil
}

func nullString(value string) sql.NullString {
	return sql.NullString{
		String: value,
		Valid:  value != "",
	}
}

package postgres

import (
	"context"
	"time"

	projectapp "github.com/ivanzakutnii/error-tracker/internal/app/projects"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

func (store *Store) FindCurrentProject(
	ctx context.Context,
	query projectapp.ProjectQuery,
) result.Result[projectapp.ProjectRecord] {
	sql := `
select
  o.slug,
  o.name,
  p.id,
  p.name,
  p.slug,
  p.ingest_ref,
  p.accepting_events,
  p.scrub_ip_addresses,
  p.first_event_at,
  p.created_at,
  k.id,
  k.public_key,
  k.label,
  k.created_at
from projects p
join organizations o on o.id = p.organization_id
join project_keys k on k.project_id = p.id and k.active = true
where p.organization_id = $1
  and p.id = $2
order by k.created_at asc
limit 1
`
	var record projectapp.ProjectRecord
	var projectIDText string
	var keyIDText string
	var publicKeyText string
	var firstEventAt *time.Time
	scanErr := store.pool.QueryRow(
		ctx,
		sql,
		query.Scope.OrganizationID.String(),
		query.Scope.ProjectID.String(),
	).Scan(
		&record.OrganizationSlug,
		&record.OrganizationName,
		&projectIDText,
		&record.Name,
		&record.Slug,
		&record.IngestRef,
		&record.AcceptingEvents,
		&record.ScrubIPAddresses,
		&firstEventAt,
		&record.CreatedAt,
		&keyIDText,
		&publicKeyText,
		&record.ActiveKey.Label,
		&record.ActiveKey.CreatedAt,
	)
	if scanErr != nil {
		return result.Err[projectapp.ProjectRecord](scanErr)
	}

	projectID, projectIDErr := domain.NewProjectID(projectIDText)
	if projectIDErr != nil {
		return result.Err[projectapp.ProjectRecord](projectIDErr)
	}

	keyID, keyIDErr := domain.NewProjectKeyID(keyIDText)
	if keyIDErr != nil {
		return result.Err[projectapp.ProjectRecord](keyIDErr)
	}

	publicKey, publicKeyErr := domain.NewProjectPublicKey(publicKeyText)
	if publicKeyErr != nil {
		return result.Err[projectapp.ProjectRecord](publicKeyErr)
	}

	record.ProjectID = projectID
	record.FirstEventAt = firstEventAt
	record.ActiveKey.ID = keyID
	record.ActiveKey.PublicKey = publicKey

	return result.Ok(record)
}

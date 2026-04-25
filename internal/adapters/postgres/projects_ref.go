package postgres

import (
	"context"
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

type ProjectScope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

func (store *Store) LookupProjectByRef(
	ctx context.Context,
	projectRef string,
) (ProjectScope, error) {
	ref, refErr := domain.NewProjectRef(projectRef)
	if refErr != nil {
		return ProjectScope{}, refErr
	}

	resolved, lookupErr := store.findProjectByRef(ctx, ref)
	if lookupErr != nil {
		return ProjectScope{}, lookupErr
	}

	if resolved.OrganizationID.String() == "" || resolved.ProjectID.String() == "" {
		return ProjectScope{}, errors.New("project not found")
	}

	return ProjectScope{
		OrganizationID: resolved.OrganizationID,
		ProjectID:      resolved.ProjectID,
	}, nil
}

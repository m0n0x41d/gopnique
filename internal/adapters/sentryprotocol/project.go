package sentryprotocol

import "github.com/ivanzakutnii/error-tracker/internal/domain"

type ProjectContext struct {
	organizationID domain.OrganizationID
	projectID      domain.ProjectID
	scrubIP        bool
}

func NewProjectContext(
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
) ProjectContext {
	return NewProjectContextWithPrivacy(organizationID, projectID, true)
}

func NewProjectContextWithPrivacy(
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	scrubIP bool,
) ProjectContext {
	return ProjectContext{
		organizationID: organizationID,
		projectID:      projectID,
		scrubIP:        scrubIP,
	}
}

func (project ProjectContext) OrganizationID() domain.OrganizationID {
	return project.organizationID
}

func (project ProjectContext) ProjectID() domain.ProjectID {
	return project.projectID
}

func (project ProjectContext) ScrubIPAddresses() bool {
	return project.scrubIP
}

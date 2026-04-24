package domain

import (
	"errors"
	"strings"
)

type ProjectRef struct {
	value string
}

type ProjectPublicKey struct {
	value string
}

type ProjectAuth struct {
	organizationID OrganizationID
	projectID      ProjectID
	scrubIP        bool
}

func NewProjectRef(input string) (ProjectRef, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return ProjectRef{}, errors.New("project ref is required")
	}

	return ProjectRef{value: value}, nil
}

func NewProjectPublicKey(input string) (ProjectPublicKey, error) {
	value, err := normalizeUUID(input)
	if err != nil {
		return ProjectPublicKey{}, err
	}

	return ProjectPublicKey{value: value}, nil
}

func NewProjectAuth(organizationID OrganizationID, projectID ProjectID) (ProjectAuth, error) {
	return NewProjectAuthWithPolicy(organizationID, projectID, true)
}

func NewProjectAuthWithPolicy(
	organizationID OrganizationID,
	projectID ProjectID,
	scrubIP bool,
) (ProjectAuth, error) {
	if organizationID.value == "" {
		return ProjectAuth{}, errors.New("organization id is required")
	}

	if projectID.value == "" {
		return ProjectAuth{}, errors.New("project id is required")
	}

	return ProjectAuth{
		organizationID: organizationID,
		projectID:      projectID,
		scrubIP:        scrubIP,
	}, nil
}

func (ref ProjectRef) String() string {
	return ref.value
}

func (key ProjectPublicKey) String() string {
	return dashedUUID(key.value)
}

func (key ProjectPublicKey) Hex() string {
	return key.value
}

func (auth ProjectAuth) OrganizationID() OrganizationID {
	return auth.organizationID
}

func (auth ProjectAuth) ProjectID() ProjectID {
	return auth.projectID
}

func (auth ProjectAuth) ScrubIPAddresses() bool {
	return auth.scrubIP
}

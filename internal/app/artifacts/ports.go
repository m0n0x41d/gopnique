package artifacts

import (
	"context"
	"errors"
	"io"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

var (
	ErrArtifactNotFound = errors.New("artifact not found")
)

type StoredArtifact struct {
	key  domain.ArtifactKey
	size int64
}

func NewStoredArtifact(key domain.ArtifactKey, size int64) StoredArtifact {
	return StoredArtifact{key: key, size: size}
}

func (artifact StoredArtifact) Key() domain.ArtifactKey {
	return artifact.key
}

func (artifact StoredArtifact) Size() int64 {
	return artifact.size
}

type ArtifactScope struct {
	organizationID domain.OrganizationID
	projectID      domain.ProjectID
	kind           domain.ArtifactKind
}

func NewArtifactScope(
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	kind domain.ArtifactKind,
) (ArtifactScope, error) {
	if organizationID.String() == "" {
		return ArtifactScope{}, errors.New("artifact scope requires organization id")
	}

	if projectID.String() == "" {
		return ArtifactScope{}, errors.New("artifact scope requires project id")
	}

	if kind.String() == "" {
		return ArtifactScope{}, errors.New("artifact scope requires kind")
	}

	return ArtifactScope{
		organizationID: organizationID,
		projectID:      projectID,
		kind:           kind,
	}, nil
}

func (scope ArtifactScope) OrganizationID() domain.OrganizationID {
	return scope.organizationID
}

func (scope ArtifactScope) ProjectID() domain.ProjectID {
	return scope.projectID
}

func (scope ArtifactScope) Kind() domain.ArtifactKind {
	return scope.kind
}

type ArtifactVault interface {
	PutArtifact(
		ctx context.Context,
		key domain.ArtifactKey,
		contents io.Reader,
	) result.Result[StoredArtifact]
	GetArtifact(
		ctx context.Context,
		key domain.ArtifactKey,
	) result.Result[io.ReadCloser]
	DeleteArtifact(
		ctx context.Context,
		key domain.ArtifactKey,
	) result.Result[struct{}]
	ListArtifacts(
		ctx context.Context,
		scope ArtifactScope,
	) result.Result[[]StoredArtifact]
}

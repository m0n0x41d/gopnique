package sourcemaps

import (
	"context"
	"errors"
	"io"

	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const sourceMapMaxBytes = 64 * 1024 * 1024

var (
	ErrSourceMapNotFound = errors.New("source map not found")
	ErrSourceMapTooLarge = errors.New("source map exceeds size limit")
)

type Stored struct {
	identity domain.SourceMapIdentity
	size     int64
}

func (stored Stored) Identity() domain.SourceMapIdentity {
	return stored.identity
}

func (stored Stored) Size() int64 {
	return stored.size
}

type Service struct {
	vault artifacts.ArtifactVault
}

func NewService(vault artifacts.ArtifactVault) (*Service, error) {
	if vault == nil {
		return nil, errors.New("source map service requires artifact vault")
	}

	return &Service{vault: vault}, nil
}

func (service *Service) Upload(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	identity domain.SourceMapIdentity,
	contents io.Reader,
) result.Result[Stored] {
	if contents == nil {
		return result.Err[Stored](errors.New("source map contents are required"))
	}

	key, keyErr := domain.NewArtifactKey(
		organizationID,
		projectID,
		domain.ArtifactKindSourceMap(),
		identity.ArtifactName(),
	)
	if keyErr != nil {
		return result.Err[Stored](keyErr)
	}

	limited := io.LimitReader(contents, sourceMapMaxBytes+1)

	putResult := service.vault.PutArtifact(ctx, key, limited)
	put, putErr := putResult.Value()
	if putErr != nil {
		return result.Err[Stored](putErr)
	}

	if put.Size() > sourceMapMaxBytes {
		_ = service.vault.DeleteArtifact(ctx, key)
		return result.Err[Stored](ErrSourceMapTooLarge)
	}

	return result.Ok(Stored{identity: identity, size: put.Size()})
}

func (service *Service) Load(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	identity domain.SourceMapIdentity,
) result.Result[SourceMap] {
	key, keyErr := domain.NewArtifactKey(
		organizationID,
		projectID,
		domain.ArtifactKindSourceMap(),
		identity.ArtifactName(),
	)
	if keyErr != nil {
		return result.Err[SourceMap](keyErr)
	}

	getResult := service.vault.GetArtifact(ctx, key)
	body, getErr := getResult.Value()
	if getErr != nil {
		if errors.Is(getErr, artifacts.ErrArtifactNotFound) {
			return result.Err[SourceMap](ErrSourceMapNotFound)
		}
		return result.Err[SourceMap](getErr)
	}
	defer body.Close()

	payload, readErr := io.ReadAll(io.LimitReader(body, sourceMapMaxBytes+1))
	if readErr != nil {
		return result.Err[SourceMap](readErr)
	}

	if len(payload) > sourceMapMaxBytes {
		return result.Err[SourceMap](ErrSourceMapTooLarge)
	}

	parsed, parseErr := ParseSourceMap(payload)
	if parseErr != nil {
		return result.Err[SourceMap](parseErr)
	}

	return result.Ok(parsed)
}

func (service *Service) Resolve(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	identity domain.SourceMapIdentity,
	position GeneratedPosition,
) result.Result[ResolvedFrame] {
	loadResult := service.Load(ctx, organizationID, projectID, identity)
	loaded, loadErr := loadResult.Value()
	if loadErr != nil {
		return result.Err[ResolvedFrame](loadErr)
	}

	resolved, ok := LookupFrame(loaded, position)
	if !ok {
		return result.Err[ResolvedFrame](ErrSourceMapNotFound)
	}

	return result.Ok(resolved)
}

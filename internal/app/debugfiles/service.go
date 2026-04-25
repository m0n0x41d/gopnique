package debugfiles

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const (
	debugFileMaxBytes        = 256 * 1024 * 1024
	debugFileSniffPrefixSize = 64
)

var (
	ErrDebugFileNotFound = errors.New("debug file not found")
	ErrDebugFileTooLarge = errors.New("debug file exceeds size limit")
)

type Stored struct {
	identity domain.DebugFileIdentity
	size     int64
}

func (stored Stored) Identity() domain.DebugFileIdentity {
	return stored.identity
}

func (stored Stored) Size() int64 {
	return stored.size
}

type Listing struct {
	storedKey domain.ArtifactKey
	size      int64
}

func (listing Listing) ArtifactKey() domain.ArtifactKey {
	return listing.storedKey
}

func (listing Listing) Size() int64 {
	return listing.size
}

type Service struct {
	vault artifacts.ArtifactVault
}

func NewService(vault artifacts.ArtifactVault) (*Service, error) {
	if vault == nil {
		return nil, errors.New("debug file service requires artifact vault")
	}

	return &Service{vault: vault}, nil
}

func (service *Service) Upload(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	identity domain.DebugFileIdentity,
	contents io.Reader,
) result.Result[Stored] {
	if contents == nil {
		return result.Err[Stored](errors.New("debug file contents are required"))
	}

	prefix := make([]byte, debugFileSniffPrefixSize)
	prefixCount, prefixErr := io.ReadFull(contents, prefix)
	if prefixErr != nil && !errors.Is(prefixErr, io.EOF) && !errors.Is(prefixErr, io.ErrUnexpectedEOF) {
		return result.Err[Stored](prefixErr)
	}
	prefix = prefix[:prefixCount]

	matchErr := MatchesDeclaredKind(identity.Kind(), prefix)
	if matchErr != nil {
		return result.Err[Stored](matchErr)
	}

	key, keyErr := domain.NewArtifactKey(
		organizationID,
		projectID,
		domain.ArtifactKindDebugFile(),
		identity.ArtifactName(),
	)
	if keyErr != nil {
		return result.Err[Stored](keyErr)
	}

	combined := io.MultiReader(bytes.NewReader(prefix), contents)
	limited := io.LimitReader(combined, debugFileMaxBytes+1)

	putResult := service.vault.PutArtifact(ctx, key, limited)
	put, putErr := putResult.Value()
	if putErr != nil {
		return result.Err[Stored](putErr)
	}

	if put.Size() > debugFileMaxBytes {
		_ = service.vault.DeleteArtifact(ctx, key)
		return result.Err[Stored](ErrDebugFileTooLarge)
	}

	return result.Ok(Stored{identity: identity, size: put.Size()})
}

func (service *Service) Get(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	identity domain.DebugFileIdentity,
) result.Result[io.ReadCloser] {
	key, keyErr := domain.NewArtifactKey(
		organizationID,
		projectID,
		domain.ArtifactKindDebugFile(),
		identity.ArtifactName(),
	)
	if keyErr != nil {
		return result.Err[io.ReadCloser](keyErr)
	}

	getResult := service.vault.GetArtifact(ctx, key)
	body, getErr := getResult.Value()
	if getErr != nil {
		if errors.Is(getErr, artifacts.ErrArtifactNotFound) {
			return result.Err[io.ReadCloser](ErrDebugFileNotFound)
		}
		return result.Err[io.ReadCloser](getErr)
	}

	return result.Ok(body)
}

func (service *Service) List(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
) result.Result[[]Listing] {
	scope, scopeErr := artifacts.NewArtifactScope(
		organizationID,
		projectID,
		domain.ArtifactKindDebugFile(),
	)
	if scopeErr != nil {
		return result.Err[[]Listing](scopeErr)
	}

	listResult := service.vault.ListArtifacts(ctx, scope)
	stored, listErr := listResult.Value()
	if listErr != nil {
		return result.Err[[]Listing](listErr)
	}

	listings := make([]Listing, 0, len(stored))
	for _, item := range stored {
		listings = append(listings, Listing{storedKey: item.Key(), size: item.Size()})
	}

	return result.Ok(listings)
}

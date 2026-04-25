package minidumps

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
	minidumpMaxBytes        = 100 * 1024 * 1024
	minidumpSniffPrefixSize = 16
)

var (
	ErrMinidumpNotFound = errors.New("minidump not found")
	ErrMinidumpTooLarge = errors.New("minidump exceeds size limit")
)

type Stored struct {
	identity domain.MinidumpIdentity
	size     int64
}

func (stored Stored) Identity() domain.MinidumpIdentity {
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
		return nil, errors.New("minidump service requires artifact vault")
	}

	return &Service{vault: vault}, nil
}

func (service *Service) Upload(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	identity domain.MinidumpIdentity,
	contents io.Reader,
) result.Result[Stored] {
	if contents == nil {
		return result.Err[Stored](errors.New("minidump contents are required"))
	}

	prefix := make([]byte, minidumpSniffPrefixSize)
	prefixCount, prefixErr := io.ReadFull(contents, prefix)
	if prefixErr != nil && !errors.Is(prefixErr, io.EOF) && !errors.Is(prefixErr, io.ErrUnexpectedEOF) {
		return result.Err[Stored](prefixErr)
	}
	prefix = prefix[:prefixCount]

	detectErr := DetectMinidump(prefix)
	if detectErr != nil {
		return result.Err[Stored](detectErr)
	}

	key, keyErr := domain.NewArtifactKey(
		organizationID,
		projectID,
		domain.ArtifactKindMinidump(),
		identity.ArtifactName(),
	)
	if keyErr != nil {
		return result.Err[Stored](keyErr)
	}

	combined := io.MultiReader(bytes.NewReader(prefix), contents)
	limited := io.LimitReader(combined, minidumpMaxBytes+1)

	putResult := service.vault.PutArtifact(ctx, key, limited)
	put, putErr := putResult.Value()
	if putErr != nil {
		return result.Err[Stored](putErr)
	}

	if put.Size() > minidumpMaxBytes {
		_ = service.vault.DeleteArtifact(ctx, key)
		return result.Err[Stored](ErrMinidumpTooLarge)
	}

	return result.Ok(Stored{identity: identity, size: put.Size()})
}

func (service *Service) Get(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	identity domain.MinidumpIdentity,
) result.Result[io.ReadCloser] {
	key, keyErr := domain.NewArtifactKey(
		organizationID,
		projectID,
		domain.ArtifactKindMinidump(),
		identity.ArtifactName(),
	)
	if keyErr != nil {
		return result.Err[io.ReadCloser](keyErr)
	}

	getResult := service.vault.GetArtifact(ctx, key)
	body, getErr := getResult.Value()
	if getErr != nil {
		if errors.Is(getErr, artifacts.ErrArtifactNotFound) {
			return result.Err[io.ReadCloser](ErrMinidumpNotFound)
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
		domain.ArtifactKindMinidump(),
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

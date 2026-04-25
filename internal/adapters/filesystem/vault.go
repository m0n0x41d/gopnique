package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const (
	dirPermissions  os.FileMode = 0o755
	filePermissions os.FileMode = 0o644
	tempFilePrefix              = ".artifact-"
)

type Vault struct {
	root string
}

func NewVault(root string) (*Vault, error) {
	cleaned := strings.TrimSpace(root)
	if cleaned == "" {
		return nil, errors.New("artifact root is required")
	}

	if !filepath.IsAbs(cleaned) {
		return nil, errors.New("artifact root must be an absolute path")
	}

	resolved := filepath.Clean(cleaned)

	mkdirErr := os.MkdirAll(resolved, dirPermissions)
	if mkdirErr != nil {
		return nil, fmt.Errorf("create artifact root: %w", mkdirErr)
	}

	info, statErr := os.Stat(resolved)
	if statErr != nil {
		return nil, fmt.Errorf("stat artifact root: %w", statErr)
	}

	if !info.IsDir() {
		return nil, errors.New("artifact root must be a directory")
	}

	return &Vault{root: resolved}, nil
}

func (vault *Vault) Root() string {
	return vault.root
}

func (vault *Vault) PutArtifact(
	ctx context.Context,
	key domain.ArtifactKey,
	contents io.Reader,
) result.Result[artifacts.StoredArtifact] {
	if contents == nil {
		return result.Err[artifacts.StoredArtifact](errors.New("artifact contents are required"))
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return result.Err[artifacts.StoredArtifact](ctxErr)
	}

	finalPath, pathErr := vault.keyPath(key)
	if pathErr != nil {
		return result.Err[artifacts.StoredArtifact](pathErr)
	}

	parentDir := filepath.Dir(finalPath)

	mkdirErr := os.MkdirAll(parentDir, dirPermissions)
	if mkdirErr != nil {
		return result.Err[artifacts.StoredArtifact](fmt.Errorf("create artifact directory: %w", mkdirErr))
	}

	tempFile, tempErr := os.CreateTemp(parentDir, tempFilePrefix)
	if tempErr != nil {
		return result.Err[artifacts.StoredArtifact](fmt.Errorf("create temp artifact: %w", tempErr))
	}

	tempPath := tempFile.Name()

	written, copyErr := io.Copy(tempFile, contents)
	closeErr := tempFile.Close()

	if copyErr != nil {
		_ = os.Remove(tempPath)
		return result.Err[artifacts.StoredArtifact](fmt.Errorf("write artifact: %w", copyErr))
	}

	if closeErr != nil {
		_ = os.Remove(tempPath)
		return result.Err[artifacts.StoredArtifact](fmt.Errorf("close artifact: %w", closeErr))
	}

	chmodErr := os.Chmod(tempPath, filePermissions)
	if chmodErr != nil {
		_ = os.Remove(tempPath)
		return result.Err[artifacts.StoredArtifact](fmt.Errorf("set artifact permissions: %w", chmodErr))
	}

	renameErr := os.Rename(tempPath, finalPath)
	if renameErr != nil {
		_ = os.Remove(tempPath)
		return result.Err[artifacts.StoredArtifact](fmt.Errorf("publish artifact: %w", renameErr))
	}

	return result.Ok(artifacts.NewStoredArtifact(key, written))
}

func (vault *Vault) GetArtifact(
	ctx context.Context,
	key domain.ArtifactKey,
) result.Result[io.ReadCloser] {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result.Err[io.ReadCloser](ctxErr)
	}

	finalPath, pathErr := vault.keyPath(key)
	if pathErr != nil {
		return result.Err[io.ReadCloser](pathErr)
	}

	file, openErr := os.Open(finalPath)
	if openErr != nil {
		if errors.Is(openErr, os.ErrNotExist) {
			return result.Err[io.ReadCloser](artifacts.ErrArtifactNotFound)
		}
		return result.Err[io.ReadCloser](fmt.Errorf("open artifact: %w", openErr))
	}

	return result.Ok[io.ReadCloser](file)
}

func (vault *Vault) DeleteArtifact(
	ctx context.Context,
	key domain.ArtifactKey,
) result.Result[struct{}] {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result.Err[struct{}](ctxErr)
	}

	finalPath, pathErr := vault.keyPath(key)
	if pathErr != nil {
		return result.Err[struct{}](pathErr)
	}

	removeErr := os.Remove(finalPath)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return result.Err[struct{}](fmt.Errorf("delete artifact: %w", removeErr))
	}

	return result.Ok(struct{}{})
}

func (vault *Vault) ListArtifacts(
	ctx context.Context,
	scope artifacts.ArtifactScope,
) result.Result[[]artifacts.StoredArtifact] {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result.Err[[]artifacts.StoredArtifact](ctxErr)
	}

	scopeDir, scopeErr := vault.scopePath(scope)
	if scopeErr != nil {
		return result.Err[[]artifacts.StoredArtifact](scopeErr)
	}

	entries, readErr := os.ReadDir(scopeDir)
	if readErr != nil {
		if errors.Is(readErr, os.ErrNotExist) {
			return result.Ok[[]artifacts.StoredArtifact](nil)
		}
		return result.Err[[]artifacts.StoredArtifact](fmt.Errorf("list artifacts: %w", readErr))
	}

	stored := make([]artifacts.StoredArtifact, 0, len(entries))

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		entryName := entry.Name()
		if strings.HasPrefix(entryName, tempFilePrefix) {
			continue
		}

		artifactName, nameErr := domain.NewArtifactName(entryName)
		if nameErr != nil {
			continue
		}

		artifactKey, keyErr := domain.NewArtifactKey(
			scope.OrganizationID(),
			scope.ProjectID(),
			scope.Kind(),
			artifactName,
		)
		if keyErr != nil {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			continue
		}

		stored = append(stored, artifacts.NewStoredArtifact(artifactKey, info.Size()))
	}

	sort.Slice(stored, func(left int, right int) bool {
		return stored[left].Key().Name().String() < stored[right].Key().Name().String()
	})

	return result.Ok(stored)
}

func (vault *Vault) keyPath(key domain.ArtifactKey) (string, error) {
	if key.Name().String() == "" || key.Kind().String() == "" {
		return "", errors.New("artifact key is incomplete")
	}

	scopeDir, scopeErr := vault.scopeDirFromKey(key)
	if scopeErr != nil {
		return "", scopeErr
	}

	candidate := filepath.Join(scopeDir, key.Name().String())
	cleaned := filepath.Clean(candidate)

	if !pathInside(vault.root, cleaned) {
		return "", errors.New("artifact path escapes root")
	}

	return cleaned, nil
}

func (vault *Vault) scopePath(scope artifacts.ArtifactScope) (string, error) {
	if scope.Kind().String() == "" {
		return "", errors.New("artifact scope is incomplete")
	}

	candidate := filepath.Join(
		vault.root,
		scope.OrganizationID().String(),
		scope.ProjectID().String(),
		scope.Kind().String(),
	)
	cleaned := filepath.Clean(candidate)

	if !pathInside(vault.root, cleaned) {
		return "", errors.New("artifact scope path escapes root")
	}

	return cleaned, nil
}

func (vault *Vault) scopeDirFromKey(key domain.ArtifactKey) (string, error) {
	candidate := filepath.Join(
		vault.root,
		key.OrganizationID().String(),
		key.ProjectID().String(),
		key.Kind().String(),
	)
	cleaned := filepath.Clean(candidate)

	if !pathInside(vault.root, cleaned) {
		return "", errors.New("artifact scope path escapes root")
	}

	return cleaned, nil
}

func pathInside(root string, candidate string) bool {
	relative, relErr := filepath.Rel(root, candidate)
	if relErr != nil {
		return false
	}

	if relative == "." {
		return true
	}

	if strings.HasPrefix(relative, "..") {
		return false
	}

	return !strings.Contains(relative, ".."+string(os.PathSeparator))
}

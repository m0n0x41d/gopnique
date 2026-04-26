package httpadapter

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	projectapp "github.com/ivanzakutnii/error-tracker/internal/app/projects"
	"github.com/ivanzakutnii/error-tracker/internal/app/sourcemaps"
	tokenapp "github.com/ivanzakutnii/error-tracker/internal/app/tokens"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const artifactUploadMultipartMemoryBytes = 8 * 1024 * 1024
const artifactUploadMultipartOverheadBytes = 1 * 1024 * 1024
const artifactChunkUploadBlobBytes = 32 * 1024 * 1024
const artifactChunkUploadMaxFileBytes = 2 * 1024 * 1024 * 1024
const artifactChunkUploadMaxRequestBytes = artifactChunkUploadBlobBytes
const artifactChunkUploadMaxFiles = 1

type artifactProjectAccess struct {
	organizationID   domain.OrganizationID
	projectID        domain.ProjectID
	organizationSlug string
	projectSlug      string
}

type artifactUploadFile struct {
	file       multipart.File
	fileHeader *multipart.FileHeader
	form       *multipart.Form
}

type sourceMapUploadResponse struct {
	Release      string `json:"release"`
	Dist         string `json:"dist"`
	Name         string `json:"name"`
	ArtifactName string `json:"artifact_name"`
	SizeBytes    int64  `json:"size_bytes"`
}

type debugFileUploadResponse struct {
	DebugID      string `json:"debugId"`
	Kind         string `json:"symbolType"`
	Name         string `json:"objectName"`
	ArtifactName string `json:"artifact_name"`
	SizeBytes    int64  `json:"size"`
}

type chunkUploadInfoResponse struct {
	URL              string   `json:"url"`
	ChunkSize        int64    `json:"chunkSize"`
	ChunksPerRequest int      `json:"chunksPerRequest"`
	MaxFileSize      int64    `json:"maxFileSize"`
	MaxRequestSize   int64    `json:"maxRequestSize"`
	Concurrency      int      `json:"concurrency"`
	HashAlgorithm    string   `json:"hashAlgorithm"`
	Compression      []string `json:"compression"`
	Accept           []string `json:"accept"`
}

type artifactBundleAssemblePayload struct {
	Checksum string   `json:"checksum"`
	Chunks   []string `json:"chunks"`
	Projects []string `json:"projects"`
	Version  string   `json:"version"`
	Dist     string   `json:"dist"`
}

type artifactAssembleResponse struct {
	State         string   `json:"state"`
	MissingChunks []string `json:"missingChunks"`
}

type artifactBundleManifest struct {
	Files   map[string]artifactBundleManifestFile `json:"files"`
	Org     string                                `json:"org"`
	Project string                                `json:"project"`
	Release string                                `json:"release"`
	Dist    string                                `json:"dist"`
}

type artifactBundleManifestFile struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type debugFileAssembleRequest map[string]debugFileAssembleFile

type debugFileAssembleFile struct {
	Name    string   `json:"name"`
	DebugID string   `json:"debug_id"`
	Chunks  []string `json:"chunks"`
}

func artifactChunkUploadInfoHandler(
	manager tokenapp.Manager,
	reader projectapp.Reader,
	vault artifacts.ArtifactVault,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if vault == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "artifact_vault_not_configured"})
			return
		}

		_, ok := requireArtifactOrganizationAccess(w, r, manager, reader)
		if !ok {
			return
		}

		writeJSON(w, http.StatusOK, chunkUploadInfoResponse{
			URL:              "/api/0/organizations/" + strings.TrimSpace(r.PathValue("organization_slug")) + "/chunk-upload/",
			ChunkSize:        artifactChunkUploadBlobBytes,
			ChunksPerRequest: artifactChunkUploadMaxFiles,
			MaxFileSize:      artifactChunkUploadMaxFileBytes,
			MaxRequestSize:   artifactChunkUploadMaxRequestBytes,
			Concurrency:      1,
			HashAlgorithm:    "sha1",
			Compression:      []string{"gzip"},
			Accept: []string{
				"debug_files",
				"release_files",
				"sources",
				"artifact_bundles",
			},
		})
	}
}

func artifactChunkUploadHandler(
	manager tokenapp.Manager,
	reader projectapp.Reader,
	vault artifacts.ArtifactVault,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleArtifactChunkUploadCarrier(w, r, manager, reader, vault)
	}
}

func artifactBundleAssembleHandler(
	manager tokenapp.Manager,
	reader projectapp.Reader,
	vault artifacts.ArtifactVault,
	store *sourcemaps.Service,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleArtifactBundleAssembleCarrier(w, r, manager, reader, vault, store)
	}
}

func debugFileAssembleHandler(
	manager tokenapp.Manager,
	reader projectapp.Reader,
	vault artifacts.ArtifactVault,
	store *debugfiles.Service,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleDebugFileAssembleCarrier(w, r, manager, reader, vault, store)
	}
}

func sourceMapUploadHandler(
	manager tokenapp.Manager,
	reader projectapp.Reader,
	store *sourcemaps.Service,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSourceMapUploadCarrier(w, r, manager, reader, store)
	}
}

func debugFileUploadHandler(
	manager tokenapp.Manager,
	reader projectapp.Reader,
	store *debugfiles.Service,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleDebugFileUploadCarrier(w, r, manager, reader, store)
	}
}

func debugFileReprocessingHandler(
	manager tokenapp.Manager,
	reader projectapp.Reader,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, ok := requireArtifactProjectAccess(w, r, manager, reader)
		if !ok {
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{})
	}
}

func handleSourceMapUploadCarrier(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	reader projectapp.Reader,
	store *sourcemaps.Service,
) {
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "source_map_store_not_configured"})
		return
	}

	access, ok := requireArtifactProjectAccess(w, r, manager, reader)
	if !ok {
		return
	}

	uploadResult := readArtifactMultipart(
		w,
		r,
		sourcemaps.MaxUploadBytes(),
		sourcemaps.ErrSourceMapTooLarge,
		[]string{"file", "source_map", "upload"},
	)
	defer cleanupMultipartForm(r)
	upload, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		writeJSON(w, sourceMapUploadHTTPStatus(uploadErr), map[string]string{"detail": sourceMapUploadDetail(uploadErr)})
		return
	}
	defer upload.file.Close()

	identityResult := sourceMapIdentityFromUpload(r, upload)
	identity, identityErr := identityResult.Value()
	if identityErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": identityErr.Error()})
		return
	}

	storedResult := store.Upload(
		r.Context(),
		access.organizationID,
		access.projectID,
		identity,
		upload.file,
	)
	stored, storeErr := storedResult.Value()
	if storeErr != nil {
		writeJSON(w, sourceMapUploadHTTPStatus(storeErr), map[string]string{"detail": sourceMapUploadDetail(storeErr)})
		return
	}

	writeJSON(w, http.StatusCreated, sourceMapUploadResponse{
		Release:      identity.Release().String(),
		Dist:         identity.Dist().String(),
		Name:         identity.FileName().String(),
		ArtifactName: identity.ArtifactName().String(),
		SizeBytes:    stored.Size(),
	})
}

func handleDebugFileUploadCarrier(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	reader projectapp.Reader,
	store *debugfiles.Service,
) {
	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "debug_file_store_not_configured"})
		return
	}

	access, ok := requireArtifactProjectAccess(w, r, manager, reader)
	if !ok {
		return
	}

	uploadResult := readArtifactMultipart(
		w,
		r,
		debugfiles.MaxUploadBytes(),
		debugfiles.ErrDebugFileTooLarge,
		[]string{"file", "debug_file", "dif", "dsym"},
	)
	defer cleanupMultipartForm(r)
	upload, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		writeJSON(w, debugFileUploadHTTPStatus(uploadErr), map[string]string{"detail": debugFileUploadDetail(uploadErr)})
		return
	}
	defer upload.file.Close()

	identityResult := debugFileIdentityFromUpload(upload)
	identity, identityErr := identityResult.Value()
	if identityErr != nil {
		writeJSON(w, debugFileUploadHTTPStatus(identityErr), map[string]string{"detail": debugFileUploadDetail(identityErr)})
		return
	}

	_, seekErr := upload.file.Seek(0, io.SeekStart)
	if seekErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid_upload_file"})
		return
	}

	storedResult := store.Upload(
		r.Context(),
		access.organizationID,
		access.projectID,
		identity,
		upload.file,
	)
	stored, storeErr := storedResult.Value()
	if storeErr != nil {
		writeJSON(w, debugFileUploadHTTPStatus(storeErr), map[string]string{"detail": debugFileUploadDetail(storeErr)})
		return
	}

	writeJSON(w, http.StatusCreated, debugFileUploadResponse{
		DebugID:      identity.DebugID().String(),
		Kind:         identity.Kind().String(),
		Name:         identity.FileName().String(),
		ArtifactName: identity.ArtifactName().String(),
		SizeBytes:    stored.Size(),
	})
}

func handleArtifactChunkUploadCarrier(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	reader projectapp.Reader,
	vault artifacts.ArtifactVault,
) {
	if vault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "artifact_vault_not_configured"})
		return
	}

	access, ok := requireArtifactOrganizationAccess(w, r, manager, reader)
	if !ok {
		return
	}

	maxRequestBytes := int64(artifactChunkUploadMaxRequestBytes + artifactUploadMultipartOverheadBytes)
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

	parseErr := r.ParseMultipartForm(artifactUploadMultipartMemoryBytes)
	defer cleanupMultipartForm(r)
	if parseErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid_multipart"})
		return
	}

	files := r.MultipartForm.File["file_gzip"]
	if len(files) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{})
		return
	}

	if len(files) > artifactChunkUploadMaxFiles {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "too_many_chunks"})
		return
	}

	for _, fileHeader := range files {
		uploadErr := storeArtifactChunk(r, vault, access, fileHeader)
		if uploadErr != nil {
			writeJSON(w, artifactChunkUploadHTTPStatus(uploadErr), map[string]string{"detail": artifactChunkUploadDetail(uploadErr)})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{})
}

func handleArtifactBundleAssembleCarrier(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	reader projectapp.Reader,
	vault artifacts.ArtifactVault,
	store *sourcemaps.Service,
) {
	if vault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "artifact_vault_not_configured"})
		return
	}

	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "source_map_store_not_configured"})
		return
	}

	access, ok := requireArtifactOrganizationAccess(w, r, manager, reader)
	if !ok {
		return
	}

	var payload artifactBundleAssemblePayload
	decodeErr := json.NewDecoder(r.Body).Decode(&payload)
	if decodeErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid_json"})
		return
	}

	if !assembleProjectsIncludeAccess(payload.Projects, access) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "forbidden"})
		return
	}

	missingResult := missingArtifactChunks(r, vault, access, payload.Chunks)
	missing, missingErr := missingResult.Value()
	if missingErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": missingErr.Error()})
		return
	}

	if len(missing) != 0 {
		writeJSON(w, http.StatusOK, artifactAssembleResponse{
			State:         "not_found",
			MissingChunks: missing,
		})
		return
	}

	assembleErr := assembleSourceMapBundle(r, vault, store, access, payload)
	if assembleErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": assembleErr.Error()})
		return
	}

	writeJSON(w, http.StatusOK, artifactAssembleResponse{
		State:         "created",
		MissingChunks: []string{},
	})
}

func handleDebugFileAssembleCarrier(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	reader projectapp.Reader,
	vault artifacts.ArtifactVault,
	store *debugfiles.Service,
) {
	if vault == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "artifact_vault_not_configured"})
		return
	}

	if store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "debug_file_store_not_configured"})
		return
	}

	access, ok := requireArtifactProjectAccess(w, r, manager, reader)
	if !ok {
		return
	}

	var payload debugFileAssembleRequest
	decodeErr := json.NewDecoder(r.Body).Decode(&payload)
	if decodeErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid_json"})
		return
	}

	response := map[string]artifactAssembleResponse{}
	for checksum, file := range payload {
		itemResponse, assembleErr := assembleDebugFile(r, vault, store, access, checksum, file)
		if assembleErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"detail": assembleErr.Error()})
			return
		}

		response[checksum] = itemResponse
	}

	writeJSON(w, http.StatusOK, response)
}

func requireArtifactProjectAccess(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	reader projectapp.Reader,
) (artifactProjectAccess, bool) {
	access, ok := requireArtifactTokenProject(w, r, manager, reader)
	if !ok {
		return artifactProjectAccess{}, false
	}

	if !artifactPathMatchesProject(r, access) {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "forbidden"})
		return artifactProjectAccess{}, false
	}

	return access, true
}

func requireArtifactOrganizationAccess(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	reader projectapp.Reader,
) (artifactProjectAccess, bool) {
	access, ok := requireArtifactTokenProject(w, r, manager, reader)
	if !ok {
		return artifactProjectAccess{}, false
	}

	organizationSlug := strings.TrimSpace(r.PathValue("organization_slug"))
	if access.organizationSlug != organizationSlug {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "forbidden"})
		return artifactProjectAccess{}, false
	}

	return access, true
}

func requireArtifactTokenProject(
	w http.ResponseWriter,
	r *http.Request,
	manager tokenapp.Manager,
	reader projectapp.Reader,
) (artifactProjectAccess, bool) {
	if reader == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "project_reader_not_configured"})
		return artifactProjectAccess{}, false
	}

	tokenAuth, tokenOK := requireProjectAPIToken(
		w,
		r,
		manager,
		tokenapp.ProjectTokenScopeAdmin,
	)
	if !tokenOK {
		return artifactProjectAccess{}, false
	}

	recordResult := reader.FindCurrentProject(
		r.Context(),
		projectapp.ProjectQuery{
			Scope: projectapp.Scope{
				OrganizationID: tokenAuth.OrganizationID,
				ProjectID:      tokenAuth.ProjectID,
			},
		},
	)
	record, recordErr := recordResult.Value()
	if recordErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "forbidden"})
		return artifactProjectAccess{}, false
	}

	return artifactProjectAccess{
		organizationID:   tokenAuth.OrganizationID,
		projectID:        tokenAuth.ProjectID,
		organizationSlug: record.OrganizationSlug,
		projectSlug:      record.Slug,
	}, true
}

func artifactPathMatchesProject(r *http.Request, access artifactProjectAccess) bool {
	organizationSlug := strings.TrimSpace(r.PathValue("organization_slug"))
	projectSlug := strings.TrimSpace(r.PathValue("project_slug"))

	if access.organizationSlug != organizationSlug {
		return false
	}

	return access.projectSlug == projectSlug
}

func storeArtifactChunk(
	r *http.Request,
	vault artifacts.ArtifactVault,
	access artifactProjectAccess,
	fileHeader *multipart.FileHeader,
) error {
	checksum := strings.TrimSpace(fileHeader.Filename)
	checksumErr := requireSHA1Checksum(checksum)
	if checksumErr != nil {
		return checksumErr
	}

	file, openErr := fileHeader.Open()
	if openErr != nil {
		return openErr
	}
	defer file.Close()

	gzipReader, gzipErr := gzip.NewReader(file)
	if gzipErr != nil {
		return errors.New("invalid_gzip_chunk")
	}
	defer gzipReader.Close()

	key, keyErr := artifactChunkKey(access, checksum)
	if keyErr != nil {
		return keyErr
	}

	limited := io.LimitReader(gzipReader, artifactChunkUploadBlobBytes+1)
	putResult := vault.PutArtifact(r.Context(), key, limited)
	stored, putErr := putResult.Value()
	if putErr != nil {
		return putErr
	}

	if stored.Size() > artifactChunkUploadBlobBytes {
		_ = vault.DeleteArtifact(r.Context(), key)
		return errors.New("chunk_too_large")
	}

	return nil
}

func artifactChunkKey(
	access artifactProjectAccess,
	checksum string,
) (domain.ArtifactKey, error) {
	checksumErr := requireSHA1Checksum(checksum)
	if checksumErr != nil {
		return domain.ArtifactKey{}, checksumErr
	}

	name, nameErr := domain.NewArtifactName("chunk-" + checksum)
	if nameErr != nil {
		return domain.ArtifactKey{}, nameErr
	}

	key, keyErr := domain.NewArtifactKey(
		access.organizationID,
		access.projectID,
		domain.ArtifactKindUploadChunk(),
		name,
	)
	if keyErr != nil {
		return domain.ArtifactKey{}, keyErr
	}

	return key, nil
}

func missingArtifactChunks(
	r *http.Request,
	vault artifacts.ArtifactVault,
	access artifactProjectAccess,
	chunks []string,
) result.Result[[]string] {
	missing := []string{}
	for _, checksum := range chunks {
		key, keyErr := artifactChunkKey(access, checksum)
		if keyErr != nil {
			return result.Err[[]string](keyErr)
		}

		getResult := vault.GetArtifact(r.Context(), key)
		reader, getErr := getResult.Value()
		if getErr != nil {
			if errors.Is(getErr, artifacts.ErrArtifactNotFound) {
				missing = append(missing, checksum)
				continue
			}

			return result.Err[[]string](getErr)
		}

		_ = reader.Close()
	}

	return result.Ok(missing)
}

func readArtifactChunks(
	r *http.Request,
	vault artifacts.ArtifactVault,
	access artifactProjectAccess,
	chunks []string,
) ([]byte, error) {
	assembled := &bytes.Buffer{}
	for _, checksum := range chunks {
		chunk, chunkErr := readSingleArtifactChunk(r, vault, access, checksum)
		if chunkErr != nil {
			return nil, chunkErr
		}

		_, writeErr := assembled.Write(chunk)
		if writeErr != nil {
			return nil, writeErr
		}
	}

	return assembled.Bytes(), nil
}

func readSingleArtifactChunk(
	r *http.Request,
	vault artifacts.ArtifactVault,
	access artifactProjectAccess,
	checksum string,
) ([]byte, error) {
	key, keyErr := artifactChunkKey(access, checksum)
	if keyErr != nil {
		return nil, keyErr
	}

	getResult := vault.GetArtifact(r.Context(), key)
	reader, getErr := getResult.Value()
	if getErr != nil {
		return nil, getErr
	}
	defer reader.Close()

	body, readErr := io.ReadAll(io.LimitReader(reader, artifactChunkUploadBlobBytes+1))
	if readErr != nil {
		return nil, readErr
	}

	if len(body) > artifactChunkUploadBlobBytes {
		return nil, errors.New("chunk_too_large")
	}

	return body, nil
}

func assembleSourceMapBundle(
	r *http.Request,
	vault artifacts.ArtifactVault,
	store *sourcemaps.Service,
	access artifactProjectAccess,
	payload artifactBundleAssemblePayload,
) error {
	checksumErr := requireSHA1Checksum(payload.Checksum)
	if checksumErr != nil {
		return checksumErr
	}

	body, readErr := readArtifactChunks(r, vault, access, payload.Chunks)
	if readErr != nil {
		return readErr
	}

	verifyErr := verifySHA1Checksum(body, payload.Checksum)
	if verifyErr != nil {
		return verifyErr
	}

	reader := bytes.NewReader(body)
	bundle, zipErr := zip.NewReader(reader, int64(len(body)))
	if zipErr != nil {
		return fmt.Errorf("invalid artifact bundle: %w", zipErr)
	}

	manifest, manifestErr := readArtifactBundleManifest(bundle)
	if manifestErr != nil {
		return manifestErr
	}

	validateErr := validateArtifactBundleManifest(manifest, access, payload)
	if validateErr != nil {
		return validateErr
	}

	releaseInput := firstNonEmpty(payload.Version, manifest.Release)
	release, releaseErr := domain.NewReleaseName(releaseInput)
	if releaseErr != nil {
		return releaseErr
	}

	distInput := firstNonEmpty(payload.Dist, manifest.Dist)
	dist, distErr := domain.NewOptionalDistName(distInput)
	if distErr != nil {
		return distErr
	}

	for zipPath, file := range manifest.Files {
		if file.Type != "source_map" {
			continue
		}

		uploadErr := uploadSourceMapBundleFile(r, store, access, bundle, zipPath, file, release, dist)
		if uploadErr != nil {
			return uploadErr
		}
	}

	return nil
}

func readArtifactBundleManifest(bundle *zip.Reader) (artifactBundleManifest, error) {
	manifestFile := findZipFile(bundle, "manifest.json")
	if manifestFile == nil {
		return artifactBundleManifest{}, errors.New("artifact bundle manifest is missing")
	}

	reader, openErr := manifestFile.Open()
	if openErr != nil {
		return artifactBundleManifest{}, openErr
	}
	defer reader.Close()

	var manifest artifactBundleManifest
	decodeErr := json.NewDecoder(reader).Decode(&manifest)
	if decodeErr != nil {
		return artifactBundleManifest{}, decodeErr
	}

	return manifest, nil
}

func validateArtifactBundleManifest(
	manifest artifactBundleManifest,
	access artifactProjectAccess,
	payload artifactBundleAssemblePayload,
) error {
	if manifest.Org != "" && manifest.Org != access.organizationSlug {
		return errors.New("artifact bundle organization mismatch")
	}

	if manifest.Project != "" && manifest.Project != access.projectSlug {
		return errors.New("artifact bundle project mismatch")
	}

	if payload.Version != "" && manifest.Release != "" && payload.Version != manifest.Release {
		return errors.New("artifact bundle release mismatch")
	}

	if len(manifest.Files) == 0 {
		return errors.New("artifact bundle has no files")
	}

	return nil
}

func uploadSourceMapBundleFile(
	r *http.Request,
	store *sourcemaps.Service,
	access artifactProjectAccess,
	bundle *zip.Reader,
	zipPath string,
	file artifactBundleManifestFile,
	release domain.ReleaseName,
	dist domain.DistName,
) error {
	sourceMap := findZipFile(bundle, zipPath)
	if sourceMap == nil {
		return errors.New("artifact bundle file is missing")
	}

	nameInput := firstNonEmpty(file.URL, zipPath)
	nameInput = normalizeSourceMapTargetName(nameInput)
	fileName, fileNameErr := domain.NewSourceMapFileName(nameInput)
	if fileNameErr != nil {
		return fileNameErr
	}

	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		return identityErr
	}

	reader, openErr := sourceMap.Open()
	if openErr != nil {
		return openErr
	}
	defer reader.Close()

	uploadResult := store.Upload(
		r.Context(),
		access.organizationID,
		access.projectID,
		identity,
		reader,
	)
	_, uploadErr := uploadResult.Value()
	return uploadErr
}

func assembleDebugFile(
	r *http.Request,
	vault artifacts.ArtifactVault,
	store *debugfiles.Service,
	access artifactProjectAccess,
	checksum string,
	file debugFileAssembleFile,
) (artifactAssembleResponse, error) {
	checksumErr := requireSHA1Checksum(checksum)
	if checksumErr != nil {
		return artifactAssembleResponse{}, checksumErr
	}

	missingResult := missingArtifactChunks(r, vault, access, file.Chunks)
	missing, missingErr := missingResult.Value()
	if missingErr != nil {
		return artifactAssembleResponse{}, missingErr
	}

	if len(missing) != 0 {
		return artifactAssembleResponse{
			State:         "not_found",
			MissingChunks: missing,
		}, nil
	}

	body, readErr := readArtifactChunks(r, vault, access, file.Chunks)
	if readErr != nil {
		return artifactAssembleResponse{}, readErr
	}

	verifyErr := verifySHA1Checksum(body, checksum)
	if verifyErr != nil {
		return artifactAssembleResponse{}, verifyErr
	}

	debugID, debugIDErr := domain.NewDebugIdentifier(file.DebugID)
	if debugIDErr != nil {
		return artifactAssembleResponse{}, debugIDErr
	}

	kindResult := debugFileKindFromBytes(body)
	kind, kindErr := kindResult.Value()
	if kindErr != nil {
		return artifactAssembleResponse{}, kindErr
	}

	fileName, fileNameErr := domain.NewDebugFileName(file.Name)
	if fileNameErr != nil {
		return artifactAssembleResponse{}, fileNameErr
	}

	identity, identityErr := domain.NewDebugFileIdentity(debugID, kind, fileName)
	if identityErr != nil {
		return artifactAssembleResponse{}, identityErr
	}

	uploadResult := store.Upload(
		r.Context(),
		access.organizationID,
		access.projectID,
		identity,
		bytes.NewReader(body),
	)
	_, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		return artifactAssembleResponse{}, uploadErr
	}

	return artifactAssembleResponse{
		State:         "created",
		MissingChunks: []string{},
	}, nil
}

func debugFileKindFromBytes(body []byte) result.Result[domain.DebugFileKind] {
	prefixSize := 64
	if len(body) < prefixSize {
		prefixSize = len(body)
	}

	kind, kindErr := debugfiles.DetectKind(body[:prefixSize])
	if kindErr != nil {
		return result.Err[domain.DebugFileKind](kindErr)
	}

	return result.Ok(kind)
}

func findZipFile(bundle *zip.Reader, name string) *zip.File {
	for _, file := range bundle.File {
		if file.Name == name {
			return file
		}
	}

	return nil
}

func assembleProjectsIncludeAccess(projects []string, access artifactProjectAccess) bool {
	if len(projects) == 0 {
		return true
	}

	for _, project := range projects {
		if strings.TrimSpace(project) == access.projectSlug {
			return true
		}
	}

	return false
}

func requireSHA1Checksum(input string) error {
	value := strings.TrimSpace(input)
	if len(value) != sha1.Size*2 {
		return errors.New("sha1 checksum is invalid")
	}

	_, decodeErr := hex.DecodeString(value)
	if decodeErr != nil {
		return errors.New("sha1 checksum is invalid")
	}

	return nil
}

func verifySHA1Checksum(body []byte, expected string) error {
	sum := sha1.Sum(body)
	actual := hex.EncodeToString(sum[:])
	if actual != strings.TrimSpace(expected) {
		return errors.New("sha1 checksum mismatch")
	}

	return nil
}

func artifactChunkUploadHTTPStatus(err error) int {
	switch err.Error() {
	case "invalid_gzip_chunk", "chunk_too_large", "sha1 checksum is invalid":
		return http.StatusBadRequest
	default:
		return http.StatusServiceUnavailable
	}
}

func artifactChunkUploadDetail(err error) string {
	switch err.Error() {
	case "chunk_too_large":
		return "payload_too_large"
	default:
		return err.Error()
	}
}

func readArtifactMultipart(
	w http.ResponseWriter,
	r *http.Request,
	maxPayloadBytes int64,
	tooLarge error,
	fileFields []string,
) result.Result[artifactUploadFile] {
	maxRequestBytes := maxPayloadBytes + artifactUploadMultipartOverheadBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)

	parseErr := r.ParseMultipartForm(artifactUploadMultipartMemoryBytes)
	if parseErr != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(parseErr, &maxBytesErr) {
			return result.Err[artifactUploadFile](tooLarge)
		}

		return result.Err[artifactUploadFile](errors.New("invalid_multipart"))
	}

	uploadResult := firstUploadFile(r.MultipartForm, fileFields)
	upload, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		return result.Err[artifactUploadFile](uploadErr)
	}

	if upload.fileHeader.Size > maxPayloadBytes {
		_ = upload.file.Close()
		return result.Err[artifactUploadFile](tooLarge)
	}

	return result.Ok(upload)
}

func firstUploadFile(
	form *multipart.Form,
	fileFields []string,
) result.Result[artifactUploadFile] {
	if form == nil {
		return result.Err[artifactUploadFile](errors.New("invalid_multipart"))
	}

	for _, fieldName := range fileFields {
		files := form.File[fieldName]
		if len(files) == 0 {
			continue
		}

		file, openErr := files[0].Open()
		if openErr != nil {
			return result.Err[artifactUploadFile](openErr)
		}

		return result.Ok(artifactUploadFile{
			file:       file,
			fileHeader: files[0],
			form:       form,
		})
	}

	return result.Err[artifactUploadFile](errors.New("missing_upload_file"))
}

func sourceMapIdentityFromUpload(
	r *http.Request,
	upload artifactUploadFile,
) result.Result[domain.SourceMapIdentity] {
	release, releaseErr := domain.NewReleaseName(r.PathValue("version"))
	if releaseErr != nil {
		return result.Err[domain.SourceMapIdentity](releaseErr)
	}

	distInput := artifactFieldValue(r, upload.form, []string{"dist", "sentry[dist]"})
	dist, distErr := domain.NewOptionalDistName(distInput)
	if distErr != nil {
		return result.Err[domain.SourceMapIdentity](distErr)
	}

	nameInput := sourceMapNameInput(r, upload)
	fileName, fileErr := domain.NewSourceMapFileName(nameInput)
	if fileErr != nil {
		return result.Err[domain.SourceMapIdentity](fileErr)
	}

	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		return result.Err[domain.SourceMapIdentity](identityErr)
	}

	return result.Ok(identity)
}

func sourceMapNameInput(r *http.Request, upload artifactUploadFile) string {
	fieldInput := artifactFieldValue(
		r,
		upload.form,
		[]string{"file_name", "fileName", "name", "filename"},
	)
	candidate := firstNonEmpty(fieldInput, upload.fileHeader.Filename)
	return normalizeSourceMapTargetName(candidate)
}

func normalizeSourceMapTargetName(input string) string {
	value := strings.TrimSpace(input)
	value = strings.TrimPrefix(value, "~/")
	value = strings.TrimPrefix(value, "/")
	value = strings.TrimSuffix(value, ".map")
	return value
}

func debugFileIdentityFromUpload(upload artifactUploadFile) result.Result[domain.DebugFileIdentity] {
	debugIDInput := artifactFieldValue(
		nil,
		upload.form,
		[]string{"debug_id", "debugId", "uuid"},
	)
	debugID, debugIDErr := domain.NewDebugIdentifier(debugIDInput)
	if debugIDErr != nil {
		return result.Err[domain.DebugFileIdentity](debugIDErr)
	}

	kindResult := debugFileKindFromUpload(upload)
	kind, kindErr := kindResult.Value()
	if kindErr != nil {
		return result.Err[domain.DebugFileIdentity](kindErr)
	}

	fileNameInput := debugFileNameInput(upload)
	fileName, fileNameErr := domain.NewDebugFileName(fileNameInput)
	if fileNameErr != nil {
		return result.Err[domain.DebugFileIdentity](fileNameErr)
	}

	identity, identityErr := domain.NewDebugFileIdentity(debugID, kind, fileName)
	if identityErr != nil {
		return result.Err[domain.DebugFileIdentity](identityErr)
	}

	return result.Ok(identity)
}

func debugFileKindFromUpload(upload artifactUploadFile) result.Result[domain.DebugFileKind] {
	kindInput := artifactFieldValue(
		nil,
		upload.form,
		[]string{"kind", "symbol_type", "symbolType", "type"},
	)
	if strings.TrimSpace(kindInput) != "" {
		kind, kindErr := domain.ParseDebugFileKind(kindInput)
		if kindErr != nil {
			return result.Err[domain.DebugFileKind](kindErr)
		}

		return result.Ok(kind)
	}

	return debugFileKindFromPayload(upload.file)
}

func debugFileKindFromPayload(file multipart.File) result.Result[domain.DebugFileKind] {
	_, startErr := file.Seek(0, io.SeekStart)
	if startErr != nil {
		return result.Err[domain.DebugFileKind](startErr)
	}

	prefix := make([]byte, 64)
	prefixCount, readErr := io.ReadFull(file, prefix)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return result.Err[domain.DebugFileKind](readErr)
	}

	_, resetErr := file.Seek(0, io.SeekStart)
	if resetErr != nil {
		return result.Err[domain.DebugFileKind](resetErr)
	}

	kind, kindErr := debugfiles.DetectKind(prefix[:prefixCount])
	if kindErr != nil {
		return result.Err[domain.DebugFileKind](kindErr)
	}

	return result.Ok(kind)
}

func debugFileNameInput(upload artifactUploadFile) string {
	fieldInput := artifactFieldValue(
		nil,
		upload.form,
		[]string{"file_name", "fileName", "name", "filename", "object_name", "objectName"},
	)
	return firstNonEmpty(fieldInput, upload.fileHeader.Filename)
}

func artifactFieldValue(
	r *http.Request,
	form *multipart.Form,
	names []string,
) string {
	for _, name := range names {
		value := firstFormValue(form, name)
		if value != "" {
			return value
		}
	}

	if r == nil {
		return ""
	}

	query := r.URL.Query()
	for _, name := range names {
		value := strings.TrimSpace(query.Get(name))
		if value != "" {
			return value
		}
	}

	return ""
}

func sourceMapUploadHTTPStatus(err error) int {
	if errors.Is(err, sourcemaps.ErrSourceMapTooLarge) {
		return http.StatusRequestEntityTooLarge
	}

	if err.Error() == "invalid_multipart" || err.Error() == "missing_upload_file" {
		return http.StatusBadRequest
	}

	return http.StatusServiceUnavailable
}

func sourceMapUploadDetail(err error) string {
	if errors.Is(err, sourcemaps.ErrSourceMapTooLarge) {
		return "payload_too_large"
	}

	return err.Error()
}

func debugFileUploadHTTPStatus(err error) int {
	if errors.Is(err, debugfiles.ErrDebugFileTooLarge) {
		return http.StatusRequestEntityTooLarge
	}

	if errors.Is(err, debugfiles.ErrUnsupportedDebugFile) || errors.Is(err, debugfiles.ErrDebugFileMismatch) {
		return http.StatusBadRequest
	}

	switch err.Error() {
	case "invalid_multipart", "missing_upload_file":
		return http.StatusBadRequest
	case "debug file kind is invalid":
		return http.StatusBadRequest
	case "debug identifier is required", "debug identifier is too short":
		return http.StatusBadRequest
	case "debug identifier is too long", "debug identifier must be hexadecimal":
		return http.StatusBadRequest
	case "debug file name is required", "debug file name must be valid utf-8":
		return http.StatusBadRequest
	case "debug file name is too long", "debug file name must not contain control characters":
		return http.StatusBadRequest
	case "debug file name must not contain backslashes or null bytes":
		return http.StatusBadRequest
	case "debug file name must not traverse paths", "debug file name must be relative":
		return http.StatusBadRequest
	case "debug file identity requires debug identifier":
		return http.StatusBadRequest
	case "debug file identity requires kind", "debug file identity requires file name":
		return http.StatusBadRequest
	default:
		return http.StatusServiceUnavailable
	}
}

func debugFileUploadDetail(err error) string {
	if errors.Is(err, debugfiles.ErrDebugFileTooLarge) {
		return "payload_too_large"
	}

	if errors.Is(err, debugfiles.ErrUnsupportedDebugFile) || errors.Is(err, debugfiles.ErrDebugFileMismatch) {
		return "invalid_debug_file"
	}

	return err.Error()
}

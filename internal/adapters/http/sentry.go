package httpadapter

import (
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/adapters/sentryprotocol"
	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	logapp "github.com/ivanzakutnii/error-tracker/internal/app/logs"
	"github.com/ivanzakutnii/error-tracker/internal/app/minidumps"
	ratelimitapp "github.com/ivanzakutnii/error-tracker/internal/app/ratelimit"
	"github.com/ivanzakutnii/error-tracker/internal/app/sourcemaps"
	userreportapp "github.com/ivanzakutnii/error-tracker/internal/app/userreports"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
	"github.com/ivanzakutnii/error-tracker/internal/plans/ingestplan"
)

const maxStoreRequestBytes = 1 * 1024 * 1024
const maxUserFeedbackRequestBytes = 1 * 1024 * 1024
const minidumpMultipartMemoryBytes = 8 * 1024 * 1024
const minidumpMultipartOverheadBytes = 1 * 1024 * 1024

var sentryKeyPattern = regexp.MustCompile(`(?:^|[\s,])(sentry_key|glitchtip_key)=([^,\s]+)`)

type SentryIngestBackend interface {
	ingest.ProjectDirectory
	ingest.IngestTransaction
	logapp.IngestTransaction
	ratelimitapp.Checker
	userreportapp.Writer
}

type sentryAuthCarrier struct {
	projectRef string
	publicKey  string
	source     string
}

type sentryRequestKind string

const (
	sentryStoreRequest    sentryRequestKind = "store"
	sentryEnvelopeRequest sentryRequestKind = "envelope"
)

type parsedSentryEvent struct {
	event       domain.CanonicalEvent
	hasEvent    bool
	logs        []domain.LogRecord
	userReports []sentryprotocol.UserReportItem
}

type minidumpUpload struct {
	file       multipart.File
	fileHeader *multipart.FileHeader
	metadata   minidumpMetadata
}

type minidumpMetadata struct {
	eventID     string
	release     string
	environment string
	level       string
	platform    string
}

type preparedMinidumpEvent struct {
	event    domain.CanonicalEvent
	identity domain.MinidumpIdentity
}

func sentryStoreHandler(backend SentryIngestBackend, enrichments IngestEnrichments) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSentryIngestCarrier(w, r, backend, enrichments, sentryStoreRequest)
	}
}

func sentryEnvelopeHandler(backend SentryIngestBackend, enrichments IngestEnrichments) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSentryIngestCarrier(w, r, backend, enrichments, sentryEnvelopeRequest)
	}
}

func sentrySecurityHandler(backend SentryIngestBackend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSentrySecurityCarrier(w, r, backend)
	}
}

func sentryMinidumpHandler(
	backend SentryIngestBackend,
	enrichments IngestEnrichments,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSentryMinidumpCarrier(w, r, backend, enrichments)
	}
}

func sentryUserFeedbackHandler(backend SentryIngestBackend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSentryUserFeedbackCarrier(w, r, backend)
	}
}

func handleSentryIngestCarrier(
	w http.ResponseWriter,
	r *http.Request,
	backend SentryIngestBackend,
	enrichments IngestEnrichments,
	kind sentryRequestKind,
) {
	if backend == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "ingest_backend_not_configured"})
		return
	}

	bodyResult := readSentryBody(w, r, kind)
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		writeJSON(w, httpStatusForBodyError(bodyErr), map[string]string{"detail": bodyErr.Error()})
		return
	}

	carrierResult := decodeSentryAuthCarrier(r, body, kind)
	carrier, authErr := carrierResult.Value()
	if authErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "denied"})
		return
	}

	authResult := resolveProjectAuth(r, backend, carrier)
	auth, projectErr := authResult.Value()
	if projectErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "denied"})
		return
	}

	receivedAt, receivedErr := domain.NewTimePoint(time.Now().UTC())
	if receivedErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": "clock_error"})
		return
	}

	project := sentryprotocol.NewProjectContextWithPrivacy(
		auth.OrganizationID(),
		auth.ProjectID(),
		auth.ScrubIPAddresses(),
	)
	eventResult := parseSentryRequest(project, receivedAt, body, kind)
	parsed, parseErr := eventResult.Value()
	if parseErr != nil {
		writeProtocolError(w, parseErr)
		return
	}

	if parsed.hasEvent {
		parsed.event = applyIngestEnrichments(r.Context(), enrichments, parsed.event)
	}

	reportCommandsResult := prepareParsedUserReports(auth, parsed.userReports)
	reportCommands, reportCommandsErr := reportCommandsResult.Value()
	if reportCommandsErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": reportCommandsErr.Error()})
		return
	}

	if !parsed.hasEvent && len(parsed.logs) == 0 {
		reportErr := submitPreparedUserReports(r.Context(), backend, reportCommands)
		if reportErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"detail": reportErr.Error()})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{})
		return
	}

	rateLimitResult := checkSentryRateLimit(r.Context(), backend, auth, carrier, receivedAt)
	rateLimit, rateLimitErr := rateLimitResult.Value()
	if rateLimitErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "rate_limit_unavailable"})
		return
	}

	if !rateLimit.Allowed() {
		writeRateLimitDecision(w, rateLimit)
		return
	}

	if len(parsed.logs) > 0 {
		receiptResult := logapp.Ingest(r.Context(), backend, logapp.NewIngestCommand(parsed.logs))
		receipt, receiptErr := receiptResult.Value()
		if receiptErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "storage_unavailable"})
			return
		}

		writeLogReceipt(w, receipt)
		return
	}

	receiptResult := ingest.IngestCanonicalEvent(r.Context(), ingest.NewIngestCommand(parsed.event), backend)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "storage_unavailable"})
		return
	}

	if receipt.Kind() == ingest.ReceiptQuotaRejected {
		writeIngestReceipt(w, receipt)
		return
	}

	reportErr := submitPreparedUserReports(r.Context(), backend, reportCommands)
	if reportErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": reportErr.Error()})
		return
	}

	writeIngestReceipt(w, receipt)
}

func handleSentryMinidumpCarrier(
	w http.ResponseWriter,
	r *http.Request,
	backend SentryIngestBackend,
	enrichments IngestEnrichments,
) {
	if backend == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "ingest_backend_not_configured"})
		return
	}

	if enrichments.MinidumpStore == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "minidump_store_not_configured"})
		return
	}

	carrierResult := decodeSentryAuthCarrier(r, nil, sentryStoreRequest)
	carrier, authErr := carrierResult.Value()
	if authErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "denied"})
		return
	}

	authResult := resolveProjectAuth(r, backend, carrier)
	auth, projectErr := authResult.Value()
	if projectErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "denied"})
		return
	}

	receivedAt, receivedErr := domain.NewTimePoint(time.Now().UTC())
	if receivedErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": "clock_error"})
		return
	}

	rateLimitResult := checkSentryRateLimit(r.Context(), backend, auth, carrier, receivedAt)
	rateLimit, rateLimitErr := rateLimitResult.Value()
	if rateLimitErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "rate_limit_unavailable"})
		return
	}

	if !rateLimit.Allowed() {
		writeRateLimitDecision(w, rateLimit)
		return
	}

	uploadResult := readMinidumpMultipart(w, r)
	defer cleanupMultipartForm(r)
	upload, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		writeJSON(w, minidumpHTTPStatus(uploadErr), map[string]string{"detail": minidumpErrorDetail(uploadErr)})
		return
	}
	defer upload.file.Close()

	metadataResult := parseMinidumpNativeMetadata(upload.file, upload.fileHeader.Size)
	metadata, metadataErr := metadataResult.Value()
	if metadataErr != nil {
		writeJSON(w, minidumpHTTPStatus(metadataErr), map[string]string{"detail": minidumpErrorDetail(metadataErr)})
		return
	}

	eventResult := prepareMinidumpEvent(auth, receivedAt, upload, metadata)
	prepared, eventErr := eventResult.Value()
	if eventErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": eventErr.Error()})
		return
	}

	prepared.event = debugfiles.ApplyToCanonicalEvent(r.Context(), enrichments.DebugFileStore, prepared.event)

	receiptResult := ingestMinidumpEvent(r.Context(), backend, enrichments.MinidumpStore, prepared, upload.file)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		writeJSON(w, minidumpHTTPStatus(receiptErr), map[string]string{"detail": minidumpErrorDetail(receiptErr)})
		return
	}

	writeIngestReceipt(w, receipt)
}

func handleSentrySecurityCarrier(
	w http.ResponseWriter,
	r *http.Request,
	backend SentryIngestBackend,
) {
	if backend == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "ingest_backend_not_configured"})
		return
	}

	bodyResult := readSentryBody(w, r, sentryStoreRequest)
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		writeJSON(w, httpStatusForBodyError(bodyErr), map[string]string{"detail": bodyErr.Error()})
		return
	}

	carrierResult := decodeSentryAuthCarrier(r, nil, sentryStoreRequest)
	carrier, authErr := carrierResult.Value()
	if authErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "denied"})
		return
	}

	authResult := resolveProjectAuth(r, backend, carrier)
	auth, projectErr := authResult.Value()
	if projectErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "denied"})
		return
	}

	receivedAt, receivedErr := domain.NewTimePoint(time.Now().UTC())
	if receivedErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": "clock_error"})
		return
	}

	project := sentryprotocol.NewProjectContextWithPrivacy(
		auth.OrganizationID(),
		auth.ProjectID(),
		auth.ScrubIPAddresses(),
	)
	eventResult := sentryprotocol.ParseCSPReport(project, receivedAt, body)
	event, eventErr := eventResult.Value()
	if eventErr != nil {
		writeProtocolError(w, eventErr)
		return
	}

	rateLimitResult := checkSentryRateLimit(r.Context(), backend, auth, carrier, receivedAt)
	rateLimit, rateLimitErr := rateLimitResult.Value()
	if rateLimitErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "rate_limit_unavailable"})
		return
	}

	if !rateLimit.Allowed() {
		writeRateLimitDecision(w, rateLimit)
		return
	}

	receiptResult := ingest.IngestCanonicalEvent(r.Context(), ingest.NewIngestCommand(event), backend)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "storage_unavailable"})
		return
	}

	writeIngestReceipt(w, receipt)
}

func handleSentryUserFeedbackCarrier(
	w http.ResponseWriter,
	r *http.Request,
	backend SentryIngestBackend,
) {
	if backend == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "ingest_backend_not_configured"})
		return
	}

	bodyResult := readSentryUserFeedbackBody(w, r)
	body, bodyErr := bodyResult.Value()
	if bodyErr != nil {
		writeJSON(w, httpStatusForBodyError(bodyErr), map[string]string{"detail": bodyErr.Error()})
		return
	}

	carrierResult := decodeSentryAuthCarrier(r, nil, sentryStoreRequest)
	carrier, authErr := carrierResult.Value()
	if authErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "denied"})
		return
	}

	authResult := resolveProjectAuth(r, backend, carrier)
	auth, projectErr := authResult.Value()
	if projectErr != nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"detail": "denied"})
		return
	}

	reportResult := parseUserFeedbackRequest(body)
	report, reportErr := reportResult.Value()
	if reportErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": reportErr.Error()})
		return
	}

	reportCommandsResult := prepareParsedUserReports(auth, []sentryprotocol.UserReportItem{report})
	reportCommands, reportCommandsErr := reportCommandsResult.Value()
	if reportCommandsErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": reportCommandsErr.Error()})
		return
	}

	submitErr := submitPreparedUserReports(r.Context(), backend, reportCommands)
	if submitErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": submitErr.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"eventID": report.EventID})
}

func decodeSentryAuthCarrier(
	r *http.Request,
	body []byte,
	kind sentryRequestKind,
) result.Result[sentryAuthCarrier] {
	sources := authSourcesFromRequest(r)
	if kind == sentryEnvelopeRequest {
		sources = append(sources, authSourcesFromEnvelope(body)...)
	}

	return agreeSentryAuthSources(sources)
}

func authSourcesFromRequest(r *http.Request) []sentryAuthCarrier {
	query := r.URL.Query()
	projectRef := strings.TrimSpace(r.PathValue("project_ref"))
	sources := []sentryAuthCarrier{}

	if value := query.Get("sentry_key"); value != "" {
		sources = append(sources, sentryAuthCarrier{
			projectRef: projectRef,
			publicKey:  strings.TrimSpace(value),
			source:     "query:sentry_key",
		})
	}

	if value := query.Get("glitchtip_key"); value != "" {
		sources = append(sources, sentryAuthCarrier{
			projectRef: projectRef,
			publicKey:  strings.TrimSpace(value),
			source:     "query:glitchtip_key",
		})
	}

	if value := headerAuthPublicKey(r.Header.Get("X-Sentry-Auth")); value != "" {
		sources = append(sources, sentryAuthCarrier{
			projectRef: projectRef,
			publicKey:  strings.TrimSpace(value),
			source:     "header:x-sentry-auth",
		})
	}

	if value := headerAuthPublicKey(r.Header.Get("Authorization")); value != "" {
		sources = append(sources, sentryAuthCarrier{
			projectRef: projectRef,
			publicKey:  strings.TrimSpace(value),
			source:     "header:authorization",
		})
	}

	return sources
}

func authSourcesFromEnvelope(body []byte) []sentryAuthCarrier {
	dsnResult := sentryprotocol.EnvelopeHeaderDSN(body)
	dsn, dsnErr := dsnResult.Value()
	if dsnErr != nil || dsn == "" {
		return []sentryAuthCarrier{}
	}

	carrier, carrierErr := authCarrierFromDSN(dsn)
	if carrierErr != nil {
		return []sentryAuthCarrier{{
			source: "envelope:dsn:invalid",
		}}
	}

	return []sentryAuthCarrier{carrier}
}

func authCarrierFromDSN(input string) (sentryAuthCarrier, error) {
	parsed, parseErr := url.Parse(input)
	if parseErr != nil {
		return sentryAuthCarrier{}, parseErr
	}

	publicKey := parsed.User.Username()
	projectRef := path.Base(strings.TrimRight(parsed.Path, "/"))
	if projectRef == "." || projectRef == "/" {
		projectRef = ""
	}

	return sentryAuthCarrier{
		projectRef: strings.TrimSpace(projectRef),
		publicKey:  strings.TrimSpace(publicKey),
		source:     "envelope:dsn",
	}, nil
}

func agreeSentryAuthSources(sources []sentryAuthCarrier) result.Result[sentryAuthCarrier] {
	canonical := sentryAuthCarrier{}

	for _, source := range sources {
		if source.projectRef == "" || source.publicKey == "" {
			return result.Err[sentryAuthCarrier](errDenied())
		}

		if canonical.projectRef == "" {
			canonical = source
			continue
		}

		if canonical.projectRef != source.projectRef || canonical.publicKey != source.publicKey {
			return result.Err[sentryAuthCarrier](errDenied())
		}
	}

	if canonical.projectRef == "" || canonical.publicKey == "" {
		return result.Err[sentryAuthCarrier](errDenied())
	}

	return result.Ok(canonical)
}

func headerAuthPublicKey(header string) string {
	matches := sentryKeyPattern.FindStringSubmatch(header)
	if len(matches) != 3 {
		return ""
	}

	return matches[2]
}

func readSentryBody(
	w http.ResponseWriter,
	r *http.Request,
	kind sentryRequestKind,
) result.Result[[]byte] {
	limit := sentryRequestLimit(kind)
	r.Body = http.MaxBytesReader(w, r.Body, limit+1)

	reader, readerErr := sentryBodyReader(r)
	if readerErr != nil {
		return result.Err[[]byte](readerErr)
	}
	defer reader.Close()

	body, readErr := io.ReadAll(io.LimitReader(reader, limit+1))
	if readErr != nil {
		return result.Err[[]byte](errors.New("payload_too_large"))
	}

	if int64(len(body)) > limit {
		return result.Err[[]byte](errors.New("payload_too_large"))
	}

	return result.Ok(body)
}

func readSentryUserFeedbackBody(w http.ResponseWriter, r *http.Request) result.Result[[]byte] {
	r.Body = http.MaxBytesReader(w, r.Body, maxUserFeedbackRequestBytes+1)

	body, readErr := io.ReadAll(io.LimitReader(r.Body, maxUserFeedbackRequestBytes+1))
	if readErr != nil {
		return result.Err[[]byte](errors.New("payload_too_large"))
	}

	if len(body) > maxUserFeedbackRequestBytes {
		return result.Err[[]byte](errors.New("payload_too_large"))
	}

	return result.Ok(body)
}

func sentryRequestLimit(kind sentryRequestKind) int64 {
	if kind == sentryStoreRequest {
		return maxStoreRequestBytes
	}

	return sentryprotocol.MaxEnvelopeBytes
}

type readCloser struct {
	io.Reader
	close func() error
}

func (reader readCloser) Close() error {
	return reader.close()
}

func sentryBodyReader(r *http.Request) (io.ReadCloser, error) {
	encoding := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Encoding")))
	if encoding == "" || encoding == "identity" {
		return r.Body, nil
	}

	if encoding == "gzip" {
		reader, err := gzip.NewReader(r.Body)
		if err != nil {
			return nil, errors.New("invalid_gzip")
		}

		return readCloser{
			Reader: reader,
			close: func() error {
				closeErr := reader.Close()
				bodyErr := r.Body.Close()
				if closeErr != nil {
					return closeErr
				}

				return bodyErr
			},
		}, nil
	}

	return nil, errors.New("unsupported_content_encoding")
}

func httpStatusForBodyError(err error) int {
	if err.Error() == "unsupported_content_encoding" {
		return http.StatusUnsupportedMediaType
	}

	if err.Error() == "payload_too_large" {
		return http.StatusRequestEntityTooLarge
	}

	return http.StatusBadRequest
}

func resolveProjectAuth(
	r *http.Request,
	backend SentryIngestBackend,
	carrier sentryAuthCarrier,
) result.Result[domain.ProjectAuth] {
	ref, refErr := domain.NewProjectRef(carrier.projectRef)
	if refErr != nil {
		return result.Err[domain.ProjectAuth](refErr)
	}

	key, keyErr := domain.NewProjectPublicKey(carrier.publicKey)
	if keyErr != nil {
		return result.Err[domain.ProjectAuth](keyErr)
	}

	return backend.ResolveProjectKey(r.Context(), ref, key)
}

func checkSentryRateLimit(
	ctx context.Context,
	checker ratelimitapp.Checker,
	auth domain.ProjectAuth,
	carrier sentryAuthCarrier,
	receivedAt domain.TimePoint,
) result.Result[ratelimitapp.Decision] {
	publicKey, keyErr := domain.NewProjectPublicKey(carrier.publicKey)
	if keyErr != nil {
		return result.Err[ratelimitapp.Decision](keyErr)
	}

	return ratelimitapp.Check(
		ctx,
		checker,
		ratelimitapp.Command{
			Scope: ratelimitapp.Scope{
				OrganizationID: auth.OrganizationID(),
				ProjectID:      auth.ProjectID(),
			},
			PublicKey: publicKey,
			Now:       receivedAt.Time(),
		},
	)
}

func parseSentryRequest(
	project sentryprotocol.ProjectContext,
	receivedAt domain.TimePoint,
	body []byte,
	kind sentryRequestKind,
) result.Result[parsedSentryEvent] {
	if kind == sentryStoreRequest {
		eventResult := sentryprotocol.ParseStoreEvent(project, receivedAt, body)
		event, eventErr := eventResult.Value()
		if eventErr != nil {
			return result.Err[parsedSentryEvent](eventErr)
		}

		return result.Ok(parsedSentryEvent{event: event, hasEvent: true})
	}

	envelopeResult := sentryprotocol.ParseEnvelope(project, receivedAt, body)
	envelope, envelopeErr := envelopeResult.Value()
	if envelopeErr != nil {
		return result.Err[parsedSentryEvent](envelopeErr)
	}

	if len(envelope.Logs()) > 0 && len(envelope.UserReports()) > 0 {
		return result.Err[parsedSentryEvent](sentryprotocol.NewProtocolError(sentryprotocol.ErrorInvalidEnvelope, "mixed log and report items"))
	}

	event, ok := envelope.Event()
	if !ok {
		if len(envelope.Logs()) > 0 {
			return result.Ok(parsedSentryEvent{logs: envelope.Logs()})
		}

		return result.Ok(parsedSentryEvent{userReports: envelope.UserReports()})
	}

	return result.Ok(parsedSentryEvent{
		event:       event,
		hasEvent:    true,
		userReports: envelope.UserReports(),
	})
}

func applyIngestEnrichments(
	ctx context.Context,
	enrichments IngestEnrichments,
	event domain.CanonicalEvent,
) domain.CanonicalEvent {
	enriched := sourcemaps.ApplyToCanonicalEvent(ctx, enrichments.SourceMapResolver, event)
	enriched = debugfiles.ApplyToCanonicalEvent(ctx, enrichments.DebugFileStore, enriched)

	return enriched
}

func readMinidumpMultipart(w http.ResponseWriter, r *http.Request) result.Result[minidumpUpload] {
	r.Body = http.MaxBytesReader(w, r.Body, maxMinidumpRequestBytes())

	parseErr := r.ParseMultipartForm(minidumpMultipartMemoryBytes)
	if parseErr != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(parseErr, &maxBytesErr) {
			return result.Err[minidumpUpload](minidumps.ErrMinidumpTooLarge)
		}

		return result.Err[minidumpUpload](errors.New("invalid_multipart"))
	}

	file, fileHeader, fileErr := r.FormFile("upload_file_minidump")
	if fileErr != nil {
		return result.Err[minidumpUpload](errors.New("missing_upload_file_minidump"))
	}

	if fileHeader.Size > minidumps.MaxUploadBytes() {
		_ = file.Close()
		return result.Err[minidumpUpload](minidumps.ErrMinidumpTooLarge)
	}

	return result.Ok(minidumpUpload{
		file:       file,
		fileHeader: fileHeader,
		metadata:   minidumpMetadataFromForm(r.MultipartForm),
	})
}

func cleanupMultipartForm(r *http.Request) {
	if r.MultipartForm == nil {
		return
	}

	_ = r.MultipartForm.RemoveAll()
}

func maxMinidumpRequestBytes() int64 {
	return minidumps.MaxUploadBytes() + minidumpMultipartOverheadBytes
}

func minidumpMetadataFromForm(form *multipart.Form) minidumpMetadata {
	metadata := minidumpMetadata{}
	metadata = mergeSentryMetadataJSON(metadata, firstFormValue(form, "sentry"))
	metadata.eventID = firstNonEmpty(metadata.eventID, firstFormValue(form, "event_id"), firstFormValue(form, "sentry[event_id]"))
	metadata.release = firstNonEmpty(metadata.release, firstFormValue(form, "release"), firstFormValue(form, "sentry[release]"))
	metadata.environment = firstNonEmpty(metadata.environment, firstFormValue(form, "environment"), firstFormValue(form, "sentry[environment]"))
	metadata.level = firstNonEmpty(metadata.level, firstFormValue(form, "level"), firstFormValue(form, "sentry[level]"))
	metadata.platform = firstNonEmpty(metadata.platform, firstFormValue(form, "platform"), firstFormValue(form, "sentry[platform]"))

	return metadata
}

func mergeSentryMetadataJSON(metadata minidumpMetadata, input string) minidumpMetadata {
	if strings.TrimSpace(input) == "" {
		return metadata
	}

	var payload struct {
		EventID     string `json:"event_id"`
		Release     string `json:"release"`
		Environment string `json:"environment"`
		Level       string `json:"level"`
		Platform    string `json:"platform"`
	}
	decodeErr := json.Unmarshal([]byte(input), &payload)
	if decodeErr != nil {
		return metadata
	}

	metadata.eventID = firstNonEmpty(metadata.eventID, payload.EventID)
	metadata.release = firstNonEmpty(metadata.release, payload.Release)
	metadata.environment = firstNonEmpty(metadata.environment, payload.Environment)
	metadata.level = firstNonEmpty(metadata.level, payload.Level)
	metadata.platform = firstNonEmpty(metadata.platform, payload.Platform)

	return metadata
}

func firstFormValue(form *multipart.Form, name string) string {
	if form == nil {
		return ""
	}

	values := form.Value[name]
	if len(values) == 0 {
		return ""
	}

	return strings.TrimSpace(values[0])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func parseMinidumpNativeMetadata(file multipart.File, size int64) result.Result[minidumps.NativeMetadata] {
	_, seekErr := file.Seek(0, io.SeekStart)
	if seekErr != nil {
		return result.Err[minidumps.NativeMetadata](seekErr)
	}

	metadataResult := minidumps.ParseNativeMetadata(file, size)
	metadata, metadataErr := metadataResult.Value()

	_, resetErr := file.Seek(0, io.SeekStart)
	if resetErr != nil {
		return result.Err[minidumps.NativeMetadata](resetErr)
	}

	if metadataErr != nil {
		return result.Err[minidumps.NativeMetadata](metadataErr)
	}

	return result.Ok(metadata)
}

func prepareMinidumpEvent(
	auth domain.ProjectAuth,
	receivedAt domain.TimePoint,
	upload minidumpUpload,
	metadata minidumps.NativeMetadata,
) result.Result[preparedMinidumpEvent] {
	eventIDResult := minidumpEventID(upload.metadata)
	eventID, eventIDErr := eventIDResult.Value()
	if eventIDErr != nil {
		return result.Err[preparedMinidumpEvent](eventIDErr)
	}

	attachmentName, attachmentNameErr := domain.NewMinidumpAttachmentName("upload_file_minidump")
	if attachmentNameErr != nil {
		return result.Err[preparedMinidumpEvent](attachmentNameErr)
	}

	identity, identityErr := domain.NewMinidumpIdentity(eventID, attachmentName)
	if identityErr != nil {
		return result.Err[preparedMinidumpEvent](identityErr)
	}

	attachment, attachmentErr := domain.NewEventAttachment(
		domain.ArtifactKindMinidump(),
		identity.ArtifactName(),
		upload.fileHeader.Size,
		upload.fileHeader.Header.Get("Content-Type"),
	)
	if attachmentErr != nil {
		return result.Err[preparedMinidumpEvent](attachmentErr)
	}

	level, levelErr := minidumpLevel(upload.metadata)
	if levelErr != nil {
		return result.Err[preparedMinidumpEvent](levelErr)
	}

	title, titleErr := domain.NewEventTitle("Native crash minidump")
	if titleErr != nil {
		return result.Err[preparedMinidumpEvent](titleErr)
	}

	event, eventErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       auth.OrganizationID(),
		ProjectID:            auth.ProjectID(),
		EventID:              eventID,
		OccurredAt:           receivedAt,
		ReceivedAt:           receivedAt,
		Kind:                 domain.EventKindError,
		Level:                level,
		Title:                title,
		Platform:             minidumpPlatform(upload.metadata),
		Release:              upload.metadata.release,
		Environment:          upload.metadata.environment,
		DefaultGroupingParts: []string{"native", "minidump"},
		Attachments:          []domain.EventAttachment{attachment},
		NativeModules:        metadata.NativeModules(),
		NativeFrames:         metadata.NativeFrames(),
	})
	if eventErr != nil {
		return result.Err[preparedMinidumpEvent](eventErr)
	}

	return result.Ok(preparedMinidumpEvent{event: event, identity: identity})
}

func minidumpEventID(metadata minidumpMetadata) result.Result[domain.EventID] {
	if metadata.eventID != "" {
		eventID, eventIDErr := domain.NewEventID(metadata.eventID)
		if eventIDErr != nil {
			return result.Err[domain.EventID](errors.New("invalid_minidump_event_id"))
		}

		return result.Ok(eventID)
	}

	eventID, eventIDErr := randomEventID()
	if eventIDErr != nil {
		return result.Err[domain.EventID](eventIDErr)
	}

	return result.Ok(eventID)
}

func randomEventID() (domain.EventID, error) {
	bytes := make([]byte, 16)
	_, readErr := rand.Read(bytes)
	if readErr != nil {
		return domain.EventID{}, readErr
	}

	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return domain.NewEventID(hex.EncodeToString(bytes))
}

func minidumpLevel(metadata minidumpMetadata) (domain.EventLevel, error) {
	if metadata.level == "" {
		return domain.EventLevelFatal, nil
	}

	return domain.NewEventLevel(metadata.level)
}

func minidumpPlatform(metadata minidumpMetadata) string {
	if metadata.platform == "" {
		return "native"
	}

	return metadata.platform
}

func ingestMinidumpEvent(
	ctx context.Context,
	backend SentryIngestBackend,
	minidumpStore *minidumps.Service,
	prepared preparedMinidumpEvent,
	file multipart.File,
) result.Result[ingest.IngestReceipt] {
	uploaded := false
	uploadEffect := func(ctx context.Context, event ingestplan.AcceptedEvent) result.Result[struct{}] {
		_, seekErr := file.Seek(0, io.SeekStart)
		if seekErr != nil {
			return result.Err[struct{}](seekErr)
		}

		uploadResult := minidumpStore.Upload(
			ctx,
			prepared.event.OrganizationID(),
			prepared.event.ProjectID(),
			prepared.identity,
			file,
		)
		_, uploadErr := uploadResult.Value()
		if uploadErr != nil {
			return result.Err[struct{}](uploadErr)
		}

		uploaded = true
		return result.Ok(struct{}{})
	}

	receiptResult := ingest.IngestCanonicalEventWithAppendEffect(
		ctx,
		ingest.NewIngestCommand(prepared.event),
		backend,
		uploadEffect,
	)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		if uploaded {
			_ = cleanupStoredMinidump(ctx, minidumpStore, prepared)
		}

		return result.Err[ingest.IngestReceipt](receiptErr)
	}

	return result.Ok(receipt)
}

func cleanupStoredMinidump(
	ctx context.Context,
	minidumpStore *minidumps.Service,
	prepared preparedMinidumpEvent,
) error {
	deleteResult := minidumpStore.Delete(
		ctx,
		prepared.event.OrganizationID(),
		prepared.event.ProjectID(),
		prepared.identity,
	)
	_, deleteErr := deleteResult.Value()
	return deleteErr
}

func minidumpHTTPStatus(err error) int {
	if errors.Is(err, minidumps.ErrMinidumpTooLarge) {
		return http.StatusRequestEntityTooLarge
	}

	if errors.Is(err, minidumps.ErrUnsupportedMinidump) {
		return http.StatusBadRequest
	}

	switch err.Error() {
	case "invalid_multipart", "missing_upload_file_minidump", "invalid_minidump_event_id":
		return http.StatusBadRequest
	default:
		return http.StatusServiceUnavailable
	}
}

func minidumpErrorDetail(err error) string {
	if errors.Is(err, minidumps.ErrUnsupportedMinidump) {
		return "invalid_minidump"
	}

	if errors.Is(err, minidumps.ErrMinidumpTooLarge) {
		return "payload_too_large"
	}

	return err.Error()
}

type userFeedbackRequest struct {
	EventID  string `json:"event_id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	Comments string `json:"comments"`
}

func parseUserFeedbackRequest(body []byte) result.Result[sentryprotocol.UserReportItem] {
	var payload userFeedbackRequest
	decodeErr := json.Unmarshal(body, &payload)
	if decodeErr != nil {
		return result.Err[sentryprotocol.UserReportItem](errors.New("invalid_user_feedback"))
	}

	return result.Ok(sentryprotocol.UserReportItem{
		EventID:  strings.TrimSpace(payload.EventID),
		Name:     strings.TrimSpace(payload.Name),
		Email:    strings.TrimSpace(payload.Email),
		Comments: strings.TrimSpace(payload.Comments),
	})
}

func prepareParsedUserReports(
	auth domain.ProjectAuth,
	reports []sentryprotocol.UserReportItem,
) result.Result[[]userreportapp.SubmitCommand] {
	commands := []userreportapp.SubmitCommand{}

	for _, report := range reports {
		commandResult := prepareParsedUserReport(auth, report)
		command, commandErr := commandResult.Value()
		if commandErr != nil {
			return result.Err[[]userreportapp.SubmitCommand](commandErr)
		}

		commands = append(commands, command)
	}

	return result.Ok(commands)
}

func prepareParsedUserReport(
	auth domain.ProjectAuth,
	report sentryprotocol.UserReportItem,
) result.Result[userreportapp.SubmitCommand] {
	eventID, eventIDErr := domain.NewEventID(report.EventID)
	if eventIDErr != nil {
		return result.Err[userreportapp.SubmitCommand](errors.New("user report event id is invalid"))
	}

	commandResult := userreportapp.PrepareSubmit(userreportapp.SubmitCommand{
		Scope: userreportapp.Scope{
			OrganizationID: auth.OrganizationID(),
			ProjectID:      auth.ProjectID(),
		},
		EventID:  eventID,
		Name:     report.Name,
		Email:    report.Email,
		Comments: report.Comments,
	})
	command, commandErr := commandResult.Value()
	if commandErr != nil {
		return result.Err[userreportapp.SubmitCommand](commandErr)
	}

	return result.Ok(command)
}

func submitPreparedUserReports(
	ctx context.Context,
	writer userreportapp.Writer,
	commands []userreportapp.SubmitCommand,
) error {
	for _, command := range commands {
		receiptResult := userreportapp.Submit(ctx, writer, command)
		_, receiptErr := receiptResult.Value()
		if receiptErr != nil {
			return receiptErr
		}
	}

	return nil
}

func writeProtocolError(w http.ResponseWriter, err error) {
	var protocolErr sentryprotocol.ProtocolError
	if errors.As(err, &protocolErr) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"detail": string(protocolErr.Code())})
		return
	}

	writeJSON(w, http.StatusBadRequest, map[string]string{"detail": "invalid_event"})
}

func writeIngestReceipt(w http.ResponseWriter, receipt ingest.IngestReceipt) {
	if receipt.Kind() == ingest.ReceiptQuotaRejected {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"detail": receipt.Reason(),
			"id":     receipt.EventID().Hex(),
		})
		return
	}

	body := map[string]any{
		"id": receipt.EventID().Hex(),
	}

	if receipt.Kind() == ingest.ReceiptDuplicateEvent {
		body["duplicate"] = true
	}

	writeJSON(w, http.StatusOK, body)
}

func writeLogReceipt(w http.ResponseWriter, receipt logapp.IngestReceipt) {
	if receipt.Kind() == logapp.ReceiptQuotaRejected {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"detail": receipt.Reason(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"accepted": receipt.Count(),
	})
}

func writeRateLimitDecision(w http.ResponseWriter, decision ratelimitapp.Decision) {
	w.Header().Set("Retry-After", retryAfterSeconds(decision.RetryAfter()))
	writeJSON(w, http.StatusTooManyRequests, map[string]string{
		"detail": decision.Reason(),
	})
}

func retryAfterSeconds(duration time.Duration) string {
	seconds := int(duration.Seconds())
	if seconds < 1 {
		seconds = 1
	}

	return strconv.Itoa(seconds)
}

type deniedError struct{}

func errDenied() deniedError {
	return deniedError{}
}

func (deniedError) Error() string {
	return "denied"
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

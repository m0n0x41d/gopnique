package httpadapter

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/adapters/sentryprotocol"
	"github.com/ivanzakutnii/error-tracker/internal/app/ingest"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const maxStoreRequestBytes = 1 * 1024 * 1024

var sentryKeyPattern = regexp.MustCompile(`(?:^|[\s,])(sentry_key|glitchtip_key)=([^,\s]+)`)

type SentryIngestBackend interface {
	ingest.ProjectDirectory
	ingest.IngestTransaction
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
	event    domain.CanonicalEvent
	hasEvent bool
}

func sentryStoreHandler(backend SentryIngestBackend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSentryIngestCarrier(w, r, backend, sentryStoreRequest)
	}
}

func sentryEnvelopeHandler(backend SentryIngestBackend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handleSentryIngestCarrier(w, r, backend, sentryEnvelopeRequest)
	}
}

func handleSentryIngestCarrier(
	w http.ResponseWriter,
	r *http.Request,
	backend SentryIngestBackend,
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

	if !parsed.hasEvent {
		writeJSON(w, http.StatusOK, map[string]string{})
		return
	}

	receiptResult := ingest.IngestCanonicalEvent(r.Context(), ingest.NewIngestCommand(parsed.event), backend)
	receipt, receiptErr := receiptResult.Value()
	if receiptErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": "storage_unavailable"})
		return
	}

	writeIngestReceipt(w, receipt)
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

	event, ok := envelope.Event()
	if !ok {
		return result.Ok(parsedSentryEvent{})
	}

	return result.Ok(parsedSentryEvent{event: event, hasEvent: true})
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
	body := map[string]any{
		"id": receipt.EventID().Hex(),
	}

	if receipt.Kind() == ingest.ReceiptDuplicateEvent {
		body["duplicate"] = true
	}

	writeJSON(w, http.StatusOK, body)
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

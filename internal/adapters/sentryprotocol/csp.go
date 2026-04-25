package sentryprotocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type rawCSPReportCarrier struct {
	Report rawCSPReport `json:"csp-report"`
}

type rawCSPReport struct {
	DocumentURI        string `json:"document-uri"`
	Referrer           string `json:"referrer"`
	ViolatedDirective  string `json:"violated-directive"`
	EffectiveDirective string `json:"effective-directive"`
	BlockedURI         string `json:"blocked-uri"`
	SourceFile         string `json:"source-file"`
	Disposition        string `json:"disposition"`
	LineNumber         int    `json:"line-number"`
	ColumnNumber       int    `json:"column-number"`
	StatusCode         int    `json:"status-code"`
}

type rawReportingAPIReport struct {
	Type string              `json:"type"`
	URL  string              `json:"url"`
	Body rawReportingCSPBody `json:"body"`
}

type rawReportingCSPBody struct {
	DocumentURL        string `json:"documentURL"`
	Referrer           string `json:"referrer"`
	ViolatedDirective  string `json:"violatedDirective"`
	EffectiveDirective string `json:"effectiveDirective"`
	BlockedURL         string `json:"blockedURL"`
	SourceFile         string `json:"sourceFile"`
	Disposition        string `json:"disposition"`
	LineNumber         int    `json:"lineNumber"`
	ColumnNumber       int    `json:"columnNumber"`
	StatusCode         int    `json:"statusCode"`
}

func ParseCSPReport(
	project ProjectContext,
	receivedAt domain.TimePoint,
	payload []byte,
) result.Result[domain.CanonicalEvent] {
	reportResult := parseCSPReportPayload(payload)
	report, reportErr := reportResult.Value()
	if reportErr != nil {
		return result.Err[domain.CanonicalEvent](reportErr)
	}

	eventID, eventIDErr := deterministicCSPEventID(payload, receivedAt)
	if eventIDErr != nil {
		return result.Err[domain.CanonicalEvent](eventIDErr)
	}

	titleResult := cspTitle(report)
	title, titleErr := titleResult.Value()
	if titleErr != nil {
		return result.Err[domain.CanonicalEvent](titleErr)
	}

	canonical, canonicalErr := domain.NewCanonicalEvent(domain.CanonicalEventParams{
		OrganizationID:       project.OrganizationID(),
		ProjectID:            project.ProjectID(),
		EventID:              eventID,
		OccurredAt:           receivedAt,
		ReceivedAt:           receivedAt,
		Kind:                 domain.EventKindDefault,
		Level:                domain.EventLevelWarning,
		Title:                title,
		Platform:             "security",
		Tags:                 cspTags(report),
		DefaultGroupingParts: cspGroupingParts(report, title),
	})
	if canonicalErr != nil {
		return result.Err[domain.CanonicalEvent](canonicalErr)
	}

	return result.Ok(canonical)
}

func parseCSPReportPayload(payload []byte) result.Result[rawCSPReport] {
	legacyResult := parseLegacyCSPReport(payload)
	legacy, legacyErr := legacyResult.Value()
	if legacyErr == nil {
		return result.Ok(legacy)
	}

	reportingResult := parseReportingAPICSPReport(payload)
	reporting, reportingErr := reportingResult.Value()
	if reportingErr == nil {
		return result.Ok(reporting)
	}

	return result.Err[rawCSPReport](NewProtocolError(ErrorInvalidEvent, "invalid csp report"))
}

func parseLegacyCSPReport(payload []byte) result.Result[rawCSPReport] {
	var carrier rawCSPReportCarrier
	decodeErr := json.Unmarshal(payload, &carrier)
	if decodeErr != nil {
		return result.Err[rawCSPReport](decodeErr)
	}

	report := normalizeCSPReport(carrier.Report)
	if !validCSPReport(report) {
		return result.Err[rawCSPReport](NewProtocolError(ErrorInvalidEvent, "csp report is incomplete"))
	}

	return result.Ok(report)
}

func parseReportingAPICSPReport(payload []byte) result.Result[rawCSPReport] {
	var report rawReportingAPIReport
	decodeErr := json.Unmarshal(payload, &report)
	if decodeErr == nil && report.Type == "csp-violation" {
		return normalizeReportingAPIReport(report)
	}

	var reports []rawReportingAPIReport
	arrayErr := json.Unmarshal(payload, &reports)
	if arrayErr != nil {
		return result.Err[rawCSPReport](arrayErr)
	}

	for _, item := range reports {
		if item.Type != "csp-violation" {
			continue
		}

		return normalizeReportingAPIReport(item)
	}

	return result.Err[rawCSPReport](NewProtocolError(ErrorInvalidEvent, "csp report is missing"))
}

func normalizeReportingAPIReport(report rawReportingAPIReport) result.Result[rawCSPReport] {
	body := report.Body
	normalized := normalizeCSPReport(rawCSPReport{
		DocumentURI:        firstNonEmpty(body.DocumentURL, report.URL),
		Referrer:           body.Referrer,
		ViolatedDirective:  body.ViolatedDirective,
		EffectiveDirective: body.EffectiveDirective,
		BlockedURI:         body.BlockedURL,
		SourceFile:         body.SourceFile,
		Disposition:        body.Disposition,
		LineNumber:         body.LineNumber,
		ColumnNumber:       body.ColumnNumber,
		StatusCode:         body.StatusCode,
	})
	if !validCSPReport(normalized) {
		return result.Err[rawCSPReport](NewProtocolError(ErrorInvalidEvent, "csp report is incomplete"))
	}

	return result.Ok(normalized)
}

func normalizeCSPReport(report rawCSPReport) rawCSPReport {
	report.DocumentURI = strings.TrimSpace(report.DocumentURI)
	report.Referrer = strings.TrimSpace(report.Referrer)
	report.ViolatedDirective = strings.TrimSpace(report.ViolatedDirective)
	report.EffectiveDirective = strings.TrimSpace(report.EffectiveDirective)
	report.BlockedURI = strings.TrimSpace(report.BlockedURI)
	report.SourceFile = strings.TrimSpace(report.SourceFile)
	report.Disposition = strings.TrimSpace(report.Disposition)

	return report
}

func validCSPReport(report rawCSPReport) bool {
	return cspDirective(report) != "" &&
		firstNonEmpty(report.DocumentURI, report.SourceFile, report.BlockedURI) != ""
}

func cspTitle(report rawCSPReport) result.Result[domain.EventTitle] {
	blocked := firstNonEmpty(report.BlockedURI, "inline")
	title, titleErr := domain.NewEventTitle(
		fmt.Sprintf("CSP violation: %s blocked %s", cspDirective(report), blocked),
	)
	if titleErr != nil {
		return result.Err[domain.EventTitle](titleErr)
	}

	return result.Ok(title)
}

func cspTags(report rawCSPReport) map[string]string {
	tags := map[string]string{
		"security.type":           "csp",
		"csp.effective_directive": cspDirective(report),
	}
	appendTag(tags, "csp.violated_directive", report.ViolatedDirective)
	appendTag(tags, "csp.blocked_uri", report.BlockedURI)
	appendTag(tags, "csp.document_uri", report.DocumentURI)
	appendTag(tags, "csp.source_file", report.SourceFile)
	appendTag(tags, "csp.disposition", report.Disposition)

	return tags
}

func cspGroupingParts(report rawCSPReport, title domain.EventTitle) []string {
	return []string{
		"csp",
		cspDirective(report),
		firstNonEmpty(report.SourceFile, report.DocumentURI),
		firstNonEmpty(report.BlockedURI, title.String()),
	}
}

func deterministicCSPEventID(
	payload []byte,
	receivedAt domain.TimePoint,
) (domain.EventID, error) {
	hash := sha256.Sum256(append(payload, []byte(receivedAt.Time().Format(time.RFC3339Nano))...))
	bytes := append([]byte{}, hash[:16]...)
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	return domain.NewEventID(hex.EncodeToString(bytes))
}

func cspDirective(report rawCSPReport) string {
	return firstNonEmpty(report.EffectiveDirective, report.ViolatedDirective)
}

func appendTag(tags map[string]string, key string, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}

	tags[key] = value
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

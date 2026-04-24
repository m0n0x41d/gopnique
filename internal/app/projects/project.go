package projects

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

type Reader interface {
	FindCurrentProject(ctx context.Context, query ProjectQuery) result.Result[ProjectRecord]
}

type Scope struct {
	OrganizationID domain.OrganizationID
	ProjectID      domain.ProjectID
}

type ProjectQuery struct {
	Scope     Scope
	PublicURL string
}

type ProjectRecord struct {
	OrganizationName string
	ProjectID        domain.ProjectID
	Name             string
	Slug             string
	IngestRef        string
	AcceptingEvents  bool
	ScrubIPAddresses bool
	FirstEventAt     *time.Time
	CreatedAt        time.Time
	ActiveKey        ProjectKeyRecord
}

type ProjectKeyRecord struct {
	ID        domain.ProjectKeyID
	PublicKey domain.ProjectPublicKey
	Label     string
	CreatedAt time.Time
}

type ProjectView struct {
	OrganizationName string
	ProjectID        string
	Name             string
	Slug             string
	IngestRef        string
	AcceptingEvents  string
	ScrubIPAddresses string
	FirstEventAt     string
	CreatedAt        string
	PublicKey        string
	PublicKeyID      string
	KeyLabel         string
	KeyCreatedAt     string
	DSN              string
	StoreEndpoint    string
	EnvelopeEndpoint string
	SDKSnippets      []SDKSnippetView
}

type SDKSnippetView struct {
	Name    string
	Package string
	Code    string
}

func ShowCurrentProject(
	ctx context.Context,
	reader Reader,
	query ProjectQuery,
) result.Result[ProjectView] {
	if reader == nil {
		return result.Err[ProjectView](errors.New("project reader is required"))
	}

	scopeErr := requireScope(query.Scope)
	if scopeErr != nil {
		return result.Err[ProjectView](scopeErr)
	}

	publicURL, publicURLErr := normalizePublicURL(query.PublicURL)
	if publicURLErr != nil {
		return result.Err[ProjectView](publicURLErr)
	}

	recordResult := reader.FindCurrentProject(ctx, query)
	record, recordErr := recordResult.Value()
	if recordErr != nil {
		return result.Err[ProjectView](recordErr)
	}

	return result.Ok(projectViewFromRecord(record, publicURL))
}

func requireScope(scope Scope) error {
	if scope.OrganizationID.String() == "" || scope.ProjectID.String() == "" {
		return errors.New("project scope is required")
	}

	return nil
}

func normalizePublicURL(input string) (string, error) {
	value := strings.TrimRight(strings.TrimSpace(input), "/")
	if value == "" {
		return "", errors.New("public url is required")
	}

	parsed, parseErr := url.Parse(value)
	if parseErr != nil {
		return "", parseErr
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("public url must use http or https")
	}

	if parsed.Host == "" {
		return "", errors.New("public url host is required")
	}

	return value, nil
}

func projectViewFromRecord(record ProjectRecord, publicURL string) ProjectView {
	dsn := dsnFromPublicURL(publicURL, record.IngestRef, record.ActiveKey.PublicKey)
	return ProjectView{
		OrganizationName: record.OrganizationName,
		ProjectID:        record.ProjectID.String(),
		Name:             record.Name,
		Slug:             record.Slug,
		IngestRef:        record.IngestRef,
		AcceptingEvents:  boolStatus(record.AcceptingEvents),
		ScrubIPAddresses: boolStatus(record.ScrubIPAddresses),
		FirstEventAt:     optionalTime(record.FirstEventAt),
		CreatedAt:        formatTime(record.CreatedAt),
		PublicKey:        record.ActiveKey.PublicKey.Hex(),
		PublicKeyID:      record.ActiveKey.ID.String(),
		KeyLabel:         record.ActiveKey.Label,
		KeyCreatedAt:     formatTime(record.ActiveKey.CreatedAt),
		DSN:              dsn,
		StoreEndpoint:    publicURL + "/api/" + record.IngestRef + "/store/",
		EnvelopeEndpoint: publicURL + "/api/" + record.IngestRef + "/envelope/",
		SDKSnippets:      sdkSnippets(dsn),
	}
}

func sdkSnippets(dsn string) []SDKSnippetView {
	return []SDKSnippetView{
		{
			Name:    "Node",
			Package: "@sentry/node",
			Code: strings.Join([]string{
				`const Sentry = require("@sentry/node");`,
				"",
				"Sentry.init({",
				`  dsn: "` + dsn + `",`,
				"  tracesSampleRate: 0,",
				"});",
			}, "\n"),
		},
		{
			Name:    "Browser",
			Package: "@sentry/browser",
			Code: strings.Join([]string{
				`import * as Sentry from "@sentry/browser";`,
				"",
				"Sentry.init({",
				`  dsn: "` + dsn + `",`,
				"  tracesSampleRate: 0,",
				"});",
			}, "\n"),
		},
		{
			Name:    "Python",
			Package: "sentry-sdk",
			Code: strings.Join([]string{
				"import sentry_sdk",
				"",
				`sentry_sdk.init(dsn="` + dsn + `")`,
			}, "\n"),
		},
		{
			Name:    "Go",
			Package: "sentry-go",
			Code: strings.Join([]string{
				`import "github.com/getsentry/sentry-go"`,
				"",
				"sentry.Init(sentry.ClientOptions{",
				`  Dsn: "` + dsn + `",`,
				"})",
			}, "\n"),
		},
	}
}

func dsnFromPublicURL(
	publicURL string,
	projectRef string,
	publicKey domain.ProjectPublicKey,
) string {
	parsed, parseErr := url.Parse(publicURL)
	if parseErr != nil {
		return ""
	}

	parsed.User = url.User(publicKey.Hex())
	parsed.Path = path.Join(parsed.Path, projectRef)

	return parsed.String()
}

func boolStatus(value bool) string {
	if value {
		return "enabled"
	}

	return "disabled"
}

func optionalTime(value *time.Time) string {
	if value == nil {
		return "none"
	}

	return formatTime(*value)
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02 15:04:05 UTC")
}

package domain

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	importSourceSystemMaxBytes = 64
	importExternalIDMaxBytes   = 256
)

var importSourceSystemPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

type ImportMode string

const (
	ImportModeDryRun ImportMode = "dry-run"
	ImportModeApply  ImportMode = "apply"
)

type ImportRecordKind string

const (
	ImportRecordKindIssue ImportRecordKind = "issue"
	ImportRecordKindEvent ImportRecordKind = "event"
)

type ImportSourceSystem struct {
	value string
}

type ImportExternalID struct {
	value string
}

type ImportManifestParams struct {
	OrganizationID OrganizationID
	ProjectID      ProjectID
	SourceSystem   ImportSourceSystem
	Mode           ImportMode
}

type ImportManifest struct {
	organizationID OrganizationID
	projectID      ProjectID
	sourceSystem   ImportSourceSystem
	mode           ImportMode
}

type ImportRecordParams struct {
	SourceSystem ImportSourceSystem
	ExternalID   ImportExternalID
	Kind         ImportRecordKind
	Event        CanonicalEvent
}

type ImportRecord struct {
	sourceSystem ImportSourceSystem
	externalID   ImportExternalID
	kind         ImportRecordKind
	event        CanonicalEvent
}

func NewImportSourceSystem(input string) (ImportSourceSystem, error) {
	value := strings.TrimSpace(input)
	value = strings.ToLower(value)

	if value == "" {
		return ImportSourceSystem{}, errors.New("import source system is required")
	}

	if len(value) > importSourceSystemMaxBytes {
		return ImportSourceSystem{}, errors.New("import source system is too long")
	}

	if !importSourceSystemPattern.MatchString(value) {
		return ImportSourceSystem{}, errors.New("import source system is invalid")
	}

	return ImportSourceSystem{value: value}, nil
}

func NewImportExternalID(input string) (ImportExternalID, error) {
	value, valueErr := normalizeImportText(input, importExternalIDMaxBytes, "import external id")
	if valueErr != nil {
		return ImportExternalID{}, valueErr
	}

	if value == "" {
		return ImportExternalID{}, errors.New("import external id is required")
	}

	return ImportExternalID{value: value}, nil
}

func NewImportMode(input string) (ImportMode, error) {
	value := ImportMode(strings.TrimSpace(input))

	if !value.valid() {
		return "", errors.New("import mode is invalid")
	}

	return value, nil
}

func NewImportRecordKind(input string) (ImportRecordKind, error) {
	value := ImportRecordKind(strings.TrimSpace(input))

	if !value.valid() {
		return "", errors.New("import record kind is invalid")
	}

	return value, nil
}

func NewImportManifest(params ImportManifestParams) (ImportManifest, error) {
	if params.OrganizationID.value == "" {
		return ImportManifest{}, errors.New("organization id is required")
	}

	if params.ProjectID.value == "" {
		return ImportManifest{}, errors.New("project id is required")
	}

	if params.SourceSystem.value == "" {
		return ImportManifest{}, errors.New("import source system is required")
	}

	if !params.Mode.valid() {
		return ImportManifest{}, errors.New("import mode is invalid")
	}

	return ImportManifest{
		organizationID: params.OrganizationID,
		projectID:      params.ProjectID,
		sourceSystem:   params.SourceSystem,
		mode:           params.Mode,
	}, nil
}

func NewImportRecord(params ImportRecordParams) (ImportRecord, error) {
	if params.SourceSystem.value == "" {
		return ImportRecord{}, errors.New("import source system is required")
	}

	if params.ExternalID.value == "" {
		return ImportRecord{}, errors.New("import external id is required")
	}

	if !params.Kind.valid() {
		return ImportRecord{}, errors.New("import record kind is invalid")
	}

	if params.Event.organizationID.value == "" || params.Event.projectID.value == "" {
		return ImportRecord{}, errors.New("import event scope is required")
	}

	if !params.Event.CreatesIssue() {
		return ImportRecord{}, errors.New("import record event must create an issue")
	}

	return ImportRecord{
		sourceSystem: params.SourceSystem,
		externalID:   params.ExternalID,
		kind:         params.Kind,
		event:        params.Event,
	}, nil
}

func normalizeImportText(input string, limit int, label string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	if !utf8.ValidString(value) {
		return "", errors.New(label + " must be valid utf-8")
	}

	if len(value) > limit {
		return "", errors.New(label + " is too long")
	}

	for _, char := range value {
		if char == '\n' || char == '\r' || char == '\t' {
			continue
		}

		if unicode.IsControl(char) {
			return "", errors.New(label + " must not contain control characters")
		}
	}

	return value, nil
}

func (mode ImportMode) valid() bool {
	return mode == ImportModeDryRun || mode == ImportModeApply
}

func (kind ImportRecordKind) valid() bool {
	return kind == ImportRecordKindIssue || kind == ImportRecordKindEvent
}

func (source ImportSourceSystem) String() string {
	return source.value
}

func (externalID ImportExternalID) String() string {
	return externalID.value
}

func (manifest ImportManifest) OrganizationID() OrganizationID {
	return manifest.organizationID
}

func (manifest ImportManifest) ProjectID() ProjectID {
	return manifest.projectID
}

func (manifest ImportManifest) SourceSystem() ImportSourceSystem {
	return manifest.sourceSystem
}

func (manifest ImportManifest) Mode() ImportMode {
	return manifest.mode
}

func (record ImportRecord) SourceSystem() ImportSourceSystem {
	return record.sourceSystem
}

func (record ImportRecord) ExternalID() ImportExternalID {
	return record.externalID
}

func (record ImportRecord) Kind() ImportRecordKind {
	return record.kind
}

func (record ImportRecord) Event() CanonicalEvent {
	return record.event
}

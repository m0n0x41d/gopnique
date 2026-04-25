package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const artifactNameMaxBytes = 255

type ArtifactKind struct {
	value string
}

type ArtifactName struct {
	value string
}

type ArtifactKey struct {
	organizationID OrganizationID
	projectID      ProjectID
	kind           ArtifactKind
	name           ArtifactName
}

func ArtifactKindSourceMap() ArtifactKind {
	return ArtifactKind{value: "source_map"}
}

func ArtifactKindDebugFile() ArtifactKind {
	return ArtifactKind{value: "debug_file"}
}

func ArtifactKindMinidump() ArtifactKind {
	return ArtifactKind{value: "minidump"}
}

func ArtifactKindAttachment() ArtifactKind {
	return ArtifactKind{value: "attachment"}
}

func ParseArtifactKind(input string) (ArtifactKind, error) {
	value := strings.TrimSpace(strings.ToLower(input))

	switch value {
	case "source_map":
		return ArtifactKindSourceMap(), nil
	case "debug_file":
		return ArtifactKindDebugFile(), nil
	case "minidump":
		return ArtifactKindMinidump(), nil
	case "attachment":
		return ArtifactKindAttachment(), nil
	default:
		return ArtifactKind{}, errors.New("artifact kind is invalid")
	}
}

func (kind ArtifactKind) String() string {
	return kind.value
}

func NewArtifactName(input string) (ArtifactName, error) {
	value := strings.TrimSpace(input)

	if value == "" {
		return ArtifactName{}, errors.New("artifact name is required")
	}

	if !utf8.ValidString(value) {
		return ArtifactName{}, errors.New("artifact name must be valid utf-8")
	}

	if len(value) > artifactNameMaxBytes {
		return ArtifactName{}, errors.New("artifact name is too long")
	}

	if strings.HasPrefix(value, ".") {
		return ArtifactName{}, errors.New("artifact name must not start with a dot")
	}

	if strings.ContainsAny(value, "/\\\x00") {
		return ArtifactName{}, errors.New("artifact name must not contain path separators")
	}

	if value == "." || value == ".." || strings.Contains(value, "/..") || strings.Contains(value, "../") {
		return ArtifactName{}, errors.New("artifact name must not traverse paths")
	}

	for _, runeValue := range value {
		if runeValue < 0x20 || runeValue == 0x7f {
			return ArtifactName{}, errors.New("artifact name must not contain control characters")
		}
	}

	return ArtifactName{value: value}, nil
}

func (name ArtifactName) String() string {
	return name.value
}

func NewArtifactKey(
	organizationID OrganizationID,
	projectID ProjectID,
	kind ArtifactKind,
	name ArtifactName,
) (ArtifactKey, error) {
	if organizationID.value == "" {
		return ArtifactKey{}, errors.New("artifact key requires organization id")
	}

	if projectID.value == "" {
		return ArtifactKey{}, errors.New("artifact key requires project id")
	}

	if kind.value == "" {
		return ArtifactKey{}, errors.New("artifact key requires kind")
	}

	if name.value == "" {
		return ArtifactKey{}, errors.New("artifact key requires name")
	}

	return ArtifactKey{
		organizationID: organizationID,
		projectID:      projectID,
		kind:           kind,
		name:           name,
	}, nil
}

func (key ArtifactKey) OrganizationID() OrganizationID {
	return key.organizationID
}

func (key ArtifactKey) ProjectID() ProjectID {
	return key.projectID
}

func (key ArtifactKey) Kind() ArtifactKind {
	return key.kind
}

func (key ArtifactKey) Name() ArtifactName {
	return key.name
}

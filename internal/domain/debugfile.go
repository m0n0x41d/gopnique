package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	debugIdentifierMinHexBytes = 32
	debugIdentifierMaxHexBytes = 40
	debugFileNameMaxBytes      = 512
)

type DebugFileKind struct {
	value string
}

type DebugIdentifier struct {
	value string
}

type DebugFileName struct {
	value string
}

type DebugFileIdentity struct {
	debugID  DebugIdentifier
	kind     DebugFileKind
	fileName DebugFileName
}

func DebugFileKindBreakpad() DebugFileKind {
	return DebugFileKind{value: "breakpad"}
}

func DebugFileKindELF() DebugFileKind {
	return DebugFileKind{value: "elf"}
}

func DebugFileKindMachO() DebugFileKind {
	return DebugFileKind{value: "macho"}
}

func DebugFileKindPDB() DebugFileKind {
	return DebugFileKind{value: "pdb"}
}

func ParseDebugFileKind(input string) (DebugFileKind, error) {
	value := strings.TrimSpace(strings.ToLower(input))

	switch value {
	case "breakpad":
		return DebugFileKindBreakpad(), nil
	case "elf":
		return DebugFileKindELF(), nil
	case "macho":
		return DebugFileKindMachO(), nil
	case "pdb":
		return DebugFileKindPDB(), nil
	default:
		return DebugFileKind{}, errors.New("debug file kind is invalid")
	}
}

func (kind DebugFileKind) String() string {
	return kind.value
}

func NewDebugIdentifier(input string) (DebugIdentifier, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return DebugIdentifier{}, errors.New("debug identifier is required")
	}

	normalized := strings.ToLower(strings.ReplaceAll(value, "-", ""))

	if len(normalized) < debugIdentifierMinHexBytes {
		return DebugIdentifier{}, errors.New("debug identifier is too short")
	}

	if len(normalized) > debugIdentifierMaxHexBytes {
		return DebugIdentifier{}, errors.New("debug identifier is too long")
	}

	for _, runeValue := range normalized {
		if !isHexDigit(runeValue) {
			return DebugIdentifier{}, errors.New("debug identifier must be hexadecimal")
		}
	}

	return DebugIdentifier{value: normalized}, nil
}

func (id DebugIdentifier) String() string {
	return id.value
}

func NewDebugFileName(input string) (DebugFileName, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return DebugFileName{}, errors.New("debug file name is required")
	}

	if !utf8.ValidString(value) {
		return DebugFileName{}, errors.New("debug file name must be valid utf-8")
	}

	if len(value) > debugFileNameMaxBytes {
		return DebugFileName{}, errors.New("debug file name is too long")
	}

	if !visibleString(value) {
		return DebugFileName{}, errors.New("debug file name must not contain control characters")
	}

	if strings.ContainsAny(value, "\\\x00") {
		return DebugFileName{}, errors.New("debug file name must not contain backslashes or null bytes")
	}

	if strings.Contains(value, "..") {
		return DebugFileName{}, errors.New("debug file name must not traverse paths")
	}

	if strings.HasPrefix(value, "/") {
		return DebugFileName{}, errors.New("debug file name must be relative")
	}

	return DebugFileName{value: value}, nil
}

func (name DebugFileName) String() string {
	return name.value
}

func NewDebugFileIdentity(
	debugID DebugIdentifier,
	kind DebugFileKind,
	fileName DebugFileName,
) (DebugFileIdentity, error) {
	if debugID.value == "" {
		return DebugFileIdentity{}, errors.New("debug file identity requires debug identifier")
	}

	if kind.value == "" {
		return DebugFileIdentity{}, errors.New("debug file identity requires kind")
	}

	if fileName.value == "" {
		return DebugFileIdentity{}, errors.New("debug file identity requires file name")
	}

	return DebugFileIdentity{
		debugID:  debugID,
		kind:     kind,
		fileName: fileName,
	}, nil
}

func (identity DebugFileIdentity) DebugID() DebugIdentifier {
	return identity.debugID
}

func (identity DebugFileIdentity) Kind() DebugFileKind {
	return identity.kind
}

func (identity DebugFileIdentity) FileName() DebugFileName {
	return identity.fileName
}

func (identity DebugFileIdentity) ArtifactName() ArtifactName {
	const fieldSeparator = "\x1f"
	hasher := sha256.New()
	hasher.Write([]byte(identity.debugID.value))
	hasher.Write([]byte(fieldSeparator))
	hasher.Write([]byte(identity.kind.value))
	hasher.Write([]byte(fieldSeparator))
	hasher.Write([]byte(identity.fileName.value))
	digest := hex.EncodeToString(hasher.Sum(nil))

	return ArtifactName{value: digest + ".dif"}
}

func isHexDigit(runeValue rune) bool {
	switch {
	case runeValue >= '0' && runeValue <= '9':
		return true
	case runeValue >= 'a' && runeValue <= 'f':
		return true
	}

	return false
}

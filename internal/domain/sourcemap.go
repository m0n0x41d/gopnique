package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	releaseNameMaxBytes       = 200
	distNameMaxBytes          = 64
	sourceMapFileNameMaxBytes = 512
)

type ReleaseName struct {
	value string
}

type DistName struct {
	value string
}

type SourceMapFileName struct {
	value string
}

type SourceMapIdentity struct {
	release  ReleaseName
	dist     DistName
	fileName SourceMapFileName
}

func NewReleaseName(input string) (ReleaseName, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return ReleaseName{}, errors.New("release is required")
	}

	if !utf8.ValidString(value) {
		return ReleaseName{}, errors.New("release must be valid utf-8")
	}

	if len(value) > releaseNameMaxBytes {
		return ReleaseName{}, errors.New("release is too long")
	}

	if !visibleString(value) {
		return ReleaseName{}, errors.New("release must not contain control characters")
	}

	return ReleaseName{value: value}, nil
}

func (release ReleaseName) String() string {
	return release.value
}

func NewOptionalDistName(input string) (DistName, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return DistName{}, nil
	}

	if !utf8.ValidString(value) {
		return DistName{}, errors.New("dist must be valid utf-8")
	}

	if len(value) > distNameMaxBytes {
		return DistName{}, errors.New("dist is too long")
	}

	if !visibleString(value) {
		return DistName{}, errors.New("dist must not contain control characters")
	}

	return DistName{value: value}, nil
}

func (dist DistName) String() string {
	return dist.value
}

func (dist DistName) HasValue() bool {
	return dist.value != ""
}

func NewSourceMapFileName(input string) (SourceMapFileName, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return SourceMapFileName{}, errors.New("source map file name is required")
	}

	if !utf8.ValidString(value) {
		return SourceMapFileName{}, errors.New("source map file name must be valid utf-8")
	}

	if len(value) > sourceMapFileNameMaxBytes {
		return SourceMapFileName{}, errors.New("source map file name is too long")
	}

	if !visibleString(value) {
		return SourceMapFileName{}, errors.New("source map file name must not contain control characters")
	}

	if strings.ContainsAny(value, "\\\x00") {
		return SourceMapFileName{}, errors.New("source map file name must not contain backslashes or null bytes")
	}

	if strings.Contains(value, "..") {
		return SourceMapFileName{}, errors.New("source map file name must not traverse paths")
	}

	if strings.HasPrefix(value, "/") {
		return SourceMapFileName{}, errors.New("source map file name must be relative")
	}

	return SourceMapFileName{value: value}, nil
}

func (file SourceMapFileName) String() string {
	return file.value
}

func NewSourceMapIdentity(
	release ReleaseName,
	dist DistName,
	fileName SourceMapFileName,
) (SourceMapIdentity, error) {
	if release.value == "" {
		return SourceMapIdentity{}, errors.New("source map identity requires release")
	}

	if fileName.value == "" {
		return SourceMapIdentity{}, errors.New("source map identity requires file name")
	}

	return SourceMapIdentity{
		release:  release,
		dist:     dist,
		fileName: fileName,
	}, nil
}

func (identity SourceMapIdentity) Release() ReleaseName {
	return identity.release
}

func (identity SourceMapIdentity) Dist() DistName {
	return identity.dist
}

func (identity SourceMapIdentity) FileName() SourceMapFileName {
	return identity.fileName
}

func (identity SourceMapIdentity) ArtifactName() ArtifactName {
	const fieldSeparator = "\x1f"
	hasher := sha256.New()
	hasher.Write([]byte(identity.release.value))
	hasher.Write([]byte(fieldSeparator))
	hasher.Write([]byte(identity.dist.value))
	hasher.Write([]byte(fieldSeparator))
	hasher.Write([]byte(identity.fileName.value))
	digest := hex.EncodeToString(hasher.Sum(nil))

	return ArtifactName{value: digest + ".map"}
}

func visibleString(input string) bool {
	for _, runeValue := range input {
		if runeValue < 0x20 || runeValue == 0x7f {
			return false
		}
	}

	return true
}

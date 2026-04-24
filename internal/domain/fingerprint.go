package domain

import (
	"errors"
	"regexp"
	"strings"
)

var fingerprintPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Fingerprint struct {
	algorithm string
	value     string
}

func NewFingerprint(algorithm string, value string) (Fingerprint, error) {
	algorithm = strings.TrimSpace(algorithm)
	value = strings.TrimSpace(strings.ToLower(value))

	if algorithm == "" {
		return Fingerprint{}, errors.New("fingerprint algorithm is required")
	}

	if !fingerprintPattern.MatchString(value) {
		return Fingerprint{}, errors.New("fingerprint value must be sha256 hex")
	}

	return Fingerprint{
		algorithm: algorithm,
		value:     value,
	}, nil
}

func (fingerprint Fingerprint) Algorithm() string {
	return fingerprint.algorithm
}

func (fingerprint Fingerprint) Value() string {
	return fingerprint.value
}

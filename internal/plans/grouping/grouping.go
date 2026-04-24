package grouping

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const Algorithm = "grouping-v0"

type Source struct {
	Kind  string
	Parts []string
}

func ComputeFingerprint(event domain.CanonicalEvent) result.Result[domain.Fingerprint] {
	source := sourceFor(event)
	encodedParts, encodeErr := json.Marshal(source.Parts)
	if encodeErr != nil {
		return result.Err[domain.Fingerprint](encodeErr)
	}

	canonical := fmt.Sprintf(
		"%s\nproject:%s\nkind:%s\nsource:%s\nparts:%s",
		Algorithm,
		event.ProjectID().String(),
		event.Kind(),
		source.Kind,
		string(encodedParts),
	)

	sum := sha256.Sum256([]byte(canonical))
	value := hex.EncodeToString(sum[:])

	fingerprint, fingerprintErr := domain.NewFingerprint(Algorithm, value)
	if fingerprintErr != nil {
		return result.Err[domain.Fingerprint](fingerprintErr)
	}

	return result.Ok(fingerprint)
}

func CanonicalString(event domain.CanonicalEvent) result.Result[string] {
	source := sourceFor(event)
	encodedParts, encodeErr := json.Marshal(source.Parts)
	if encodeErr != nil {
		return result.Err[string](encodeErr)
	}

	canonical := fmt.Sprintf(
		"%s\nproject:%s\nkind:%s\nsource:%s\nparts:%s",
		Algorithm,
		event.ProjectID().String(),
		event.Kind(),
		source.Kind,
		string(encodedParts),
	)

	return result.Ok(canonical)
}

func sourceFor(event domain.CanonicalEvent) Source {
	explicit := event.ExplicitFingerprint()
	if len(explicit) > 0 {
		return Source{
			Kind:  "explicit",
			Parts: expandDefault(explicit, event.DefaultGroupingParts()),
		}
	}

	switch event.Kind() {
	case domain.EventKindError:
		return Source{Kind: "exception", Parts: event.DefaultGroupingParts()}
	case domain.EventKindDefault:
		return Source{Kind: "message", Parts: event.DefaultGroupingParts()}
	case domain.EventKindTransaction:
		return Source{Kind: "transaction", Parts: event.DefaultGroupingParts()}
	default:
		return Source{Kind: "unknown", Parts: []string{event.Title().String()}}
	}
}

func expandDefault(parts []string, defaults []string) []string {
	result := make([]string, 0, len(parts)+len(defaults))

	for _, part := range parts {
		if strings.TrimSpace(part) == "{{ default }}" {
			result = append(result, defaults...)
			continue
		}

		result = append(result, part)
	}

	return result
}

func EnsureIssueFingerprint(event domain.CanonicalEvent) result.Result[domain.Fingerprint] {
	if !event.CreatesIssue() {
		return result.Err[domain.Fingerprint](errors.New("event does not create issue"))
	}

	return ComputeFingerprint(event)
}

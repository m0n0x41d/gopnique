package sourcemaps

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

// ApplyToCanonicalEvent walks the JS stacktrace frames carried by event and
// resolves each one through resolver where a source map for the event's
// release is stored. Already-resolved frames, frames whose abs_path does not
// produce a valid source map identity, and frames whose generated position
// does not map cleanly are left untouched.
//
// The resolver may be nil; in that case the event is returned unchanged.
// The event's release is required to compute the source map identity; events
// without a release are returned unchanged.
//
// The function is pure with respect to the input event; it returns a new
// CanonicalEvent value when at least one frame is resolved and otherwise
// returns the original event unchanged.
func ApplyToCanonicalEvent(
	ctx context.Context,
	resolver *Service,
	event domain.CanonicalEvent,
) domain.CanonicalEvent {
	if resolver == nil {
		return event
	}

	frames := event.JsStacktrace()
	if len(frames) == 0 {
		return event
	}

	release, releaseErr := domain.NewReleaseName(event.Release())
	if releaseErr != nil {
		return event
	}

	dist, distErr := domain.NewOptionalDistName("")
	if distErr != nil {
		return event
	}

	updated := make([]domain.JsStacktraceFrame, len(frames))
	hasUpdate := false
	for index, frame := range frames {
		resolved, ok := resolveSingleFrame(
			ctx,
			resolver,
			event.OrganizationID(),
			event.ProjectID(),
			release,
			dist,
			frame,
		)
		if !ok {
			updated[index] = frame
			continue
		}

		updated[index] = resolved
		hasUpdate = true
	}

	if !hasUpdate {
		return event
	}

	return event.WithJsStacktrace(updated)
}

func resolveSingleFrame(
	ctx context.Context,
	resolver *Service,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	release domain.ReleaseName,
	dist domain.DistName,
	frame domain.JsStacktraceFrame,
) (domain.JsStacktraceFrame, bool) {
	if _, hasResolution := frame.Resolution(); hasResolution {
		return frame, false
	}

	fileName, fileErr := sourceMapFileNameFromAbsPath(frame.AbsPath())
	if fileErr != nil {
		return frame, false
	}

	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		return frame, false
	}

	position := NewGeneratedPosition(frame.GeneratedLine()-1, frame.GeneratedColumn())
	lookup := resolver.Resolve(ctx, organizationID, projectID, identity, position)
	resolvedFrame, lookupErr := lookup.Value()
	if lookupErr != nil {
		return frame, false
	}

	symbol := frame.Function()
	if name, hasName := resolvedFrame.Name(); hasName {
		symbol = name
	}

	resolved, resolvedErr := domain.NewResolvedJsStacktraceFrame(
		frame.AbsPath(),
		frame.Function(),
		frame.GeneratedLine(),
		frame.GeneratedColumn(),
		resolvedFrame.Source(),
		symbol,
		resolvedFrame.OriginalLine()+1,
		resolvedFrame.OriginalColumn(),
	)
	if resolvedErr != nil {
		return frame, false
	}

	return resolved, true
}

// sourceMapFileNameFromAbsPath maps a JS frame abs_path to a source map
// file name suitable for SourceMapIdentity. URL-style abs paths use their
// path portion (with leading slash stripped); bare relative paths are used
// as-is. Query strings, fragments, and userinfo are discarded.
func sourceMapFileNameFromAbsPath(absPath string) (domain.SourceMapFileName, error) {
	trimmed := strings.TrimSpace(absPath)
	if trimmed == "" {
		return domain.SourceMapFileName{}, errors.New("abs path is empty")
	}

	candidate := trimmed
	parsed, parseErr := url.Parse(trimmed)
	if parseErr == nil && (parsed.Scheme != "" || parsed.Host != "") && parsed.Path != "" {
		candidate = strings.TrimPrefix(parsed.Path, "/")
	}

	return domain.NewSourceMapFileName(candidate)
}

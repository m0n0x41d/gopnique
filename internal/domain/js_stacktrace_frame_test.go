package domain

import (
	"strings"
	"testing"
)

func TestNewUnresolvedJsStacktraceFrameAcceptsTypicalValues(t *testing.T) {
	frame, err := NewUnresolvedJsStacktraceFrame(
		"https://cdn.example.com/app.min.js",
		"renderHome",
		1,
		1024,
	)
	if err != nil {
		t.Fatalf("unresolved frame: %v", err)
	}

	if frame.AbsPath() != "https://cdn.example.com/app.min.js" {
		t.Fatalf("unexpected abs path: %s", frame.AbsPath())
	}

	if frame.Function() != "renderHome" {
		t.Fatalf("unexpected function: %s", frame.Function())
	}

	if frame.GeneratedLine() != 1 {
		t.Fatalf("unexpected generated line: %d", frame.GeneratedLine())
	}

	if frame.GeneratedColumn() != 1024 {
		t.Fatalf("unexpected generated column: %d", frame.GeneratedColumn())
	}

	if _, hasResolution := frame.Resolution(); hasResolution {
		t.Fatalf("expected unresolved frame")
	}
}

func TestNewResolvedJsStacktraceFrameCarriesResolution(t *testing.T) {
	frame, err := NewResolvedJsStacktraceFrame(
		"https://cdn.example.com/app.min.js",
		"r",
		1,
		2048,
		"webpack:///./src/home.tsx",
		"renderHome",
		42,
		8,
	)
	if err != nil {
		t.Fatalf("resolved frame: %v", err)
	}

	resolution, hasResolution := frame.Resolution()
	if !hasResolution {
		t.Fatalf("expected resolved frame")
	}

	if resolution.Source() != "webpack:///./src/home.tsx" {
		t.Fatalf("unexpected resolved source: %s", resolution.Source())
	}

	if resolution.Symbol() != "renderHome" {
		t.Fatalf("unexpected resolved symbol: %s", resolution.Symbol())
	}

	if resolution.OriginalLine() != 42 {
		t.Fatalf("unexpected resolved line: %d", resolution.OriginalLine())
	}

	if resolution.OriginalColumn() != 8 {
		t.Fatalf("unexpected resolved column: %d", resolution.OriginalColumn())
	}
}

func TestNewJsStacktraceFrameRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		label           string
		absPath         string
		function        string
		generatedLine   int
		generatedColumn int
	}{
		{label: "missing abs path", absPath: "", function: "", generatedLine: 1, generatedColumn: 0},
		{label: "non-positive generated line", absPath: "https://x/a.js", function: "", generatedLine: 0, generatedColumn: 0},
		{label: "negative generated column", absPath: "https://x/a.js", function: "", generatedLine: 1, generatedColumn: -1},
		{label: "control character in abs path", absPath: "https://x/a\x01.js", function: "", generatedLine: 1, generatedColumn: 0},
		{label: "oversized abs path", absPath: strings.Repeat("a", jsStacktraceAbsPathMaxBytes+1), function: "", generatedLine: 1, generatedColumn: 0},
		{label: "oversized function", absPath: "https://x/a.js", function: strings.Repeat("a", jsStacktraceFunctionMaxBytes+1), generatedLine: 1, generatedColumn: 0},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := NewUnresolvedJsStacktraceFrame(tc.absPath, tc.function, tc.generatedLine, tc.generatedColumn)
			if err == nil {
				t.Fatalf("expected %s to be rejected", tc.label)
			}
		})
	}
}

func TestNewResolvedJsStacktraceFrameRejectsInvalidResolution(t *testing.T) {
	cases := []struct {
		label          string
		source         string
		symbol         string
		originalLine   int
		originalColumn int
	}{
		{label: "missing source", source: "", symbol: "", originalLine: 1, originalColumn: 0},
		{label: "non-positive original line", source: "src/home.tsx", symbol: "", originalLine: 0, originalColumn: 0},
		{label: "negative original column", source: "src/home.tsx", symbol: "", originalLine: 1, originalColumn: -1},
		{label: "oversized source", source: strings.Repeat("s", jsStacktraceSourceMaxBytes+1), symbol: "", originalLine: 1, originalColumn: 0},
		{label: "oversized symbol", source: "src/home.tsx", symbol: strings.Repeat("a", jsStacktraceSymbolMaxBytes+1), originalLine: 1, originalColumn: 0},
		{label: "control character in symbol", source: "src/home.tsx", symbol: "render\x01home", originalLine: 1, originalColumn: 0},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := NewResolvedJsStacktraceFrame(
				"https://x/a.js",
				"r",
				1,
				0,
				tc.source,
				tc.symbol,
				tc.originalLine,
				tc.originalColumn,
			)
			if err == nil {
				t.Fatalf("expected %s to be rejected", tc.label)
			}
		})
	}
}

func TestCanonicalEventReturnsJsStacktraceCopy(t *testing.T) {
	frame, frameErr := NewUnresolvedJsStacktraceFrame(
		"https://cdn.example.com/app.min.js",
		"renderHome",
		1,
		512,
	)
	if frameErr != nil {
		t.Fatalf("frame: %v", frameErr)
	}

	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:         EventKindError,
		Level:        EventLevelError,
		Title:        mustTitle(t, "TypeError"),
		JsStacktrace: []JsStacktraceFrame{frame},
	})

	first := event.JsStacktrace()
	if len(first) != 1 {
		t.Fatalf("expected one js frame, got %d", len(first))
	}

	first[0] = JsStacktraceFrame{}

	second := event.JsStacktrace()
	if len(second) != 1 {
		t.Fatalf("expected one js frame after mutation, got %d", len(second))
	}

	if second[0].AbsPath() != "https://cdn.example.com/app.min.js" {
		t.Fatalf("expected js frame to remain stable, got %q", second[0].AbsPath())
	}
}

func TestCanonicalEventWithoutJsStacktraceReturnsEmpty(t *testing.T) {
	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:  EventKindDefault,
		Level: EventLevelInfo,
		Title: mustTitle(t, "no js stacktrace"),
	})

	if frames := event.JsStacktrace(); len(frames) != 0 {
		t.Fatalf("expected no js frames, got %d", len(frames))
	}
}

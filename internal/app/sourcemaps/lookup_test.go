package sourcemaps

import "testing"

func TestLookupFrameResolvesKnownColumn(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{"alpha", "beta"},
		"AAAAA,EAACC,EAACA",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	resolved, ok := LookupFrame(sm, NewGeneratedPosition(0, 3))
	if !ok {
		t.Fatal("expected resolved frame")
	}

	if resolved.Source() != "original.js" {
		t.Fatalf("unexpected source: %q", resolved.Source())
	}

	if resolved.OriginalColumn() != 1 {
		t.Fatalf("expected original column 1, got %d", resolved.OriginalColumn())
	}

	name, hasName := resolved.Name()
	if !hasName || name != "beta" {
		t.Fatalf("expected name beta, got %q hasName=%v", name, hasName)
	}
}

func TestLookupFramePicksLastSegmentBeforeColumn(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{},
		"AAAA,EAAC,EAAC",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	resolved, ok := LookupFrame(sm, NewGeneratedPosition(0, 5))
	if !ok {
		t.Fatal("expected resolved frame")
	}

	if resolved.OriginalColumn() != 2 {
		t.Fatalf("expected last segment original column 2, got %d", resolved.OriginalColumn())
	}
}

func TestLookupFrameFailsBeforeFirstSegment(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{},
		"EAAA",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	_, ok := LookupFrame(sm, NewGeneratedPosition(0, 1))
	if ok {
		t.Fatal("expected no resolved frame before first segment")
	}
}

func TestLookupFrameFailsForUnknownLine(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{},
		"AAAA",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	_, ok := LookupFrame(sm, NewGeneratedPosition(99, 0))
	if ok {
		t.Fatal("expected no resolved frame on missing line")
	}
}

func TestLookupFrameFailsForGeneratedOnlySegment(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{},
		"A",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	_, ok := LookupFrame(sm, NewGeneratedPosition(0, 0))
	if ok {
		t.Fatal("expected no resolved frame for generated-only segment")
	}
}

func TestLookupFrameDegradesWhenSourceIndexOutOfRange(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"only.js"},
		[]string{},
		";AECAA",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	_, ok := LookupFrame(sm, NewGeneratedPosition(1, 0))
	if ok {
		t.Fatal("expected no resolved frame when source index out of range")
	}
}

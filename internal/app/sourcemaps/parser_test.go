package sourcemaps

import (
	"strings"
	"testing"
)

func TestParseSourceMapRequiresVersion3(t *testing.T) {
	payload := []byte(`{"version":2,"sources":[],"names":[],"mappings":""}`)

	_, err := ParseSourceMap(payload)
	if err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestParseSourceMapRejectsEmptyPayload(t *testing.T) {
	_, err := ParseSourceMap(nil)
	if err == nil {
		t.Fatal("expected error for empty payload")
	}
}

func TestParseSourceMapRejectsInvalidJSON(t *testing.T) {
	_, err := ParseSourceMap([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestParseSourceMapDecodesMappings(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{"computeTotal"},
		"AAAAA",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(sm.lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(sm.lines))
	}

	segments := sm.lines[0].segments
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}

	want := segment{
		generatedColumn: 0,
		sourceIndex:     0,
		originalLine:    0,
		originalColumn:  0,
		nameIndex:       0,
	}

	if segments[0] != want {
		t.Fatalf("segment mismatch: %+v", segments[0])
	}
}

func TestParseSourceMapJoinsSourceRoot(t *testing.T) {
	payload := []byte(`{
		"version": 3,
		"sourceRoot": "src/",
		"sources": ["a.js", "b.js"],
		"names": [],
		"mappings": ""
	}`)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if sm.Sources()[0] != "src/a.js" {
		t.Fatalf("expected joined source, got %q", sm.Sources()[0])
	}

	if sm.Sources()[1] != "src/b.js" {
		t.Fatalf("expected joined source, got %q", sm.Sources()[1])
	}
}

func TestParseSourceMapDecodesMultipleSegments(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{"alpha", "beta"},
		"AAAAA,EAACA,EAACA",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(sm.lines[0].segments) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(sm.lines[0].segments))
	}

	first := sm.lines[0].segments[0]
	if first.generatedColumn != 0 || first.originalLine != 0 || first.originalColumn != 0 {
		t.Fatalf("unexpected first: %+v", first)
	}

	second := sm.lines[0].segments[1]
	if second.generatedColumn != 2 || second.originalColumn != 1 {
		t.Fatalf("unexpected second: %+v", second)
	}

	third := sm.lines[0].segments[2]
	if third.generatedColumn != 4 || third.originalColumn != 2 {
		t.Fatalf("unexpected third: %+v", third)
	}
}

func TestParseSourceMapDecodesMultiLineMappings(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{"alpha"},
		"AAAAA;ACAAC",
	)

	sm, err := ParseSourceMap(payload)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if len(sm.lines) != 2 {
		t.Fatalf("expected 2 generated lines, got %d", len(sm.lines))
	}

	if len(sm.lines[1].segments) != 1 {
		t.Fatalf("expected 1 segment on line 2, got %d", len(sm.lines[1].segments))
	}
}

func TestParseSourceMapRejectsInvalidVLQ(t *testing.T) {
	payload := buildSourceMapPayload(
		[]string{"original.js"},
		[]string{"alpha"},
		"@",
	)

	_, err := ParseSourceMap(payload)
	if err == nil {
		t.Fatal("expected error for invalid vlq")
	}
}

func TestDecodeVLQRoundTripsKnownValues(t *testing.T) {
	cases := []struct {
		input    string
		expected int
	}{
		{"A", 0},
		{"C", 1},
		{"D", -1},
		{"E", 2},
		{"F", -2},
		{"gB", 16},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			value, consumed, err := decodeVLQ(tc.input, 0)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}

			if consumed != len(tc.input) {
				t.Fatalf("expected to consume %d, consumed %d", len(tc.input), consumed)
			}

			if value != tc.expected {
				t.Fatalf("expected %d, got %d", tc.expected, value)
			}
		})
	}
}

func buildSourceMapPayload(sources []string, names []string, mappings string) []byte {
	body := strings.Builder{}
	body.WriteString(`{"version":3,"sources":[`)
	body.WriteString(stringList(sources))
	body.WriteString(`],"names":[`)
	body.WriteString(stringList(names))
	body.WriteString(`],"mappings":"`)
	body.WriteString(mappings)
	body.WriteString(`"}`)

	return []byte(body.String())
}

func stringList(values []string) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = `"` + value + `"`
	}

	return strings.Join(parts, ",")
}

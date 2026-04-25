package sourcemaps

import "sort"

type GeneratedPosition struct {
	line   int
	column int
}

type ResolvedFrame struct {
	source         string
	name           string
	originalLine   int
	originalColumn int
	hasName        bool
}

func NewGeneratedPosition(line int, column int) GeneratedPosition {
	return GeneratedPosition{line: line, column: column}
}

func (position GeneratedPosition) Line() int {
	return position.line
}

func (position GeneratedPosition) Column() int {
	return position.column
}

func (frame ResolvedFrame) Source() string {
	return frame.source
}

func (frame ResolvedFrame) Name() (string, bool) {
	return frame.name, frame.hasName
}

func (frame ResolvedFrame) OriginalLine() int {
	return frame.originalLine
}

func (frame ResolvedFrame) OriginalColumn() int {
	return frame.originalColumn
}

func LookupFrame(sm SourceMap, position GeneratedPosition) (ResolvedFrame, bool) {
	if position.line < 0 || position.line >= len(sm.lines) {
		return ResolvedFrame{}, false
	}

	segments := sm.lines[position.line].segments
	if len(segments) == 0 {
		return ResolvedFrame{}, false
	}

	candidate := sort.Search(len(segments), func(index int) bool {
		return segments[index].generatedColumn > position.column
	})

	if candidate == 0 {
		return ResolvedFrame{}, false
	}

	chosen := segments[candidate-1]
	if chosen.sourceIndex == noIndex || chosen.sourceIndex >= len(sm.sources) {
		return ResolvedFrame{}, false
	}

	resolved := ResolvedFrame{
		source:         sm.sources[chosen.sourceIndex],
		originalLine:   chosen.originalLine,
		originalColumn: chosen.originalColumn,
	}

	if chosen.nameIndex != noIndex && chosen.nameIndex < len(sm.names) {
		resolved.name = sm.names[chosen.nameIndex]
		resolved.hasName = true
	}

	return resolved, true
}

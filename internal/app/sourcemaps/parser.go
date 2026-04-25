package sourcemaps

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const supportedSourceMapVersion = 3

const noIndex = -1

const base64Alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

var base64Decode [128]int

func init() {
	for index := range base64Decode {
		base64Decode[index] = -1
	}

	for index := 0; index < len(base64Alphabet); index++ {
		base64Decode[base64Alphabet[index]] = index
	}
}

type SourceMap struct {
	sources []string
	names   []string
	lines   []generatedLine
}

type generatedLine struct {
	segments []segment
}

type segment struct {
	generatedColumn int
	sourceIndex     int
	originalLine    int
	originalColumn  int
	nameIndex       int
}

type rawSourceMap struct {
	Version    int      `json:"version"`
	File       string   `json:"file"`
	SourceRoot string   `json:"sourceRoot"`
	Sources    []string `json:"sources"`
	Names      []string `json:"names"`
	Mappings   string   `json:"mappings"`
}

func ParseSourceMap(payload []byte) (SourceMap, error) {
	if len(payload) == 0 {
		return SourceMap{}, errors.New("source map payload is empty")
	}

	var raw rawSourceMap
	unmarshalErr := json.Unmarshal(payload, &raw)
	if unmarshalErr != nil {
		return SourceMap{}, fmt.Errorf("decode source map: %w", unmarshalErr)
	}

	if raw.Version != supportedSourceMapVersion {
		return SourceMap{}, fmt.Errorf("unsupported source map version: %d", raw.Version)
	}

	sources := make([]string, len(raw.Sources))
	for index, value := range raw.Sources {
		if raw.SourceRoot == "" {
			sources[index] = value
			continue
		}

		sources[index] = joinSourceRoot(raw.SourceRoot, value)
	}

	lines, decodeErr := decodeMappings(raw.Mappings)
	if decodeErr != nil {
		return SourceMap{}, decodeErr
	}

	return SourceMap{
		sources: sources,
		names:   append([]string{}, raw.Names...),
		lines:   lines,
	}, nil
}

func joinSourceRoot(root string, source string) string {
	if strings.HasSuffix(root, "/") {
		return root + source
	}

	return root + "/" + source
}

func (sm SourceMap) Sources() []string {
	return append([]string{}, sm.sources...)
}

func (sm SourceMap) Names() []string {
	return append([]string{}, sm.names...)
}

func decodeMappings(input string) ([]generatedLine, error) {
	if input == "" {
		return nil, nil
	}

	rawLines := strings.Split(input, ";")
	lines := make([]generatedLine, len(rawLines))

	sourceIndex := 0
	originalLine := 0
	originalColumn := 0
	nameIndex := 0

	for lineNumber, rawLine := range rawLines {
		generatedColumn := 0
		segments := []segment{}

		if rawLine == "" {
			lines[lineNumber] = generatedLine{segments: segments}
			continue
		}

		rawSegments := strings.Split(rawLine, ",")

		for _, rawSegment := range rawSegments {
			if rawSegment == "" {
				continue
			}

			fields, decodeErr := decodeSegment(rawSegment)
			if decodeErr != nil {
				return nil, decodeErr
			}

			generatedColumn += fields[0]

			seg := segment{
				generatedColumn: generatedColumn,
				sourceIndex:     noIndex,
				originalLine:    noIndex,
				originalColumn:  noIndex,
				nameIndex:       noIndex,
			}

			if len(fields) >= 4 {
				sourceIndex += fields[1]
				originalLine += fields[2]
				originalColumn += fields[3]
				seg.sourceIndex = sourceIndex
				seg.originalLine = originalLine
				seg.originalColumn = originalColumn
			}

			if len(fields) == 5 {
				nameIndex += fields[4]
				seg.nameIndex = nameIndex
			}

			segments = append(segments, seg)
		}

		sort.SliceStable(segments, func(left int, right int) bool {
			return segments[left].generatedColumn < segments[right].generatedColumn
		})

		lines[lineNumber] = generatedLine{segments: segments}
	}

	return lines, nil
}

func decodeSegment(input string) ([]int, error) {
	fields := []int{}
	position := 0

	for position < len(input) {
		value, consumed, decodeErr := decodeVLQ(input, position)
		if decodeErr != nil {
			return nil, decodeErr
		}

		fields = append(fields, value)
		position += consumed
	}

	switch len(fields) {
	case 1, 4, 5:
		return fields, nil
	default:
		return nil, fmt.Errorf("invalid source map segment field count: %d", len(fields))
	}
}

func decodeVLQ(input string, position int) (int, int, error) {
	const continuationBit = 32
	const valueMask = 31
	const signBit = 1

	value := 0
	shift := 0
	consumed := 0

	for {
		if position+consumed >= len(input) {
			return 0, 0, errors.New("unexpected end of vlq")
		}

		character := input[position+consumed]
		consumed++

		if character >= 128 {
			return 0, 0, fmt.Errorf("invalid vlq character: %q", character)
		}

		digit := base64Decode[character]
		if digit < 0 {
			return 0, 0, fmt.Errorf("invalid vlq character: %q", character)
		}

		hasContinuation := digit&continuationBit != 0
		value |= (digit & valueMask) << shift
		shift += 5

		if !hasContinuation {
			break
		}
	}

	negative := value&signBit != 0
	value >>= 1

	if negative {
		value = -value
	}

	return value, consumed, nil
}

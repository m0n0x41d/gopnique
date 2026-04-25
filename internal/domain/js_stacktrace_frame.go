package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	jsStacktraceAbsPathMaxBytes  = 1024
	jsStacktraceFunctionMaxBytes = 256
	jsStacktraceSourceMaxBytes   = 1024
	jsStacktraceSymbolMaxBytes   = 256
)

type JsStacktraceFrame struct {
	absPath         string
	function        string
	generatedLine   int
	generatedColumn int
	hasResolution   bool
	resolution      JsStacktraceResolution
}

type JsStacktraceResolution struct {
	source         string
	symbol         string
	originalLine   int
	originalColumn int
}

func NewUnresolvedJsStacktraceFrame(
	absPath string,
	function string,
	generatedLine int,
	generatedColumn int,
) (JsStacktraceFrame, error) {
	normalizedAbsPath, absPathErr := normalizeJsAbsPath(absPath)
	if absPathErr != nil {
		return JsStacktraceFrame{}, absPathErr
	}

	normalizedFunction, functionErr := normalizeJsFunction(function)
	if functionErr != nil {
		return JsStacktraceFrame{}, functionErr
	}

	if generatedLine < 1 {
		return JsStacktraceFrame{}, errors.New("js stacktrace frame generated line must be at least 1")
	}

	if generatedColumn < 0 {
		return JsStacktraceFrame{}, errors.New("js stacktrace frame generated column must not be negative")
	}

	return JsStacktraceFrame{
		absPath:         normalizedAbsPath,
		function:        normalizedFunction,
		generatedLine:   generatedLine,
		generatedColumn: generatedColumn,
	}, nil
}

func NewResolvedJsStacktraceFrame(
	absPath string,
	function string,
	generatedLine int,
	generatedColumn int,
	resolvedSource string,
	resolvedSymbol string,
	resolvedLine int,
	resolvedColumn int,
) (JsStacktraceFrame, error) {
	frame, frameErr := NewUnresolvedJsStacktraceFrame(absPath, function, generatedLine, generatedColumn)
	if frameErr != nil {
		return JsStacktraceFrame{}, frameErr
	}

	resolution, resolutionErr := newJsStacktraceResolution(
		resolvedSource,
		resolvedSymbol,
		resolvedLine,
		resolvedColumn,
	)
	if resolutionErr != nil {
		return JsStacktraceFrame{}, resolutionErr
	}

	frame.hasResolution = true
	frame.resolution = resolution

	return frame, nil
}

func newJsStacktraceResolution(
	source string,
	symbol string,
	originalLine int,
	originalColumn int,
) (JsStacktraceResolution, error) {
	normalizedSource, sourceErr := normalizeJsResolvedSource(source)
	if sourceErr != nil {
		return JsStacktraceResolution{}, sourceErr
	}

	normalizedSymbol, symbolErr := normalizeJsResolvedSymbol(symbol)
	if symbolErr != nil {
		return JsStacktraceResolution{}, symbolErr
	}

	if originalLine < 1 {
		return JsStacktraceResolution{}, errors.New("js stacktrace resolution original line must be at least 1")
	}

	if originalColumn < 0 {
		return JsStacktraceResolution{}, errors.New("js stacktrace resolution original column must not be negative")
	}

	return JsStacktraceResolution{
		source:         normalizedSource,
		symbol:         normalizedSymbol,
		originalLine:   originalLine,
		originalColumn: originalColumn,
	}, nil
}

func (frame JsStacktraceFrame) AbsPath() string {
	return frame.absPath
}

func (frame JsStacktraceFrame) Function() string {
	return frame.function
}

func (frame JsStacktraceFrame) GeneratedLine() int {
	return frame.generatedLine
}

func (frame JsStacktraceFrame) GeneratedColumn() int {
	return frame.generatedColumn
}

func (frame JsStacktraceFrame) Resolution() (JsStacktraceResolution, bool) {
	return frame.resolution, frame.hasResolution
}

func (resolution JsStacktraceResolution) Source() string {
	return resolution.source
}

func (resolution JsStacktraceResolution) Symbol() string {
	return resolution.symbol
}

func (resolution JsStacktraceResolution) OriginalLine() int {
	return resolution.originalLine
}

func (resolution JsStacktraceResolution) OriginalColumn() int {
	return resolution.originalColumn
}

func copyJsStacktraceFrames(frames []JsStacktraceFrame) []JsStacktraceFrame {
	if len(frames) == 0 {
		return nil
	}

	copied := make([]JsStacktraceFrame, len(frames))
	copy(copied, frames)

	return copied
}

func normalizeJsAbsPath(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("js stacktrace frame abs path is required")
	}

	if !utf8.ValidString(value) {
		return "", errors.New("js stacktrace frame abs path must be valid utf-8")
	}

	if len(value) > jsStacktraceAbsPathMaxBytes {
		return "", errors.New("js stacktrace frame abs path is too long")
	}

	if !visibleString(value) {
		return "", errors.New("js stacktrace frame abs path must not contain control characters")
	}

	return value, nil
}

func normalizeJsFunction(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	if !utf8.ValidString(value) {
		return "", errors.New("js stacktrace frame function must be valid utf-8")
	}

	if len(value) > jsStacktraceFunctionMaxBytes {
		return "", errors.New("js stacktrace frame function is too long")
	}

	if !visibleString(value) {
		return "", errors.New("js stacktrace frame function must not contain control characters")
	}

	return value, nil
}

func normalizeJsResolvedSource(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("js stacktrace resolution source is required")
	}

	if !utf8.ValidString(value) {
		return "", errors.New("js stacktrace resolution source must be valid utf-8")
	}

	if len(value) > jsStacktraceSourceMaxBytes {
		return "", errors.New("js stacktrace resolution source is too long")
	}

	if !visibleString(value) {
		return "", errors.New("js stacktrace resolution source must not contain control characters")
	}

	return value, nil
}

func normalizeJsResolvedSymbol(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	if !utf8.ValidString(value) {
		return "", errors.New("js stacktrace resolution symbol must be valid utf-8")
	}

	if len(value) > jsStacktraceSymbolMaxBytes {
		return "", errors.New("js stacktrace resolution symbol is too long")
	}

	if !visibleString(value) {
		return "", errors.New("js stacktrace resolution symbol must not contain control characters")
	}

	return value, nil
}

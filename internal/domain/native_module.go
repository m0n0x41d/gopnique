package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const nativeCodeFileMaxBytes = 1024

type NativeModule struct {
	debugID   DebugIdentifier
	codeFile  string
	imageAddr uint64
	imageSize uint64
}

func NewNativeModule(
	debugID DebugIdentifier,
	codeFile string,
	imageAddr uint64,
	imageSize uint64,
) (NativeModule, error) {
	if debugID.value == "" {
		return NativeModule{}, errors.New("native module requires debug identifier")
	}

	normalizedCodeFile, codeFileErr := normalizeNativeCodeFile(codeFile)
	if codeFileErr != nil {
		return NativeModule{}, codeFileErr
	}

	return NativeModule{
		debugID:   debugID,
		codeFile:  normalizedCodeFile,
		imageAddr: imageAddr,
		imageSize: imageSize,
	}, nil
}

func (module NativeModule) DebugID() DebugIdentifier {
	return module.debugID
}

func (module NativeModule) CodeFile() string {
	return module.codeFile
}

func (module NativeModule) ImageAddr() uint64 {
	return module.imageAddr
}

func (module NativeModule) ImageSize() uint64 {
	return module.imageSize
}

func copyNativeModules(modules []NativeModule) []NativeModule {
	if len(modules) == 0 {
		return nil
	}

	copied := make([]NativeModule, len(modules))
	copy(copied, modules)

	return copied
}

func normalizeNativeCodeFile(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", errors.New("native module requires code file")
	}

	if !utf8.ValidString(value) {
		return "", errors.New("native module code file must be valid utf-8")
	}

	if len(value) > nativeCodeFileMaxBytes {
		return "", errors.New("native module code file is too long")
	}

	if !visibleString(value) {
		return "", errors.New("native module code file must not contain control characters")
	}

	return value, nil
}

package domain

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const (
	nativeFunctionMaxBytes = 512
	nativePackageMaxBytes  = 512
)

type NativeFrame struct {
	instructionAddr uint64
	moduleDebugID   DebugIdentifier
	hasModule       bool
	function        string
	pkg             string
}

func NewNativeFrame(
	instructionAddr uint64,
	function string,
	pkg string,
) (NativeFrame, error) {
	normalizedFunction, functionErr := normalizeNativeFunction(function)
	if functionErr != nil {
		return NativeFrame{}, functionErr
	}

	normalizedPackage, packageErr := normalizeNativePackage(pkg)
	if packageErr != nil {
		return NativeFrame{}, packageErr
	}

	return NativeFrame{
		instructionAddr: instructionAddr,
		function:        normalizedFunction,
		pkg:             normalizedPackage,
	}, nil
}

func NewNativeFrameWithModule(
	instructionAddr uint64,
	moduleDebugID DebugIdentifier,
	function string,
	pkg string,
) (NativeFrame, error) {
	if moduleDebugID.value == "" {
		return NativeFrame{}, errors.New("native frame module debug identifier is required")
	}

	frame, frameErr := NewNativeFrame(instructionAddr, function, pkg)
	if frameErr != nil {
		return NativeFrame{}, frameErr
	}

	frame.moduleDebugID = moduleDebugID
	frame.hasModule = true

	return frame, nil
}

func (frame NativeFrame) InstructionAddr() uint64 {
	return frame.instructionAddr
}

func (frame NativeFrame) ModuleDebugID() (DebugIdentifier, bool) {
	return frame.moduleDebugID, frame.hasModule
}

func (frame NativeFrame) Function() string {
	return frame.function
}

func (frame NativeFrame) Package() string {
	return frame.pkg
}

func copyNativeFrames(frames []NativeFrame) []NativeFrame {
	if len(frames) == 0 {
		return nil
	}

	copied := make([]NativeFrame, len(frames))
	copy(copied, frames)

	return copied
}

func normalizeNativeFunction(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	if !utf8.ValidString(value) {
		return "", errors.New("native frame function must be valid utf-8")
	}

	if len(value) > nativeFunctionMaxBytes {
		return "", errors.New("native frame function is too long")
	}

	if !visibleString(value) {
		return "", errors.New("native frame function must not contain control characters")
	}

	return value, nil
}

func normalizeNativePackage(input string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return "", nil
	}

	if !utf8.ValidString(value) {
		return "", errors.New("native frame package must be valid utf-8")
	}

	if len(value) > nativePackageMaxBytes {
		return "", errors.New("native frame package is too long")
	}

	if !visibleString(value) {
		return "", errors.New("native frame package must not contain control characters")
	}

	return value, nil
}

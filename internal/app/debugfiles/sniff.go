package debugfiles

import (
	"bytes"
	"errors"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

var (
	ErrUnsupportedDebugFile = errors.New("debug file format is not supported")
	ErrDebugFileMismatch    = errors.New("debug file payload does not match declared kind")
)

var (
	elfMagic    = []byte{0x7f, 0x45, 0x4c, 0x46}
	machoMagics = [][]byte{
		{0xfe, 0xed, 0xfa, 0xce},
		{0xfe, 0xed, 0xfa, 0xcf},
		{0xce, 0xfa, 0xed, 0xfe},
		{0xcf, 0xfa, 0xed, 0xfe},
		{0xca, 0xfe, 0xba, 0xbe},
		{0xbe, 0xba, 0xfe, 0xca},
	}
	pdbMagic      = []byte("Microsoft C/C++ MSF 7.00\r\n\x1aDS")
	breakpadMagic = []byte("MODULE ")
)

func DetectKind(prefix []byte) (domain.DebugFileKind, error) {
	if len(prefix) == 0 {
		return domain.DebugFileKind{}, errors.New("debug file payload is empty")
	}

	if bytes.HasPrefix(prefix, elfMagic) {
		return domain.DebugFileKindELF(), nil
	}

	if bytes.HasPrefix(prefix, pdbMagic) {
		return domain.DebugFileKindPDB(), nil
	}

	if bytes.HasPrefix(prefix, breakpadMagic) {
		return domain.DebugFileKindBreakpad(), nil
	}

	for _, magic := range machoMagics {
		if bytes.HasPrefix(prefix, magic) {
			return domain.DebugFileKindMachO(), nil
		}
	}

	return domain.DebugFileKind{}, ErrUnsupportedDebugFile
}

func MatchesDeclaredKind(declared domain.DebugFileKind, prefix []byte) error {
	detected, detectErr := DetectKind(prefix)
	if detectErr != nil {
		return detectErr
	}

	if detected.String() != declared.String() {
		return ErrDebugFileMismatch
	}

	return nil
}

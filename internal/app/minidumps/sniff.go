package minidumps

import (
	"bytes"
	"errors"
)

// minidumpMagic is the four ASCII bytes ("MDMP") that begin a Microsoft
// Minidump payload at file offset 0.
var minidumpMagic = []byte{'M', 'D', 'M', 'P'}

var (
	ErrUnsupportedMinidump = errors.New("minidump payload is not supported")
)

// DetectMinidump reports whether the supplied prefix bytes start with the
// canonical Minidump magic value. It is a pure function so the boundary can be
// tested without IO.
func DetectMinidump(prefix []byte) error {
	if len(prefix) == 0 {
		return errors.New("minidump payload is empty")
	}

	if !bytes.HasPrefix(prefix, minidumpMagic) {
		return ErrUnsupportedMinidump
	}

	return nil
}

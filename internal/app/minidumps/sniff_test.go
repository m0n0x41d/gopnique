package minidumps

import (
	"errors"
	"testing"
)

func TestDetectMinidumpAcceptsCanonicalMagic(t *testing.T) {
	prefix := []byte{'M', 'D', 'M', 'P', 0x93, 0xa7, 0x00, 0x00}
	if err := DetectMinidump(prefix); err != nil {
		t.Fatalf("expected MDMP magic to be accepted, got %v", err)
	}
}

func TestDetectMinidumpRejectsUnknownPrefixes(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
	}{
		{"empty", nil},
		{"reversed magic", []byte{'P', 'M', 'D', 'M'}},
		{"elf magic", []byte{0x7f, 'E', 'L', 'F'}},
		{"breakpad magic", []byte("MODULE Linux x86_64")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := DetectMinidump(tc.payload)
			if err == nil {
				t.Fatal("expected error for unsupported prefix")
			}

			if len(tc.payload) > 0 && !errors.Is(err, ErrUnsupportedMinidump) {
				t.Fatalf("expected ErrUnsupportedMinidump, got %v", err)
			}
		})
	}
}

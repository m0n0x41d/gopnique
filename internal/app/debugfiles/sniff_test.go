package debugfiles

import (
	"errors"
	"testing"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

func TestDetectKindAcceptsKnownMagicPrefixes(t *testing.T) {
	cases := []struct {
		name     string
		prefix   []byte
		expected domain.DebugFileKind
	}{
		{"ELF debug", []byte{0x7f, 0x45, 0x4c, 0x46, 0x02, 0x01}, domain.DebugFileKindELF()},
		{"Mach-O 64 little", []byte{0xcf, 0xfa, 0xed, 0xfe, 0x07}, domain.DebugFileKindMachO()},
		{"Mach-O fat", []byte{0xca, 0xfe, 0xba, 0xbe, 0x00}, domain.DebugFileKindMachO()},
		{"PDB", append([]byte("Microsoft C/C++ MSF 7.00\r\n\x1aDS"), 0x00, 0x00), domain.DebugFileKindPDB()},
		{"Breakpad", []byte("MODULE Linux x86_64 deadbeefcafe libapp.so\nFUNC 0 0 0 main"), domain.DebugFileKindBreakpad()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			detected, err := DetectKind(tc.prefix)
			if err != nil {
				t.Fatalf("detect: %v", err)
			}

			if detected.String() != tc.expected.String() {
				t.Fatalf("expected %q, got %q", tc.expected.String(), detected.String())
			}
		})
	}
}

func TestDetectKindRejectsUnknownPrefixes(t *testing.T) {
	cases := []struct {
		name   string
		prefix []byte
	}{
		{"empty", []byte{}},
		{"text without breakpad header", []byte("just some text payload")},
		{"random binary", []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DetectKind(tc.prefix)
			if err == nil {
				t.Fatal("expected unsupported prefix to fail")
			}
		})
	}
}

func TestMatchesDeclaredKindFailsWhenDeclaredKindDoesNotMatchPayload(t *testing.T) {
	err := MatchesDeclaredKind(domain.DebugFileKindBreakpad(), []byte{0x7f, 0x45, 0x4c, 0x46, 0x02})
	if !errors.Is(err, ErrDebugFileMismatch) {
		t.Fatalf("expected ErrDebugFileMismatch, got %v", err)
	}
}

func TestMatchesDeclaredKindAcceptsAlignment(t *testing.T) {
	err := MatchesDeclaredKind(domain.DebugFileKindELF(), []byte{0x7f, 0x45, 0x4c, 0x46, 0x02, 0x01})
	if err != nil {
		t.Fatalf("expected match, got %v", err)
	}
}

func TestMatchesDeclaredKindFailsForUnsupportedPayload(t *testing.T) {
	err := MatchesDeclaredKind(domain.DebugFileKindBreakpad(), []byte{0x00, 0x00, 0x00})
	if !errors.Is(err, ErrUnsupportedDebugFile) {
		t.Fatalf("expected ErrUnsupportedDebugFile, got %v", err)
	}
}

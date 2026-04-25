package domain

import (
	"strings"
	"testing"
)

func TestNewNativeModuleAcceptsTypicalValues(t *testing.T) {
	debugID, debugIDErr := NewDebugIdentifier("0123456789abcdef0123456789abcdef")
	if debugIDErr != nil {
		t.Fatalf("debug id: %v", debugIDErr)
	}

	module, err := NewNativeModule(debugID, "/usr/lib/libfoo.so", 0x10000000, 0x4000)
	if err != nil {
		t.Fatalf("native module: %v", err)
	}

	if module.DebugID().String() != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected debug id: %s", module.DebugID().String())
	}

	if module.CodeFile() != "/usr/lib/libfoo.so" {
		t.Fatalf("unexpected code file: %s", module.CodeFile())
	}

	if module.ImageAddr() != 0x10000000 {
		t.Fatalf("unexpected image addr: %d", module.ImageAddr())
	}

	if module.ImageSize() != 0x4000 {
		t.Fatalf("unexpected image size: %d", module.ImageSize())
	}
}

func TestNewNativeModuleRejectsInvalidValues(t *testing.T) {
	debugID, debugIDErr := NewDebugIdentifier("0123456789abcdef0123456789abcdef")
	if debugIDErr != nil {
		t.Fatalf("debug id: %v", debugIDErr)
	}

	cases := []struct {
		label    string
		debugID  DebugIdentifier
		codeFile string
	}{
		{label: "missing debug id", debugID: DebugIdentifier{}, codeFile: "/usr/lib/libfoo.so"},
		{label: "missing code file", debugID: debugID, codeFile: ""},
		{label: "control character in code file", debugID: debugID, codeFile: "/usr/lib/lib\x01foo.so"},
		{label: "oversized code file", debugID: debugID, codeFile: strings.Repeat("a", nativeCodeFileMaxBytes+1)},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := NewNativeModule(tc.debugID, tc.codeFile, 0, 0)
			if err == nil {
				t.Fatalf("expected %s to be rejected", tc.label)
			}
		})
	}
}

func TestCanonicalEventReturnsNativeModulesCopy(t *testing.T) {
	debugID, debugIDErr := NewDebugIdentifier("0123456789abcdef0123456789abcdef")
	if debugIDErr != nil {
		t.Fatalf("debug id: %v", debugIDErr)
	}

	module, moduleErr := NewNativeModule(debugID, "/usr/lib/libfoo.so", 0, 0)
	if moduleErr != nil {
		t.Fatalf("native module: %v", moduleErr)
	}

	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:          EventKindError,
		Level:         EventLevelError,
		Title:         mustTitle(t, "SegFault"),
		NativeModules: []NativeModule{module},
	})

	first := event.NativeModules()
	if len(first) != 1 {
		t.Fatalf("expected one native module, got %d", len(first))
	}

	first[0] = NativeModule{}

	second := event.NativeModules()
	if len(second) != 1 {
		t.Fatalf("expected one native module after mutation, got %d", len(second))
	}

	if second[0].CodeFile() != "/usr/lib/libfoo.so" {
		t.Fatalf("expected native module to remain stable, got %q", second[0].CodeFile())
	}
}

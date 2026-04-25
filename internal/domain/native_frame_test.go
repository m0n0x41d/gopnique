package domain

import (
	"strings"
	"testing"
)

func TestNewNativeFrameAcceptsTypicalValues(t *testing.T) {
	frame, err := NewNativeFrame(0x10001234, "render_home", "app")
	if err != nil {
		t.Fatalf("native frame: %v", err)
	}

	if frame.InstructionAddr() != 0x10001234 {
		t.Fatalf("unexpected instruction addr: %d", frame.InstructionAddr())
	}

	if frame.Function() != "render_home" {
		t.Fatalf("unexpected function: %s", frame.Function())
	}

	if frame.Package() != "app" {
		t.Fatalf("unexpected package: %s", frame.Package())
	}

	if _, hasModule := frame.ModuleDebugID(); hasModule {
		t.Fatalf("expected no module reference for plain frame")
	}
}

func TestNewNativeFrameWithModuleCarriesDebugID(t *testing.T) {
	debugID, debugIDErr := NewDebugIdentifier("0123456789abcdef0123456789abcdef")
	if debugIDErr != nil {
		t.Fatalf("debug id: %v", debugIDErr)
	}

	frame, err := NewNativeFrameWithModule(0x10001234, debugID, "render_home", "app")
	if err != nil {
		t.Fatalf("native frame with module: %v", err)
	}

	moduleDebugID, hasModule := frame.ModuleDebugID()
	if !hasModule {
		t.Fatalf("expected module reference")
	}

	if moduleDebugID.String() != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("unexpected module debug id: %s", moduleDebugID.String())
	}
}

func TestNewNativeFrameRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		label    string
		function string
		pkg      string
	}{
		{label: "control character in function", function: "render\x01home", pkg: ""},
		{label: "oversized function", function: strings.Repeat("a", nativeFunctionMaxBytes+1), pkg: ""},
		{label: "oversized package", function: "render_home", pkg: strings.Repeat("a", nativePackageMaxBytes+1)},
	}

	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, err := NewNativeFrame(0, tc.function, tc.pkg)
			if err == nil {
				t.Fatalf("expected %s to be rejected", tc.label)
			}
		})
	}
}

func TestNewNativeFrameWithModuleRejectsMissingDebugID(t *testing.T) {
	_, err := NewNativeFrameWithModule(0, DebugIdentifier{}, "render_home", "app")
	if err == nil {
		t.Fatalf("expected missing debug id to be rejected")
	}
}

func TestCanonicalEventReturnsNativeFramesCopy(t *testing.T) {
	frame, frameErr := NewNativeFrame(0x10001234, "render_home", "app")
	if frameErr != nil {
		t.Fatalf("native frame: %v", frameErr)
	}

	event := mustCanonicalEvent(t, CanonicalEventParams{
		Kind:         EventKindError,
		Level:        EventLevelError,
		Title:        mustTitle(t, "SegFault"),
		NativeFrames: []NativeFrame{frame},
	})

	first := event.NativeFrames()
	if len(first) != 1 {
		t.Fatalf("expected one native frame, got %d", len(first))
	}

	first[0] = NativeFrame{}

	second := event.NativeFrames()
	if len(second) != 1 {
		t.Fatalf("expected one native frame after mutation, got %d", len(second))
	}

	if second[0].Function() != "render_home" {
		t.Fatalf("expected native frame to remain stable, got %q", second[0].Function())
	}
}

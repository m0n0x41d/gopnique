package minidumps

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unicode/utf16"
)

func TestParseNativeMetadataExtractsModuleAndExceptionFrame(t *testing.T) {
	body := buildNativeMetadataMinidumpFixture()

	metadata, metadataErr := ParseNativeMetadata(bytes.NewReader(body), int64(len(body))).Value()
	if metadataErr != nil {
		t.Fatalf("parse metadata: %v", metadataErr)
	}

	modules := metadata.NativeModules()
	if len(modules) != 1 {
		t.Fatalf("expected one module, got %d", len(modules))
	}

	module := modules[0]
	if module.DebugID().String() != "deadbeefcafef00ddeadbeefcafef00d" {
		t.Fatalf("unexpected debug id: %s", module.DebugID().String())
	}

	if module.CodeFile() != "/usr/lib/libapp.so" {
		t.Fatalf("unexpected code file: %s", module.CodeFile())
	}

	if module.ImageAddr() != 0x10000000 || module.ImageSize() != 0x2000 {
		t.Fatalf("unexpected module range: addr=%x size=%x", module.ImageAddr(), module.ImageSize())
	}

	frames := metadata.NativeFrames()
	if len(frames) != 1 {
		t.Fatalf("expected one frame, got %d", len(frames))
	}

	frame := frames[0]
	if frame.InstructionAddr() != 0x10001004 {
		t.Fatalf("unexpected instruction address: %x", frame.InstructionAddr())
	}

	debugID, hasModule := frame.ModuleDebugID()
	if !hasModule {
		t.Fatal("expected frame module reference")
	}

	if debugID.String() != module.DebugID().String() {
		t.Fatalf("unexpected frame debug id: %s", debugID.String())
	}
}

func TestParseNativeMetadataDegradesWhenStreamsAreAbsent(t *testing.T) {
	body := []byte{'M', 'D', 'M', 'P', 0x93, 0xa7, 0, 0}
	body = append(body, bytes.Repeat([]byte{0}, 72)...)

	metadata, metadataErr := ParseNativeMetadata(bytes.NewReader(body), int64(len(body))).Value()
	if metadataErr != nil {
		t.Fatalf("parse metadata: %v", metadataErr)
	}

	if len(metadata.NativeModules()) != 0 {
		t.Fatalf("expected no modules, got %d", len(metadata.NativeModules()))
	}

	if len(metadata.NativeFrames()) != 0 {
		t.Fatalf("expected no frames, got %d", len(metadata.NativeFrames()))
	}
}

func buildNativeMetadataMinidumpFixture() []byte {
	const (
		directoryRVA  = 32
		moduleListRVA = 64
		exceptionRVA  = 256
		nameRVA       = 512
		codeViewRVA   = 640
		contextRVA    = 768
		fixtureSize   = 1024
	)

	body := make([]byte, fixtureSize)
	copy(body[0:4], []byte{'M', 'D', 'M', 'P'})
	binary.LittleEndian.PutUint32(body[4:8], 0x0000a793)
	binary.LittleEndian.PutUint32(body[8:12], 2)
	binary.LittleEndian.PutUint32(body[12:16], directoryRVA)

	writeDirectoryEntry(body[directoryRVA:directoryRVA+12], minidumpModuleListStream, 4+minidumpModuleBytes, moduleListRVA)
	writeDirectoryEntry(body[directoryRVA+12:directoryRVA+24], minidumpExceptionStream, 168, exceptionRVA)

	binary.LittleEndian.PutUint32(body[moduleListRVA:moduleListRVA+4], 1)
	module := body[moduleListRVA+4 : moduleListRVA+4+minidumpModuleBytes]
	binary.LittleEndian.PutUint64(module[minidumpModuleBase:minidumpModuleBase+8], 0x10000000)
	binary.LittleEndian.PutUint32(module[minidumpModuleSize:minidumpModuleSize+4], 0x2000)
	binary.LittleEndian.PutUint32(module[minidumpModuleNameRVA:minidumpModuleNameRVA+4], nameRVA)
	binary.LittleEndian.PutUint32(module[minidumpModuleCvSize:minidumpModuleCvSize+4], 24)
	binary.LittleEndian.PutUint32(module[minidumpModuleCvRVA:minidumpModuleCvRVA+4], codeViewRVA)

	writeMinidumpString(body[nameRVA:], "/usr/lib/libapp.so")
	writeCodeViewPDB70(body[codeViewRVA : codeViewRVA+24])

	exception := body[exceptionRVA : exceptionRVA+168]
	binary.LittleEndian.PutUint32(exception[minidumpExceptionCtxSize:minidumpExceptionCtxSize+4], 256)
	binary.LittleEndian.PutUint32(exception[minidumpExceptionCtxRVA:minidumpExceptionCtxRVA+4], contextRVA)
	binary.LittleEndian.PutUint64(body[contextRVA+minidumpX64RipOffset:contextRVA+minidumpX64RipOffset+8], 0x10001004)

	return body
}

func writeDirectoryEntry(entry []byte, streamType uint32, dataSize uint32, rva uint32) {
	binary.LittleEndian.PutUint32(entry[0:4], streamType)
	binary.LittleEndian.PutUint32(entry[4:8], dataSize)
	binary.LittleEndian.PutUint32(entry[8:12], rva)
}

func writeMinidumpString(target []byte, value string) {
	encoded := utf16.Encode([]rune(value))
	binary.LittleEndian.PutUint32(target[0:4], uint32(len(encoded)*2))
	for index, unit := range encoded {
		offset := 4 + index*2
		binary.LittleEndian.PutUint16(target[offset:offset+2], unit)
	}
}

func writeCodeViewPDB70(target []byte) {
	copy(target[0:4], codeViewPDB70Signature)
	copy(target[4:20], []byte{
		0xef, 0xbe, 0xad, 0xde,
		0xfe, 0xca,
		0x0d, 0xf0,
		0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xf0, 0x0d,
	})
}

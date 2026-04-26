package httpadapter

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf16"

	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	"github.com/ivanzakutnii/error-tracker/internal/app/minidumps"
)

func TestSentryMinidumpRouteParsesAndSymbolicatesNativeFrame(t *testing.T) {
	vault := newCapturingVault()
	minidumpStore, minidumpErr := minidumps.NewService(vault)
	if minidumpErr != nil {
		t.Fatalf("minidump store: %v", minidumpErr)
	}

	debugFileStore, debugFileErr := debugfiles.NewService(vault)
	if debugFileErr != nil {
		t.Fatalf("debug file store: %v", debugFileErr)
	}

	uploadFixtureBreakpadSymbols(
		t,
		debugFileStore,
		"1111111111114111a111111111111111",
		"2222222222224222a222222222222222",
		"deadbeefcafef00ddeadbeefcafef00d",
		"libapp.so.sym",
		[]byte(
			"MODULE Linux x86_64 deadbeefcafef00ddeadbeefcafef00d libapp.so\n"+
				"FUNC 1000 20 0 minidump_crash\n",
		),
	)

	captured := &capturingBackend{
		fakeSentryBackend: newFakeSentryBackend(t),
	}
	mux := newMux(
		nil,
		captured,
		captured,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		captured,
		IngestEnrichments{DebugFileStore: debugFileStore, MinidumpStore: minidumpStore},
		NewSessionCodec("test-secret"),
		AuthSettings{PublicURL: "http://example.test"},
	)

	body, contentType := minidumpMultipartBody(t, buildNativeMinidumpHTTPFixture(), `{
		"event_id":"660e8400e29b41d4a716446655440000",
		"release":"native@1.0.0",
		"environment":"production"
	}`)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/42/minidump/?sentry_key=550e8400e29b41d4a716446655440000",
		bytes.NewReader(body),
	)
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	mux.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", response.Code, response.Body.String())
	}

	captured.mu.Lock()
	event := captured.event
	appended := captured.appended
	captured.mu.Unlock()

	if !appended {
		t.Fatal("expected backend to receive an Append call")
	}

	modules := event.NativeModules()
	if len(modules) != 1 {
		t.Fatalf("expected one native module, got %d", len(modules))
	}

	frames := event.NativeFrames()
	if len(frames) != 1 {
		t.Fatalf("expected one native frame, got %d", len(frames))
	}

	if frames[0].Function() != "minidump_crash" {
		t.Fatalf("unexpected symbolicated function: %q", frames[0].Function())
	}

	if frames[0].Package() != "/usr/lib/libapp.so" {
		t.Fatalf("unexpected frame package: %q", frames[0].Package())
	}
}

func buildNativeMinidumpHTTPFixture() []byte {
	const (
		directoryRVA         = 32
		moduleListRVA        = 64
		exceptionRVA         = 256
		nameRVA              = 512
		codeViewRVA          = 640
		contextRVA           = 768
		moduleListStream     = 4
		exceptionStream      = 6
		moduleBytes          = 108
		moduleBase           = 0
		moduleSize           = 8
		moduleNameRVA        = 20
		moduleCodeViewSize   = 76
		moduleCodeViewRVA    = 80
		exceptionContextSize = 160
		exceptionContextRVA  = 164
		x64RIPOffset         = 248
		fixtureSize          = 1024
	)

	body := make([]byte, fixtureSize)
	copy(body[0:4], []byte{'M', 'D', 'M', 'P'})
	binary.LittleEndian.PutUint32(body[4:8], 0x0000a793)
	binary.LittleEndian.PutUint32(body[8:12], 2)
	binary.LittleEndian.PutUint32(body[12:16], directoryRVA)

	writeMinidumpHTTPDirectoryEntry(body[directoryRVA:directoryRVA+12], moduleListStream, 4+moduleBytes, moduleListRVA)
	writeMinidumpHTTPDirectoryEntry(body[directoryRVA+12:directoryRVA+24], exceptionStream, 168, exceptionRVA)

	binary.LittleEndian.PutUint32(body[moduleListRVA:moduleListRVA+4], 1)
	module := body[moduleListRVA+4 : moduleListRVA+4+moduleBytes]
	binary.LittleEndian.PutUint64(module[moduleBase:moduleBase+8], 0x10000000)
	binary.LittleEndian.PutUint32(module[moduleSize:moduleSize+4], 0x2000)
	binary.LittleEndian.PutUint32(module[moduleNameRVA:moduleNameRVA+4], nameRVA)
	binary.LittleEndian.PutUint32(module[moduleCodeViewSize:moduleCodeViewSize+4], 24)
	binary.LittleEndian.PutUint32(module[moduleCodeViewRVA:moduleCodeViewRVA+4], codeViewRVA)

	writeMinidumpHTTPString(body[nameRVA:], "/usr/lib/libapp.so")
	writeMinidumpHTTPCodeViewPDB70(body[codeViewRVA : codeViewRVA+24])

	exception := body[exceptionRVA : exceptionRVA+168]
	binary.LittleEndian.PutUint32(exception[exceptionContextSize:exceptionContextSize+4], 256)
	binary.LittleEndian.PutUint32(exception[exceptionContextRVA:exceptionContextRVA+4], contextRVA)
	binary.LittleEndian.PutUint64(body[contextRVA+x64RIPOffset:contextRVA+x64RIPOffset+8], 0x10001004)

	return body
}

func writeMinidumpHTTPDirectoryEntry(entry []byte, streamType uint32, dataSize uint32, rva uint32) {
	binary.LittleEndian.PutUint32(entry[0:4], streamType)
	binary.LittleEndian.PutUint32(entry[4:8], dataSize)
	binary.LittleEndian.PutUint32(entry[8:12], rva)
}

func writeMinidumpHTTPString(target []byte, value string) {
	encoded := utf16.Encode([]rune(value))
	binary.LittleEndian.PutUint32(target[0:4], uint32(len(encoded)*2))
	for index, unit := range encoded {
		offset := 4 + index*2
		binary.LittleEndian.PutUint16(target[offset:offset+2], unit)
	}
}

func writeMinidumpHTTPCodeViewPDB70(target []byte) {
	copy(target[0:4], []byte{'R', 'S', 'D', 'S'})
	copy(target[4:20], []byte{
		0xef, 0xbe, 0xad, 0xde,
		0xfe, 0xca,
		0x0d, 0xf0,
		0xde, 0xad, 0xbe, 0xef, 0xca, 0xfe, 0xf0, 0x0d,
	})
}

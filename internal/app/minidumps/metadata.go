package minidumps

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"

	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/kernel/result"
)

const (
	minidumpHeaderBytes      = 32
	minidumpDirectoryBytes   = 12
	minidumpModuleBytes      = 108
	minidumpMaxStreamCount   = 128
	minidumpMaxModuleCount   = 512
	minidumpMaxStringBytes   = 4096
	minidumpMaxCodeViewBytes = 4096
	minidumpModuleListStream = 4
	minidumpExceptionStream  = 6
	minidumpX64RipOffset     = 248
	minidumpExceptionCtxSize = 160
	minidumpExceptionCtxRVA  = 164
	minidumpModuleBase       = 0
	minidumpModuleSize       = 8
	minidumpModuleNameRVA    = 20
	minidumpModuleCvSize     = 76
	minidumpModuleCvRVA      = 80
)

var codeViewPDB70Signature = []byte{'R', 'S', 'D', 'S'}

type NativeMetadata struct {
	modules []domain.NativeModule
	frames  []domain.NativeFrame
}

type streamDirectoryEntry struct {
	streamType uint32
	dataSize   uint32
	rva        uint32
}

func (metadata NativeMetadata) NativeModules() []domain.NativeModule {
	return append([]domain.NativeModule(nil), metadata.modules...)
}

func (metadata NativeMetadata) NativeFrames() []domain.NativeFrame {
	return append([]domain.NativeFrame(nil), metadata.frames...)
}

func ParseNativeMetadata(
	reader io.ReaderAt,
	size int64,
) result.Result[NativeMetadata] {
	if reader == nil {
		return result.Err[NativeMetadata](errors.New("minidump reader is required"))
	}

	prefix, prefixOK := readMinidumpBytes(reader, size, 0, uint32(len(minidumpMagic)))
	if !prefixOK {
		return result.Err[NativeMetadata](errors.New("minidump payload is empty"))
	}

	if !bytes.Equal(prefix, minidumpMagic) {
		return result.Err[NativeMetadata](ErrUnsupportedMinidump)
	}

	entries, entriesOK := readMinidumpDirectory(reader, size)
	if !entriesOK {
		return result.Ok(NativeMetadata{})
	}

	modules := parseMinidumpModules(reader, size, entries)
	frames := parseMinidumpFrames(reader, size, entries, modules)

	return result.Ok(NativeMetadata{modules: modules, frames: frames})
}

func readMinidumpDirectory(
	reader io.ReaderAt,
	size int64,
) ([]streamDirectoryEntry, bool) {
	if size < minidumpHeaderBytes {
		return nil, false
	}

	streamCount, streamCountOK := readMinidumpUint32(reader, size, 8)
	if !streamCountOK {
		return nil, false
	}

	if streamCount == 0 {
		return nil, true
	}

	if streamCount > minidumpMaxStreamCount {
		return nil, false
	}

	directoryRVA, directoryRVAOK := readMinidumpUint32(reader, size, 12)
	if !directoryRVAOK {
		return nil, false
	}

	entries := make([]streamDirectoryEntry, 0, streamCount)
	for index := uint32(0); index < streamCount; index++ {
		entryOffset := directoryRVA + index*minidumpDirectoryBytes
		entry, entryOK := readMinidumpDirectoryEntry(reader, size, entryOffset)
		if !entryOK {
			return nil, false
		}

		entries = append(entries, entry)
	}

	return entries, true
}

func readMinidumpDirectoryEntry(
	reader io.ReaderAt,
	size int64,
	offset uint32,
) (streamDirectoryEntry, bool) {
	streamType, streamTypeOK := readMinidumpUint32(reader, size, offset)
	if !streamTypeOK {
		return streamDirectoryEntry{}, false
	}

	dataSize, dataSizeOK := readMinidumpUint32(reader, size, offset+4)
	if !dataSizeOK {
		return streamDirectoryEntry{}, false
	}

	rva, rvaOK := readMinidumpUint32(reader, size, offset+8)
	if !rvaOK {
		return streamDirectoryEntry{}, false
	}

	return streamDirectoryEntry{streamType: streamType, dataSize: dataSize, rva: rva}, true
}

func parseMinidumpModules(
	reader io.ReaderAt,
	size int64,
	entries []streamDirectoryEntry,
) []domain.NativeModule {
	entry, entryOK := findMinidumpStream(entries, minidumpModuleListStream)
	if !entryOK {
		return nil
	}

	moduleCount, moduleCountOK := readMinidumpUint32(reader, size, entry.rva)
	if !moduleCountOK {
		return nil
	}

	if moduleCount == 0 {
		return nil
	}

	if moduleCount > minidumpMaxModuleCount {
		return nil
	}

	modules := make([]domain.NativeModule, 0, moduleCount)
	for index := uint32(0); index < moduleCount; index++ {
		offset := entry.rva + 4 + index*minidumpModuleBytes
		if !minidumpRangeWithinStream(entry, offset, minidumpModuleBytes) {
			break
		}

		module, moduleOK := parseMinidumpModule(reader, size, offset)
		if !moduleOK {
			continue
		}

		modules = append(modules, module)
	}

	return modules
}

func parseMinidumpModule(
	reader io.ReaderAt,
	size int64,
	offset uint32,
) (domain.NativeModule, bool) {
	imageAddr, imageAddrOK := readMinidumpUint64(reader, size, offset+minidumpModuleBase)
	if !imageAddrOK {
		return domain.NativeModule{}, false
	}

	imageSize, imageSizeOK := readMinidumpUint32(reader, size, offset+minidumpModuleSize)
	if !imageSizeOK {
		return domain.NativeModule{}, false
	}

	nameRVA, nameRVAOK := readMinidumpUint32(reader, size, offset+minidumpModuleNameRVA)
	if !nameRVAOK {
		return domain.NativeModule{}, false
	}

	cvSize, cvSizeOK := readMinidumpUint32(reader, size, offset+minidumpModuleCvSize)
	if !cvSizeOK {
		return domain.NativeModule{}, false
	}

	cvRVA, cvRVAOK := readMinidumpUint32(reader, size, offset+minidumpModuleCvRVA)
	if !cvRVAOK {
		return domain.NativeModule{}, false
	}

	debugID, debugIDOK := parseMinidumpCodeViewDebugID(reader, size, cvRVA, cvSize)
	if !debugIDOK {
		return domain.NativeModule{}, false
	}

	codeFile, codeFileOK := readMinidumpString(reader, size, nameRVA)
	if !codeFileOK {
		return domain.NativeModule{}, false
	}

	module, moduleErr := domain.NewNativeModule(debugID, codeFile, imageAddr, uint64(imageSize))
	if moduleErr != nil {
		return domain.NativeModule{}, false
	}

	return module, true
}

func parseMinidumpCodeViewDebugID(
	reader io.ReaderAt,
	size int64,
	rva uint32,
	dataSize uint32,
) (domain.DebugIdentifier, bool) {
	if dataSize < 24 {
		return domain.DebugIdentifier{}, false
	}

	if dataSize > minidumpMaxCodeViewBytes {
		return domain.DebugIdentifier{}, false
	}

	payload, payloadOK := readMinidumpBytes(reader, size, rva, dataSize)
	if !payloadOK {
		return domain.DebugIdentifier{}, false
	}

	if !bytes.HasPrefix(payload, codeViewPDB70Signature) {
		return domain.DebugIdentifier{}, false
	}

	age := binary.LittleEndian.Uint32(payload[20:24])
	debugID, debugIDErr := domain.NewDebugIdentifier(pdb70DebugIdentifier(payload[4:20], age))
	if debugIDErr != nil {
		return domain.DebugIdentifier{}, false
	}

	return debugID, true
}

func pdb70DebugIdentifier(guid []byte, age uint32) string {
	prefix := fmt.Sprintf(
		"%08x%04x%04x%s",
		binary.LittleEndian.Uint32(guid[0:4]),
		binary.LittleEndian.Uint16(guid[4:6]),
		binary.LittleEndian.Uint16(guid[6:8]),
		hex.EncodeToString(guid[8:16]),
	)

	if age == 0 {
		return prefix
	}

	return fmt.Sprintf("%s%x", prefix, age)
}

func parseMinidumpFrames(
	reader io.ReaderAt,
	size int64,
	entries []streamDirectoryEntry,
	modules []domain.NativeModule,
) []domain.NativeFrame {
	instructionAddr, instructionAddrOK := parseMinidumpExceptionInstruction(reader, size, entries)
	if !instructionAddrOK {
		return nil
	}

	module, moduleOK := minidumpModuleForInstructionAddr(modules, instructionAddr)
	if moduleOK {
		frame, frameErr := domain.NewNativeFrameWithModule(
			instructionAddr,
			module.DebugID(),
			"",
			module.CodeFile(),
		)
		if frameErr == nil {
			return []domain.NativeFrame{frame}
		}
	}

	frame, frameErr := domain.NewNativeFrame(instructionAddr, "", "")
	if frameErr != nil {
		return nil
	}

	return []domain.NativeFrame{frame}
}

func parseMinidumpExceptionInstruction(
	reader io.ReaderAt,
	size int64,
	entries []streamDirectoryEntry,
) (uint64, bool) {
	entry, entryOK := findMinidumpStream(entries, minidumpExceptionStream)
	if !entryOK {
		return 0, false
	}

	if entry.dataSize < minidumpExceptionCtxRVA+4 {
		return 0, false
	}

	contextSize, contextSizeOK := readMinidumpUint32(reader, size, entry.rva+minidumpExceptionCtxSize)
	if !contextSizeOK {
		return 0, false
	}

	contextRVA, contextRVAOK := readMinidumpUint32(reader, size, entry.rva+minidumpExceptionCtxRVA)
	if !contextRVAOK {
		return 0, false
	}

	if contextSize < minidumpX64RipOffset+8 {
		return 0, false
	}

	instructionAddr, instructionAddrOK := readMinidumpUint64(reader, size, contextRVA+minidumpX64RipOffset)
	if !instructionAddrOK {
		return 0, false
	}

	if instructionAddr == 0 {
		return 0, false
	}

	return instructionAddr, true
}

func minidumpModuleForInstructionAddr(
	modules []domain.NativeModule,
	instructionAddr uint64,
) (domain.NativeModule, bool) {
	for _, module := range modules {
		if !minidumpModuleContainsInstructionAddr(module, instructionAddr) {
			continue
		}

		return module, true
	}

	return domain.NativeModule{}, false
}

func minidumpModuleContainsInstructionAddr(
	module domain.NativeModule,
	instructionAddr uint64,
) bool {
	if instructionAddr < module.ImageAddr() {
		return false
	}

	if module.ImageSize() == 0 {
		return instructionAddr == module.ImageAddr()
	}

	return instructionAddr-module.ImageAddr() < module.ImageSize()
}

func readMinidumpString(
	reader io.ReaderAt,
	size int64,
	rva uint32,
) (string, bool) {
	byteCount, byteCountOK := readMinidumpUint32(reader, size, rva)
	if !byteCountOK {
		return "", false
	}

	if byteCount == 0 {
		return "", false
	}

	if byteCount > minidumpMaxStringBytes {
		return "", false
	}

	if byteCount%2 != 0 {
		return "", false
	}

	payload, payloadOK := readMinidumpBytes(reader, size, rva+4, byteCount)
	if !payloadOK {
		return "", false
	}

	units := make([]uint16, 0, len(payload)/2)
	for index := 0; index < len(payload); index += 2 {
		units = append(units, binary.LittleEndian.Uint16(payload[index:index+2]))
	}

	return string(utf16.Decode(units)), true
}

func findMinidumpStream(
	entries []streamDirectoryEntry,
	streamType uint32,
) (streamDirectoryEntry, bool) {
	for _, entry := range entries {
		if entry.streamType != streamType {
			continue
		}

		return entry, true
	}

	return streamDirectoryEntry{}, false
}

func minidumpRangeWithinStream(
	entry streamDirectoryEntry,
	offset uint32,
	byteCount uint32,
) bool {
	start := uint64(entry.rva)
	end := start + uint64(entry.dataSize)
	rangeStart := uint64(offset)
	rangeEnd := rangeStart + uint64(byteCount)

	return rangeStart >= start && rangeEnd <= end
}

func readMinidumpUint32(
	reader io.ReaderAt,
	size int64,
	offset uint32,
) (uint32, bool) {
	payload, payloadOK := readMinidumpBytes(reader, size, offset, 4)
	if !payloadOK {
		return 0, false
	}

	return binary.LittleEndian.Uint32(payload), true
}

func readMinidumpUint64(
	reader io.ReaderAt,
	size int64,
	offset uint32,
) (uint64, bool) {
	payload, payloadOK := readMinidumpBytes(reader, size, offset, 8)
	if !payloadOK {
		return 0, false
	}

	return binary.LittleEndian.Uint64(payload), true
}

func readMinidumpBytes(
	reader io.ReaderAt,
	size int64,
	offset uint32,
	byteCount uint32,
) ([]byte, bool) {
	start := int64(offset)
	end := start + int64(byteCount)
	if start < 0 || end < start || end > size {
		return nil, false
	}

	payload := make([]byte, byteCount)
	_, readErr := reader.ReadAt(payload, start)
	if readErr != nil {
		return nil, false
	}

	return payload, true
}

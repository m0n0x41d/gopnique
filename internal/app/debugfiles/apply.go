package debugfiles

import (
	"context"
	"errors"
	"io"

	"github.com/ivanzakutnii/error-tracker/internal/app/artifacts"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
)

// ApplyToCanonicalEvent resolves native frame symbols through stored Breakpad
// debug files. Missing or unsupported debug files leave frames unchanged.
func ApplyToCanonicalEvent(
	ctx context.Context,
	store *Service,
	event domain.CanonicalEvent,
) domain.CanonicalEvent {
	if store == nil {
		return event
	}

	frames := event.NativeFrames()
	if len(frames) == 0 {
		return event
	}

	modules := event.NativeModules()
	updated := make([]domain.NativeFrame, len(frames))
	hasUpdate := false

	for index, frame := range frames {
		symbolicated, ok := symbolicateSingleFrame(ctx, store, event, modules, frame)
		if !ok {
			updated[index] = frame
			continue
		}

		updated[index] = symbolicated
		hasUpdate = true
	}

	if !hasUpdate {
		return event
	}

	return event.WithNativeFrames(updated)
}

func symbolicateSingleFrame(
	ctx context.Context,
	store *Service,
	event domain.CanonicalEvent,
	modules []domain.NativeModule,
	frame domain.NativeFrame,
) (domain.NativeFrame, bool) {
	debugID, hasModule := frame.ModuleDebugID()
	if !hasModule {
		return frame, false
	}

	module, hasNativeModule := nativeModuleForDebugID(modules, debugID)
	offset, offsetOK := nativeInstructionOffset(frame, module, hasNativeModule)
	if !offsetOK {
		return frame, false
	}

	symbols, symbolsErr := store.FindBreakpadSymbols(
		ctx,
		event.OrganizationID(),
		event.ProjectID(),
		debugID,
	)
	if symbolsErr != nil {
		return frame, false
	}

	function, found := symbols.lookup(offset)
	if !found {
		return frame, false
	}

	pkg := nativeFramePackage(frame, module, hasNativeModule)
	symbolicated, frameErr := domain.NewNativeFrameWithModule(
		frame.InstructionAddr(),
		debugID,
		function,
		pkg,
	)
	if frameErr != nil {
		return frame, false
	}

	return symbolicated, true
}

func (service *Service) FindBreakpadSymbols(
	ctx context.Context,
	organizationID domain.OrganizationID,
	projectID domain.ProjectID,
	debugID domain.DebugIdentifier,
) (breakpadSymbols, error) {
	scope, scopeErr := artifacts.NewArtifactScope(
		organizationID,
		projectID,
		domain.ArtifactKindDebugFile(),
	)
	if scopeErr != nil {
		return breakpadSymbols{}, scopeErr
	}

	listResult := service.vault.ListArtifacts(ctx, scope)
	listings, listErr := listResult.Value()
	if listErr != nil {
		return breakpadSymbols{}, listErr
	}

	for _, listing := range listings {
		symbols, openErr := service.openBreakpadSymbols(ctx, listing.Key())
		if openErr != nil {
			continue
		}

		if symbols.debugID.String() != debugID.String() {
			continue
		}

		return symbols, nil
	}

	return breakpadSymbols{}, ErrDebugFileNotFound
}

func (service *Service) openBreakpadSymbols(
	ctx context.Context,
	key domain.ArtifactKey,
) (breakpadSymbols, error) {
	getResult := service.vault.GetArtifact(ctx, key)
	reader, getErr := getResult.Value()
	if getErr != nil {
		if errors.Is(getErr, artifacts.ErrArtifactNotFound) {
			return breakpadSymbols{}, ErrDebugFileNotFound
		}

		return breakpadSymbols{}, getErr
	}
	defer reader.Close()

	payload, readErr := io.ReadAll(io.LimitReader(reader, debugFileMaxBytes+1))
	if readErr != nil {
		return breakpadSymbols{}, readErr
	}

	if len(payload) > debugFileMaxBytes {
		return breakpadSymbols{}, ErrDebugFileTooLarge
	}

	symbols, parseErr := parseBreakpadSymbols(payload)
	if parseErr != nil {
		return breakpadSymbols{}, parseErr
	}

	return symbols, nil
}

func nativeModuleForDebugID(
	modules []domain.NativeModule,
	debugID domain.DebugIdentifier,
) (domain.NativeModule, bool) {
	for _, module := range modules {
		if module.DebugID().String() != debugID.String() {
			continue
		}

		return module, true
	}

	return domain.NativeModule{}, false
}

func nativeInstructionOffset(
	frame domain.NativeFrame,
	module domain.NativeModule,
	hasModule bool,
) (uint64, bool) {
	if !hasModule {
		return frame.InstructionAddr(), true
	}

	imageAddr := module.ImageAddr()
	instructionAddr := frame.InstructionAddr()
	if instructionAddr < imageAddr {
		return 0, false
	}

	if module.ImageSize() == 0 {
		return instructionAddr - imageAddr, true
	}

	offset := instructionAddr - imageAddr
	if offset >= module.ImageSize() {
		return 0, false
	}

	return offset, true
}

func nativeFramePackage(
	frame domain.NativeFrame,
	module domain.NativeModule,
	hasModule bool,
) string {
	if frame.Package() != "" {
		return frame.Package()
	}

	if !hasModule {
		return ""
	}

	return module.CodeFile()
}

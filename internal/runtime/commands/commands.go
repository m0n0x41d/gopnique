package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/ivanzakutnii/error-tracker/internal/adapters/discord"
	emailadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/email"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/filesystem"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/googlechat"
	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/netresolver"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/ntfy"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/teams"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/telegram"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/webhook"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/zulip"
	"github.com/ivanzakutnii/error-tracker/internal/app/debugfiles"
	"github.com/ivanzakutnii/error-tracker/internal/app/minidumps"
	"github.com/ivanzakutnii/error-tracker/internal/app/sourcemaps"
	"github.com/ivanzakutnii/error-tracker/internal/domain"
	"github.com/ivanzakutnii/error-tracker/internal/runtime/config"
	"github.com/ivanzakutnii/error-tracker/internal/runtime/importcheck"
	"github.com/ivanzakutnii/error-tracker/internal/runtime/worker"
)

const Version = "0.0.0-m3-dev"

func Run(args []string, env []string, stdout io.Writer, stderr io.Writer) int {
	command := firstArg(args)

	switch command {
	case "server":
		return runServer(env, stdout, stderr, config.ModeServer)
	case "worker":
		return runWorker(env, stdout, stderr)
	case "all-in-one":
		return runAllInOne(env, stdout, stderr)
	case "migrate":
		return runMigrate(args, env, stdout, stderr)
	case "admin":
		return runAdmin(args, env, stdout, stderr)
	case "version":
		_, _ = fmt.Fprintln(stdout, Version)
		return 0
	default:
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker server|worker|all-in-one|migrate|admin|version")
		return 2
	}
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}

	return args[0]
}

func runServer(env []string, stdout io.Writer, stderr io.Writer, mode config.Mode) int {
	cfg, cfgErr := config.Load(env, mode)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	enrichments, enrichmentsErr := buildIngestEnrichments(cfg)
	if enrichmentsErr != nil {
		_, _ = fmt.Fprintln(stderr, enrichmentsErr)
		return 1
	}

	server := httpadapter.New(
		cfg.HTTPAddr,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		netresolver.New(nil),
		store,
		enrichments,
		httpadapter.AuthSettings{PublicURL: cfg.PublicURL, SecretKey: cfg.SecretKey},
	)
	_, _ = fmt.Fprintln(stdout, "listening on "+cfg.HTTPAddr)

	err := server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func runWorker(env []string, stdout io.Writer, stderr io.Writer) int {
	cfg, cfgErr := config.Load(env, config.ModeWorker)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	runner, runnerErr := newWorker(cfg, store)
	if runnerErr != nil {
		_, _ = fmt.Fprintln(stderr, runnerErr)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	_, _ = fmt.Fprintln(stdout, "worker started")
	err := runner.Run(ctx)
	if err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func runAllInOne(env []string, stdout io.Writer, stderr io.Writer) int {
	cfg, cfgErr := config.Load(env, config.ModeAllInOne)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	enrichments, enrichmentsErr := buildIngestEnrichments(cfg)
	if enrichmentsErr != nil {
		_, _ = fmt.Fprintln(stderr, enrichmentsErr)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	server := httpadapter.New(
		cfg.HTTPAddr,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		store,
		netresolver.New(nil),
		store,
		enrichments,
		httpadapter.AuthSettings{PublicURL: cfg.PublicURL, SecretKey: cfg.SecretKey},
	)
	errs := make(chan error, 2)
	runner, runnerErr := newWorker(cfg, store)
	if runnerErr != nil {
		_, _ = fmt.Fprintln(stderr, runnerErr)
		return 1
	}

	go func() {
		errs <- server.ListenAndServe()
	}()

	go func() {
		errs <- runner.Run(ctx)
	}()

	_, _ = fmt.Fprintln(stdout, "all-in-one listening on "+cfg.HTTPAddr)
	err := <-errs
	if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}

	return 0
}

func buildIngestEnrichments(cfg config.Config) (httpadapter.IngestEnrichments, error) {
	if cfg.ArtifactRoot == "" {
		return httpadapter.IngestEnrichments{}, nil
	}

	vault, vaultErr := filesystem.NewVault(cfg.ArtifactRoot)
	if vaultErr != nil {
		return httpadapter.IngestEnrichments{}, vaultErr
	}

	resolver, resolverErr := sourcemaps.NewService(vault)
	if resolverErr != nil {
		return httpadapter.IngestEnrichments{}, resolverErr
	}

	return httpadapter.IngestEnrichments{SourceMapResolver: resolver}, nil
}

func runMigrate(args []string, env []string, stdout io.Writer, stderr io.Writer) int {
	subcommand := secondArg(args)
	if subcommand != "status" && subcommand != "up" {
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker migrate status|up")
		return 2
	}

	cfg, cfgErr := config.Load(env, config.ModeMigrate)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	if subcommand == "up" {
		result, migrationErr := store.ApplyMigrations(context.Background())
		if migrationErr != nil {
			_, _ = fmt.Fprintln(stderr, migrationErr)
			return 1
		}

		_, _ = fmt.Fprintf(stdout, "applied migrations: %d\n", len(result.Applied))
		_, _ = fmt.Fprintf(stdout, "skipped migrations: %d\n", len(result.Skipped))
		return 0
	}

	status, statusErr := store.MigrationStatus(context.Background())
	if statusErr != nil {
		_, _ = fmt.Fprintln(stderr, statusErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "applied migrations: %d\n", status.AppliedCount)
	return 0
}

func runAdmin(args []string, env []string, stdout io.Writer, stderr io.Writer) int {
	subcommand := secondArg(args)
	if subcommand != "check-imports" && subcommand != "bootstrap" && subcommand != "telegram" && subcommand != "alert" && subcommand != "sourcemap" && subcommand != "debugfile" && subcommand != "minidump" {
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker admin bootstrap|check-imports|telegram|alert|sourcemap|debugfile|minidump")
		return 2
	}

	if subcommand == "bootstrap" {
		return runBootstrap(env, stdout, stderr)
	}

	if subcommand == "telegram" {
		return runTelegramAdmin(args[2:], env, stdout, stderr)
	}

	if subcommand == "alert" {
		return runAlertAdmin(args[2:], env, stdout, stderr)
	}

	if subcommand == "sourcemap" {
		return runSourceMapAdmin(args[2:], env, stdout, stderr)
	}

	if subcommand == "debugfile" {
		return runDebugFileAdmin(args[2:], env, stdout, stderr)
	}

	if subcommand == "minidump" {
		return runMinidumpAdmin(args[2:], env, stdout, stderr)
	}

	violations, checkErr := importcheck.Check(".")
	if checkErr != nil {
		_, _ = fmt.Fprintln(stderr, checkErr)
		return 1
	}

	for _, violation := range violations {
		_, _ = fmt.Fprintf(stderr, "%s imports %s: %s\n", violation.File, violation.Import, violation.Reason)
	}

	err := importcheck.ErrViolations(violations)
	if err != nil {
		return 1
	}

	_, _ = fmt.Fprintln(stdout, "import boundaries ok")
	return 0
}

func runSourceMapAdmin(args []string, env []string, stdout io.Writer, stderr io.Writer) int {
	subcommand := firstArg(args)
	if subcommand != "upload" && subcommand != "resolve" {
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker admin sourcemap upload|resolve")
		return 2
	}

	cfg, cfgErr := config.Load(env, config.ModeAdmin)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	if cfg.ArtifactRoot == "" {
		_, _ = fmt.Fprintln(stderr, "ARTIFACT_ROOT is required for source map admin")
		return 1
	}

	vault, vaultErr := filesystem.NewVault(cfg.ArtifactRoot)
	if vaultErr != nil {
		_, _ = fmt.Fprintln(stderr, vaultErr)
		return 1
	}

	service, serviceErr := sourcemaps.NewService(vault)
	if serviceErr != nil {
		_, _ = fmt.Fprintln(stderr, serviceErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	if subcommand == "upload" {
		return runSourceMapUpload(args[1:], store, service, stdout, stderr)
	}

	return runSourceMapResolve(args[1:], store, service, stdout, stderr)
}

func runSourceMapUpload(
	args []string,
	store *postgres.Store,
	service *sourcemaps.Service,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("sourcemap upload", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRef := flags.String("project-ref", "", "project ingest ref")
	releaseInput := flags.String("release", "", "release identifier")
	distInput := flags.String("dist", "", "optional distribution identifier")
	fileInput := flags.String("file-name", "", "minified file name (eg static/js/app.min.js)")
	pathInput := flags.String("path", "", "filesystem path to source map .map file")
	parseErr := flags.Parse(args)
	if parseErr != nil {
		return 2
	}

	identity, scope, identityErr := loadSourceMapIdentity(
		context.Background(),
		store,
		*projectRef,
		*releaseInput,
		*distInput,
		*fileInput,
	)
	if identityErr != nil {
		_, _ = fmt.Fprintln(stderr, identityErr)
		return 1
	}

	if *pathInput == "" {
		_, _ = fmt.Fprintln(stderr, "--path is required")
		return 1
	}

	body, openErr := os.Open(*pathInput)
	if openErr != nil {
		_, _ = fmt.Fprintln(stderr, openErr)
		return 1
	}
	defer body.Close()

	uploadResult := service.Upload(
		context.Background(),
		scope.OrganizationID,
		scope.ProjectID,
		identity,
		body,
	)
	stored, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		_, _ = fmt.Fprintln(stderr, uploadErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "organization_id: %s\n", scope.OrganizationID.String())
	_, _ = fmt.Fprintf(stdout, "project_id: %s\n", scope.ProjectID.String())
	_, _ = fmt.Fprintf(stdout, "release: %s\n", identity.Release().String())
	_, _ = fmt.Fprintf(stdout, "dist: %s\n", identity.Dist().String())
	_, _ = fmt.Fprintf(stdout, "file_name: %s\n", identity.FileName().String())
	_, _ = fmt.Fprintf(stdout, "artifact_name: %s\n", identity.ArtifactName().String())
	_, _ = fmt.Fprintf(stdout, "size_bytes: %d\n", stored.Size())

	return 0
}

func runSourceMapResolve(
	args []string,
	store *postgres.Store,
	service *sourcemaps.Service,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("sourcemap resolve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRef := flags.String("project-ref", "", "project ingest ref")
	releaseInput := flags.String("release", "", "release identifier")
	distInput := flags.String("dist", "", "optional distribution identifier")
	fileInput := flags.String("file-name", "", "minified file name (eg static/js/app.min.js)")
	lineInput := flags.String("line", "", "zero-based generated line")
	columnInput := flags.String("column", "", "zero-based generated column")
	parseErr := flags.Parse(args)
	if parseErr != nil {
		return 2
	}

	identity, scope, identityErr := loadSourceMapIdentity(
		context.Background(),
		store,
		*projectRef,
		*releaseInput,
		*distInput,
		*fileInput,
	)
	if identityErr != nil {
		_, _ = fmt.Fprintln(stderr, identityErr)
		return 1
	}

	line, lineErr := strconv.Atoi(*lineInput)
	if lineErr != nil || line < 0 {
		_, _ = fmt.Fprintln(stderr, "--line must be a non-negative integer")
		return 1
	}

	column, columnErr := strconv.Atoi(*columnInput)
	if columnErr != nil || column < 0 {
		_, _ = fmt.Fprintln(stderr, "--column must be a non-negative integer")
		return 1
	}

	resolveResult := service.Resolve(
		context.Background(),
		scope.OrganizationID,
		scope.ProjectID,
		identity,
		sourcemaps.NewGeneratedPosition(line, column),
	)
	resolved, resolveErr := resolveResult.Value()
	if resolveErr != nil {
		if errors.Is(resolveErr, sourcemaps.ErrSourceMapNotFound) {
			_, _ = fmt.Fprintln(stderr, "unresolved")
			return 1
		}

		_, _ = fmt.Fprintln(stderr, resolveErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "source: %s\n", resolved.Source())
	_, _ = fmt.Fprintf(stdout, "original_line: %d\n", resolved.OriginalLine())
	_, _ = fmt.Fprintf(stdout, "original_column: %d\n", resolved.OriginalColumn())
	if name, ok := resolved.Name(); ok {
		_, _ = fmt.Fprintf(stdout, "name: %s\n", name)
	}

	return 0
}

func loadSourceMapIdentity(
	ctx context.Context,
	store *postgres.Store,
	projectRef string,
	releaseInput string,
	distInput string,
	fileInput string,
) (domain.SourceMapIdentity, postgres.ProjectScope, error) {
	if projectRef == "" {
		return domain.SourceMapIdentity{}, postgres.ProjectScope{}, errors.New("--project-ref is required")
	}

	scope, scopeErr := store.LookupProjectByRef(ctx, projectRef)
	if scopeErr != nil {
		return domain.SourceMapIdentity{}, postgres.ProjectScope{}, scopeErr
	}

	release, releaseErr := domain.NewReleaseName(releaseInput)
	if releaseErr != nil {
		return domain.SourceMapIdentity{}, postgres.ProjectScope{}, releaseErr
	}

	dist, distErr := domain.NewOptionalDistName(distInput)
	if distErr != nil {
		return domain.SourceMapIdentity{}, postgres.ProjectScope{}, distErr
	}

	fileName, fileErr := domain.NewSourceMapFileName(fileInput)
	if fileErr != nil {
		return domain.SourceMapIdentity{}, postgres.ProjectScope{}, fileErr
	}

	identity, identityErr := domain.NewSourceMapIdentity(release, dist, fileName)
	if identityErr != nil {
		return domain.SourceMapIdentity{}, postgres.ProjectScope{}, identityErr
	}

	return identity, scope, nil
}

func runDebugFileAdmin(args []string, env []string, stdout io.Writer, stderr io.Writer) int {
	subcommand := firstArg(args)
	if subcommand != "upload" && subcommand != "list" {
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker admin debugfile upload|list")
		return 2
	}

	cfg, cfgErr := config.Load(env, config.ModeAdmin)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	if cfg.ArtifactRoot == "" {
		_, _ = fmt.Fprintln(stderr, "ARTIFACT_ROOT is required for debug file admin")
		return 1
	}

	vault, vaultErr := filesystem.NewVault(cfg.ArtifactRoot)
	if vaultErr != nil {
		_, _ = fmt.Fprintln(stderr, vaultErr)
		return 1
	}

	service, serviceErr := debugfiles.NewService(vault)
	if serviceErr != nil {
		_, _ = fmt.Fprintln(stderr, serviceErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	if subcommand == "upload" {
		return runDebugFileUpload(args[1:], store, service, stdout, stderr)
	}

	return runDebugFileList(args[1:], store, service, stdout, stderr)
}

func runDebugFileUpload(
	args []string,
	store *postgres.Store,
	service *debugfiles.Service,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("debugfile upload", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRef := flags.String("project-ref", "", "project ingest ref")
	debugIDInput := flags.String("debug-id", "", "build/debug identifier (hex, dashes optional)")
	kindInput := flags.String("kind", "", "debug file kind (breakpad|elf|macho|pdb)")
	fileInput := flags.String("file-name", "", "debug file name (eg libapp.so.sym)")
	pathInput := flags.String("path", "", "filesystem path to debug file payload")
	parseErr := flags.Parse(args)
	if parseErr != nil {
		return 2
	}

	if *projectRef == "" {
		_, _ = fmt.Fprintln(stderr, "--project-ref is required")
		return 1
	}

	scope, scopeErr := store.LookupProjectByRef(context.Background(), *projectRef)
	if scopeErr != nil {
		_, _ = fmt.Fprintln(stderr, scopeErr)
		return 1
	}

	debugID, debugIDErr := domain.NewDebugIdentifier(*debugIDInput)
	if debugIDErr != nil {
		_, _ = fmt.Fprintln(stderr, debugIDErr)
		return 1
	}

	kind, kindErr := domain.ParseDebugFileKind(*kindInput)
	if kindErr != nil {
		_, _ = fmt.Fprintln(stderr, kindErr)
		return 1
	}

	fileName, fileErr := domain.NewDebugFileName(*fileInput)
	if fileErr != nil {
		_, _ = fmt.Fprintln(stderr, fileErr)
		return 1
	}

	identity, identityErr := domain.NewDebugFileIdentity(debugID, kind, fileName)
	if identityErr != nil {
		_, _ = fmt.Fprintln(stderr, identityErr)
		return 1
	}

	if *pathInput == "" {
		_, _ = fmt.Fprintln(stderr, "--path is required")
		return 1
	}

	body, openErr := os.Open(*pathInput)
	if openErr != nil {
		_, _ = fmt.Fprintln(stderr, openErr)
		return 1
	}
	defer body.Close()

	uploadResult := service.Upload(
		context.Background(),
		scope.OrganizationID,
		scope.ProjectID,
		identity,
		body,
	)
	stored, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		_, _ = fmt.Fprintln(stderr, uploadErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "organization_id: %s\n", scope.OrganizationID.String())
	_, _ = fmt.Fprintf(stdout, "project_id: %s\n", scope.ProjectID.String())
	_, _ = fmt.Fprintf(stdout, "debug_id: %s\n", identity.DebugID().String())
	_, _ = fmt.Fprintf(stdout, "kind: %s\n", identity.Kind().String())
	_, _ = fmt.Fprintf(stdout, "file_name: %s\n", identity.FileName().String())
	_, _ = fmt.Fprintf(stdout, "artifact_name: %s\n", identity.ArtifactName().String())
	_, _ = fmt.Fprintf(stdout, "size_bytes: %d\n", stored.Size())

	return 0
}

func runDebugFileList(
	args []string,
	store *postgres.Store,
	service *debugfiles.Service,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("debugfile list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRef := flags.String("project-ref", "", "project ingest ref")
	parseErr := flags.Parse(args)
	if parseErr != nil {
		return 2
	}

	if *projectRef == "" {
		_, _ = fmt.Fprintln(stderr, "--project-ref is required")
		return 1
	}

	scope, scopeErr := store.LookupProjectByRef(context.Background(), *projectRef)
	if scopeErr != nil {
		_, _ = fmt.Fprintln(stderr, scopeErr)
		return 1
	}

	listResult := service.List(
		context.Background(),
		scope.OrganizationID,
		scope.ProjectID,
	)
	listings, listErr := listResult.Value()
	if listErr != nil {
		_, _ = fmt.Fprintln(stderr, listErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "organization_id: %s\n", scope.OrganizationID.String())
	_, _ = fmt.Fprintf(stdout, "project_id: %s\n", scope.ProjectID.String())
	_, _ = fmt.Fprintf(stdout, "debug_files: %d\n", len(listings))

	for _, listing := range listings {
		_, _ = fmt.Fprintf(stdout, "- artifact_name: %s size_bytes: %d\n", listing.ArtifactKey().Name().String(), listing.Size())
	}

	return 0
}

func runMinidumpAdmin(args []string, env []string, stdout io.Writer, stderr io.Writer) int {
	subcommand := firstArg(args)
	if subcommand != "upload" && subcommand != "list" {
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker admin minidump upload|list")
		return 2
	}

	cfg, cfgErr := config.Load(env, config.ModeAdmin)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	if cfg.ArtifactRoot == "" {
		_, _ = fmt.Fprintln(stderr, "ARTIFACT_ROOT is required for minidump admin")
		return 1
	}

	vault, vaultErr := filesystem.NewVault(cfg.ArtifactRoot)
	if vaultErr != nil {
		_, _ = fmt.Fprintln(stderr, vaultErr)
		return 1
	}

	service, serviceErr := minidumps.NewService(vault)
	if serviceErr != nil {
		_, _ = fmt.Fprintln(stderr, serviceErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	if subcommand == "upload" {
		return runMinidumpUpload(args[1:], store, service, stdout, stderr)
	}

	return runMinidumpList(args[1:], store, service, stdout, stderr)
}

func runMinidumpUpload(
	args []string,
	store *postgres.Store,
	service *minidumps.Service,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("minidump upload", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRef := flags.String("project-ref", "", "project ingest ref")
	eventIDInput := flags.String("event-id", "", "Sentry-format event id (32 hex chars)")
	attachmentNameInput := flags.String("attachment-name", "", "minidump attachment name (eg upload_file_minidump)")
	pathInput := flags.String("path", "", "filesystem path to .dmp payload")
	parseErr := flags.Parse(args)
	if parseErr != nil {
		return 2
	}

	if *projectRef == "" {
		_, _ = fmt.Fprintln(stderr, "--project-ref is required")
		return 1
	}

	scope, scopeErr := store.LookupProjectByRef(context.Background(), *projectRef)
	if scopeErr != nil {
		_, _ = fmt.Fprintln(stderr, scopeErr)
		return 1
	}

	eventID, eventIDErr := domain.NewEventID(*eventIDInput)
	if eventIDErr != nil {
		_, _ = fmt.Fprintln(stderr, eventIDErr)
		return 1
	}

	attachmentName, attachmentErr := domain.NewMinidumpAttachmentName(*attachmentNameInput)
	if attachmentErr != nil {
		_, _ = fmt.Fprintln(stderr, attachmentErr)
		return 1
	}

	identity, identityErr := domain.NewMinidumpIdentity(eventID, attachmentName)
	if identityErr != nil {
		_, _ = fmt.Fprintln(stderr, identityErr)
		return 1
	}

	if *pathInput == "" {
		_, _ = fmt.Fprintln(stderr, "--path is required")
		return 1
	}

	body, openErr := os.Open(*pathInput)
	if openErr != nil {
		_, _ = fmt.Fprintln(stderr, openErr)
		return 1
	}
	defer body.Close()

	uploadResult := service.Upload(
		context.Background(),
		scope.OrganizationID,
		scope.ProjectID,
		identity,
		body,
	)
	stored, uploadErr := uploadResult.Value()
	if uploadErr != nil {
		_, _ = fmt.Fprintln(stderr, uploadErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "organization_id: %s\n", scope.OrganizationID.String())
	_, _ = fmt.Fprintf(stdout, "project_id: %s\n", scope.ProjectID.String())
	_, _ = fmt.Fprintf(stdout, "event_id: %s\n", identity.EventID().String())
	_, _ = fmt.Fprintf(stdout, "attachment_name: %s\n", identity.AttachmentName().String())
	_, _ = fmt.Fprintf(stdout, "artifact_name: %s\n", identity.ArtifactName().String())
	_, _ = fmt.Fprintf(stdout, "size_bytes: %d\n", stored.Size())

	return 0
}

func runMinidumpList(
	args []string,
	store *postgres.Store,
	service *minidumps.Service,
	stdout io.Writer,
	stderr io.Writer,
) int {
	flags := flag.NewFlagSet("minidump list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRef := flags.String("project-ref", "", "project ingest ref")
	parseErr := flags.Parse(args)
	if parseErr != nil {
		return 2
	}

	if *projectRef == "" {
		_, _ = fmt.Fprintln(stderr, "--project-ref is required")
		return 1
	}

	scope, scopeErr := store.LookupProjectByRef(context.Background(), *projectRef)
	if scopeErr != nil {
		_, _ = fmt.Fprintln(stderr, scopeErr)
		return 1
	}

	listResult := service.List(
		context.Background(),
		scope.OrganizationID,
		scope.ProjectID,
	)
	listings, listErr := listResult.Value()
	if listErr != nil {
		_, _ = fmt.Fprintln(stderr, listErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "organization_id: %s\n", scope.OrganizationID.String())
	_, _ = fmt.Fprintf(stdout, "project_id: %s\n", scope.ProjectID.String())
	_, _ = fmt.Fprintf(stdout, "minidumps: %d\n", len(listings))

	for _, listing := range listings {
		_, _ = fmt.Fprintf(stdout, "- artifact_name: %s size_bytes: %d\n", listing.ArtifactKey().Name().String(), listing.Size())
	}

	return 0
}

func runAlertAdmin(args []string, env []string, stdout io.Writer, stderr io.Writer) int {
	if firstArg(args) != "issue-opened-telegram" {
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker admin alert issue-opened-telegram --project-ref <ref> --destination-id <id> --name <name>")
		return 2
	}

	flags := flag.NewFlagSet("alert issue-opened-telegram", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRef := flags.String("project-ref", "", "project ingest ref")
	destinationID := flags.String("destination-id", "", "telegram destination id")
	name := flags.String("name", "", "operator-visible alert rule name")
	parseErr := flags.Parse(args[1:])
	if parseErr != nil {
		return 2
	}

	cfg, cfgErr := config.Load(env, config.ModeAdmin)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	alert, alertErr := store.AddIssueOpenedTelegramAlert(
		context.Background(),
		postgres.IssueOpenedTelegramAlertInput{
			ProjectRef:    *projectRef,
			DestinationID: *destinationID,
			Name:          *name,
		},
	)
	if alertErr != nil {
		_, _ = fmt.Fprintln(stderr, alertErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "alert_rule_id: %s\n", alert.RuleID)
	_, _ = fmt.Fprintf(stdout, "alert_action_id: %s\n", alert.ActionID)
	_, _ = fmt.Fprintf(stdout, "telegram_destination_id: %s\n", alert.DestinationID)
	_, _ = fmt.Fprintf(stdout, "project_id: %s\n", alert.ProjectID)
	_, _ = fmt.Fprintf(stdout, "name: %s\n", alert.Name)

	return 0
}

func newWorker(cfg config.Config, store *postgres.Store) (worker.Worker, error) {
	resolver := netresolver.New(nil)
	tasks := []worker.Task{
		worker.NewWebhookTask(
			store,
			resolver,
			webhook.NewSender(http.DefaultClient),
			worker.WebhookTaskConfig{
				PublicURL: cfg.PublicURL,
				BatchSize: cfg.NotificationBatchSize,
			},
		),
		worker.NewDiscordTask(
			store,
			resolver,
			discord.NewSender(http.DefaultClient),
			worker.DiscordTaskConfig{
				PublicURL: cfg.PublicURL,
				BatchSize: cfg.NotificationBatchSize,
			},
		),
		worker.NewGoogleChatTask(
			store,
			resolver,
			googlechat.NewSender(http.DefaultClient),
			worker.GoogleChatTaskConfig{
				PublicURL: cfg.PublicURL,
				BatchSize: cfg.NotificationBatchSize,
			},
		),
		worker.NewNtfyTask(
			store,
			resolver,
			ntfy.NewSender(http.DefaultClient),
			worker.NtfyTaskConfig{
				PublicURL: cfg.PublicURL,
				BatchSize: cfg.NotificationBatchSize,
			},
		),
		worker.NewTeamsTask(
			store,
			resolver,
			teams.NewSender(http.DefaultClient),
			worker.TeamsTaskConfig{
				PublicURL: cfg.PublicURL,
				BatchSize: cfg.NotificationBatchSize,
			},
		),
		worker.NewZulipTask(
			store,
			resolver,
			zulip.NewSender(http.DefaultClient),
			worker.ZulipTaskConfig{
				PublicURL: cfg.PublicURL,
				BatchSize: cfg.NotificationBatchSize,
			},
		),
	}
	if cfg.TelegramBotToken != "" {
		sender, senderErr := telegram.NewSender(
			http.DefaultClient,
			cfg.TelegramAPIBaseURL,
			cfg.TelegramBotToken,
		)
		if senderErr != nil {
			return worker.Worker{}, senderErr
		}

		tasks = append(tasks, worker.NewTelegramTask(
			store,
			sender,
			worker.TelegramTaskConfig{
				PublicURL: cfg.PublicURL,
				BatchSize: cfg.NotificationBatchSize,
			},
		))
	}

	if cfg.SMTPEnabled() {
		sender, senderErr := emailadapter.NewSender(
			emailadapter.SMTPMailer{},
			emailadapter.SenderConfig{
				Addr:     cfg.SMTPAddr,
				Username: cfg.SMTPUsername,
				Password: cfg.SMTPPassword,
				From:     cfg.SMTPFrom,
			},
		)
		if senderErr != nil {
			return worker.Worker{}, senderErr
		}

		tasks = append(tasks, worker.NewEmailTask(
			store,
			sender,
			worker.EmailTaskConfig{
				PublicURL: cfg.PublicURL,
				BatchSize: cfg.NotificationBatchSize,
			},
		))
	}

	tasks = append(tasks, worker.NewRetentionTask(
		store,
		worker.RetentionTaskConfig{BatchSize: cfg.RetentionBatchSize},
	))

	return worker.New(tasks...), nil
}

func runTelegramAdmin(args []string, env []string, stdout io.Writer, stderr io.Writer) int {
	if firstArg(args) != "add" {
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker admin telegram add --project-ref <ref> --chat-id <id> --label <label>")
		return 2
	}

	flags := flag.NewFlagSet("telegram add", flag.ContinueOnError)
	flags.SetOutput(stderr)
	projectRef := flags.String("project-ref", "", "project ingest ref")
	chatID := flags.String("chat-id", "", "telegram chat id")
	label := flags.String("label", "", "operator-visible destination label")
	parseErr := flags.Parse(args[1:])
	if parseErr != nil {
		return 2
	}

	cfg, cfgErr := config.Load(env, config.ModeAdmin)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	destination, destinationErr := store.AddTelegramDestination(
		context.Background(),
		postgres.TelegramDestinationInput{
			ProjectRef: *projectRef,
			ChatID:     *chatID,
			Label:      *label,
		},
	)
	if destinationErr != nil {
		_, _ = fmt.Fprintln(stderr, destinationErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "telegram_destination_id: %s\n", destination.DestinationID)
	_, _ = fmt.Fprintf(stdout, "project_id: %s\n", destination.ProjectID)
	_, _ = fmt.Fprintf(stdout, "chat_id: %s\n", destination.ChatID)
	_, _ = fmt.Fprintf(stdout, "label: %s\n", destination.Label)

	return 0
}

func runBootstrap(env []string, stdout io.Writer, stderr io.Writer) int {
	cfg, cfgErr := config.Load(env, config.ModeAdmin)
	if cfgErr != nil {
		_, _ = fmt.Fprintln(stderr, cfgErr)
		return 1
	}

	store, storeErr := postgres.NewStore(context.Background(), cfg.DatabaseURL)
	if storeErr != nil {
		_, _ = fmt.Fprintln(stderr, storeErr)
		return 1
	}
	defer store.Close()

	result, bootstrapErr := store.Bootstrap(context.Background(), postgres.BootstrapInput{
		PublicURL: cfg.PublicURL,
	})
	if bootstrapErr != nil {
		_, _ = fmt.Fprintln(stderr, bootstrapErr)
		return 1
	}

	_, _ = fmt.Fprintf(stdout, "organization_id: %s\n", result.OrganizationID)
	_, _ = fmt.Fprintf(stdout, "project_id: %s\n", result.ProjectID)
	_, _ = fmt.Fprintf(stdout, "project_ref: %s\n", result.ProjectRef)
	_, _ = fmt.Fprintf(stdout, "public_key: %s\n", result.PublicKey)
	_, _ = fmt.Fprintf(stdout, "dsn: %s\n", result.DSN)

	return 0
}

func secondArg(args []string) string {
	if len(args) < 2 {
		return ""
	}

	return args[1]
}

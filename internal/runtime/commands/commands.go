package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os/signal"
	"syscall"

	httpadapter "github.com/ivanzakutnii/error-tracker/internal/adapters/http"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/netresolver"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/postgres"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/telegram"
	"github.com/ivanzakutnii/error-tracker/internal/adapters/webhook"
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
		netresolver.New(nil),
		store,
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
		netresolver.New(nil),
		store,
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
	if subcommand != "check-imports" && subcommand != "bootstrap" && subcommand != "telegram" && subcommand != "alert" {
		_, _ = fmt.Fprintln(stderr, "usage: error-tracker admin bootstrap|check-imports|telegram|alert")
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

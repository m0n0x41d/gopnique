package config

import (
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

type Mode string

const (
	ModeServer   Mode = "server"
	ModeWorker   Mode = "worker"
	ModeAllInOne Mode = "all-in-one"
	ModeMigrate  Mode = "migrate"
	ModeAdmin    Mode = "admin"
)

type Config struct {
	AppMode               Mode
	PublicURL             string
	DatabaseURL           string
	SecretKey             string
	LogLevel              string
	HTTPAddr              string
	WorkerConcurrency     int
	MigrationsDir         string
	TelegramBotToken      string
	TelegramAPIBaseURL    string
	SMTPAddr              string
	SMTPUsername          string
	SMTPPassword          string
	SMTPFrom              string
	NotificationBatchSize int
	RetentionBatchSize    int
}

func Load(env []string, mode Mode) (Config, error) {
	values := envMap(env)
	workerConcurrency, workerConcurrencyErr := intValueOr(values, "WORKER_CONCURRENCY", 4)
	if workerConcurrencyErr != nil {
		return Config{}, workerConcurrencyErr
	}

	notificationBatchSize, notificationBatchSizeErr := intValueOr(values, "NOTIFICATION_BATCH_SIZE", 10)
	if notificationBatchSizeErr != nil {
		return Config{}, notificationBatchSizeErr
	}

	retentionBatchSize, retentionBatchSizeErr := intValueOr(values, "RETENTION_BATCH_SIZE", 500)
	if retentionBatchSizeErr != nil {
		return Config{}, retentionBatchSizeErr
	}

	cfg := Config{
		AppMode:               mode,
		PublicURL:             values["PUBLIC_URL"],
		DatabaseURL:           values["DATABASE_URL"],
		SecretKey:             values["SECRET_KEY"],
		LogLevel:              valueOr(values, "LOG_LEVEL", "info"),
		HTTPAddr:              valueOr(values, "HTTP_ADDR", "127.0.0.1:8080"),
		WorkerConcurrency:     workerConcurrency,
		MigrationsDir:         valueOr(values, "MIGRATIONS_DIR", "migrations"),
		TelegramBotToken:      values["TELEGRAM_BOT_TOKEN"],
		TelegramAPIBaseURL:    valueOr(values, "TELEGRAM_API_BASE_URL", "https://api.telegram.org"),
		SMTPAddr:              strings.TrimSpace(values["SMTP_ADDR"]),
		SMTPUsername:          strings.TrimSpace(values["SMTP_USERNAME"]),
		SMTPPassword:          values["SMTP_PASSWORD"],
		SMTPFrom:              strings.TrimSpace(values["SMTP_FROM"]),
		NotificationBatchSize: notificationBatchSize,
		RetentionBatchSize:    retentionBatchSize,
	}

	err := validate(cfg)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func envMap(env []string) map[string]string {
	values := make(map[string]string, len(env))

	for _, pair := range env {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}

	return values
}

func valueOr(values map[string]string, key string, fallback string) string {
	value := values[key]
	if value != "" {
		return value
	}

	return fallback
}

func intValueOr(values map[string]string, key string, fallback int) (int, error) {
	value := values[key]
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}

	return parsed, nil
}

func validate(cfg Config) error {
	if !cfg.AppMode.valid() {
		return errors.New("APP_MODE is invalid")
	}

	if cfg.DatabaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}

	databaseURLErr := validateDatabaseURL(cfg.DatabaseURL)
	if databaseURLErr != nil {
		return databaseURLErr
	}

	if cfg.requiresPublicURL() && cfg.PublicURL == "" {
		return errors.New("PUBLIC_URL is required")
	}

	if cfg.PublicURL != "" {
		publicURLErr := validateHTTPURL("PUBLIC_URL", cfg.PublicURL)
		if publicURLErr != nil {
			return publicURLErr
		}
	}

	if cfg.requiresSecretKey() && cfg.SecretKey == "" {
		return errors.New("SECRET_KEY is required")
	}

	if !validLogLevel(cfg.LogLevel) {
		return errors.New("LOG_LEVEL must be debug, info, warn, or error")
	}

	httpAddrErr := validateHTTPAddr(cfg.HTTPAddr)
	if httpAddrErr != nil {
		return httpAddrErr
	}

	if cfg.WorkerConcurrency < 1 {
		return errors.New("WORKER_CONCURRENCY must be positive")
	}

	if cfg.NotificationBatchSize < 1 {
		return errors.New("NOTIFICATION_BATCH_SIZE must be positive")
	}

	if cfg.RetentionBatchSize < 1 {
		return errors.New("RETENTION_BATCH_SIZE must be positive")
	}

	if strings.TrimSpace(cfg.MigrationsDir) == "" {
		return errors.New("MIGRATIONS_DIR is required")
	}

	telegramURLErr := validateHTTPURL("TELEGRAM_API_BASE_URL", cfg.TelegramAPIBaseURL)
	if telegramURLErr != nil {
		return telegramURLErr
	}

	smtpErr := validateSMTP(cfg)
	if smtpErr != nil {
		return smtpErr
	}

	return nil
}

func (mode Mode) valid() bool {
	return mode == ModeServer ||
		mode == ModeWorker ||
		mode == ModeAllInOne ||
		mode == ModeMigrate ||
		mode == ModeAdmin
}

func (cfg Config) requiresPublicURL() bool {
	return cfg.AppMode == ModeServer ||
		cfg.AppMode == ModeWorker ||
		cfg.AppMode == ModeAllInOne
}

func (cfg Config) requiresSecretKey() bool {
	return cfg.AppMode == ModeServer ||
		cfg.AppMode == ModeAllInOne
}

func (cfg Config) SMTPEnabled() bool {
	return cfg.SMTPAddr != ""
}

func validateDatabaseURL(input string) error {
	parsed, parseErr := url.Parse(input)
	if parseErr != nil {
		return errors.New("DATABASE_URL must be a valid URL")
	}

	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return errors.New("DATABASE_URL must use postgres or postgresql")
	}

	if parsed.Host == "" {
		return errors.New("DATABASE_URL host is required")
	}

	if strings.TrimSpace(parsed.Path) == "" || parsed.Path == "/" {
		return errors.New("DATABASE_URL database name is required")
	}

	return nil
}

func validateHTTPURL(name string, input string) error {
	parsed, parseErr := url.Parse(input)
	if parseErr != nil {
		return fmt.Errorf("%s must be a valid URL", name)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use http or https", name)
	}

	if parsed.Host == "" {
		return fmt.Errorf("%s host is required", name)
	}

	return nil
}

func validateHTTPAddr(input string) error {
	host, port, splitErr := net.SplitHostPort(input)
	if splitErr != nil {
		return errors.New("HTTP_ADDR must be host:port")
	}

	if strings.Contains(host, "/") {
		return errors.New("HTTP_ADDR host is invalid")
	}

	parsedPort, portErr := strconv.Atoi(port)
	if portErr != nil {
		return errors.New("HTTP_ADDR port must be an integer")
	}

	if parsedPort < 1 || parsedPort > 65535 {
		return errors.New("HTTP_ADDR port must be between 1 and 65535")
	}

	return nil
}

func validateSMTP(cfg Config) error {
	if cfg.SMTPAddr == "" && cfg.SMTPFrom == "" && cfg.SMTPUsername == "" && cfg.SMTPPassword == "" {
		return nil
	}

	if cfg.SMTPAddr == "" {
		return errors.New("SMTP_ADDR is required when SMTP is configured")
	}

	_, _, splitErr := net.SplitHostPort(cfg.SMTPAddr)
	if splitErr != nil {
		return errors.New("SMTP_ADDR must be host:port")
	}

	if cfg.SMTPFrom == "" {
		return errors.New("SMTP_FROM is required when SMTP is configured")
	}

	parsedFrom, fromErr := mail.ParseAddress(cfg.SMTPFrom)
	if fromErr != nil || parsedFrom.Address != cfg.SMTPFrom {
		return errors.New("SMTP_FROM must be an email address")
	}

	if cfg.SMTPUsername == "" && cfg.SMTPPassword != "" {
		return errors.New("SMTP_USERNAME is required when SMTP_PASSWORD is set")
	}

	return nil
}

func validLogLevel(input string) bool {
	switch input {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

package config

import (
	"strings"
	"testing"
)

func TestLoadAcceptsServerConfig(t *testing.T) {
	cfg, cfgErr := Load(validEnv(), ModeServer)
	if cfgErr != nil {
		t.Fatalf("load config: %v", cfgErr)
	}

	if cfg.AppMode != ModeServer {
		t.Fatalf("unexpected mode: %s", cfg.AppMode)
	}

	if cfg.WorkerConcurrency != 4 {
		t.Fatalf("unexpected worker concurrency: %d", cfg.WorkerConcurrency)
	}

	if cfg.NotificationBatchSize != 10 {
		t.Fatalf("unexpected notification batch size: %d", cfg.NotificationBatchSize)
	}
}

func TestLoadRequiresOnlyDatabaseForMigrate(t *testing.T) {
	cfg, cfgErr := Load(
		[]string{"DATABASE_URL=postgres://error_tracker:error_tracker@127.0.0.1:55432/postgres?sslmode=disable"},
		ModeMigrate,
	)
	if cfgErr != nil {
		t.Fatalf("migrate config should not require public url or secret key: %v", cfgErr)
	}

	if cfg.DatabaseURL == "" {
		t.Fatal("expected database url")
	}
}

func TestLoadAdminDoesNotRequireUnrelatedSessionSecret(t *testing.T) {
	cfg, cfgErr := Load(
		[]string{"DATABASE_URL=postgres://error_tracker:error_tracker@127.0.0.1:55432/postgres?sslmode=disable"},
		ModeAdmin,
	)
	if cfgErr != nil {
		t.Fatalf("admin config should not require session secret: %v", cfgErr)
	}

	if cfg.SecretKey != "" {
		t.Fatalf("unexpected secret key: %s", cfg.SecretKey)
	}
}

func TestLoadRejectsInvalidBoundaryValues(t *testing.T) {
	cases := []struct {
		name    string
		mode    Mode
		env     []string
		message string
	}{
		{
			name:    "invalid mode",
			mode:    Mode("bad"),
			env:     validEnv(),
			message: "APP_MODE is invalid",
		},
		{
			name:    "missing database",
			mode:    ModeServer,
			env:     without(validEnv(), "DATABASE_URL"),
			message: "DATABASE_URL is required",
		},
		{
			name:    "invalid database scheme",
			mode:    ModeServer,
			env:     replace(validEnv(), "DATABASE_URL=mysql://user:pass@example.test/app"),
			message: "DATABASE_URL must use postgres or postgresql",
		},
		{
			name:    "missing public url",
			mode:    ModeServer,
			env:     without(validEnv(), "PUBLIC_URL"),
			message: "PUBLIC_URL is required",
		},
		{
			name:    "invalid public url scheme",
			mode:    ModeServer,
			env:     replace(validEnv(), "PUBLIC_URL=ftp://example.test"),
			message: "PUBLIC_URL must use http or https",
		},
		{
			name:    "missing secret key",
			mode:    ModeServer,
			env:     without(validEnv(), "SECRET_KEY"),
			message: "SECRET_KEY is required",
		},
		{
			name:    "invalid http addr",
			mode:    ModeServer,
			env:     replace(validEnv(), "HTTP_ADDR=127.0.0.1"),
			message: "HTTP_ADDR must be host:port",
		},
		{
			name:    "invalid log level",
			mode:    ModeServer,
			env:     replace(validEnv(), "LOG_LEVEL=trace"),
			message: "LOG_LEVEL must be debug, info, warn, or error",
		},
		{
			name:    "invalid worker concurrency integer",
			mode:    ModeWorker,
			env:     replace(validEnv(), "WORKER_CONCURRENCY=fast"),
			message: "WORKER_CONCURRENCY must be an integer",
		},
		{
			name:    "nonpositive worker concurrency",
			mode:    ModeWorker,
			env:     replace(validEnv(), "WORKER_CONCURRENCY=0"),
			message: "WORKER_CONCURRENCY must be positive",
		},
		{
			name:    "invalid telegram api base url",
			mode:    ModeWorker,
			env:     replace(validEnv(), "TELEGRAM_API_BASE_URL=file:///tmp/socket"),
			message: "TELEGRAM_API_BASE_URL must use http or https",
		},
		{
			name:    "invalid notification batch size integer",
			mode:    ModeWorker,
			env:     replace(validEnv(), "NOTIFICATION_BATCH_SIZE=many"),
			message: "NOTIFICATION_BATCH_SIZE must be an integer",
		},
		{
			name:    "nonpositive notification batch size",
			mode:    ModeWorker,
			env:     replace(validEnv(), "NOTIFICATION_BATCH_SIZE=0"),
			message: "NOTIFICATION_BATCH_SIZE must be positive",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, cfgErr := Load(tc.env, tc.mode)
			if cfgErr == nil {
				t.Fatal("expected config load to fail")
			}

			if !strings.Contains(cfgErr.Error(), tc.message) {
				t.Fatalf("expected %q, got %q", tc.message, cfgErr.Error())
			}
		})
	}
}

func validEnv() []string {
	return []string{
		"PUBLIC_URL=http://example.test",
		"DATABASE_URL=postgres://error_tracker:error_tracker@127.0.0.1:55432/postgres?sslmode=disable",
		"SECRET_KEY=test-secret",
		"LOG_LEVEL=info",
		"HTTP_ADDR=127.0.0.1:8080",
		"WORKER_CONCURRENCY=4",
		"MIGRATIONS_DIR=migrations",
		"TELEGRAM_API_BASE_URL=https://api.telegram.org",
		"NOTIFICATION_BATCH_SIZE=10",
	}
}

func replace(env []string, pair string) []string {
	key := strings.SplitN(pair, "=", 2)[0]
	result := make([]string, 0, len(env)+1)
	replaced := false

	for _, existing := range env {
		if strings.HasPrefix(existing, key+"=") {
			result = append(result, pair)
			replaced = true
			continue
		}

		result = append(result, existing)
	}

	if !replaced {
		result = append(result, pair)
	}

	return result
}

func without(env []string, key string) []string {
	result := make([]string, 0, len(env))

	for _, pair := range env {
		if strings.HasPrefix(pair, key+"=") {
			continue
		}

		result = append(result, pair)
	}

	return result
}

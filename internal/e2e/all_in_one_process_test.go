//go:build integration

package e2e

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAllInOneProcessSmoke(t *testing.T) {
	ctx := context.Background()
	adminURL := os.Getenv("ERROR_TRACKER_E2E_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("ERROR_TRACKER_E2E_POSTGRES_URL is required")
	}

	databaseURL := createTestDatabase(t, ctx, adminURL)
	addr := freeHTTPAddr(t)
	baseURL := "http://" + addr
	repoRoot := filepath.Clean("../..")

	runCommand(t, repoRoot, []string{
		"DATABASE_URL=" + databaseURL,
	}, "go", "run", "./cmd/error-tracker", "migrate", "up")

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	cmd := exec.Command("go", "run", "./cmd/error-tracker", "all-in-one")
	cmd.Dir = repoRoot
	cmd.Env = append(
		os.Environ(),
		"DATABASE_URL="+databaseURL,
		"PUBLIC_URL="+baseURL,
		"SECRET_KEY=all-in-one-smoke-secret",
		"HTTP_ADDR="+addr,
	)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	startErr := cmd.Start()
	if startErr != nil {
		t.Fatalf("start all-in-one: %v", startErr)
	}
	t.Cleanup(func() {
		terminateProcessGroup(cmd)
	})

	waitForReady(t, baseURL, &stdout, &stderr)

	client := newE2EClient(t)
	beforeSetup := request(t, client, http.MethodGet, baseURL+"/issues", "", nil)
	if beforeSetup.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected setup redirect, got %d", beforeSetup.StatusCode)
	}

	setup := request(
		t,
		client,
		http.MethodPost,
		baseURL+"/setup",
		"application/x-www-form-urlencoded",
		strings.NewReader("organization_name=Smoke&project_name=Smoke&email=operator%40example.test&password=correct-horse-battery-staple"),
	)
	if setup.StatusCode != http.StatusOK {
		t.Fatalf("expected setup ok, got %d: %s", setup.StatusCode, setup.Body)
	}
	if !strings.Contains(setup.Body, "Project is ready") {
		t.Fatalf("expected setup completion: %s", setup.Body)
	}

	publicKey := projectPublicKey(t, ctx, databaseURL)
	storeReceipt := postStoreEvent(t, client, baseURL, publicKey)
	if storeReceipt.StatusCode != http.StatusOK {
		t.Fatalf("expected store ok, got %d: %s", storeReceipt.StatusCode, storeReceipt.Body)
	}

	issues := request(t, client, http.MethodGet, baseURL+"/issues", "", nil)
	if issues.StatusCode != http.StatusOK {
		t.Fatalf("expected issues ok, got %d", issues.StatusCode)
	}
	if !strings.Contains(issues.Body, "dimension persistence visible issue") {
		t.Fatalf("expected ingested issue in all-in-one UI: %s", issues.Body)
	}
}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}

	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		return
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
	}
}

func freeHTTPAddr(t *testing.T) string {
	t.Helper()

	listener, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatalf("listen: %v", listenErr)
	}
	defer listener.Close()

	return listener.Addr().String()
}

func runCommand(
	t *testing.T,
	dir string,
	env []string,
	name string,
	args ...string,
) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, runErr, string(output))
	}
}

func waitForReady(
	t *testing.T,
	baseURL string,
	stdout *bytes.Buffer,
	stderr *bytes.Buffer,
) {
	t.Helper()

	deadline := time.Now().Add(20 * time.Second)
	client := http.Client{Timeout: 500 * time.Millisecond}

	for time.Now().Before(deadline) {
		res, resErr := client.Get(baseURL + "/health/ready")
		if resErr == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return
			}
		}

		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf(
		"all-in-one did not become ready at %s\nstdout:\n%s\nstderr:\n%s",
		baseURL,
		stdout.String(),
		stderr.String(),
	)
}

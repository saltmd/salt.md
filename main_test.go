package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"salt/server"
)

// Run main in a child process so its exit status and startup side effects are
// observable without binding the test runner to the application's lifecycle.
func TestCLIProcess(t *testing.T) {
	if os.Getenv("SALT_TEST_CLI") != "1" {
		return
	}
	os.Args = []string{"salt", os.Args[len(os.Args)-1]}
	main()
	os.Exit(0)
}

func runCLI(t *testing.T, arg string) (string, string, error) {
	t.Helper()
	data := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCLIProcess$", "--", arg)
	cmd.Env = []string{
		"SALT_TEST_CLI=1", "SALT_DATA=" + data,
		"SALT_ADDR=127.0.0.1:0", "SALT_UPDATE_CHECK=0",
		"HOME=" + data, "TMPDIR=" + data,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%q did not exit; it may have started the server: %s", arg, stderr.String())
	}
	entries, readErr := os.ReadDir(data)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("%q modified the data directory: %v", arg, entries)
	}
	return stdout.String(), stderr.String(), err
}

func TestUnknownCommandDoesNotStartServer(t *testing.T) {
	for _, arg := range []string{"versoin", "--version", "--help"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, err := runCLI(t, arg)
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 {
				t.Fatalf("want exit status 2, got %v; stderr: %s", err, stderr)
			}
			if stdout != "" {
				t.Fatalf("unexpected stdout: %q", stdout)
			}
			if !strings.Contains(stderr, "unknown command") || !strings.Contains(stderr, arg) || !strings.Contains(stderr, "usage: salt") {
				t.Fatalf("missing command error or usage: %q", stderr)
			}
		})
	}
}

func TestVersionCommandStillWorks(t *testing.T) {
	stdout, stderr, err := runCLI(t, "version")
	if err != nil || stderr != "" || strings.TrimSpace(stdout) != server.Version {
		t.Fatalf("version: stdout=%q stderr=%q error=%v", stdout, stderr, err)
	}
}

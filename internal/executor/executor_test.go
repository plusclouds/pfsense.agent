package executor

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestExecuteWithStdin_RoundTrips(t *testing.T) {
	core, _ := observer.New(zap.DebugLevel)
	e := New(zap.New(core))

	stdout, stderr, err := e.ExecuteWithStdin(context.Background(), []byte("secret-value"), "cat")
	if err != nil {
		t.Fatalf("ExecuteWithStdin failed: %v (stderr=%q)", err, stderr)
	}
	if stdout != "secret-value" {
		t.Fatalf("expected stdout %q, got %q", "secret-value", stdout)
	}
}

func TestExecuteWithStdin_DoesNotLogStdin(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	e := New(zap.New(core))

	_, _, err := e.ExecuteWithStdin(context.Background(), []byte("super-secret-password"), "cat")
	if err != nil {
		t.Fatalf("ExecuteWithStdin failed: %v", err)
	}

	for _, entry := range logs.All() {
		if strings.Contains(entry.Message, "super-secret-password") {
			t.Fatalf("log message leaked stdin content: %q", entry.Message)
		}
		for _, f := range entry.Context {
			if strings.Contains(f.String, "super-secret-password") {
				t.Fatalf("log field %q leaked stdin content", f.Key)
			}
		}
	}
}

func TestExecute_NoStdin(t *testing.T) {
	core, _ := observer.New(zap.DebugLevel)
	e := New(zap.New(core))

	stdout, _, err := e.Execute(context.Background(), "echo", "hello")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if strings.TrimSpace(stdout) != "hello" {
		t.Fatalf("expected stdout %q, got %q", "hello", stdout)
	}
}

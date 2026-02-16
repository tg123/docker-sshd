package crisshd

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/tg123/docker-sshd/pkg/bridge"
)

func TestExecBuildsCrictlCommand(t *testing.T) {
	t.Cleanup(func() {
		commandContext = exec.CommandContext
	})

	var gotName string
	var gotArgs []string
	commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "sh", "-c", "cat >/dev/null")
	}

	p, err := New("container-id", "/run/containerd/containerd.sock", "")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	var out bytes.Buffer
	r, err := p.Exec(context.Background(), bridge.ExecConfig{
		Input:  strings.NewReader(""),
		Output: &out,
		Tty:    true,
		Cmd:    []string{"/bin/sh", "-c", "echo ok"},
	})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}

	result := <-r
	if result.Error != nil {
		t.Fatalf("expected nil error, got %v", result.Error)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}

	if gotName != "crictl" {
		t.Fatalf("expected command crictl, got %q", gotName)
	}

	wantArgs := []string{
		"--runtime-endpoint", "/run/containerd/containerd.sock",
		"exec", "-i", "-t", "container-id", "/bin/sh", "-c", "echo ok",
	}

	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("unexpected arg length, got %d want %d: %#v", len(gotArgs), len(wantArgs), gotArgs)
	}

	for i := range wantArgs {
		if gotArgs[i] != wantArgs[i] {
			t.Fatalf("unexpected arg at %d, got %q want %q", i, gotArgs[i], wantArgs[i])
		}
	}
}

func TestExecReturnsExitCodeFromCrictl(t *testing.T) {
	t.Cleanup(func() {
		commandContext = exec.CommandContext
	})

	commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "exit 17")
	}

	p, err := New("container-id", "", "")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	var out bytes.Buffer
	r, err := p.Exec(context.Background(), bridge.ExecConfig{
		Input:  strings.NewReader(""),
		Output: &out,
		Tty:    false,
		Cmd:    []string{"true"},
	})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}

	result := <-r
	if result.Error == nil {
		t.Fatal("expected non-nil error")
	}
	if result.ExitCode != 17 {
		t.Fatalf("expected exit code 17, got %d", result.ExitCode)
	}
}

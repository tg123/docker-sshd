package e2e

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestDockerSSHD(t *testing.T) {
	if os.Getenv("DOCKER_SSHD_E2E") != "1" {
		t.Skip("set DOCKER_SSHD_E2E=1 to run")
	}

	bin := os.Getenv("DOCKER_SSHD_BIN")
	if bin == "" {
		t.Fatal("DOCKER_SSHD_BIN is required")
	}

	container := os.Getenv("DOCKER_E2E_CONTAINER")
	if container == "" {
		container = "docker-sshd-e2e-test"
	}

	key := t.TempDir() + "/docker-sshd-e2e-key"
	mustRun(t, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", key)

	cmd := exec.Command(bin, "--address", "127.0.0.1", "--port", "2232", "--server-key", key)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start docker-sshd: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	out := retrySSH(t, "2232", container+"@127.0.0.1")
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("expected ok, got %q", out)
	}
}

func TestKubeSSHD(t *testing.T) {
	if os.Getenv("KUBE_SSHD_E2E") != "1" {
		t.Skip("set KUBE_SSHD_E2E=1 to run")
	}

	bin := os.Getenv("KUBE_SSHD_BIN")
	if bin == "" {
		t.Fatal("KUBE_SSHD_BIN is required")
	}

	pod := "kube-sshd-e2e-" + strings.ToLower(time.Now().Format("150405.000000000"))
	pod = strings.ReplaceAll(pod, ".", "-")
	mustRun(t, "kubectl", "run", pod, "--image=busybox:1.36", "--restart=Never", "--command", "--", "sleep", "300")
	defer run(t, "kubectl", "delete", "pod", pod, "--ignore-not-found=true", "--wait=false")
	mustRun(t, "kubectl", "wait", "--for=condition=Ready", "pod/"+pod, "--timeout=120s")

	key := t.TempDir() + "/kube-sshd-e2e-key"
	mustRun(t, "ssh-keygen", "-t", "ed25519", "-N", "", "-f", key)

	cmd := exec.Command(bin, "--address", "127.0.0.1", "--port", "2233", "--server-key", key, "--namespace", "default")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start kube-sshd: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	out := retrySSH(t, "2233", pod+"@127.0.0.1")
	if strings.TrimSpace(out) != "ok" {
		t.Fatalf("expected ok, got %q", out)
	}
}

func retrySSH(t *testing.T, port, userHost string) string {
	t.Helper()

	var out string
	var err error
	for range 30 {
		out, err = run(t,
			"ssh",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "PreferredAuthentications=none",
			"-p", port,
			userHost,
			"echo", "ok",
		)
		if err == nil {
			return out
		}
		time.Sleep(time.Second)
	}

	t.Fatalf("ssh did not succeed: %v", err)
	return ""
}

func mustRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := run(t, name, args...)
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return out
}

func run(t *testing.T, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

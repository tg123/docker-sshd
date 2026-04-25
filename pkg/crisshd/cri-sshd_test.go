package crisshd

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/tg123/docker-sshd/pkg/bridge"
	"google.golang.org/grpc"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	k8sexec "k8s.io/client-go/util/exec"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type fakeRuntimeClient struct {
	resp *runtimeapi.ExecResponse
	err  error
	req  *runtimeapi.ExecRequest
}

func (f *fakeRuntimeClient) Exec(_ context.Context, in *runtimeapi.ExecRequest, _ ...grpc.CallOption) (*runtimeapi.ExecResponse, error) {
	f.req = in
	return f.resp, f.err
}

type fakeCloser struct {
	closed bool
}

func (f *fakeCloser) Close() error {
	f.closed = true
	return nil
}

type fakeExecutor struct {
	streamErr error
	opts      remotecommand.StreamOptions
}

func (f *fakeExecutor) Stream(remotecommand.StreamOptions) error {
	return nil
}

func (f *fakeExecutor) StreamWithContext(_ context.Context, opts remotecommand.StreamOptions) error {
	f.opts = opts
	return f.streamErr
}

func TestExecCallsCRIAPI(t *testing.T) {
	t.Cleanup(func() {
		dialRuntimeClient = dialRuntimeClientDefault
		newWebSocketExecutor = remotecommand.NewWebSocketExecutor
	})

	var gotEndpoint string
	client := &fakeRuntimeClient{
		resp: &runtimeapi.ExecResponse{Url: "ws://127.0.0.1/exec"},
	}
	closer := &fakeCloser{}
	dialRuntimeClient = func(_ context.Context, endpoint string) (runtimeExecClient, io.Closer, error) {
		gotEndpoint = endpoint
		return client, closer, nil
	}

	executor := &fakeExecutor{}
	var gotMethod, gotURL string
	newWebSocketExecutor = func(_ *restclient.Config, method, url string) (remotecommand.Executor, error) {
		gotMethod = method
		gotURL = url
		return executor, nil
	}

	p, err := New("container-id", "/run/containerd/containerd.sock", "")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	r, err := p.Exec(context.Background(), bridge.ExecConfig{
		Input:  strings.NewReader(""),
		Output: io.Discard,
		Tty:    true,
		Cmd:    []string{"/bin/sh", "-c", "echo ok"},
	})
	if err != nil {
		t.Fatalf("Exec returned error: %v", err)
	}

	result := <-r
	if result.Error != nil || result.ExitCode != 0 {
		t.Fatalf("unexpected exec result: %#v", result)
	}

	if gotEndpoint != "/run/containerd/containerd.sock" {
		t.Fatalf("unexpected endpoint: %q", gotEndpoint)
	}

	if client.req == nil {
		t.Fatal("expected exec request to be sent")
	}
	if client.req.ContainerId != "container-id" {
		t.Fatalf("unexpected container id: %q", client.req.ContainerId)
	}
	if len(client.req.Cmd) != 3 || client.req.Cmd[0] != "/bin/sh" {
		t.Fatalf("unexpected cmd: %#v", client.req.Cmd)
	}
	if !client.req.Stdin || !client.req.Stdout || !client.req.Stderr || !client.req.Tty {
		t.Fatalf("unexpected exec flags: %#v", client.req)
	}
	if gotMethod != "GET" || gotURL != "ws://127.0.0.1/exec" {
		t.Fatalf("unexpected executor setup: method=%q url=%q", gotMethod, gotURL)
	}
	if executor.opts.TerminalSizeQueue == nil {
		t.Fatal("expected terminal size queue")
	}
	if !closer.closed {
		t.Fatal("expected runtime connection to be closed")
	}
}

func TestExecReturnsExitCodeFromStreamError(t *testing.T) {
	t.Cleanup(func() {
		dialRuntimeClient = dialRuntimeClientDefault
		newWebSocketExecutor = remotecommand.NewWebSocketExecutor
	})

	dialRuntimeClient = func(_ context.Context, endpoint string) (runtimeExecClient, io.Closer, error) {
		return &fakeRuntimeClient{
			resp: &runtimeapi.ExecResponse{Url: "ws://127.0.0.1/exec"},
		}, &fakeCloser{}, nil
	}
	newWebSocketExecutor = func(_ *restclient.Config, method, url string) (remotecommand.Executor, error) {
		return &fakeExecutor{streamErr: k8sexec.CodeExitError{Code: 17}}, nil
	}
	p, err := New("container-id", "", "")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	r, err := p.Exec(context.Background(), bridge.ExecConfig{
		Input:  strings.NewReader(""),
		Output: io.Discard,
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

func TestNormalizeEndpoint(t *testing.T) {
	if got := normalizeEndpoint(""); got != defaultRuntimeEndpoint {
		t.Fatalf("unexpected default endpoint: %q", got)
	}
	if got := normalizeEndpoint("/run/containerd/containerd.sock"); got != "unix:///run/containerd/containerd.sock" {
		t.Fatalf("unexpected unix endpoint: %q", got)
	}
	if got := normalizeEndpoint("tcp://127.0.0.1:10010"); got != "tcp://127.0.0.1:10010" {
		t.Fatalf("unexpected tcp endpoint: %q", got)
	}
}

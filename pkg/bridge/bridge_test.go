package bridge

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

type fakeProvider struct {
	mu          sync.Mutex
	resizeCalls []ResizeOptions
	execCalls   []ExecConfig
	execResults chan ExecResult
	execErr     error
	resizeErr   error
}

func (f *fakeProvider) Resize(ctx context.Context, size ResizeOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resizeCalls = append(f.resizeCalls, size)
	return f.resizeErr
}

func (f *fakeProvider) Exec(ctx context.Context, cfg ExecConfig) (<-chan ExecResult, error) {
	f.mu.Lock()
	f.execCalls = append(f.execCalls, cfg)
	resultChan := f.execResults
	execErr := f.execErr
	f.mu.Unlock()

	if execErr != nil {
		return nil, execErr
	}

	if resultChan == nil {
		resultChan = make(chan ExecResult, 1)
	}

	return resultChan, nil
}

type fakeChannel struct {
	closedCh  chan struct{}
	closeOnce sync.Once
	stderr    fakeBuffer

	mu       sync.Mutex
	requests []string
}

type fakeBuffer struct{}

func (fakeBuffer) Read(p []byte) (int, error)  { return 0, io.EOF }
func (fakeBuffer) Write(p []byte) (int, error) { return len(p), nil }

func newFakeChannel() *fakeChannel {
	return &fakeChannel{closedCh: make(chan struct{})}
}

func (c *fakeChannel) Read(p []byte) (int, error)  { return 0, io.EOF }
func (c *fakeChannel) Write(p []byte) (int, error) { return len(p), nil }

func (c *fakeChannel) Close() error {
	c.closeOnce.Do(func() { close(c.closedCh) })
	return nil
}

func (c *fakeChannel) CloseWrite() error { return nil }

func (c *fakeChannel) SendRequest(name string, wantReply bool, payload []byte) (bool, error) {
	c.mu.Lock()
	c.requests = append(c.requests, name)
	c.mu.Unlock()
	return true, nil
}

func (c *fakeChannel) Stderr() io.ReadWriter { return c.stderr }

func TestSessionHandleEnv(t *testing.T) {
	s := &session{}
	payload := ssh.Marshal(struct {
		Name     string
		Variable string
	}{
		Name:     "TERM",
		Variable: "xterm-256color",
	})

	if err := s.handleEnv(payload); err != nil {
		t.Fatalf("handleEnv returned error: %v", err)
	}

	if len(s.env) != 1 || s.env[0] != "TERM=xterm-256color" {
		t.Fatalf("expected env to include TERM=xterm-256color, got %#v", s.env)
	}
}

func TestSessionResizeCallsProvider(t *testing.T) {
	provider := &fakeProvider{}
	s := &session{bridge: &Bridge{provider: provider}}

	if err := s.resize(80, 24); err != nil {
		t.Fatalf("resize returned error: %v", err)
	}

	provider.mu.Lock()
	defer provider.mu.Unlock()

	if len(provider.resizeCalls) != 1 {
		t.Fatalf("expected 1 resize call, got %d", len(provider.resizeCalls))
	}

	call := provider.resizeCalls[0]
	if call.Width != 80 || call.Height != 24 {
		t.Fatalf("unexpected resize options: %#v", call)
	}
}

func TestSessionExecUsesProviderConfig(t *testing.T) {
	provider := &fakeProvider{execResults: make(chan ExecResult, 1)}
	channel := newFakeChannel()
	s := &session{
		bridge:       &Bridge{provider: provider},
		channel:      channel,
		ptyRequested: true,
		env:          []string{"FOO=BAR"},
	}

	if err := s.exec("echo hello"); err != nil {
		t.Fatalf("exec returned error: %v", err)
	}

	provider.execResults <- ExecResult{ExitCode: 0}

	select {
	case <-channel.closedCh:
	case <-time.After(time.Second):
		t.Fatal("expected channel to close after exec result")
	}

	provider.mu.Lock()
	if len(provider.execCalls) != 1 {
		provider.mu.Unlock()
		t.Fatalf("expected 1 exec call, got %d", len(provider.execCalls))
	}
	call := provider.execCalls[0]
	provider.mu.Unlock()

	if len(call.Cmd) != 2 || call.Cmd[0] != "echo" || call.Cmd[1] != "hello" {
		t.Fatalf("unexpected exec cmd: %#v", call.Cmd)
	}

	if !call.Tty {
		t.Fatalf("expected TTY to be true")
	}

	if len(call.Env) != 1 || call.Env[0] != "FOO=BAR" {
		t.Fatalf("unexpected env: %#v", call.Env)
	}

	channel.mu.Lock()
	requests := append([]string(nil), channel.requests...)
	channel.mu.Unlock()

	if len(requests) != 1 || requests[0] != "exit-status" {
		t.Fatalf("expected exit-status request, got %#v", requests)
	}

	if err := s.exec("echo again"); err == nil {
		t.Fatal("expected error when exec called twice")
	}
}

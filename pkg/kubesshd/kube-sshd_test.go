package kubesshd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tg123/docker-sshd/pkg/bridge"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

func TestNew(t *testing.T) {
	cfg := &rest.Config{}
	provider, err := New(cfg, "ns", "pod", "container")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	k, ok := provider.(*kubesshdconn)
	if !ok {
		t.Fatalf("provider has unexpected type %T", provider)
	}

	if k.namespace != "ns" || k.pod != "pod" || k.container != "container" {
		t.Fatalf("New() created unexpected connection state: %+v", k)
	}

	if k.config != cfg {
		t.Fatalf("New() created unexpected config: %v", k.config)
	}

	if k.resizeQueue == nil {
		t.Fatalf("New() created unexpected resizeQueue")
	}
}

func TestClose(t *testing.T) {
	called := false
	k := &kubesshdconn{
		cancel: func() {
			called = true
		},
	}

	if err := k.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if !called {
		t.Fatalf("Close() did not call cancel")
	}
}

func TestCloseWithoutCancel(t *testing.T) {
	k := &kubesshdconn{}

	if err := k.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNext(t *testing.T) {
	k := &kubesshdconn{
		resizeQueue: make(chan *remotecommand.TerminalSize, 1),
	}

	want := &remotecommand.TerminalSize{
		Height: 24,
		Width:  80,
	}

	k.resizeQueue <- want

	got := k.Next()
	if got == nil || got.Height != want.Height || got.Width != want.Width {
		t.Fatalf("Next() = %#v, want %#v", got, want)
	}

	close(k.resizeQueue)
	if k.Next() != nil {
		t.Fatalf("Next() should return nil on closed queue")
	}
}

func TestResize(t *testing.T) {
	k := &kubesshdconn{
		resizeQueue: make(chan *remotecommand.TerminalSize, 1),
	}

	if err := k.Resize(context.Background(), bridge.ResizeOptions{Height: 40, Width: 120}); err != nil {
		t.Fatalf("Resize() error = %v", err)
	}

	select {
	case got := <-k.resizeQueue:
		if got.Height != 40 || got.Width != 120 {
			t.Fatalf("Resize() enqueued wrong size: %#v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("Resize() did not enqueue terminal size")
	}
}

func TestResizeQueueFull(t *testing.T) {
	k := &kubesshdconn{
		resizeQueue: make(chan *remotecommand.TerminalSize, 1),
	}

	k.resizeQueue <- &remotecommand.TerminalSize{Height: 1, Width: 1}

	err := k.Resize(context.Background(), bridge.ResizeOptions{Height: 2, Width: 2})
	if err == nil {
		t.Fatalf("Resize() expected error when queue is full")
	}

	if !strings.Contains(err.Error(), "resize failed") {
		t.Fatalf("Resize() error = %v, want resize failed", err)
	}
}

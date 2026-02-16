package dockersshd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/docker/docker/client"
	"github.com/tg123/docker-sshd/pkg/bridge"
)

func newTestDockerClient(t *testing.T, srv *httptest.Server) *client.Client {
	t.Helper()

	cli, err := client.NewClientWithOpts(
		client.WithHost(srv.URL),
		client.WithVersion("1.41"),
		client.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}

	return cli
}

func TestResizeBeforeExecStoresInitSize(t *testing.T) {
	conn := &dockersshdconn{}

	size := bridge.ResizeOptions{
		Height: 30,
		Width:  100,
	}

	if err := conn.Resize(context.Background(), size); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}

	if conn.initSize != size {
		t.Fatalf("unexpected init size: got %+v want %+v", conn.initSize, size)
	}
}

func TestResizeAfterExecCallsDockerAPI(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1.41/exec/exec-1/resize" {
			called = true
			if got, want := r.URL.Query().Get("h"), "24"; got != want {
				t.Fatalf("unexpected height query: got %q want %q", got, want)
			}
			if got, want := r.URL.Query().Get("w"), "80"; got != want {
				t.Fatalf("unexpected width query: got %q want %q", got, want)
			}
			w.WriteHeader(http.StatusOK)
			return
		}

		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
	}))
	defer srv.Close()

	conn := &dockersshdconn{
		dockercli: newTestDockerClient(t, srv),
		execId:    "exec-1",
	}

	if err := conn.Resize(context.Background(), bridge.ResizeOptions{Height: 24, Width: 80}); err != nil {
		t.Fatalf("Resize returned error: %v", err)
	}

	if !called {
		t.Fatal("expected resize API call")
	}
}

func TestExecCreateErrorIncludesDockerMessage(t *testing.T) {
	var req struct {
		AttachStderr bool     `json:"AttachStderr"`
		Tty          bool     `json:"Tty"`
		Env          []string `json:"Env"`
		Cmd          []string `json:"Cmd"`
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v1.41/containers/c1/exec" {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("failed to decode request: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"create failed"}`))
			return
		}

		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
	}))
	defer srv.Close()

	conn := &dockersshdconn{
		containerName: "c1",
		dockercli:     newTestDockerClient(t, srv),
		initSize:      bridge.ResizeOptions{Height: 20, Width: 40},
	}

	_, err := conn.Exec(context.Background(), bridge.ExecConfig{
		Tty: false,
		Env: []string{"A=B"},
		Cmd: []string{"/bin/sh"},
	})
	if err == nil {
		t.Fatal("expected error from exec create")
	}
	if !strings.Contains(err.Error(), "create failed") {
		t.Fatalf("unexpected error: %v", err)
	}

	if got, want := req.AttachStderr, false; got != want {
		t.Fatalf("unexpected AttachStderr: got %v want %v", got, want)
	}
	if got, want := req.Tty, false; got != want {
		t.Fatalf("unexpected Tty: got %v want %v", got, want)
	}
	if len(req.Cmd) != 1 || req.Cmd[0] != "/bin/sh" {
		t.Fatalf("unexpected Cmd: got %#v", req.Cmd)
	}
	if len(req.Env) != 1 || req.Env[0] != "A=B" {
		t.Fatalf("unexpected Env: got %#v", req.Env)
	}
}

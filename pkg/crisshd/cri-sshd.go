package crisshd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/tg123/docker-sshd/pkg/bridge"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	k8sexec "k8s.io/client-go/util/exec"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultRuntimeEndpoint = "unix:///run/containerd/containerd.sock"

type runtimeExecClient interface {
	Exec(ctx context.Context, in *runtimeapi.ExecRequest, opts ...grpc.CallOption) (*runtimeapi.ExecResponse, error)
}

var dialRuntimeClientDefault = func(ctx context.Context, endpoint string) (runtimeExecClient, io.Closer, error) {
	endpoint = normalizeEndpoint(endpoint)

	conn, err := grpc.DialContext(ctx, endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, err
	}

	return runtimeapi.NewRuntimeServiceClient(conn), conn, nil
}

var dialRuntimeClient = dialRuntimeClientDefault

var newWebSocketExecutor = remotecommand.NewWebSocketExecutor

var _ bridge.SessionProvider = (*crisshdconn)(nil)

type crisshdconn struct {
	containerName   string
	runtimeEndpoint string
	imageEndpoint   string
	resizeQueue     chan *remotecommand.TerminalSize
}

func (c *crisshdconn) Exec(ctx context.Context, execconfig bridge.ExecConfig) (<-chan bridge.ExecResult, error) {
	criClient, closer, err := dialRuntimeClient(ctx, c.runtimeEndpoint)
	if err != nil {
		return nil, err
	}

	resp, err := criClient.Exec(ctx, &runtimeapi.ExecRequest{
		ContainerId: c.containerName,
		Cmd:         execconfig.Cmd,
		Tty:         execconfig.Tty,
		Stdin:       true,
		Stdout:      true,
		Stderr:      true,
	})
	if err != nil {
		_ = closer.Close()
		return nil, err
	}

	if resp.GetUrl() == "" {
		_ = closer.Close()
		return nil, fmt.Errorf("empty exec stream url from cri runtime")
	}

	executor, err := newWebSocketExecutor(&restclient.Config{}, "GET", resp.GetUrl())
	if err != nil {
		_ = closer.Close()
		return nil, err
	}

	r := make(chan bridge.ExecResult, 1)

	go func() {
		defer closer.Close()

		err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             execconfig.Input,
			Stdout:            execconfig.Output,
			Stderr:            execconfig.Output,
			Tty:               execconfig.Tty,
			TerminalSizeQueue: c,
		})

		exitCode := 0
		if exitErr, ok := err.(k8sexec.CodeExitError); ok {
			exitCode = exitErr.ExitStatus()
		} else if err != nil {
			exitCode = -1
		}

		r <- bridge.ExecResult{
			ExitCode: exitCode,
			Error:    err,
		}
	}()

	return r, nil
}

func (c *crisshdconn) Next() *remotecommand.TerminalSize {
	size, ok := <-c.resizeQueue
	if !ok {
		return nil
	}

	return size
}

func (c *crisshdconn) Resize(_ context.Context, size bridge.ResizeOptions) error {
	select {
	case c.resizeQueue <- &remotecommand.TerminalSize{
		Height: uint16(size.Height),
		Width:  uint16(size.Width),
	}:
		return nil
	default:
	}

	return fmt.Errorf("resize failed")
}

func New(containerName, runtimeEndpoint, imageEndpoint string) (bridge.SessionProvider, error) {
	return &crisshdconn{
		containerName:   containerName,
		runtimeEndpoint: runtimeEndpoint,
		imageEndpoint:   imageEndpoint,
		resizeQueue:     make(chan *remotecommand.TerminalSize, 1),
	}, nil
}

func normalizeEndpoint(endpoint string) string {
	if endpoint == "" {
		return defaultRuntimeEndpoint
	}

	if strings.Contains(endpoint, "://") {
		return endpoint
	}

	return "unix://" + endpoint
}

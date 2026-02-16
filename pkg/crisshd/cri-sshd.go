package crisshd

import (
	"context"
	"os/exec"

	"github.com/tg123/docker-sshd/pkg/bridge"
)

var commandContext = exec.CommandContext

var _ bridge.SessionProvider = (*crisshdconn)(nil)

type crisshdconn struct {
	containerName   string
	runtimeEndpoint string
	imageEndpoint   string
}

func (c *crisshdconn) Exec(ctx context.Context, execconfig bridge.ExecConfig) (<-chan bridge.ExecResult, error) {
	args := []string{}

	if c.runtimeEndpoint != "" {
		args = append(args, "--runtime-endpoint", c.runtimeEndpoint)
	}

	if c.imageEndpoint != "" {
		args = append(args, "--image-endpoint", c.imageEndpoint)
	}

	args = append(args, "exec", "-i")
	if execconfig.Tty {
		args = append(args, "-t")
	}

	args = append(args, c.containerName)
	args = append(args, execconfig.Cmd...)

	cmd := commandContext(ctx, "crictl", args...)
	cmd.Stdin = execconfig.Input
	cmd.Stdout = execconfig.Output
	cmd.Stderr = execconfig.Output

	r := make(chan bridge.ExecResult, 1)

	go func() {
		err := cmd.Run()
		exitCode := 0
		if err != nil {
			exitCode = -1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}

		r <- bridge.ExecResult{
			ExitCode: exitCode,
			Error:    err,
		}
	}()

	return r, nil
}

func (c *crisshdconn) Resize(context.Context, bridge.ResizeOptions) error {
	return nil
}

func New(containerName, runtimeEndpoint, imageEndpoint string) (bridge.SessionProvider, error) {
	return &crisshdconn{
		containerName:   containerName,
		runtimeEndpoint: runtimeEndpoint,
		imageEndpoint:   imageEndpoint,
	}, nil
}

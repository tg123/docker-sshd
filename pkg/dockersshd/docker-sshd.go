package dockersshd

import (
	"context"
	"io"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
	"github.com/tg123/docker-sshd/pkg/bridge"
)

var _ bridge.SessionProvider = (*dockersshdconn)(nil)

const execTimeout = 10 * time.Second

type dockersshdconn struct {
	containerName string
	dockercli     *client.Client
	execId        string
	initSize      bridge.ResizeOptions
}

func (d *dockersshdconn) Close() error {
	return nil
}

func (d *dockersshdconn) Exec(ctx context.Context, execconfig bridge.ExecConfig) (<-chan bridge.ExecResult, error) {
	exec, err := d.dockercli.ContainerExecCreate(ctx, d.containerName, container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: execconfig.Tty, // only attach stderr if tty is enabled
		Tty:          execconfig.Tty,
		Env:          execconfig.Env,
		Cmd:          execconfig.Cmd,
		ConsoleSize:  &[2]uint{d.initSize.Height, d.initSize.Width},
	})

	if err != nil {
		return nil, err
	}

	execID := exec.ID
	d.execId = exec.ID

	attach, err := d.dockercli.ContainerExecAttach(ctx, execID, container.ExecAttachOptions{
		Detach: false,
		Tty:    true,
	})

	if err != nil {
		return nil, err
	}

	log.Debugf("docker exec [%v] in container [%v] started", execconfig.Cmd, d.containerName)

	r := make(chan bridge.ExecResult)

	go func() {
		defer attach.Close()

		done := make(chan error, 2)

		go func() {
			_, _ = io.Copy(attach.Conn, execconfig.Input)
			// stdin is closed by client but it still need to wait for stdout close
		}()

		go func() {
			_, err := io.Copy(execconfig.Output, attach.Reader)
			done <- err
		}()

		var err error
		select {
		case err = <-done:
		case <-ctx.Done():
			log.Warningf("exec [%v] in container [%v] context cancelled", execconfig.Cmd, d.containerName)
			return
		}

		log.Debugf("docker exec [%v] in container [%v] done error [%v]", execconfig.Cmd, d.containerName, err)

		exitCode := -1
		st := time.Now()

		for {
			if time.Since(st) > execTimeout {
				log.Warningf("exec [%v] is still running or inspect error after %v timeout", execID, execTimeout)
				break
			}

			exec, err := d.dockercli.ContainerExecInspect(context.Background(), execID)
			if err != nil {
				log.Warningf("inspect exec %v failed %v", execID, err)
				time.Sleep(1 * time.Second)
				continue
			}

			if exec.Running { // this should not happen
				log.Warnf("exec %v is still running in container %v", execID, exec.ContainerID)
				time.Sleep(1 * time.Second)
				continue
			}

			exitCode = exec.ExitCode
			break
		}

		if exitCode == 0 {
			err = nil
		}

		r <- bridge.ExecResult{
			ExitCode: exitCode,
			Error:    err,
		}

	}()

	return r, nil
}

func (d *dockersshdconn) Resize(ctx context.Context, size bridge.ResizeOptions) error {
	if d.execId == "" {
		d.initSize = size
		return nil
	}

	return d.dockercli.ContainerExecResize(ctx, d.execId, container.ResizeOptions{
		Height: size.Height,
		Width:  size.Width,
	})
}

func New(dockercli *client.Client, containerName string) (bridge.SessionProvider, error) {
	return &dockersshdconn{
		containerName: containerName,
		dockercli:     dockercli,
	}, nil
}

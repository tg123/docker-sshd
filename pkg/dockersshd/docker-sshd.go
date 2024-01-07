package dockersshd

import (
	"context"
	"io"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	log "github.com/sirupsen/logrus"
	"github.com/tg123/docker-sshd/pkg/bridge"
)

var _ bridge.SessionProvider = (*dockersshdconn)(nil)

const execTimeout = 10 * time.Second

type dockersshdconn struct {
	containerName string
	dockercli     *client.Client
	execId        string
}

func (d *dockersshdconn) Close() error {
	return nil
}

func (d *dockersshdconn) Exec(ctx context.Context, execconfig bridge.ExecConfig) (<-chan bridge.ExecResult, error) {

	exec, err := d.dockercli.ContainerExecCreate(ctx, d.containerName, types.ExecConfig{
		AttachStdin:  execconfig.Stdin != nil,
		AttachStdout: execconfig.Stdout != nil,
		AttachStderr: execconfig.Stderr != nil,
		Env:          execconfig.Env,
		Tty:          execconfig.Tty,
		Cmd:          execconfig.Cmd,
	})

	if err != nil {
		return nil, err
	}

	execID := exec.ID
	d.execId = exec.ID

	attach, err := d.dockercli.ContainerExecAttach(ctx, execID, types.ExecStartCheck{
		Detach: false,
		// Tty: true,
	})

	if err != nil {
		return nil, err
	}

	r := make(chan bridge.ExecResult)

	go func() {
		defer attach.Close()

		done := make(chan error, 2)

		go func() {

			stdout := execconfig.Stdout
			stderr := execconfig.Stderr

			// this should not happend as we did not call for open stdout if stdout is nil, but just for safety
			if stdout == nil {
				stdout = io.Discard
			}

			if stderr == nil {
				stderr = io.Discard
			}

			_, err := stdcopy.StdCopy(stdout, stderr, attach.Reader)
			done <- err
		}()

		go func() {
			_, err := io.Copy(attach.Conn, execconfig.Stdin)
			done <- err
		}()

		var err error
		select {
		case err = <-done:
		case <-ctx.Done():
			log.Warningf("exec [%v] in container [%v] context cancelled", execconfig.Cmd, d.containerName)
			return
		}

		log.Infof("docker exec [%v] in container [%v] done", execconfig.Cmd, d.containerName)

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

func (d *dockersshdconn) Resize(ctx context.Context, width uint, height uint) error {

	if d.execId == "" {
		return nil
	}

	return d.dockercli.ContainerExecResize(ctx, d.execId, types.ResizeOptions{
		Height: height,
		Width:  width,
	})
}

func New(dockercli *client.Client, containerName string) (bridge.SessionProvider, error) {
	return &dockersshdconn{
		containerName: containerName,
		dockercli:     dockercli,
	}, nil
}

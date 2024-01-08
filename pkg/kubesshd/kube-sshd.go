package kubesshd

import (
	"context"
	"fmt"

	"github.com/tg123/docker-sshd/pkg/bridge"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/util/exec"
)

var _ bridge.SessionProvider = (*kubesshdconn)(nil)

type kubesshdconn struct {
	config    *restclient.Config
	namespace string
	pod       string
	container string

	cancel      context.CancelFunc
	resizeQueue chan *remotecommand.TerminalSize
}

func (k *kubesshdconn) Close() error {
	if k.cancel != nil {
		k.cancel()
	}
	return nil
}

func (k *kubesshdconn) Next() *remotecommand.TerminalSize {
	size, ok := <-k.resizeQueue
	if !ok {
		return nil
	}
	return size
}

func (k *kubesshdconn) Exec(ctx context.Context, execconfig bridge.ExecConfig) (<-chan bridge.ExecResult, error) {
	corev1client, err := corev1.NewForConfig(k.config)
	if err != nil {
		return nil, err
	}

	req := corev1client.RESTClient().Post().
		Resource("pods").
		Name(k.pod).
		Namespace(k.namespace).
		SubResource("exec")

	executor, err := remotecommand.NewSPDYExecutor(k.config, "POST", req.VersionedParams(
		&v1.PodExecOptions{
			Container: k.container,
			Command:   execconfig.Cmd,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       execconfig.Tty,
		},
		scheme.ParameterCodec,
	).URL())
	if err != nil {
		return nil, err
	}

	r := make(chan bridge.ExecResult)

	go func() {
		defer k.Close()

		ctx, cancel := context.WithCancel(ctx)
		k.cancel = cancel

		err := executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:             execconfig.Input,
			Stdout:            execconfig.Output,
			Stderr:            execconfig.Output,
			Tty:               execconfig.Tty,
			TerminalSizeQueue: k,
		})

		exitCode := 0

		if exitErr, ok := err.(exec.CodeExitError); ok {
			exitCode = exitErr.ExitStatus()
		}

		r <- bridge.ExecResult{
			ExitCode: exitCode,
			Error:    err,
		}
	}()

	return r, nil
}

func (k *kubesshdconn) Resize(ctx context.Context, size bridge.ResizeOptions) error {
	select {
	case k.resizeQueue <- &remotecommand.TerminalSize{
		Height: uint16(size.Height),
		Width:  uint16(size.Width),
	}:
		return nil
	default:
	}

	return fmt.Errorf("resize failed")
}

func New(config *restclient.Config, namespace, pod, container string) (bridge.SessionProvider, error) {

	return &kubesshdconn{
		config:      config,
		resizeQueue: make(chan *remotecommand.TerminalSize, 1),
		pod:         pod,
		namespace:   namespace,
		container:   container,
	}, nil

}

package main

// import (
// 	"context"
// 	"flag"
// 	"os"
// 	"path/filepath"

// 	"github.com/tg123/docker-sshd/pkg/bridge"
// 	v1 "k8s.io/api/core/v1"
// 	"k8s.io/client-go/kubernetes"
// 	"k8s.io/client-go/kubernetes/scheme"
// 	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
// 	restclient "k8s.io/client-go/rest"
// 	"k8s.io/client-go/tools/clientcmd"
// 	"k8s.io/client-go/tools/remotecommand"
// 	"k8s.io/client-go/util/exec"
// )

// type kubesshdconn struct {
// 	client      corev1.CoreV1Interface
// 	config      *restclient.Config
// 	namespace   string
// 	pod         string
// 	container   string
// 	resizeQueue chan *remotecommand.TerminalSize
// }

// func (k *kubesshdconn) Next() *remotecommand.TerminalSize {
// 	size, ok := <-k.resizeQueue
// 	if !ok {
// 		return nil
// 	}
// 	return size
// }

// func (k *kubesshdconn) Exec(ctx context.Context, execconfig bridge.ExecConfig) (<-chan bridge.ExecResult, error) {
// 	req := k.client.RESTClient().
// 		Post().
// 		Resource("pods").
// 		Name(k.pod).
// 		Namespace(k.namespace).
// 		SubResource("exec")

// 	executor, err := remotecommand.NewSPDYExecutor(k.config, "POST", req.VersionedParams(
// 		&v1.PodExecOptions{
// 			Container: k.container,
// 			Command:   execconfig.Cmd,
// 			Stdin:     execconfig.Stdin != nil,
// 			Stdout:    execconfig.Stdout != nil,
// 			Stderr:    execconfig.Stderr != nil,
// 			TTY:       execconfig.Tty,
// 		},
// 		scheme.ParameterCodec,
// 	).URL())
// 	if err != nil {
// 		return nil, err
// 	}

// 	r := make(chan bridge.ExecResult)

// 	go func() {
// 		defer close(k.resizeQueue)

// 		err := executor.StreamWithContext(context.Background(), remotecommand.StreamOptions{
// 			Stdin:             execconfig.Stdin,
// 			Stdout:            execconfig.Stdout,
// 			Stderr:            execconfig.Stderr,
// 			Tty:               true,
// 			TerminalSizeQueue: k,
// 		})

// 		exitCode := 0

// 		if exitErr, ok := err.(exec.CodeExitError); ok {
// 			exitCode = exitErr.ExitStatus()
// 		}

// 		r <- bridge.ExecResult{
// 			ExitCode: exitCode,
// 			Error:    err,
// 		}
// 	}()

// 	return r, nil
// }

// func (k *kubesshdconn) Resize(ctx context.Context, height, width uint) error {
// 	select {
// 	case k.resizeQueue <- &remotecommand.TerminalSize{
// 		Height: uint16(height),
// 		Width:  uint16(width),
// 	}:
// 	default:
// 	}

// 	return nil
// }

// func new() (*kubesshdconn, error) {
// 	var kubeconfig *string
// 	if home := os.Getenv("HOME"); home != "" {
// 		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
// 	} else {
// 		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
// 	}
// 	flag.Parse()

// 	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
// 	if err != nil {
// 		return nil, err
// 	}

// 	clientset, err := kubernetes.NewForConfig(config)
// 	if err != nil {
// 		return nil, err
// 	}

// 	return &kubesshdconn{
// 		client:      clientset.CoreV1(),
// 		config:      config,
// 		resizeQueue: make(chan *remotecommand.TerminalSize, 1),
// 		pod:         "ngx",
// 		namespace:   "default",
// 	}, nil

// }

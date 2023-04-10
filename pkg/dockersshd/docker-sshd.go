package dockersshd

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

const execTimeout = 10 * time.Second

type BridgeConfig struct {
	Cmd                 string
	ContainerNameFinder func(ssh.ConnMetadata) string
	DockerClient        *client.Client
}

type Bridge struct {
	containerName string
	dockercli     *client.Client
	defaultcmd    string
	sshConn       ssh.Conn
	chans         <-chan ssh.NewChannel
}

func (b *Bridge) Start() {
	b.handleNewChannels(b.chans)
}

func (b *Bridge) Stop() error {
	return b.sshConn.Close()
}

func (b *Bridge) handleNewChannels(chans <-chan ssh.NewChannel) {
	handlers := map[string]func(ssh.Channel, <-chan *ssh.Request, []byte){
		"session":      b.handleSession,
		"direct-tcpip": b.handleDirectTcpip,
	}

	for newChannel := range chans {
		t := newChannel.ChannelType()
		handler, ok := handlers[t]
		if !ok {
			log.Warnf("channel type is not supported, got [%v]", t)
			newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			log.Warnf("could not accept channel %v", err)
			continue
		}

		go handler(channel, requests, newChannel.ExtraData())
	}
}

type session struct {
	bridge  *Bridge
	channel ssh.Channel

	ptyRequested bool

	execId string

	width         uint32
	height        uint32
	term          string // unused
	resizePending bool
	resizeLock    sync.Mutex
}

func (s *session) handlePty(payload []byte) error {

	msg := struct {
		Term         string
		Width        uint32
		Height       uint32
		WidthPixels  uint32
		HeightPixels uint32
		Encoded      string
	}{}

	if err := ssh.Unmarshal(payload, &msg); err != nil {

		return err
	}

	s.term = msg.Term
	s.ptyRequested = true
	return s.resize(msg.Width, msg.Height)
}

func (s *session) handleWindowChanged(payload []byte) error {

	msg := struct {
		Width        uint32
		Height       uint32
		WidthPixels  uint32
		HeightPixels uint32
	}{}

	if err := ssh.Unmarshal(payload, &msg); err != nil {
		return err
	}

	return s.resize(msg.Width, msg.Height)
}

func (s *session) resize(width, height uint32) error {
	s.resizePending = width > 0 && height > 0

	s.width = width
	s.height = height
	return s.doResize()
}

func (s *session) doResize() error {

	if !s.resizePending {
		return nil
	}

	if s.execId == "" {
		return nil
	}

	s.resizeLock.Lock()
	defer s.resizeLock.Unlock()

	log.Debugf("resize %v %v", s.width, s.height)

	if err := s.bridge.dockercli.ContainerExecResize(context.Background(), s.execId, types.ResizeOptions{
		Height: uint(s.height),
		Width:  uint(s.width),
	}); err != nil {
		return err
	}

	s.resizePending = false
	return nil
}

func (s *session) exec(cmd string) error {

	log.Infof("docker exec [%v] in container [%v]", cmd, s.bridge.containerName)

	exec, err := s.bridge.dockercli.ContainerExecCreate(context.Background(), s.bridge.containerName, types.ExecConfig{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          s.ptyRequested,
		Cmd:          []string{cmd},
	})

	if err != nil {
		return err
	}

	execID := exec.ID
	s.execId = execID

	attach, err := s.bridge.dockercli.ContainerExecAttach(context.Background(), execID, types.ExecStartCheck{
		Detach: false,
		Tty:    true,
	})

	if err != nil {
		return err
	}

	if err := s.doResize(); err != nil {
		return err
	}

	go func() {
		defer attach.Close()
		defer s.channel.Close()

		done := make(chan error, 2)

		go func() {
			_, err := io.Copy(attach.Conn, s.channel)
			done <- err
		}()

		go func() {
			_, err := io.Copy(s.channel, attach.Reader)
			done <- err
		}()

		err := <-done

		log.Infof("docker exec [%v] in container [%v] done with err %v", cmd, s.bridge.containerName, err)

		exitCode := -1
		st := time.Now()

		for {
			if time.Since(st) > execTimeout {
				log.Warningf("exec [%v] is still running or inspect error after %v timeout", execID, execTimeout)
				break
			}

			exec, err := s.bridge.dockercli.ContainerExecInspect(context.Background(), execID)
			if err != nil {
				log.Warningf("inspect exec %v failed %v", execID, err)
				time.Sleep(1 * time.Second)
				continue
			}

			if exec.Running {
				log.Warnf("exec %v is still running in container %v", execID, exec.ContainerID)
				time.Sleep(1 * time.Second)
				continue
			}

			exitCode = exec.ExitCode
			break
		}

		log.Infof("docker exec [%v] in container [%v] exit status %v", cmd, s.bridge.containerName, exitCode)

		ok, err := s.channel.SendRequest("exit-status", false, ssh.Marshal(&struct{ uint32 }{uint32(exitCode)}))
		log.Printf("send exit status %v %v", ok, err)
	}()

	return nil
}

func (s *session) handleEnv(payload []byte) error {
	msg := struct {
		Name    string
		Varible string
	}{}

	if err := ssh.Unmarshal(payload, &msg); err != nil {
		return err
	}

	return fmt.Errorf("env not supported")
}

func (s *session) handleExec(payload []byte) error {
	msg := struct {
		Command string
	}{}

	if err := ssh.Unmarshal(payload, &msg); err != nil {
		return err
	}

	return s.exec(msg.Command)
}

func (b *Bridge) handleSession(channel ssh.Channel, requests <-chan *ssh.Request, _ []byte) {

	s := &session{
		bridge:  b,
		channel: channel,
	}

	for req := range requests {
		var err error

		switch req.Type {
		case "pty-req":
			err = s.handlePty(req.Payload)
		case "shell":
			err = s.exec(b.defaultcmd)
		case "exec":
			err = s.handleExec(req.Payload)
		case "env":
			err = s.handleEnv(req.Payload)
		case "window-change":
			err = s.handleWindowChanged(req.Payload)
		default:
			err = fmt.Errorf("unknown request type: %v", req.Type)
		}

		if err != nil {
			log.Warnf("failed to handle request: %v", err)
		}

		if req.WantReply {
			req.Reply(err == nil, nil)
		}
	}
}

func handleKeepAlive(reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type == "keepalive@openssh.com" {
			req.Reply(true, nil)
			continue
		}
		log.Printf("recieved out-of-band request: %v", req)
	}
}

func (b *Bridge) handleDirectTcpip(channel ssh.Channel, requests <-chan *ssh.Request, payload []byte) {
	msg := struct {
		HostToConnect  string
		PortToConnect  uint32
		OriginatorIp   string
		OriginatorPort uint32
	}{}

	defer channel.Close()
	go ssh.DiscardRequests(requests)

	if err := ssh.Unmarshal(payload, &msg); err != nil {
		log.Errorf("failed to unmarshal direct-tcpip payload: %v", err)
		return
	}

	exec, err := b.dockercli.ContainerExecCreate(context.Background(), b.containerName, types.ExecConfig{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: false,
		Tty:          false,
		Cmd:          []string{"nc", msg.HostToConnect, fmt.Sprintf("%v", msg.PortToConnect)},
	})

	if err != nil {
		log.Errorf("direct-tcpip requires [nc] installed inside container, launch nc failed: %v", err)
		return
	}

	execID := exec.ID

	attach, err := b.dockercli.ContainerExecAttach(context.Background(), execID, types.ExecStartCheck{
		Detach: false,
		Tty:    true,
	})

	if err != nil {
		log.Errorf("failed to attach [nc] exec %v", err)
		return
	}

	defer attach.Close()
	defer channel.Close()

	done := make(chan error, 2)

	go func() {
		_, err := io.Copy(attach.Conn, channel)
		done <- err
	}()

	go func() {
		_, err := io.Copy(channel, attach.Conn)
		done <- err
	}()

	if err := <-done; err != nil {
		log.Warningf("direct-tcpip io copy failed: %v", err)
	}
}

func New(conn net.Conn, sshconfig *ssh.ServerConfig, bridgeconfig *BridgeConfig) (*Bridge, error) {

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, sshconfig)
	if err != nil {
		return nil, err
	}

	b := &Bridge{
		dockercli:     bridgeconfig.DockerClient,
		containerName: bridgeconfig.ContainerNameFinder(sshConn),
		chans:         chans,
		defaultcmd:    bridgeconfig.Cmd,
	}

	go handleKeepAlive(reqs)

	return b, nil
}

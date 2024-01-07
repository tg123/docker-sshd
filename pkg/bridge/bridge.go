package bridge

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

type ExecConfig struct {
	Tty    bool
	Stdin  io.Reader
	Stderr io.Writer
	Stdout io.Writer
	Env    []string
	Cmd    []string
}

type ExecResult struct {
	ExitCode int
	Error    error
}

type SessionProvider interface {
	// Resize send resize request to container
	Resize(context.Context, uint, uint) error

	// Exec start command in container, will be called only once
	Exec(context.Context, ExecConfig) (<-chan ExecResult, error)

	// Close close provider
	Close() error
}

type BridgeConfig struct {
	DefaultCmd  string
	ExecTimeout time.Duration
}

type Bridge struct {
	defaultcmd string
	sshConn    ssh.Conn
	chans      <-chan ssh.NewChannel
	provider   SessionProvider
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
			_ = newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
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
	bridge *Bridge

	channel      ssh.Channel
	ptyRequested bool

	env []string

	width  uint32
	height uint32
	term   string // unused

	resizePending bool
	resizeLock    sync.Mutex

	execCalled bool
	execLock   sync.Mutex
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
	log.Debugf("resize %v %v", s.width, s.height)

	if !s.resizePending {
		return nil
	}

	s.resizeLock.Lock()
	defer s.resizeLock.Unlock()

	if err := s.bridge.provider.Resize(
		context.Background(),
		uint(s.height),
		uint(s.width),
	); err != nil {
		return err
	}

	s.resizePending = false
	return nil
}

func (s *session) exec(cmd string) error {

	if s.execCalled {
		return fmt.Errorf("exec can only be called once")
	}

	s.execLock.Lock()
	defer s.execLock.Unlock()

	s.execCalled = true

	log.Debugf("exec [%v] in container", cmd)

	r, err := s.bridge.provider.Exec(context.Background(), ExecConfig{
		Stdin:  s.channel,
		Stdout: s.channel,
		Stderr: s.channel,
		Env:    s.env,
		Tty:    s.ptyRequested,
		Cmd:    strings.Split(cmd, " "),
	})

	if err != nil {
		return err
	}

	if err := s.doResize(); err != nil {
		return err
	}

	go func() {
		defer s.channel.Close()
		result := <-r
		exitCode := result.ExitCode

		log.Infof("exec [%v] in container exit status %v", cmd, exitCode)

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

	s.env = append(s.env, fmt.Sprintf("%s=%s", msg.Name, msg.Varible))

	return nil
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
			log.Warnf("failed to handle %v request: %v", req.Type, err)
		}

		if req.WantReply {
			_ = req.Reply(err == nil, nil)
		}
	}
}

func handleKeepAlive(reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type == "keepalive@openssh.com" {
			_ = req.Reply(true, nil)
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

	r, err := b.provider.Exec(context.Background(), ExecConfig{
		Stdin:  channel,
		Stdout: channel,
		Cmd:    []string{"nc", msg.HostToConnect, fmt.Sprintf("%v", msg.PortToConnect)},
	})

	if err != nil {
		log.Errorf("direct-tcpip requires [nc] installed inside container, launch nc failed: %v", err)
		return
	}

	if err := (<-r).Error; err != nil {
		log.Warningf("direct-tcpip io copy failed: %v", err)
	}
}

func New(conn net.Conn, sshconfig *ssh.ServerConfig, bridgeconfig *BridgeConfig, providerCreater func(*ssh.ServerConn) (SessionProvider, error)) (*Bridge, error) {

	sshConn, chans, reqs, err := ssh.NewServerConn(conn, sshconfig)
	if err != nil {
		return nil, err
	}

	provider, err := providerCreater(sshConn)
	if err != nil {
		return nil, err
	}

	b := &Bridge{
		provider:   provider,
		chans:      chans,
		defaultcmd: bridgeconfig.DefaultCmd,
	}

	go handleKeepAlive(reqs)

	return b, nil
}

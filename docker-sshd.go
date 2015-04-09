package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"

	"golang.org/x/crypto/ssh"

	"github.com/docker/docker/pkg/mflag"
	"github.com/fsouza/go-dockerclient"
)

var (
	logger = log.New(os.Stdout, "", log.Ldate|log.Ltime)
)

func main() {

	config := struct {
		ListenAddr  string
		Port        uint
		KeyFile     string
		ShowHelp    bool
		ShowVersion bool
		Host        string
		Cmd         string
	}{}

	mflag.StringVar(&config.Host, []string{"H", "-host"}, "unix:///var/run/docker.sock", "docker host socket")
	mflag.StringVar(&config.ListenAddr, []string{"l", "-listen_addr"}, "0.0.0.0", "Listening Address")
	mflag.UintVar(&config.Port, []string{"p", "-port"}, 2232, "Listening Port")
	mflag.StringVar(&config.KeyFile, []string{"i", "-server_key"}, "/etc/ssh/ssh_host_rsa_key", "Key file for SSH")
	mflag.StringVar(&config.Cmd, []string{"c", "-command"}, "/bin/bash", "default exec command")
	mflag.BoolVar(&config.ShowHelp, []string{"h", "-help"}, false, "Print help and exit")

	// TODO version
	mflag.BoolVar(&config.ShowVersion, []string{"-version"}, false, "Print version and exit")

	mflag.Parse()

	if config.ShowHelp {
		mflag.PrintDefaults()
		return
	}

	client, err := docker.NewClient(config.Host)
	if err != nil {
		logger.Fatalln("Cannot connect to docker %v", err)
	}

	server := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			// TODO query
			return nil, nil
		},
	}

	privateBytes, err := ioutil.ReadFile(config.KeyFile)
	if err != nil {
		logger.Fatalln(err)
	}

	private, err := ssh.ParsePrivateKey(privateBytes)
	if err != nil {
		logger.Fatalln(err)
	}

	server.AddHostKey(private)

	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", config.ListenAddr, config.Port))
	if err != nil {
		logger.Fatalln("failed to listen for connection: %v", err)
	}
	defer listener.Close()

	logger.Printf("Docker.sshd started")

	for {
		c, err := listener.Accept()
		if err != nil {
			logger.Printf("failed to accept connection: %v", err)
			continue
		}

		logger.Printf("connection accepted: %v", c.RemoteAddr())

		go func() {
			sshConn, chans, reqs, err := ssh.NewServerConn(c, server)

			if err != nil {
				logger.Printf("failed to establish ssh connection: %v", err)
				return
			}

			exec, err := client.CreateExec(docker.CreateExecOptions{
				Container:    sshConn.User(),
				AttachStdin:  true,
				AttachStdout: true,
				AttachStderr: true,
				Tty:          true,
				Cmd:          []string{config.Cmd},
			})

			if err != nil {
				logger.Printf("failed to create docker exec: %v", err)
				sshConn.Close()
				return
			}

			go handleRequests(reqs)
			// Accept all channels
			go handleChannels(client, exec.ID, chans)
		}()
	}
}

func handleRequests(reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type == "keepalive@openssh.com" {
			req.Reply(true, nil)
			continue
		}
		logger.Printf("recieved out-of-band request: %+v", req)
	}
}

func handleChannels(client *docker.Client, execID string, chans <-chan ssh.NewChannel) {
	// Service the incoming Channel channel.
	for newChannel := range chans {
		// Since we're handling the execution of a shell, we expect a
		// channel type of "session". However, there are also: "x11", "direct-tcpip"
		// and "forwarded-tcpip" channel types.
		if t := newChannel.ChannelType(); t != "session" {
			newChannel.Reject(ssh.UnknownChannelType, fmt.Sprintf("unknown channel type: %s", t))
			continue
		}

		// At this point, we have the opportunity to reject the client's
		// request for another logical connection
		channel, requests, err := newChannel.Accept()
		if err != nil {
			logger.Printf("could not accept channel (%s)", err)
			continue
		}

		go func() {
			err := client.StartExec(execID, docker.StartExecOptions{
				Detach:       false,
				OutputStream: channel,
				ErrorStream:  channel,
				InputStream:  channel,
				RawTerminal:  false,
			})

			// this call block until exec done
			exit_status := 0

			if err != nil {
				exit_status = -1
			}

			channel.SendRequest("exit-status", false, ssh.Marshal(&struct{ uint32 }{uint32(exit_status)}))
			channel.Close()

			logger.Printf("session closed")
		}()

		// Sessions have out-of-band requests such as "shell", "pty-req" and "env"
		// https://tools.ietf.org/html/rfc4254#
		// TODO impl more

		go func(in <-chan *ssh.Request) {
			for req := range in {
				ok := false

				switch req.Type {

				case "shell":
					if len(req.Payload) == 0 {
						ok = true
					}

				case "pty-req":
					// Responding 'ok' here will let the client
					// know we have a pty ready for input
					ok = true

					msg := struct {
						Term   string
						Width  uint32
						Height uint32
					}{}

					ssh.Unmarshal(req.Payload, &msg)

					client.ResizeExecTTY(execID, int(msg.Height), int(msg.Width))

					logger.Printf("pty-req '%v' %v * %v", msg.Term, msg.Height, msg.Width)

				case "window-change":

					msg := struct {
						Width  uint32
						Height uint32
					}{}

					ssh.Unmarshal(req.Payload, &msg)

					client.ResizeExecTTY(execID, int(msg.Height), int(msg.Width))

					logger.Printf("windows-changed %v * %v", msg.Height, msg.Width)

					// find way for this
					//case "env":

					//	msg := struct {
					//		Name  string
					//		Value string
					//	}{}

					//	ssh.Unmarshal(req.Payload, &msg)

					//    fmt.Println(msg)
				default:
					logger.Printf("unhandled req type %v", req.Type)
				}

				if req.WantReply {
					req.Reply(ok, nil)
				}
			}
		}(requests)
	}
}

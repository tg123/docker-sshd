package main

import (
	"fmt"
	"net"
	"os"

	"github.com/tg123/docker-sshd/pkg/bridge"
	"github.com/tg123/docker-sshd/pkg/crisshd"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"golang.org/x/crypto/ssh"
)

func main() {
	config := struct {
		ListenAddr      string
		Port            int
		KeyFile         string
		Cmd             string
		RuntimeEndpoint string
		ImageEndpoint   string
	}{}

	log.SetLevel(log.DebugLevel)

	app := &cli.App{
		Name:  "cri-sshd",
		Usage: "make cri container sshable",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "address",
				Aliases:     []string{"l"},
				Value:       "0.0.0.0",
				Usage:       "listening address",
				Destination: &config.ListenAddr,
			},
			&cli.IntFlag{
				Name:        "port",
				Aliases:     []string{"p"},
				Value:       2232,
				Usage:       "listening port",
				Destination: &config.Port,
			},
			&cli.StringFlag{
				Name:        "server-key",
				Aliases:     []string{"i"},
				Usage:       "server key files, support wildcard",
				Value:       "/etc/ssh/ssh_host_ed25519_key",
				Destination: &config.KeyFile,
			},
			&cli.StringFlag{
				Name:        "command",
				Aliases:     []string{"c"},
				Usage:       "default exec command",
				Value:       "/bin/sh",
				Destination: &config.Cmd,
			},
			&cli.StringFlag{
				Name:        "runtime-endpoint",
				Usage:       "CRI runtime endpoint",
				Destination: &config.RuntimeEndpoint,
			},
			&cli.StringFlag{
				Name:        "image-endpoint",
				Usage:       "CRI image endpoint",
				Destination: &config.ImageEndpoint,
			},
		},
		Action: func(c *cli.Context) error {
			privateBytes, err := os.ReadFile(config.KeyFile)
			if err != nil {
				return err
			}

			private, err := ssh.ParsePrivateKey(privateBytes)
			if err != nil {
				return err
			}

			sshserver := &ssh.ServerConfig{
				NoClientAuth: true,

				NoClientAuthCallback: func(cm ssh.ConnMetadata) (*ssh.Permissions, error) {
					return nil, nil
				},

				PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
					return nil, nil
				},

				PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
					return nil, nil
				},

				KeyboardInteractiveCallback: func(conn ssh.ConnMetadata, client ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
					return nil, nil
				},
			}

			sshserver.AddHostKey(private)
			addr := net.JoinHostPort(config.ListenAddr, fmt.Sprintf("%d", config.Port))
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			defer listener.Close()

			log.Printf("cri-sshd started, listening at %v", addr)

			for {
				c, err := listener.Accept()
				if err != nil {
					log.Printf("failed to accept connection: %v", err)
					continue
				}

				b, err := bridge.New(c, sshserver, &bridge.BridgeConfig{
					DefaultCmd: config.Cmd,
				}, func(sc *ssh.ServerConn) (bridge.SessionProvider, error) {
					return crisshd.New(sc.User(), config.RuntimeEndpoint, config.ImageEndpoint)
				})

				if err != nil {
					log.Printf("failed to establish ssh connection: %v", err)
					continue
				}

				go b.Start()
			}
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

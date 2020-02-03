package sshconn

import (
	"fmt"
	"net"
	"os"
	osuser "os/user"
	"syscall"
	"time"

	"github.com/kevinburke/ssh_config"
	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/terminal"
)

type Config struct {
	Host string
	Port string
	User string

	KnownHostsFile string
	DialTimeout    time.Duration
}

type Conn struct {
	sshc *ssh.Client
}

func New(cfg Config) (*Conn, error) {
	host := cfg.Host
	if altHost := ssh_config.Get(host, "Hostname"); altHost != "" {
		host = altHost
	}

	port := cfg.Port
	if port == "" {
		if altPort := ssh_config.Get(host, "Port"); altPort != "" {
			port = altPort
		} else {
			port = "22"
		}
	}
	fmt.Println(host, port)

	user := cfg.User
	if user == "" {
		if altUser := ssh_config.Get(host, "User"); altUser != "" {
			user = altUser
		} else {
			currentUser, err := osuser.Current()
			if err != nil {
				return nil, errors.Wrap(err, "failed to get current user")
			}
			user = currentUser.Username
		}
	}

	hostKeyCheck := ssh.InsecureIgnoreHostKey()
	if cfg.KnownHostsFile != "" {
		var err error
		hostKeyCheck, err = knownHostsCallback(cfg.KnownHostsFile)
		if err != nil {
			return nil, err
		}
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			agentAuth(),
			passwordAuth(),
		},
		HostKeyCallback: hostKeyCheck,
		Timeout:         cfg.DialTimeout,
	}

	sshc, err := ssh.Dial("tcp", host+":"+port, config)
	if err != nil {
		return nil, err
	}

	return &Conn{
		sshc: sshc,
	}, nil
}

func (c *Conn) Close() error {
	return c.sshc.Close()
}

func passwordAuth() ssh.AuthMethod {
	return ssh.PasswordCallback(func() (string, error) {
		fmt.Print("Enter password: ")
		bytePassword, err := terminal.ReadPassword(syscall.Stdin)
		if err != nil {
			return "", err
		}
		fmt.Print("\n")
		return string(bytePassword), nil
	})
}

func agentAuth() ssh.AuthMethod {
	if sshAgent, err := net.Dial("unix", os.Getenv("SSH_AUTH_SOCK")); err == nil {
		return ssh.PublicKeysCallback(agent.NewClient(sshAgent).Signers)
	}
	return nil
}

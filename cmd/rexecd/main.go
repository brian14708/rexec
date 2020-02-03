package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/BurntSushi/toml"
	"github.com/brian14708/rexec/internal/cmdutil"
	"github.com/brian14708/rexec/internal/protocol"
	"github.com/brian14708/rexec/internal/sandbox"
	"github.com/brian14708/rexec/internal/sshconn"
	"github.com/sirupsen/logrus"
	"github.com/xtaci/smux"
	"golang.org/x/crypto/ssh"
)

var (
	flagNoSandbox = flag.Bool("no-sandbox", false, "Run without sandbox")
)

type Config struct {
	Paths struct {
		ReadOnly []string
		Writable []string
		Hidden   []string
	}
	Servers map[string]struct {
		Host string
		Port int
		User string
	}
}

func main() {
	flag.Parse()

	configDir := cmdutil.ConfigDir()

	var config Config
	_, err := toml.DecodeFile(filepath.Join(configDir, "config.toml"), &config)
	if err != nil {
		logrus.Fatalf("cannot find parse config file: %v", err)
	}
	fmt.Println(config)
	os.Exit(1)

	if !*flagNoSandbox {
		execSandbox(configDir, config)
		return
	}

	conns := map[string]*sshconn.Conn{}
	for name, server := range config.Servers {
		port := ""
		if server.Port != 0 {
			port = fmt.Sprintf("%d", server.Port)
		}
		conn, err := sshconn.New(sshconn.Config{
			Host: server.Host,
			Port: port,
			User: server.User,

			KnownHostsFile: filepath.Join(configDir, "known_hosts"),
		})
		if err != nil {
			logrus.Warnf("failed to connect to %s: %v", name, err)
		}
		conns[name] = conn
		conn.RemoteMount(context.TODO(), "/", fmt.Sprintf("/tmp/rexec-%s-%s", "hostname", name), "-o kernel_cache -o auto_cache -o negative_timeout=5 -o entry_timeout=5 -o attr_timeout=5 -o max_readahead=90000")
	}

	sockPath := filepath.Join(configDir, "daemon.sock")
	lockPath := filepath.Join(configDir, "daemon.sock.lock")
	lock, err := os.OpenFile(lockPath, os.O_RDONLY|os.O_CREATE, 0600)
	if err != nil {
		logrus.Fatalf("cannot create lock file: %v", err)
	}
	err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		logrus.Fatal("another daemon instance is running")
	}
	defer func() {
		syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		lock.Close()
		os.Remove(lockPath)
	}()
	if err := os.Remove(sockPath); err != nil {
		if !os.IsNotExist(err) {
			logrus.Fatalf("cannot remove socket: %v", err)
		}
	}
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		logrus.Fatalf("listen failed: %v", err)
	}
	os.Chmod(sockPath, 0600)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		ln.Close()
		signal.Stop(sig)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			logrus.Warnf("accept error: %v", err)
			break
		}
		go handleConnection(conn, conns["local"])
	}

	for _, conn := range conns {
		conn.Close()
	}
	os.Exit(9)
}

func handleConnection(c net.Conn, conn *sshconn.Conn) {
	defer c.Close()

	// Setup server side of smux
	session, err := smux.Server(c, nil)
	if err != nil {
		panic(err)
	}
	defer session.Close()

	// Accept a stream
	stream, err := session.AcceptStream()
	if err != nil {
		panic(err)
	}
	cmd := protocol.NewCommandChan(stream)
	defer cmd.Close()

	req, err := cmd.RecvRequest()
	if err != nil {
		logrus.Warnf("invalid request format: %v", err)
		return
	}
	fmt.Println(req)

	inStream, err := session.AcceptStream()
	if err != nil {
		panic(err)
	}

	outStream, err := session.AcceptStream()
	if err != nil {
		panic(err)
	}

	errStream, err := session.AcceptStream()
	if err != nil {
		panic(err)
	}

	s := &sandbox.Spec{
		Command:    req.Exec.Command,
		Args:       req.Exec.Args,
		WorkingDir: req.Exec.WorkingDir,
		Env: append(req.Exec.Env,
			"REXEC=1",
		),
		Bind: map[string]string{
			"/": "/tmp/rexec-hostname-local",
		},
		TmpFS: []string{
			os.Getenv("REXEC_TMPDIR"),
		},
		UnshareNamespace: true,
	}

	var cc *sshconn.Cmd
	if !req.Exec.DisablePTY {
		modes := ssh.TerminalModes{
			ssh.TTY_OP_ISPEED: 115200,
			ssh.TTY_OP_OSPEED: 115200,
		}

		args := s.CommandArgs()
		cc, _ = conn.RunCommand(context.TODO(), args[0], args[1:]...)
		cc.Stdout = outStream
		cc.Stderr = errStream
		cc.Stdin = inStream

		err = cc.StartPTY(req.Exec.TerminalName, req.Exec.TerminalLines, req.Exec.TerminalCols, modes)

	} else {
		args := s.CommandArgs()
		cc, err = conn.RunCommand(context.TODO(), args[0], args[1:]...)
		if err != nil {
			panic(err)
		}
		cc.Stdout = outStream
		cc.Stderr = errStream
		cc.Stdin = inStream

		err = cc.Start()
	}

	go func() {
		for req := range cmd.RecvNotification() {
			if wc := req.WindowChange; wc != nil {
				cc.WindowChange(wc.TerminalLines, wc.TerminalCols)
			}
		}
	}()
	err = cc.Wait()
	outStream.Close()
	errStream.Close()

	exitCode := 0
	if err != nil {
		exitCode = 255
		if e, ok := err.(*ssh.ExitError); ok {
			exitCode = e.Waitmsg.ExitStatus()
		}
	}

	cmd.SendNotification(&protocol.Notification{
		Exit: &protocol.ExitStatus{
			ExitCode: exitCode,
		},
	})
}

func ensureConfigDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "rexec")
	err = os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return "", err
	}
	if path, err = filepath.Abs(path); err != nil {
		return "", err
	}
	return path, nil
}

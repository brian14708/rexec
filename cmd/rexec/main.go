package main

import (
	"flag"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/alessio/shellescape"
	"github.com/brian14708/rexec/internal/cmdutil"
	"github.com/brian14708/rexec/internal/protocol"
	"github.com/sirupsen/logrus"
	"github.com/xtaci/smux"
	"golang.org/x/crypto/ssh/terminal"
)

var (
	flagShell      = flag.Bool("s", false, "Execute inside of shell")
	flagDisablePTY = flag.Bool("T", false, "Disable PTY")
)

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		logrus.Fatalf("missing command")
	}

	configDir := cmdutil.ConfigDir()

	cwd, err := os.Getwd()
	if err != nil {
		logrus.Fatalf("cannot get current working directory: %v", err)
	}

	pty := !*flagDisablePTY
	if _, _, err = terminal.GetSize(syscall.Stdin); err != nil {
		pty = false
	}

	var cols, lines int
	var term string
	var sigWinCh chan os.Signal
	if pty {
		sigWinCh = make(chan os.Signal, 1)
		signal.Notify(sigWinCh, syscall.SIGWINCH)

		cols, lines, err = terminal.GetSize(syscall.Stdin)
		if err != nil {
			logrus.Warnf("cannot get current terminal dimensions: %v", err)
			cols = 80
			lines = 40
		}
		term = os.Getenv("TERM")
		if term == "" {
			logrus.Warnf("cannot get current terminal type")
			term = "vt100"
		}
	}

	cmd := flag.Args()[0]
	args := flag.Args()[1:]
	if *flagShell {
		sh := os.Getenv("SHELL")
		if sh == "" {
			sh = "/bin/sh"
		}

		var b strings.Builder
		b.WriteString(shellescape.Quote(cmd))
		for _, arg := range args {
			b.WriteString(" ")
			b.WriteString(shellescape.Quote(arg))
		}
		cmd = sh
		if pty {
			args = []string{"-i", "-c", b.String()}
		} else {
			args = []string{"-c", b.String()}
		}
	}

	req := &protocol.Request{
		Exec: &protocol.ExecRequest{
			Command:    cmd,
			Args:       args,
			WorkingDir: cwd,
			Env:        os.Environ(),

			DisablePTY:    !pty,
			TerminalName:  term,
			TerminalCols:  cols,
			TerminalLines: lines,
		},
	}

	ec := func() int {
		sockPath := filepath.Join(configDir, "daemon.sock")
		sock, err := net.Dial("unix", sockPath)
		if err != nil {
			logrus.Fatalf("failed to connect to daemon: %v", err)
		}
		defer sock.Close()

		sess, err := smux.Client(sock, nil)
		if err != nil {
			logrus.Fatalf("failed to create smux session: %v", err)
		}
		defer sess.Close()

		cmdStream, err := sess.OpenStream()
		if err != nil {
			panic(err)
		}
		cmd := protocol.NewCommandChan(cmdStream)
		defer cmd.Close()

		cmd.SendRequest(req)

		inStream, err := sess.OpenStream()
		if err != nil {
			panic(err)
		}
		defer inStream.Close()

		outStream, err := sess.OpenStream()
		if err != nil {
			panic(err)
		}
		defer outStream.Close()

		errStream, err := sess.OpenStream()
		if err != nil {
			panic(err)
		}
		defer errStream.Close()

		if pty {
			oldState, _ := terminal.MakeRaw(syscall.Stdin)
			defer terminal.Restore(syscall.Stdin, oldState)
			go func() {
				for range sigWinCh {
					cols, lines, err := terminal.GetSize(syscall.Stdin)
					if err == nil {
						cmd.SendNotification(&protocol.Notification{
							WindowChange: &protocol.WindowChange{
								TerminalCols:  cols,
								TerminalLines: lines,
							},
						})
					}
				}
			}()
		}

		go func() {
			io.Copy(inStream, os.Stdin)
			inStream.Close()
		}()

		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			io.Copy(os.Stderr, errStream)
			wg.Done()
		}()

		wg.Add(1)
		go func() {
			io.Copy(os.Stdout, outStream)
			wg.Done()
		}()

		wg.Add(1)
		exitCode := -1
		go func() {
			for resp := range cmd.RecvNotification() {
				if resp.Exit != nil {
					exitCode = resp.Exit.ExitCode
				}
			}
			wg.Done()
		}()

		wg.Wait()
		return exitCode
	}()
	os.Exit(ec)
}

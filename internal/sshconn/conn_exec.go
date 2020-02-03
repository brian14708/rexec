package sshconn

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/alessio/shellescape"
	"golang.org/x/crypto/ssh"
)

type Cmd struct {
	sess *ssh.Session
	args string

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (c *Cmd) Start() error {
	c.sess.Stdin = c.Stdin
	c.sess.Stdout = c.Stdout
	c.sess.Stderr = c.Stderr
	return c.sess.Start(c.args)
}

func (c *Cmd) StartPTY(term string, h, w int, termmodes ssh.TerminalModes) error {
	c.sess.Stdin = c.Stdin
	c.sess.Stdout = c.Stdout
	c.sess.Stderr = c.Stderr

	if err := c.sess.RequestPty(term, h, w, termmodes); err != nil {
		c.sess.Close()
		return err
	}
	fmt.Println(c.args)
	return c.sess.Start(c.args)
}

func (c *Cmd) Wait() error {
	err := c.sess.Wait()
	c.sess.Close()
	return err
}

func (c *Cmd) WindowChange(h, w int) error {
	return c.sess.WindowChange(h, w)
}

func (c *Cmd) StdinPipe() (io.WriteCloser, error) {
	return c.sess.StdinPipe()
}

func (c *Cmd) StdoutPipe() (io.Reader, error) {
	return c.sess.StdoutPipe()
}

func (c *Cmd) StderrPipe() (io.Reader, error) {
	return c.sess.StderrPipe()
}

func (c *Conn) RunCommandRaw(ctx context.Context, cmd string) (*Cmd, error) {
	sess, err := c.sshc.NewSession()
	if err != nil {
		return nil, err
	}

	return &Cmd{
		sess: sess,
		args: cmd,
	}, nil
}

func (c *Conn) RunCommand(ctx context.Context, name string, args ...string) (*Cmd, error) {
	var b strings.Builder
	b.WriteString(shellescape.Quote(name))
	for _, arg := range args {
		b.WriteString(" ")
		b.WriteString(shellescape.Quote(arg))
	}
	return c.RunCommandRaw(ctx, b.String())
}

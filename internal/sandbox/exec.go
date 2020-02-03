package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

type Cmd struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc

	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	SysProcAttr *syscall.SysProcAttr

	args    []string
	argFile *os.File

	files []*os.File
}

func Exec(ctx context.Context, s *Spec) (*Cmd, error) {
	prefix, args, exe := s.commandArgs()
	prefix = append(prefix, "--args", "3")
	prefix = append(prefix, exe...)

	cctx, cancel := context.WithCancel(ctx)
	cmd := &Cmd{
		cmd:    exec.CommandContext(cctx, prefix[0], prefix[1:]...),
		cancel: cancel,

		args: args,
	}
	_, f, err := cmd.addWritePipe()
	if err != nil {
		return nil, err
	}
	cmd.argFile = f
	return cmd, nil
}

func (c *Cmd) addReadPipe() (uintptr, *os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return 0, nil, err
	}
	c.files = append(c.files, r, w)
	c.cmd.ExtraFiles = append(c.cmd.ExtraFiles, w)
	return uintptr(len(c.cmd.ExtraFiles) + 2), r, nil
}

func (c *Cmd) addWritePipe() (uintptr, *os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return 0, nil, err
	}
	c.files = append(c.files, r, w)
	c.cmd.ExtraFiles = append(c.cmd.ExtraFiles, r)
	return uintptr(len(c.cmd.ExtraFiles) + 2), w, nil
}

func (c *Cmd) InfoFile() (io.Reader, error) {
	fd, f, err := c.addReadPipe()
	if err != nil {
		return nil, err
	}
	c.args = append(c.args, "--info-fd", fmt.Sprintf("%d", fd))
	return f, nil
}

func (c *Cmd) Run() error {
	c.cmd.Stdout = c.Stdout
	c.cmd.Stderr = c.Stderr
	c.cmd.Stdin = c.Stdin
	c.cmd.SysProcAttr = c.SysProcAttr

	if err := c.cmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan error, 2)
	go func() {
		defer wg.Done()
		errCh <- c.cmd.Wait()
	}()
	go func() {
		defer wg.Done()
		for _, a := range c.args {
			if _, err := c.argFile.WriteString(a); err != nil {
				errCh <- err
				return
			}
			if _, err := c.argFile.Write([]byte{0}); err != nil {
				errCh <- err
				return
			}
		}
		if err := c.argFile.Close(); err != nil {
			errCh <- err
			return
		}
	}()

	err := <-errCh
	c.cancel()
	wg.Wait()
	return err
}

func (c *Cmd) Close() {
	c.cancel()
	for _, f := range c.files {
		f.Close()
	}
}

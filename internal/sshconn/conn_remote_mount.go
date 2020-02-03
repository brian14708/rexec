package sshconn

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"

	"github.com/alessio/shellescape"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"
)

type MountTask struct {
	errCh      <-chan error
	cancelFunc context.CancelFunc
}

func (m *MountTask) Wait() error {
	err := <-m.errCh
	for range m.errCh {
	}
	return err
}

func (m *MountTask) stop() {
	m.cancelFunc()
	m.Wait()
}

func findSSHBinary(name string) (string, error) {
	dirs := []string{
		"/usr/lib/openssh",
		"/usr/lib/ssh",
		"/usr/lib64/ssh",
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, name)
		stat, err := os.Stat(path)
		if err == nil && stat.Mode()&syscall.S_IEXEC != 0 {
			return path, nil
		}
	}
	return "", fmt.Errorf("cannot find binary: %s", name)
}

// mount local dir to remote
func (c *Conn) RemoteMount(ctx context.Context, local string, remote string, extraArgs string) (*MountTask, error) {
	local = shellescape.Quote(local)
	remote = shellescape.Quote(remote)

	sftpBin, err := findSSHBinary("sftp-server")
	if err != nil {
		return nil, err
	}

	sess, err := c.sshc.NewSession()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ssh session")
	}

	cctx, cancel := context.WithCancel(ctx)
	sftp := exec.CommandContext(cctx, sftpBin, "-e", "-l", "INFO")

	sess.Stdin, err = sftp.StdoutPipe()
	if err != nil {
		cancel()
		return nil, errors.Wrap(err, "failed to connect sftp stdout")
	}
	sess.Stdout, err = sftp.StdinPipe()
	if err != nil {
		cancel()
		return nil, errors.Wrap(err, "failed to connect sftp stdin")
	}

	sftp.Stderr = logrus.WithFields(logrus.Fields{"module": "sftp-server"}).WriterLevel(logrus.ErrorLevel)
	sess.Stderr = logrus.WithFields(logrus.Fields{"module": "sshfs"}).WriterLevel(logrus.ErrorLevel)

	err = sftp.Start()
	if err != nil {
		cancel()
		return nil, errors.Wrap(err, "sftp-server failed to start")
	}

	err = sess.Start(fmt.Sprintf("mkdir -p %s && sshfs %s -o slave :%s %s", remote, extraArgs, local, remote))
	if err != nil {
		cancel()
		return nil, errors.Wrap(err, "failed to start sshfs")
	}

	cnt := int32(2)
	ch := make(chan error, cnt)
	putError := func(err error) {
		if err != nil {
			ch <- err
		}
		v := atomic.AddInt32(&cnt, -1)
		if v == 0 {
			close(ch)
		}
	}

	go func() {
		err := sftp.Wait()
		fmt.Println("SFTP", err)
		putError(err)
		sess.Signal(ssh.SIGTERM)
		sess.Close()
	}()

	go func() {
		err := sess.Wait()
		fmt.Println("SSHFS", err)
		putError(err)
		cancel()
	}()

	mnt := &MountTask{
		errCh:      ch,
		cancelFunc: cancel,
	}

	cmd, err := c.RunCommandRaw(cctx, fmt.Sprintf(`
sleep 0.1
for i in $(seq 0 10); do
	mountpoint -q %s && exit 0
	sleep 0.5
done
exit 1
	`, remote))
	if err != nil {
		mnt.stop()
		return nil, errors.Wrap(err, "failed when checking mountpoint")
	}
	if err = cmd.Start(); err != nil {
		mnt.stop()
		return nil, errors.Wrap(err, "failed when checking mountpoint")
	}
	if err = cmd.Wait(); err != nil {
		mnt.stop()
		return nil, fmt.Errorf("not mounted")
	}

	return mnt, nil
}

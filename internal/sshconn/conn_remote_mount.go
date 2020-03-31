package sshconn

import (
	"context"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/alessio/shellescape"
	"github.com/pkg/errors"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type MountTask struct {
	errCh <-chan error
	sess  *ssh.Session
}

func (m *MountTask) Wait() error {
	err := <-m.errCh
	for range m.errCh {
	}
	return err
}

func (m *MountTask) stop() {
	m.sess.Signal(ssh.SIGTERM)
	m.sess.Close()
	m.Wait()
}

// mount local dir to remote
func (c *Conn) RemoteMount(ctx context.Context, local string, remote string, extraArgs string) (*MountTask, error) {
	local = shellescape.Quote(local)
	remote = shellescape.Quote(remote)

	sess, err := c.sshc.NewSession()
	if err != nil {
		return nil, errors.Wrap(err, "failed to create ssh session")
	}

	r, err := sess.StdoutPipe()
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect session stdout")
	}
	w, err := sess.StdinPipe()
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect session stdin")
	}
	srv, err := sftp.NewServer(struct {
		io.Reader
		io.WriteCloser
	}{
		r, w,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to create sftp server")
	}

	err = sess.Start(fmt.Sprintf(`
cleanup() {
	rmdir %s
}
trap cleanup EXIT
mkdir %s
sshfs %s -o idmap=user -o slave :%s %s
`, remote, remote, extraArgs, local, remote))
	if err != nil {
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

	mnt := &MountTask{
		errCh: ch,
		sess:  sess,
	}

	go func() {
		err := srv.Serve()
		putError(err)
		sess.Signal(ssh.SIGTERM)
		sess.Close()
	}()

	go func() {
		err := sess.Wait()
		putError(err)
		sess.Close()
	}()

	cmd, err := c.RunCommandRaw(ctx, fmt.Sprintf(`
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

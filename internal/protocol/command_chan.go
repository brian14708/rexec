package protocol

import (
	"bufio"
	"encoding/json"
	"io"
)

type CommandChan struct {
	r *bufio.Reader
	w io.WriteCloser
}

func NewCommandChan(conn io.ReadWriteCloser) *CommandChan {
	return &CommandChan{
		r: bufio.NewReader(conn),
		w: conn,
	}
}

func (n *CommandChan) RecvRequest() (*Request, error) {
	s, err := n.r.ReadBytes('\x00')
	if err != nil {
		return nil, err
	}
	var req Request
	err = json.Unmarshal(s[:len(s)-1], &req)
	return &req, err
}

func (n *CommandChan) SendRequest(r *Request) error {
	req, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = n.w.Write(append(req, '\x00'))
	return err
}

func (n *CommandChan) RecvNotification() <-chan *Notification {
	c := make(chan *Notification)
	go func() {
		for {
			s, err := n.r.ReadBytes('\x00')
			if err != nil {
				break
			}
			var req Notification
			err = json.Unmarshal(s[:len(s)-1], &req)
			if err != nil {
				continue
			}
			c <- &req
		}
		close(c)
	}()
	return c
}

func (n *CommandChan) SendNotification(r *Notification) error {
	req, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = n.w.Write(append(req, '\x00'))
	return err
}

func (n *CommandChan) Close() error {
	return n.w.Close()
}

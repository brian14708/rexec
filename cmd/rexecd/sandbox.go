package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/brian14708/rexec/internal/sandbox"
	"github.com/sirupsen/logrus"
)

func execSandbox(configDir string, config Config) {
	cmd := "/run/rexec"
	args := append(os.Args[1:], "--no-sandbox", "--config-dir=/run/config")

	spec := &sandbox.Spec{
		Command: cmd,
		Args:    args,
	}
	for _, b := range config.Environment.Bind {
		src := b.Source
		if src == "" {
			src = b.Path
		}
		spec.Bind = append(spec.Bind, sandbox.BindSpec{
			Dst:  b.Path,
			Src:  src,
			Type: b.Mode,
		})
	}
	spec.Bind = append(spec.Bind, sandbox.BindSpec{
		Dst:  "/run",
		Type: sandbox.BindTmpFS,
	})
	spec.Bind = append(spec.Bind, sandbox.BindSpec{
		Dst:  cmd,
		Src:  self(),
		Type: sandbox.BindReadOnly,
	})
	spec.Bind = append(spec.Bind, sandbox.BindSpec{
		Dst:  "/run/config",
		Src:  configDir,
		Type: sandbox.BindReadWrite,
	})
	if e := os.Getenv("SSH_AUTH_SOCK"); e != "" {
		spec.Bind = append(spec.Bind, sandbox.BindSpec{
			Dst:  "/run/ssh.sock",
			Src:  e,
			Type: sandbox.BindReadWrite,
		})
		spec.Env = append(spec.Env, "SSH_AUTH_SOCK=/run/ssh.sock")
	}

	fmt.Println(spec.CommandArgs())
	cc, _ := sandbox.Exec(context.TODO(), spec)
	defer cc.Close()

	cc.Stdout = os.Stdout
	cc.Stderr = os.Stderr
	cc.Stdin = os.Stdin
	cc.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	r, _ := cc.InfoFile()
	go func() {
		var m struct {
			ChildPID int `json:"child-pid"`
		}
		json.NewDecoder(r).Decode(&m)

		if m.ChildPID != 0 {
			// wait for single term signal
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			s := <-sig
			signal.Stop(sig)

			syscall.Kill(m.ChildPID, s.(syscall.Signal))
		}
	}()

	err := cc.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		if e, ok := err.(*exec.ExitError); ok {
			exitCode = e.ExitCode()
		}
	}
	os.Exit(exitCode)
}

func self() string {
	name := os.Args[0]
	if filepath.Base(name) == name {
		if lp, err := exec.LookPath(name); err == nil {
			return lp
		}
	}
	if absName, err := filepath.Abs(name); err == nil {
		return absName
	}
	return name
}

func sanitizePath(path string) string {
	if !filepath.IsAbs(path) {
		logrus.Fatalf("paths need be absoulte: %s", path)
	}
	return path
}

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/brian14708/rexec/internal/sandbox"
	"github.com/sirupsen/logrus"
)

func execSandbox(configDir string, config Config) {
	roBind := map[string]string{}
	bind := map[string]string{
		configDir: configDir,
	}
	for _, p := range config.Paths.ReadOnly {
		roBind[p] = sanitizePath(p)
	}
	/*
		for _, p := range config.Paths.Hidden {
			tmp := filepath.Join(tmpdir, fmt.Sprintf("%x", md5.Sum([]byte(p)))[:12])
			err := os.MkdirAll(tmp, os.ModePerm)
			if err != nil {
				log.Fatalf("cannot make temp directory: %v", err)
			}
			bind[p] = sanitizePath(tmp)
		}
	*/
	for _, p := range config.Paths.Writable {
		bind[p] = sanitizePath(p)
	}

	cmd := self()
	args := append(os.Args[1:], "--no-sandbox")

	spec := &sandbox.Spec{
		Command:      cmd,
		Args:         args,
		ReadOnlyBind: roBind,
		Bind:         bind,
	}
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
		exitCode = 255
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

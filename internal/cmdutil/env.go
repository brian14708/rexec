package cmdutil

import (
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

func configDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "rexec")
	if path, err = filepath.Abs(path); err != nil {
		return "", err
	}
	err = os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return "", err
	}
	return path, nil
}

func ConfigDir() string {
	path, err := configDir()
	if err != nil {
		logrus.Fatalf("cannot find config directory: %v", err)
	}
	return path
}

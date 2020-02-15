package sandbox

import (
	"fmt"
	"os"
	"syscall"

	"github.com/sirupsen/logrus"
)

var (
	currentGid      string
	currentUid      string
	currentHostname string
)

func init() {

	hostname, err := os.Hostname()
	if err != nil {
		logrus.Fatalf("cannot get hostname: %v", err)
	}

	currentUid = fmt.Sprintf("%d", syscall.Getuid())
	currentGid = fmt.Sprintf("%d", syscall.Getgid())
	currentHostname = hostname
}

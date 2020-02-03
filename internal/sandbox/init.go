package sandbox

import (
	"os"
	osuser "os/user"

	"github.com/sirupsen/logrus"
)

var (
	currentGid      string
	currentUid      string
	currentHostname string
)

func init() {
	user, err := osuser.Current()
	if err != nil {
		logrus.Fatalf("cannot get current user: %v", err)
	}

	hostname, err := os.Hostname()
	if err != nil {
		logrus.Fatalf("cannot get hostname: %v", err)
	}

	currentUid = user.Uid
	currentGid = user.Gid
	currentHostname = hostname
}

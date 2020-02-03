package sshconn

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/pkg/errors"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"golang.org/x/crypto/ssh/terminal"
)

// modified from:
// - https://github.com/starkandwayne/safe/blob/abcf32597856c0d9a0a7c284ad974ee26ad9bb53/vault/proxy.go#L215

func knownHostsCallback(knownHostsFile string) (ssh.HostKeyCallback, error) {
	// create file if not exist
	if _, err := os.Stat(knownHostsFile); os.IsNotExist(err) {
		f, err := os.OpenFile(knownHostsFile, os.O_CREATE|os.O_RDWR, 0600)
		if err == nil {
			f.Close()
		}
	}

	callback, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open knownhosts")
	}

	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := callback(hostname, remote, key)
		if err == nil {
			return nil
		}

		// If we're here, we got some sort of error
		// Let's check if it was because the key wasn't trusted
		errAsKeyError, isKeyError := err.(*knownhosts.KeyError)
		if !isKeyError {
			return err
		}

		// If the error has hostnames listed under Want, it means that there was
		// a conflicting host key
		if len(errAsKeyError.Want) > 0 {
			wantedKey := errAsKeyError.Want[0]
			for _, k := range errAsKeyError.Want {
				if wantedKey.Key.Type() == key.Type() {
					wantedKey = k
				}
			}

			hostKeyConflictError := `
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @
@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@
IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!
Someone could be eavesdropping on you right now (man-in-the-middle attack)!
It is also possible that a host key has just been changed.
The fingerprint for the %[1]s key sent by the remote host is
%[2]s.
Please contact your system administrator.
Add correct host key in %[3]s to get rid of this message.
Offending %[1]s key in %[3]s:%[4]d
%[1]s host key for %[5]s has changed and safe uses strict checking.
Host key verification failed.
`
			return fmt.Errorf(hostKeyConflictError,
				key.Type(), ssh.FingerprintSHA256(key), knownHostsFile, wantedKey.Line, hostname)
		}

		// If not, then the key doesn't exist in the host key file
		// Let's see if we can ask the user if they want to add it
		if !terminal.IsTerminal(syscall.Stderr) || !promptAddNewKnownHost(hostname, remote, key) {
			// If its not a terminal or the user declined, we're rejecting it
			return errors.New("Host key verification failed")
		}

		err = writeKnownHosts(knownHostsFile, hostname, key)
		if err != nil {
			return err
		}

		return nil
	}, nil
}

func promptAddNewKnownHost(hostname string, remote net.Addr, key ssh.PublicKey) bool {
	// Otherwise, let's ask the user
	fmt.Fprintf(os.Stderr, `The authenticity of host '%[1]s (%[2]s)' can't be established.
%[3]s key fingerprint is %[4]s
Are you sure you want to continue connecting (yes/no)? `, hostname, remote.String(), key.Type(), ssh.FingerprintSHA256(key))

	var response string
	fmt.Scanln(&response)
	for response != "yes" && response != "no" {
		fmt.Fprintf(os.Stderr, "Please type 'yes' or 'no': ")
		fmt.Scanln(&response)
	}

	return response == "yes"
}

func writeKnownHosts(knownHostsFile, hostname string, key ssh.PublicKey) error {
	normalizedHostname := knownhosts.Normalize(hostname)
	f, err := os.OpenFile(knownHostsFile, os.O_APPEND|os.O_RDWR, 0600)
	if err != nil {
		return errors.Wrap(err, "failed to open knownhosts")
	}

	newKnownHostsLine := knownhosts.Line([]string{normalizedHostname}, key)
	_, err = f.WriteString(newKnownHostsLine + "\n")
	if err != nil {
		return errors.Wrap(err, "failed writing to knownhosts")
	}

	fmt.Fprintf(os.Stderr, "Warning: Permanently added '%s' (%s) to the list of known hosts.\n", hostname, key.Type())
	return nil
}

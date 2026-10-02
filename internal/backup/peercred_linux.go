//go:build linux

package backup

import (
	"net"

	"golang.org/x/sys/unix"
)

const peerCredSupported = true

// peerUID reads the connecting process's uid with SO_PEERCRED, which the
// kernel fills in at connect; the peer cannot forge it.
func peerUID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return -1, err
	}
	uid := -1
	var gerr error
	if err := raw.Control(func(fd uintptr) {
		var cred *unix.Ucred
		cred, gerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if gerr == nil {
			uid = int(cred.Uid)
		}
	}); err != nil {
		return -1, err
	}
	return uid, gerr
}

//go:build darwin || freebsd

package backup

import (
	"net"

	"golang.org/x/sys/unix"
)

const peerCredSupported = true

// peerUID reads the connecting process's uid with LOCAL_PEERCRED, which the
// kernel fills in at connect; the peer cannot forge it.
func peerUID(c *net.UnixConn) (int, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return -1, err
	}
	uid := -1
	var gerr error
	if err := raw.Control(func(fd uintptr) {
		var cred *unix.Xucred
		cred, gerr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if gerr == nil {
			uid = int(cred.Uid)
		}
	}); err != nil {
		return -1, err
	}
	return uid, gerr
}

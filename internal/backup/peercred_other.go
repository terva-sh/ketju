//go:build !linux && !darwin && !freebsd

package backup

import "net"

// Elsewhere there is no peer-credential call this package uses, so serve
// opens no control socket and only an offline backup works.
const peerCredSupported = false

func peerUID(*net.UnixConn) (int, error) { return -1, ErrUnsupported }

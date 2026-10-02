package backup

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Version is the control protocol's version. The protocol is internal to one
// binary: a command and a serve of different versions refuse each other
// plainly rather than half understand.
//
// A client sends one request line and reads one answer line. After an
// answer naming a staged snapshot it keeps the connection open while it
// takes the staged database, and closes it when it has. Serve then removes
// the staging directory, and it does so too if the client goes away first,
// or holds the connection past Options.Claim (Terva review 1979).
const Version = 1

// Request is the one line a client sends.
type Request struct {
	V  int    `json:"v"`
	Op string `json:"op"`
}

// OpSnapshot is the one operation.
const OpSnapshot = "snapshot"

// Answer is the one line the server sends back.
type Answer struct {
	OK       bool   `json:"ok"`
	Staging  string `json:"staging,omitempty"`
	PausedMS int64  `json:"paused_ms,omitempty"`
	Error    string `json:"error,omitempty"`
}

// The refusals a client can tell apart.
const (
	ErrForbidden   = "forbidden"
	ErrBusy        = "busy"
	ErrBadVersion  = "unsupported protocol version"
	ErrBadRequest  = "bad request"
	ErrUnknownOp   = "unknown operation"
	maxRequestSize = 4 << 10
	ioDeadline     = 10 * time.Second
)

// ClaimTimeout is how long a client has, by default, to take a staged
// snapshot before serve removes it: long enough to copy a large database to
// another disk, and short of leaving one behind for good.
const ClaimTimeout = 30 * time.Minute

// ErrUnsupported is Listen on a platform with no way to learn a peer's uid.
var ErrUnsupported = errors.New("backup: this platform cannot check a control socket peer's uid")

// ErrPathTooLong is Listen where the socket's path is past the platform's
// limit for a Unix socket.
var ErrPathTooLong = errors.New("backup: the control socket's path is too long for a Unix socket")

// maxSocketPath is sun_path's size less its terminator: 108 on Linux, 104 on
// darwin and the BSDs.
func maxSocketPath() int {
	if runtime.GOOS == "linux" {
		return 107
	}
	return 103
}

// SocketPath is the control socket's path for dataDir, and ErrPathTooLong
// when that is too long to bind or dial.
func SocketPath(dataDir string) (string, error) {
	p := filepath.Join(dataDir, SocketName)
	if len(p) > maxSocketPath() {
		return p, fmt.Errorf("%w: %s is %d bytes, over %d", ErrPathTooLong, p, len(p), maxSocketPath())
	}
	return p, nil
}

// Options configure a control socket.
type Options struct {
	// Snapshot takes one snapshot; serve passes a closure over Snapshot.
	Snapshot func(ctx context.Context) (Result, error)
	Logger   *slog.Logger
	// PeerUID reads a connection's peer uid; nil is the platform's
	// peer-credential call. A test reports another uid with it.
	PeerUID func(*net.UnixConn) (int, error)
	// UID is the uid a peer must have; zero means os.Getuid. A test sets
	// it with PeerUID.
	UID int
	// Claim is how long a client may hold its connection after the answer
	// before its staged snapshot is removed anyway; zero is ClaimTimeout.
	Claim time.Duration
}

// Server is a listening control socket.
type Server struct {
	ln     *net.UnixListener
	path   string
	opts   Options
	uid    int
	busy   atomic.Bool
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// Listen creates the control socket in dataDir, mode 0600, and serves it
// until Close. Each connection carries one request. A peer whose uid is not
// the server's is answered "forbidden" and nothing is held; a second
// snapshot while one runs is answered "busy".
//
// Any socket file already at the path is removed first: serve holds the
// data directory's lock, so nothing else can be using it.
func Listen(dataDir string, o Options) (*Server, error) {
	if o.PeerUID == nil {
		if !peerCredSupported {
			return nil, ErrUnsupported
		}
		o.PeerUID = peerUID
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	uid := o.UID
	if uid == 0 {
		uid = os.Getuid()
	}
	if o.Claim <= 0 {
		o.Claim = ClaimTimeout
	}
	path, err := SocketPath(dataDir)
	if err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("backup: removing a stale %s: %w", path, err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("backup: listening on %s: %w", path, err)
	}
	// The peer check below is the guard; the mode keeps other users from
	// even connecting. A connection in the moment before the chmod still
	// meets the peer check.
	if err := os.Chmod(path, 0o600); err != nil {
		ln.Close()
		os.Remove(path)
		return nil, fmt.Errorf("backup: setting %s to 0600: %w", path, err)
	}
	ln.SetUnlinkOnClose(true)
	s := &Server{ln: ln, path: path, opts: o, uid: uid}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

// Path is the socket's path.
func (s *Server) Path() string { return s.path }

// Close stops accepting, cancels a snapshot in progress, and waits up to wait
// for its handler to return. The socket file is removed.
func (s *Server) Close(wait time.Duration) error {
	err := s.ln.Close()
	s.cancel()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(wait):
		return errors.Join(err, fmt.Errorf("backup: a control request was still running after %s", wait))
	}
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}
	return err
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.AcceptUnix()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				s.opts.Logger.Warn("the control socket stopped accepting", "err", err)
			}
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer c.Close()
			s.serve(c)
		}()
	}
}

func (s *Server) serve(c *net.UnixConn) {
	reply := func(a Answer) {
		_ = c.SetWriteDeadline(time.Now().Add(ioDeadline))
		b, _ := json.Marshal(a)
		_, _ = c.Write(append(b, '\n'))
	}
	uid, err := s.opts.PeerUID(c)
	if err != nil || uid != s.uid {
		s.opts.Logger.Warn("refused a control socket request from another user", "peer_uid", uid, "err", err)
		reply(Answer{Error: ErrForbidden})
		return
	}
	_ = c.SetReadDeadline(time.Now().Add(ioDeadline))
	line, err := bufio.NewReader(io.LimitReader(c, maxRequestSize)).ReadBytes('\n')
	if err != nil {
		reply(Answer{Error: ErrBadRequest})
		return
	}
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		reply(Answer{Error: ErrBadRequest})
		return
	}
	switch {
	case req.V != Version:
		reply(Answer{Error: fmt.Sprintf("%s %d; this serve speaks %d", ErrBadVersion, req.V, Version)})
		return
	case req.Op != OpSnapshot:
		reply(Answer{Error: ErrUnknownOp})
		return
	}
	if !s.busy.CompareAndSwap(false, true) {
		reply(Answer{Error: ErrBusy})
		return
	}

	// The client sends nothing after its request, so a read ends only when
	// it closes the connection or dies. That cancels a snapshot still being
	// taken, and releases one already answered.
	ctx, cancel := context.WithCancel(s.ctx)
	defer cancel()
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		defer cancel()
		_ = c.SetReadDeadline(time.Time{})
		_, _ = io.Copy(io.Discard, c)
	}()

	res, err := s.opts.Snapshot(ctx)
	s.busy.Store(false)
	if err != nil {
		s.opts.Logger.Error("a backup snapshot failed", "err", err)
		reply(Answer{Error: err.Error()})
		return
	}
	s.opts.Logger.Info("took a backup snapshot", "staging", res.Staging, "paused_ms", res.Paused.Milliseconds())
	reply(Answer{OK: true, Staging: res.Staging, PausedMS: res.Paused.Milliseconds()})

	claim := time.NewTimer(s.opts.Claim)
	defer claim.Stop()
	select {
	case <-gone:
	case <-claim.C:
		s.opts.Logger.Warn("a backup client did not take its snapshot in time, so it was removed",
			"staging", res.Staging, "claim", s.opts.Claim)
	case <-s.ctx.Done():
	}
	s.removeStaging(res.Staging)
}

// removeStaging removes the staging directory a snapshot's path names, when
// it is one: a directory directly in the data directory, with StagingPrefix.
func (s *Server) removeStaging(staged string) {
	dir := filepath.Dir(filepath.Clean(staged))
	if !strings.HasPrefix(dir, StagingPrefix) || strings.ContainsRune(dir, filepath.Separator) {
		s.opts.Logger.Error("a snapshot named a staging path outside the data directory; left it", "staging", staged)
		return
	}
	if err := os.RemoveAll(filepath.Join(filepath.Dir(s.path), dir)); err != nil {
		s.opts.Logger.Warn("removing a backup snapshot's staging directory", "staging", dir, "err", err)
	}
}

// Ask sends one snapshot request to the control socket at path and returns
// the answer, and done, which the caller calls once it has taken the staged
// database or given up on it: serve then removes the staging directory. ctx
// bounds the request and its answer. done may be called more than once, and
// is never nil.
func Ask(ctx context.Context, path string) (a Answer, done func(), err error) {
	var d net.Dialer
	c, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		return Answer{}, func() {}, err
	}
	var once sync.Once
	done = func() { once.Do(func() { c.Close() }) }
	defer func() {
		if err != nil || !a.OK {
			done()
		}
	}()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, func() { _ = c.SetDeadline(time.Now()) })
	defer stop()
	b, _ := json.Marshal(Request{V: Version, Op: OpSnapshot})
	// A server that refuses the peer answers before reading, and may have
	// closed by the time the request is written, so a failed write is
	// reported only if there is no answer to read.
	_, werr := c.Write(append(b, '\n'))
	line, err := bufio.NewReader(io.LimitReader(c, maxRequestSize)).ReadBytes('\n')
	if err != nil {
		if werr != nil {
			return Answer{}, done, werr
		}
		return Answer{}, done, fmt.Errorf("reading the answer: %w", err)
	}
	if err := json.Unmarshal(line, &a); err != nil {
		return Answer{}, done, fmt.Errorf("reading the answer: %w", err)
	}
	return a, done, nil
}

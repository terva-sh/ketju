// Package backup is the serve side of `ketju backup` (TKT-01M29CXK35, docs/02
// "Backups"): the snapshot a running server takes for it, and the Unix
// socket the command asks for one through.
//
// A snapshot holds every thread append at posts.Writer, catches the
// projection up, checkpoints the WAL, writes the database with VACUUM INTO
// to a staging file inside the data directory, and lets the appends go. Its
// projection_state then records, for every thread, a length its log has
// reached, so the command copies each log up to that length and the
// backup's logs and database agree exactly.
package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/terva-sh/ketju/internal/store/db"
)

// SocketName is the control socket's name in the data directory.
const SocketName = "ketju.sock"

// StagingPrefix begins the name of each snapshot's staging directory in the
// data directory.
const StagingPrefix = "backup.staging-"

// StagedDB is the snapshot's file name inside its staging directory.
const StagedDB = "ketju.db"

// PauseCap bounds how long a snapshot may hold appends, counted from the
// moment they start to wait. Past it the step in progress is abandoned and
// the request fails.
const PauseCap = 60 * time.Second

// Deps are what a snapshot needs of the running server.
type Deps struct {
	// DataDir is the data directory; the staging directory is made in it.
	DataDir string
	// DB is the server's database.
	DB *db.DB
	// Hold is posts.Writer.Hold.
	Hold func(ctx context.Context) (release func(), err error)
	// CatchUp runs the projection's catch-up sweep, as serve's startup does.
	CatchUp func(ctx context.Context) error
	// Cap overrides PauseCap; zero is PauseCap. Tests shorten it.
	Cap time.Duration
	// WhileHeld, when set, runs while appends are held, before the
	// catch-up. Tests hold the pause open with it.
	WhileHeld func(ctx context.Context)
}

// Result is a snapshot taken.
type Result struct {
	// Staging is the snapshot file, relative to the data directory:
	// backup.staging-<random>/ketju.db. The caller moves it out and removes
	// its directory.
	Staging string
	// Paused is how long appends were held.
	Paused time.Duration
}

// Snapshot takes one snapshot. One deadline, Cap from the call, covers the
// hold and every step under it, so the cap counts from the moment appends
// start waiting. When it passes, the step in progress is abandoned (the
// hold gives up, the catch-up stops between threads, VACUUM INTO is
// interrupted), the appends are released, the staging directory is removed,
// and Snapshot fails.
func Snapshot(ctx context.Context, d Deps) (res Result, err error) {
	limit := d.Cap
	if limit <= 0 {
		limit = PauseCap
	}
	stage, err := os.MkdirTemp(d.DataDir, StagingPrefix)
	if err != nil {
		return res, fmt.Errorf("backup: making a staging directory: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(stage)
		}
	}()

	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	start := time.Now()
	release, err := d.Hold(ctx)
	if err != nil {
		return res, fmt.Errorf("backup: holding appends: %w", err)
	}
	defer func() {
		release()
		res.Paused = time.Since(start)
	}()
	if d.WhileHeld != nil {
		d.WhileHeld(ctx)
	}
	if err := d.CatchUp(ctx); err != nil {
		return res, fmt.Errorf("backup: catching the projection up: %w", err)
	}
	if err := d.DB.CheckpointPassive(ctx); err != nil {
		return res, fmt.Errorf("backup: checkpointing the WAL: %w", err)
	}
	if err := d.DB.SnapshotInto(ctx, filepath.Join(stage, StagedDB)); err != nil {
		return res, fmt.Errorf("backup: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return res, fmt.Errorf("backup: the pause passed its cap of %s: %w", limit, err)
	}
	return Result{Staging: filepath.Join(filepath.Base(stage), StagedDB)}, nil
}

// RemoveStale removes a control socket and staging directories a stopped
// process left in dataDir. Call it under the lock, before serve listens.
func RemoveStale(dataDir string) error {
	var errs []error
	if err := os.Remove(filepath.Join(dataDir, SocketName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	es, err := os.ReadDir(dataDir)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, e := range es {
		if e.IsDir() && strings.HasPrefix(e.Name(), StagingPrefix) {
			if err := os.RemoveAll(filepath.Join(dataDir, e.Name())); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

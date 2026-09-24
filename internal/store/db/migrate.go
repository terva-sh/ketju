package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/terva-sh/ketju/internal/store/db/dbq"
)

// Migrations are forward-only SQL files named NNNN_name.sql, numbered from
// 0001 without gaps. Each runs in its own write transaction and is recorded in
// schema_migrations with a checksum of its text, so an edited migration is
// caught rather than silently skipped. They are sqlc's schema source too, so a
// migration that sqlc cannot parse fails `go generate` before it can ship.
//
// Two branches that each add a migration will collide on the number. The
// second to merge renumbers on rebase (settled in grooming, TKT-01M29BW9Q1).
//
//go:embed migrations/*.sql
var migrations embed.FS

// ErrSchemaNewer is returned when the database records a migration this
// binary does not have: a newer binary has run against it. Starting anyway
// would run old queries against a schema they were not written for.
var ErrSchemaNewer = errors.New("db: database schema is newer than this binary")

// ErrMigrations is returned when the embedded migrations are malformed or
// disagree with what the database recorded. It is a build or deployment
// fault, never something a retry fixes.
var ErrMigrations = errors.New("db: migrations are inconsistent")

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version  int64
	name     string
	sql      string
	checksum string
}

// Migrate brings the schema up to date with the migrations in this binary.
// Running it against an up-to-date database changes nothing.
func (d *DB) Migrate(ctx context.Context) error {
	return d.migrate(ctx, migrations, "migrations")
}

func (d *DB) migrate(ctx context.Context, fsys fs.FS, dir string) error {
	want, err := loadMigrations(fsys, dir)
	if err != nil {
		return err
	}

	// Checked and applied under the write connection, so nothing else in
	// this process can migrate between the check and the apply.
	return d.Write(ctx, func(q *dbq.Queries, tx *sql.Tx) error {
		have, err := recordedMigrations(ctx, q, tx)
		if err != nil {
			return err
		}
		if len(have) > 0 {
			newest := have[len(have)-1].Version
			if newest > int64(len(want)) {
				return fmt.Errorf("%w: the database is at schema version %d and this binary knows up to %d, "+
					"so this binary is older than the database", ErrSchemaNewer, newest, len(want))
			}
		}
		for i, h := range have {
			w := want[i]
			if h.Version != w.version || h.Name != w.name || h.Checksum != w.checksum {
				return fmt.Errorf("%w: recorded migration %04d_%s does not match the embedded %04d_%s "+
					"(a migration was edited or renumbered after it ran)", ErrMigrations, h.Version, h.Name, w.version, w.name)
			}
		}
		for _, m := range want[len(have):] {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return fmt.Errorf("db: applying migration %04d_%s: %w", m.version, m.name, err)
			}
			if err := q.InsertMigration(ctx, dbq.InsertMigrationParams{
				Version:   m.version,
				Name:      m.name,
				Checksum:  m.checksum,
				CreatedAt: time.Now().UnixMilli(),
			}); err != nil {
				return fmt.Errorf("db: recording migration %04d_%s: %w", m.version, m.name, err)
			}
		}
		return nil
	})
}

// recordedMigrations lists what the database has applied, or nothing if it
// has never been migrated. The existence probe is hand-written because it
// reads sqlite_schema, which is not part of the schema sqlc compiles against.
func recordedMigrations(ctx context.Context, q *dbq.Queries, tx *sql.Tx) ([]dbq.SchemaMigration, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_schema WHERE type = 'table' AND name = 'schema_migrations'`).Scan(&n)
	if err != nil || n == 0 {
		return nil, err
	}
	return q.ListMigrations(ctx)
}

// loadMigrations reads dir, checks the names, and returns the migrations in
// order. Every file in dir must be a migration: a stray file is more likely a
// misnamed migration than something meant to be ignored.
func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("db: reading migrations: %w", err)
	}
	var out []migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			return nil, fmt.Errorf("%w: %s is not named NNNN_name.sql", ErrMigrations, e.Name())
		}
		body, err := fs.ReadFile(fsys, path.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("db: reading %s: %w", e.Name(), err)
		}
		v, _ := strconv.ParseInt(m[1], 10, 64)
		sum := sha256.Sum256(body)
		out = append(out, migration{version: v, name: m[2], sql: string(body), checksum: hex.EncodeToString(sum[:])})
	}
	slices.SortFunc(out, func(a, b migration) int { return int(a.version - b.version) })
	for i, m := range out {
		if m.version != int64(i+1) {
			if i > 0 && m.version == out[i-1].version {
				return nil, fmt.Errorf("%w: two migrations are numbered %04d", ErrMigrations, m.version)
			}
			return nil, fmt.Errorf("%w: expected migration %04d, found %04d_%s", ErrMigrations, i+1, m.version, m.name)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no migrations found", ErrMigrations)
	}
	return out, nil
}

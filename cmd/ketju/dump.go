package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/terva-sh/ketju/internal/store/threadid"
	"github.com/terva-sh/ketju/internal/store/threadlog"
)

// dumpLine is one output record. Every field is named rather than positional,
// because this output is read by people at three in the morning and piped into
// jq by the same people ten minutes later.
type dumpLine struct {
	Offset   int64  `json:"offset"`
	Size     int    `json:"size,omitempty"`
	Type     uint8  `json:"type"`
	TypeName string `json:"type_name"`

	Encoding uint8  `json:"encoding"`
	Flags    uint16 `json:"flags"`
	Ordinal  uint32 `json:"ordinal,omitempty"`
	Revision uint32 `json:"revision,omitempty"`
	TimeMS   int64  `json:"time_ms"`
	Actor    uint64 `json:"actor"`

	// Body is the decoded record. Absent when the body could not be decoded,
	// in which case BodyRaw carries the bytes and BodyError says why.
	Body      any    `json:"body,omitempty"`
	BodyRaw   string `json:"body_raw,omitempty"`
	BodyError string `json:"body_error,omitempty"`

	// Error is set for a damaged frame. The line is still printed: a dump that
	// stops at the first bad record is useless for diagnosing a bad record.
	Error string `json:"error,omitempty"`
	Fatal bool   `json:"fatal,omitempty"`
}

func runDump(args []string) error { return dump(os.Stdout, args) }

func dump(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("dump", flag.ContinueOnError)
	root := fs.String("root", "", "data directory, when naming a thread by id rather than by path")
	rawBodies := fs.Bool("raw", false, "include the undecoded body bytes on every line, base64")
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), `usage: ketju dump [--root DIR] <thread-id | path/to/thread.log>

Prints one JSON object per record. Reads the log directly: it does not open the
database and does not consult the index, which is what makes it useful when one
of those is the thing that is wrong.

A record failing its checksum is reported on its own line and the walk
continues. The scan stops only at damage that makes the next record
unfindable — a length out of range, or one running past the end of the file.

`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return errUsage{err}
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errUsage{errors.New("expected exactly one thread id or log path")}
	}

	path, err := resolveLogPath(fs.Arg(0), *root)
	if err != nil {
		return err
	}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}

	out := bufio.NewWriter(w)
	defer out.Flush()
	enc := json.NewEncoder(out)

	var damaged int
	scanErr := threadlog.ScanFrames(f, fi.Size(), func(rec threadlog.ScanRecord) bool {
		line := dumpLine{
			Offset: rec.Offset, Size: rec.Size,
			Type: uint8(rec.Header.Type), TypeName: rec.Header.Type.String(),
			Encoding: uint8(rec.Header.Encoding), Flags: uint16(rec.Header.Flags),
			Ordinal: rec.Header.Ordinal, Revision: rec.Header.Revision,
			TimeMS: rec.Header.TimestampMS, Actor: rec.Header.Actor,
		}
		if rec.Err != nil {
			damaged++
			line.Error = rec.Err.Error()
			line.Fatal = rec.Fatal
		} else {
			if v, err := threadlog.DecodeBody(rec.Header.Type, rec.Header.Encoding, rec.Body); err != nil {
				line.BodyError = err.Error()
				line.BodyRaw = base64.StdEncoding.EncodeToString(rec.Body)
			} else {
				line.Body = v
				if *rawBodies {
					line.BodyRaw = base64.StdEncoding.EncodeToString(rec.Body)
				}
			}
		}
		if err := enc.Encode(line); err != nil {
			return false
		}
		return true
	})
	if scanErr != nil {
		return scanErr
	}
	if err := out.Flush(); err != nil {
		return err
	}
	if damaged > 0 {
		return errCorrupt{fmt.Errorf("%d damaged record(s) in %s", damaged, path)}
	}
	return nil
}

// resolveLogPath accepts either a path to a log or a thread id with a root.
//
// Deciding between them by looking at the string rather than asking for a flag:
// a base32 id is thirteen characters with no separator and no extension, and
// anything else is a path. Getting that wrong is cheap — the error says which
// way it was read.
func resolveLogPath(arg, root string) (string, error) {
	looksLikeID := !strings.ContainsRune(arg, filepath.Separator) &&
		filepath.Ext(arg) == "" &&
		len(arg) == threadid.EncodedLen

	if !looksLikeID {
		return arg, nil
	}
	id, err := threadid.Parse(arg)
	if err != nil {
		return "", fmt.Errorf("%q is %d characters with no path separator, so it was read as a thread id: %w",
			arg, len(arg), err)
	}
	if root == "" {
		return "", errUsage{fmt.Errorf("%q is a thread id, so --root is needed to find its log", arg)}
	}
	return id.LogPath(root), nil
}

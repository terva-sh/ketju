package threadid

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WalkLogs lists every log under root's threads directory with its size,
// from the directory entries alone, following the two-level shard layout
// (Dir). A file that is not a log or index at the path its id names is a
// stray, which is how a log in the wrong shard is caught. No threads
// directory at all is a data directory with no threads yet.
//
// The projection's catch-up and reindex walk with it, and so does the
// search indexer's catch-up (TKT-01M29CRJ3B).
func WalkLogs(root string) (map[ID]int64, []string, error) {
	top := filepath.Join(root, ThreadsDir)
	logs := map[ID]int64{}
	var strays []string

	read := func(dir string) ([]fs.DirEntry, error) {
		es, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", dir, err)
		}
		return es, nil
	}
	l1, err := os.ReadDir(top)
	if errors.Is(err, fs.ErrNotExist) {
		return logs, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("reading %s: %w", top, err)
	}
	for _, a := range l1 {
		pa := filepath.Join(top, a.Name())
		if !a.IsDir() {
			strays = append(strays, pa)
			continue
		}
		l2, err := read(pa)
		if err != nil {
			return nil, nil, err
		}
		for _, b := range l2 {
			pb := filepath.Join(pa, b.Name())
			if !b.IsDir() {
				strays = append(strays, pb)
				continue
			}
			files, err := read(pb)
			if err != nil {
				return nil, nil, err
			}
			for _, f := range files {
				path := filepath.Join(pb, f.Name())
				id, err := FromLogPath(path)
				// FromLogPath refuses a name that is not an id and a file in a
				// shard its id does not name, for an index as for a log
				// (review 448). A valid index is not a log, and is skipped.
				switch {
				case err != nil || f.IsDir():
					strays = append(strays, path)
					continue
				case filepath.Ext(path) == IndexExt:
					continue
				}
				info, err := f.Info()
				if err != nil {
					return nil, nil, fmt.Errorf("reading the size of %s: %w", path, err)
				}
				logs[id] = info.Size()
			}
		}
	}
	return logs, strays, nil
}

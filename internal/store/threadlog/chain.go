package threadlog

import (
	"fmt"
	"slices"
)

// ChainRecord is one record of a post's chain as the log holds it: its Post
// or one of its Edits, with the frame it came in. A move copies chains into
// its target (docs/03 "How a move is written"): each copied frame keeps the
// original's actor, timestamp, revision and flags, so the header is the
// original's, and the body is copied byte for byte but for reply_to.
type ChainRecord struct {
	Header FrameHeader
	Post   *PostBody // set for the Post
	Edit   *EditBody // set for an Edit
}

// Chains returns the Post and every Edit of each post in ordinals, in log
// order, reading the log up to upTo bytes. A post with no record in that
// range is absent from the map. Deletes and Restores are not chain records:
// only posts shown as themselves move, so none is copied.
//
// Like History it walks the whole log, once for all the posts: a move is a
// moderator's deliberate act, and nothing on the page path calls it.
func (r *Reader) Chains(ordinals []uint32, upTo int64) (map[uint32][]ChainRecord, error) {
	want := slices.Clone(ordinals)
	slices.Sort(want)
	out := make(map[uint32][]ChainRecord, len(want))
	var scanErr error
	err := ScanFramesFrom(r.log, FileHeaderSize, upTo, func(rec ScanRecord) bool {
		h := rec.Header
		if rec.Err != nil {
			scanErr = &ReadError{Ordinal: h.Ordinal, Offset: uint64(rec.Offset), Err: rec.Err}
			return false
		}
		if h.Type != TypePost && h.Type != TypeEdit {
			return true
		}
		if _, ok := slices.BinarySearch(want, h.Ordinal); !ok {
			return true
		}
		v, err := DecodeBody(h.Type, h.Encoding, rec.Body)
		if err != nil {
			scanErr = &ReadError{Ordinal: h.Ordinal, Offset: uint64(rec.Offset), Err: err}
			return false
		}
		c := ChainRecord{Header: h}
		switch b := v.(type) {
		case *PostBody:
			c.Post = b
		case *EditBody:
			c.Edit = b
		}
		out[h.Ordinal] = append(out[h.Ordinal], c)
		return true
	})
	if err == nil {
		err = scanErr
	}
	return out, err
}

// RecordAt reads the one frame that starts at offset, in a log read up to
// upTo bytes, and decodes its body. A move's finisher reads the source's
// PostsSplitOut this way, from the offset its projection recorded. An
// offset that is not the start of a frame is ErrMalformed, or a checksum
// error, as the scan finds it.
func (r *Reader) RecordAt(offset, upTo int64) (FrameHeader, any, error) {
	var (
		h    FrameHeader
		body any
		err  error
		seen bool
	)
	scanErr := ScanFramesFrom(r.log, offset, upTo, func(rec ScanRecord) bool {
		seen, h = true, rec.Header
		if rec.Err != nil {
			err = &ReadError{Ordinal: h.Ordinal, Offset: uint64(rec.Offset), Err: rec.Err}
			return false
		}
		body, err = DecodeBody(h.Type, h.Encoding, rec.Body)
		return false
	})
	if scanErr != nil {
		return h, nil, scanErr
	}
	if !seen {
		return h, nil, fmt.Errorf("%w: no frame starts at %d in a %d-byte log", ErrMalformed, offset, upTo)
	}
	return h, body, err
}

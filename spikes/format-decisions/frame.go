package format

import (
	"encoding/binary"
	"hash/crc32"
)

// The docs/03 record frame. Fields occupy bytes 0..31, the body starts at 32,
// and a CRC32C over bytes 0..4+L-1 follows the body. L counts the fixed part
// after the length field plus the body, so L = 28 + len(body).
const FrameFixed = 32

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// EncodeFrame builds one complete frame: length, fixed header, body, CRC.
func EncodeFrame(typ, enc byte, flags uint16, ordinal, revision uint32, ts int64, actor uint64, body []byte) []byte {
	l := uint32(FrameFixed - 4 + len(body))
	buf := make([]byte, 4+int(l)+4)
	binary.LittleEndian.PutUint32(buf[0:], l)
	buf[4] = typ
	buf[5] = enc
	binary.LittleEndian.PutUint16(buf[6:], flags)
	binary.LittleEndian.PutUint32(buf[8:], ordinal)
	binary.LittleEndian.PutUint32(buf[12:], revision)
	binary.LittleEndian.PutUint64(buf[16:], uint64(ts))
	binary.LittleEndian.PutUint64(buf[24:], actor)
	copy(buf[32:], body)
	binary.LittleEndian.PutUint32(buf[4+l:], crc32.Checksum(buf[:4+l], castagnoli))
	return buf
}

// AuthorAt decodes the actor field from a 32-byte frame prefix. This is the
// read a 16-byte index entry forces for anything the entry does not carry.
func AuthorAt(prefix []byte) uint64 {
	return binary.LittleEndian.Uint64(prefix[24:32])
}

// Entry40 is the docs/03 index entry: offset, length, reply_to, author,
// created_at, revision, flags, reserved.
type Entry40 struct {
	Offset   int64
	Length   uint32
	ReplyTo  uint32
	Author   uint64
	Created  int64
	Revision uint16
	Flags    uint16
	_        uint32
}

// Entry16 is the offset-only alternative.
type Entry16 struct {
	Offset int64
	Length uint32
	_      uint32
}

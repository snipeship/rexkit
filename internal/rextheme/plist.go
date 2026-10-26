package rextheme

import (
	"bytes"
	"encoding/binary"
	"errors"
	"unicode/utf16"
)

// errPlist is any binary property list rexkit cannot read.
var errPlist = errors.New("rextheme: not a binary property list it can read")

// plistStrings reads the string values of the named top-level keys of a
// binary property list (bplist00, what `defaults` writes); a data value
// comes back as its bytes. Keys that are missing or hold something else are
// left out. It reads only
// what it needs: the top dictionary's keys and the requested values.
//
// Running `defaults read` would do, but costs about 7 ms a key; this reads
// Rex's 20 KB preferences in well under one.
func plistStrings(data []byte, keys ...string) (map[string]string, error) {
	p, err := newPlist(data)
	if err != nil {
		return nil, err
	}
	top, err := p.object(p.top)
	if err != nil {
		return nil, err
	}
	marker := data[top]
	if marker>>4 != 0xd {
		return nil, errPlist
	}
	n, at, err := p.count(top)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	out := map[string]string{}
	for i := range n {
		key, err := p.stringRef(at + i*p.refSize)
		if err != nil || !want[key] {
			continue
		}
		if v, err := p.stringRef(at + (n+i)*p.refSize); err == nil {
			out[key] = v
		}
	}
	return out, nil
}

type plist struct {
	data              []byte
	offSize, refSize  int
	objects, top      uint64
	offsetTable, size int
}

func newPlist(data []byte) (*plist, error) {
	if len(data) < 8+32 || !bytes.HasPrefix(data, []byte("bplist00")) {
		return nil, errPlist
	}
	tr := data[len(data)-32:]
	p := &plist{
		data:        data,
		offSize:     int(tr[6]),
		refSize:     int(tr[7]),
		objects:     binary.BigEndian.Uint64(tr[8:]),
		top:         binary.BigEndian.Uint64(tr[16:]),
		offsetTable: int(binary.BigEndian.Uint64(tr[24:])),
		size:        len(data) - 32,
	}
	if p.offSize < 1 || p.offSize > 8 || p.refSize < 1 || p.refSize > 8 ||
		p.offsetTable < 8 || uint64(p.offsetTable)+p.objects*uint64(p.offSize) > uint64(p.size) {
		return nil, errPlist
	}
	return p, nil
}

// uint reads a big-endian unsigned integer of n bytes at off.
func (p *plist) uint(off, n int) (uint64, error) {
	if off < 0 || n < 1 || n > 8 || off+n > p.size {
		return 0, errPlist
	}
	var v uint64
	for _, b := range p.data[off : off+n] {
		v = v<<8 | uint64(b)
	}
	return v, nil
}

// object returns the offset of object ref.
func (p *plist) object(ref uint64) (int, error) {
	if ref >= p.objects {
		return 0, errPlist
	}
	off, err := p.uint(p.offsetTable+int(ref)*p.offSize, p.offSize)
	if err != nil || off < 8 || off >= uint64(p.size) {
		return 0, errPlist
	}
	return int(off), nil
}

// count reads the length of the object at off (its low nibble, or the int
// object that follows when that is 0xf) and where its payload starts.
func (p *plist) count(off int) (n, payload int, err error) {
	n = int(p.data[off] & 0xf)
	payload = off + 1
	if n != 0xf {
		return n, payload, nil
	}
	if payload >= p.size || p.data[payload]>>4 != 0x1 {
		return 0, 0, errPlist
	}
	width := 1 << (p.data[payload] & 0xf)
	v, err := p.uint(payload+1, width)
	if err != nil || v > uint64(p.size) {
		return 0, 0, errPlist
	}
	return int(v), payload + 1 + width, nil
}

// stringRef reads the object reference at off and returns it as a string
// (a data object's bytes as they are).
func (p *plist) stringRef(off int) (string, error) {
	ref, err := p.uint(off, p.refSize)
	if err != nil {
		return "", err
	}
	obj, err := p.object(ref)
	if err != nil {
		return "", err
	}
	n, at, err := p.count(obj)
	if err != nil {
		return "", err
	}
	switch p.data[obj] >> 4 {
	case 0x4, 0x5: // data, ASCII
		if at+n > p.size {
			return "", errPlist
		}
		return string(p.data[at : at+n]), nil
	case 0x6: // UTF-16BE
		if at+2*n > p.size {
			return "", errPlist
		}
		units := make([]uint16, n)
		for i := range units {
			units[i] = binary.BigEndian.Uint16(p.data[at+2*i:])
		}
		return string(utf16.Decode(units)), nil
	}
	return "", errPlist
}

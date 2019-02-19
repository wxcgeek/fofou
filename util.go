// This code is under BSD license. See license-bsd.txt
package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"unicode/utf8"
)

func panicif(cond bool, args ...interface{}) {
	if !cond {
		return
	}

	var msg string
	s, ok := args[0].(string)
	if ok {
		msg = s
		if len(s) > 1 {
			msg = fmt.Sprintf(msg, args[1:]...)
		}
	} else {
		msg = fmt.Sprintf("%v", args[0])
	}
	panic(msg)
}

func httpErrorf(w http.ResponseWriter, format string, args ...interface{}) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	http.Error(w, msg, http.StatusBadRequest)
}

func bytesToPlane0String(buf []byte) string {
	str := make([]byte, 0, len(buf))
	enc := make([]byte, 3)

	for i := 0; i < len(buf); {
		if buf[i] < 128 {
			str = append(str, buf[i])
			i++
			continue
		}

		ln := 2 * int((buf[i]&0x7f)+1)
		if i+1+ln > len(buf) {
			return ""
		}

		for j := i + 1; j < i+1+ln; j += 2 {
			n := utf8.EncodeRune(enc, rune(buf[j])<<8+rune(buf[j+1]))
			str = append(str, enc[:n]...)
		}

		i += 1 + ln
	}

	return string(str)
}

func plane0StringToBytes(str string) []byte {
	buf := make([]byte, 0, len(str))
	queue := make([]byte, 0, 256)

	appendQueue := func() {
		buf = append(buf, byte(len(queue)/2-1)|0x80)
		buf = append(buf, queue...)
		queue = queue[:0]
	}

	for _, r := range str {
		if r < 128 {
			if len(queue) > 0 {
				appendQueue()
			}
			buf = append(buf, byte(r))
		} else {
			queue = append(queue, byte(r>>8), byte(r))
			if len(queue)/2 == 128 {
				appendQueue()
			}
		}
	}

	if len(queue) > 0 {
		appendQueue()
	}

	return buf
}

func ipAddrToInternal(ipAddr string) (v [8]byte) {
	ip := net.ParseIP(ipAddr)
	if len(ip) == 0 {
		return
	}
	ipv4 := ip.To4()
	if len(ipv4) == 0 {
		copy(v[:], ip)
		return
	}
	copy(v[:], ipv4[:3])
	return
}

func ipAddrInternalToOriginal(s string) string {
	switch len(s) {
	case 8:
		if d, err := hex.DecodeString(s); err == nil {
			return fmt.Sprintf("%d.%d.%d.%d", d[0], d[1], d[2], d[3])
		}
	case 6:
		if d, err := hex.DecodeString(s); err == nil {
			return fmt.Sprintf("%d.%d.%d.0/24", d[0], d[1], d[2])
		}
	case 4:
		if d, err := hex.DecodeString(s); err == nil {
			return fmt.Sprintf("%d.%d.0.0/16", d[0], d[1])
		}
	case 5:
		if d, err := hex.DecodeString(s + "0"); err == nil {
			return fmt.Sprintf("%d.%d.%d.0/20", d[0], d[1], d[2])
		}
	}
	// other format (ipv6?)
	return s
}

type buffer struct {
	p      bytes.Buffer
	r      io.Reader
	pos    int
	postmp int
	one    [1]byte
}

func (b *buffer) Bytes() []byte {
	return b.p.Bytes()
}

func (b *buffer) SetReader(r io.Reader) {
	b.r = r
	b.pos = 0
	b.postmp = 0
}

func (b *buffer) ReadByte() (byte, error) {
	_, err := b.r.Read(b.one[:1])
	if err != nil {
		return 0, err
	}
	b.pos++
	return b.one[0], nil
}

func (b *buffer) Read2Bytes() (byte, byte, error) {
	v0, err := b.ReadByte()
	v1, err := b.ReadByte()
	if err != nil {
		return 0, 0, err
	}
	return v0, v1, nil
}

func (b *buffer) Write8Bytes(v [8]byte) *buffer {
	for _, s := range v {
		b.p.WriteByte(s)
	}
	return b
}

func (b *buffer) WriteUInt32(v uint32) *buffer {
	b.p.WriteByte(byte(v >> 24))
	b.p.WriteByte(byte(v >> 16))
	b.p.WriteByte(byte(v >> 8))
	b.p.WriteByte(byte(v))
	return b
}

func (b *buffer) WriteUInt16(v uint16) *buffer {
	b.p.WriteByte(byte(v >> 8))
	b.p.WriteByte(byte(v))
	return b
}

func (b *buffer) WriteByte(v byte) *buffer {
	b.p.WriteByte(v)
	return b
}

func (b *buffer) Read8Bytes() (res [8]byte, err error) {
	for i := 0; i < 8; i++ {
		res[i], err = b.ReadByte()
		if err != nil {
			return
		}
	}
	return
}

func (b *buffer) ReadUInt32() (uint32, error) {
	v0, err := b.ReadByte()
	v1, err := b.ReadByte()
	v2, err := b.ReadByte()
	v3, err := b.ReadByte()
	if err != nil {
		return 0, err
	}
	return uint32(v0)<<24 + uint32(v1)<<16 + uint32(v2)<<8 + uint32(v3), nil
}

func (b *buffer) ReadUInt16() (uint16, error) {
	v0, err := b.ReadByte()
	v1, err := b.ReadByte()
	if err != nil {
		return 0, err
	}
	return uint16(v0)<<8 + uint16(v1), nil
}

// Plane 0 only
func (b *buffer) WriteString(str string) *buffer {
	queue := make([]byte, 0, 256)

	appendQueue := func() {
		b.p.WriteByte(byte(len(queue)/2-1) | 0x80)
		b.p.Write(queue)
		queue = queue[:0]
	}

	for _, r := range str {
		if r < 128 {
			if len(queue) > 0 {
				appendQueue()
			}
			b.p.WriteByte(byte(r))
		} else {
			queue = append(queue, byte(r>>8), byte(r))
			if len(queue)/2 == 128 {
				appendQueue()
			}
		}
	}

	if len(queue) > 0 {
		appendQueue()
	}

	b.p.WriteByte(0)
	return b
}

func (b *buffer) ReadString() (string, error) {
	str := make([]byte, 0)
	enc := make([]byte, 3)

	b.postmp = b.pos
	for {
		v, err := b.ReadByte()
		if err != nil {
			return "", err
		}
		if v == 0 {
			break
		}

		if v < 128 {
			str = append(str, v)
			continue
		}

		ln := int((v & 0x7f) + 1)
		for i := 0; i < ln; i++ {
			v0, v1, err := b.Read2Bytes()
			if err != nil {
				return "", err
			}
			n := utf8.EncodeRune(enc, rune(v0)<<8+rune(v1))
			str = append(str, enc[:n]...)
		}
	}

	return string(str), nil

}

func (b *buffer) LastStringCheckpoint() int { return b.postmp }

func (b *buffer) LastByteCheckpoint() int { return b.pos }

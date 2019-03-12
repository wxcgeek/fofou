package server

import (
	"bytes"
	"io"
	"unicode/utf8"
)

type buffer struct {
	p      bytes.Buffer
	r      io.Reader
	pos    int64
	postmp int64
	one    [1]byte
}

func (b *buffer) Bytes() []byte {
	return b.p.Bytes()
}

func (b *buffer) Reset() *buffer {
	b.p.Reset()
	return b
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

func (b *buffer) ReadBool() (bool, error) {
	v, err := b.ReadByte()
	return v == 1, err
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

func (b *buffer) WriteUInt48(v uint64) *buffer {
	b.p.WriteByte(byte(v >> 40))
	b.p.WriteByte(byte(v >> 32))
	b.p.WriteByte(byte(v >> 24))
	b.p.WriteByte(byte(v >> 16))
	b.p.WriteByte(byte(v >> 8))
	b.p.WriteByte(byte(v))
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

func (b *buffer) WriteBool(v bool) *buffer {
	if v {
		b.p.WriteByte(1)
	} else {
		b.p.WriteByte(0)
	}
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

	h := byte(0)
	for _, r := range str {
		if r < 128 {
			if len(queue) > 0 {
				appendQueue()
			}
			b.p.WriteByte(byte(r))
			h = crc8(h, byte(r))
			continue
		}

		queue = append(queue, byte(r>>8), byte(r))
		if len(queue)/2 == 128 {
			appendQueue()
		}

		h = crc8(crc8(h, byte(r>>8)), byte(r))
	}

	if len(queue) > 0 {
		appendQueue()
	}

	b.p.WriteByte(0)
	b.p.WriteByte(h)
	return b
}

func (b *buffer) ReadString() (string, error) {
	str := make([]byte, 0)
	enc := make([]byte, 3)
	h := byte(0)

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
			h = crc8(h, v)
			continue
		}

		ln := int((v & 0x7f) + 1)
		for i := 0; i < ln; i++ {
			v0, v1, err := b.Read2Bytes()
			if err != nil {
				return "", err
			}
			n := utf8.EncodeRune(enc, rune(v0)<<8+rune(v1))
			h = crc8(crc8(h, v0), v1)
			str = append(str, enc[:n]...)
		}
	}

	h2, err := b.ReadByte()
	if err != nil {
		return "", err
	}

	if h != h2 {
		return "", errInvalidHash
	}

	return string(str), nil

}

func (b *buffer) LastStringCheckpoint() int { return int(b.postmp) }

func (b *buffer) LastByteCheckpoint() int { return int(b.pos) }

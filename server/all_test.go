package server

import (
	"testing"

	"github.com/coyove/common/rand"
)

var _test = true

const str = "hello world, 你好世界"

func TestBufferReadWrite(t *testing.T) {
	b := buffer{}
	b.WriteUInt32(42)
	b.WriteString(str)

	b2 := buffer{}
	b2.SetReader(&b.p)

	v, _ := b2.ReadUInt32()
	if v != 42 {
		t.FailNow()
	}

	str2, _ := b2.ReadString()
	if str2 != str {
		t.FailNow()
	}
}

func TestBufferReadWriteRandom(t *testing.T) {
	b := buffer{}
	r := rand.New()
	ln := r.Intn(1024) + 1024
	buf := make([]rune, ln)
	for i := 0; i < ln; i++ {
		buf[i] = rune(r.Intn(65535) + 1)
	}

	str := string(buf)
	b.WriteString(str)

	b2 := buffer{}
	b2.SetReader(&b.p)

	str2, _ := b2.ReadString()
	if str2 != str {
		t.FailNow()
	}
}

func TestBufferError(t *testing.T) {
	b := buffer{}
	b.WriteUInt32(42)
	b.WriteString(str)
	b.p.Bytes()[b.p.Len()-1] = 1

	b2 := buffer{}
	b2.SetReader(&b.p)
	b2.ReadUInt32()
	_, err := b2.ReadString()
	if err == nil {
		t.FailNow()
	}

	if b2.LastStringCheckpoint() != 4 {
		t.FailNow()
	}
}

func TestCRC8(t *testing.T) {
	if crc8Bytes([]byte("123456789")) != 0xf4 {
		t.FailNow()
	}
	if crc8Bytes([]byte("https://crccalc.com/")) != 0x56 {
		t.FailNow()
	}
}

func TestStringCompare(t *testing.T) {
	t.Log(stringCompare("abc defg", "abcdef", nil))
}

func TestLongID(t *testing.T) {
	makeid := func(a uint32, b uint16) uint64 {
		p := Post{ID: b, Topic: &Topic{ID: a}}
		return p.LongID()
	}

	r := rand.New()
	for i := 0; i < 10000; i++ {
		for i := 0; i < 1<<12; i++ {
			k := r.Intn(1 << 32)
			longid := makeid(uint32(k), uint16(i))
			k2, i2 := SplitID(longid)
			if k2 != uint32(k) {
				t.Fatal(k2, k)
			}
			if i2 != uint16(i) {
				t.Fatal(i2, i)
			}
		}
	}
}

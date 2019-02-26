// This code is under BSD license. See license-bsd.txt
package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
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
	copy(v[4:], ipv4[:3])
	return
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
func (b *buffer) ReadStringBytes() ([]byte, error) {
	b.postmp = b.pos
	p := bytes.Buffer{}

	for {
		v, err := b.ReadByte()
		if err != nil {
			return nil, err
		}
		p.WriteByte(v)
		if v == 0 {
			break
		}
	}

	return p.Bytes(), nil

}

func (b *buffer) LastStringCheckpoint() int { return b.postmp }

func (b *buffer) LastByteCheckpoint() int { return b.pos }

func IPAddress(ip [8]byte) string {
	buf := bytes.Buffer{}
	for i := 0; i < len(ip); i += 2 {
		buf.WriteString(fmt.Sprintf("%x:", int(ip[i])*256+int(ip[i+1])))
	}
	buf.Truncate(buf.Len() - 1)
	return buf.String()
}

const _salt = "testsalt123456"
const _password = "testpassword"

func getSecureCookie(r *http.Request) string {
	uid, err := r.Cookie("uid")
	if err != nil {
		return ""
	}

	parts := strings.Split(uid.Value, "|")
	if len(parts) != 2 {
		return ""
	}

	if strings.HasPrefix(parts[0], "a:") {
		if parts[1] == _password {
			return parts[0]
		}
	}

	x := sha256.Sum256([]byte(parts[0] + _salt))
	for i := 0; i < 16; i++ {
		x = sha256.Sum256(x[:])
	}

	if fmt.Sprintf("%x", x) == parts[1] {
		return parts[0]
	}

	return ""
}

func setSecureCookie(w http.ResponseWriter, username string) {
	x := sha256.Sum256([]byte(username + _salt))
	for i := 0; i < 16; i++ {
		x = sha256.Sum256(x[:])
	}

	v := username + "|" + fmt.Sprintf("%x", x)
	if strings.HasPrefix(username, "a:") {
		v = username + "|" + _password
	}

	cookie := &http.Cookie{
		Name:    "uid",
		Value:   v,
		Path:    "/",
		Expires: time.Now().AddDate(1, 0, 0),
	}
	http.SetCookie(w, cookie)
}

func adminOpCode(forum *Forum, msg string) bool {
	if !strings.HasPrefix(msg, "!!") {
		return false
	}
	msg = msg[2:]
	parts := strings.Split(msg, "=")
	if len(parts) != 2 {
		return false
	}

	v := parts[1]
	vint, _ := strconv.ParseInt(v, 10, 64)
	switch parts[0] {
	case "cookie-moat":
		forum.NoMoreNewUsers = v == "true"
		return true
	case "delete":
		topicID, postID := uint32(vint>>16), uint16(vint)
		forum.Store.DeletePost(topicID, postID)
		return true
	case "stick":
		forum.Store.OperateTopic(uint32(vint), OP_STICKY)
		return true
	case "lock":
		forum.Store.OperateTopic(uint32(vint), OP_LOCK)
		return true
	case "purge":
		forum.Store.OperateTopic(uint32(vint), OP_PURGE)
		return true
	case "free-reply":
		forum.Store.OperateTopic(uint32(vint), OP_FREEREPLY)
		return true
	case "block-ip":
		forum.Store.BlockIP(v)
		return true
	case "block-user":
		forum.Store.BlockUser(v)
		return true
	}
	return false
}

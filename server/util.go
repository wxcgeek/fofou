// This code is under BSD license. See license-bsd.txt
package server

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	stdTimeFormat  = "2006-01-02 15:04:05"
	urlRx          = regexp.MustCompile(`(https?://[[:^space:]]+|<|\n| |` + "```[\\s\\S]+```" + `)`)
	base32Encoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding('1')
	default8Bytes  = [8]byte{}
)

var (
	PostsPerPage int = 20
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

func Format8Bytes(b [8]byte) (string, string) {
	buf, bufid := bytes.Buffer{}, bytes.Buffer{}

	if b[0] == 0 && b[1] == 0 && b[2] == 0 && b[3] == 0 && b[7] == 0 {
		buf.WriteString(fmt.Sprintf("%d.%d.%d.", b[4], b[5], b[6]))
	} else {
		for i := 0; i < len(b); i += 2 {
			buf.WriteString(fmt.Sprintf("%x:", int(b[i])*256+int(b[i+1])))
		}
	}
	buf.WriteString("x")

	if b[0] == 'a' && b[1] == ':' {
		bufid.WriteString(string(b[:]))
	} else {
		base64.NewEncoder(base64.URLEncoding, &bufid).Write(b[:6])
	}
	return buf.String(), bufid.String()
}

func Parse8Bytes(str string) (b [8]byte) {
	if strings.HasSuffix(str, ".x") {
		parts := strings.Split(str, ".")
		if len(parts) == 4 {
			first := func(a int64, e error) byte { return byte(a) }
			b[4] = first(strconv.ParseInt(parts[0], 10, 8))
			b[5] = first(strconv.ParseInt(parts[1], 10, 8))
			b[6] = first(strconv.ParseInt(parts[2], 10, 8))
		}
		return
	}
	if strings.HasSuffix(str, ":x") {
		parts := strings.Split(str, ":")
		if len(parts) == 4 {
			first := func(a uint64, e error) (byte, byte) { return byte(a >> 8), byte(a) }
			b[0], b[1] = first(strconv.ParseUint(parts[0], 10, 64))
			b[2], b[3] = first(strconv.ParseUint(parts[1], 10, 64))
			b[4], b[5] = first(strconv.ParseUint(parts[2], 10, 64))
			b[6], b[7] = first(strconv.ParseUint(parts[3], 10, 64))
		}
		return
	}
	if strings.HasPrefix(str, "a:") {
		copy(b[:], str)
		return
	}
	base64.URLEncoding.Decode(b[:], []byte(str))
	return
}

const _salt = "testsalt123456"

func GetUser(r *http.Request) User {
	uid, err := r.Cookie("uid")
	if err != nil {
		return User{}
	}

	if len(uid.Value) < 48 {
		return User{}
	}

	user := uid.Value[:len(uid.Value)-48]
	hash := uid.Value[len(uid.Value)-48:]

	x := sha256.Sum256([]byte(user + _salt))
	for i := 0; i < 16; i++ {
		x = sha256.Sum256(x[:])
	}

	if base32Encoding.EncodeToString(x[:30]) != hash {
		return User{}
	}

	u := User{}
	buf, _ := base32Encoding.DecodeString(user)
	json.Unmarshal(buf, &u)

	{
		x, n := u.Posts, u.N
		if n >= 5 && n <= 20 {
			// tan((y - 0.5 - 0.01) * pi) = n - x
			// if x < n, then there is a high chance that this user needs a test (recaptcha)
			p := math.Atan(float64(n-x))/math.Pi + 0.5 + 0.01
			u.noTest = rand.New(rand.NewSource(time.Now().UnixNano())).Float64() >= p
		}
	}

	if u.IsAdmin() {
		u.noTest = true
	}
	return u
}

func SetUser(w http.ResponseWriter, u User) {
	u.Posts++
	buf, _ := json.Marshal(&u)
	user := base32Encoding.EncodeToString(buf)

	x := sha256.Sum256([]byte(user + _salt))
	for i := 0; i < 16; i++ {
		x = sha256.Sum256(x[:])
	}

	cookie := &http.Cookie{
		Name:    "uid",
		Value:   user + base32Encoding.EncodeToString(x[:30]),
		Path:    "/",
		Expires: time.Now().AddDate(1, 0, 0),
	}

	if w != nil {
		http.SetCookie(w, cookie)
	} else {
		fmt.Println(cookie.Value)
	}
}

func AdminOPCode(forum *Forum, msg string) bool {
	r := bufio.NewReader(strings.NewReader(msg))
	opcode := false
	for {
		line, _, err := r.ReadLine()
		if err != nil {
			break
		}

		msg := string(line)
		if !strings.HasPrefix(msg, "!!") {
			break
		}

		parts := strings.Split(msg[2:], "=")
		if len(parts) != 2 {
			break
		}

		v := parts[1]
		vint, _ := strconv.ParseInt(v, 10, 64)
		switch parts[0] {
		case "moat":
			switch v {
			case "cookie":
				forum.NoMoreNewUsers = !forum.NoMoreNewUsers
			case "image":
				forum.NoImageUpload = !forum.NoImageUpload
			}
			opcode = true
		case "delete":
			forum.Store.DeletePost(uint64(vint), func(img string) { os.Remove("data/images/" + img) })
			opcode = true
		case "stick":
			forum.Store.OperateTopic(uint32(vint), OP_STICKY)
			opcode = true
		case "lock":
			forum.Store.OperateTopic(uint32(vint), OP_LOCK)
			opcode = true
		case "purge":
			forum.Store.OperateTopic(uint32(vint), OP_PURGE)
			opcode = true
		case "free-reply":
			forum.Store.OperateTopic(uint32(vint), OP_FREEREPLY)
			opcode = true
		case "sage":
			forum.Store.OperateTopic(uint32(vint), OP_SAGE)
			opcode = true
		case "block":
			forum.Store.Block(Parse8Bytes(v))
			opcode = true
		}
	}

	return opcode
}

// returns 5 ~ 20
//func weightMessage(store *Store, msg string) int {
//	ln := 0
//	s := 0
//	for _, r := range msg {
//		if r < 128 {
//			ln++
//			s++
//		} else {
//			ln += 2
//			if r < 0x2e00 {
//				s += 2
//			}
//		}
//	}
//
//	factor := 1.0
//	if s >= ln*3/4 {
//		factor = 2.0
//	}
//}

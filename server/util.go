// This code is under BSD license. See license-bsd.txt
package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

var (
	stdTimeFormat  = "2006-01-02 15:04:05"
	urlRx          = regexp.MustCompile(`(https?://[[:^space:]]+|<|\n| |` + "```[\\s\\S]+```" + `|>>\d+)`)
	base32Encoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding('1')
	default8Bytes  = [8]byte{}
	errInvalidHash = fmt.Errorf("corrupted string hash")
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

	if b[0] == '^' {
		idx := bytes.IndexByte(b[:], 0)
		if idx == -1 {
			idx = 8
		}
		bufid.WriteString(string(b[:idx]))
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
	if strings.HasPrefix(str, "^") {
		copy(b[:], str)
		return
	}
	base64.URLEncoding.Decode(b[:], []byte(str))
	return
}

func (f *Forum) GetUser(r *http.Request) User {
	uid, err := r.Cookie("uid")
	if err != nil {
		return User{}
	}

	u := User{}
	bufp := &SafeJSON{Buffer: bytes.NewBuffer([]byte(uid.Value))}
	if err := json.NewDecoder(bufp).Decode(&u); err != nil {
		return User{}
	}

	user := [userStructSize + 16]byte{}
	copy(user[:], (*(*[userStructSize]byte)(unsafe.Pointer(&u)))[:])
	copy(user[userStructSize:], f.Salt[:])

	x := sha256.Sum256(user[:])
	for i := 0; i < 16; i++ {
		x = sha256.Sum256(x[:])
	}

	if base32Encoding.EncodeToString(x[:30]) != u.Hash {
		return User{}
	}

	return u
}

func (u User) PassRoll() bool {
	x, n := u.Posts, u.N
	if n >= 5 && n <= 20 {
		// tan((y - 0.5 - 0.01) * pi) = n - x
		// if x < n, then there is a high chance that this user needs a test (recaptcha)
		p := math.Atan(float64(n-x))/math.Pi + 0.5 + 0.01
		return rand.New(rand.NewSource(time.Now().UnixNano())).Float64() >= p
	}
	return false
}

func (f *Forum) SetUser(w http.ResponseWriter, u User) string {
	u.Posts++
	u.T = time.Now().Unix()

	user := [userStructSize + 16]byte{}
	copy(user[:], (*(*[userStructSize]byte)(unsafe.Pointer(&u)))[:])
	copy(user[userStructSize:], f.Salt[:])

	x := sha256.Sum256(user[:])
	for i := 0; i < 16; i++ {
		x = sha256.Sum256(x[:])
	}
	u.Hash = base32Encoding.EncodeToString(x[:30])

	bufp := &SafeJSON{Buffer: &bytes.Buffer{}}
	json.NewEncoder(bufp).Encode(&u)

	cookie := &http.Cookie{
		Name:    "uid",
		Value:   bufp.String(),
		Path:    "/",
		Expires: time.Now().AddDate(1, 0, 0),
	}

	if w != nil {
		http.SetCookie(w, cookie)
	} else {
		fmt.Println(cookie.Value)
	}

	return cookie.Value
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

func stringCompare(s1 string, s2 string, m []uint32) (bool, []uint32) {
	if m == nil {
		m = make([]uint32, 0, len(s2))
		lastr := uint16(0)
		for i, r := range s2 {
			if r == ' ' {
				continue
			}

			if r >= 0x41 && r <= 0x5a { // A-Z
				r += 0x20 // a-z
			}

			if i > 0 {
				m = append(m, uint32(lastr)<<16+uint32(uint16(r)))
			}

			if len(m) > 128 {
				break
			}

			lastr = uint16(r)
		}
		sort.Slice(m, func(i, j int) bool { return m[i] < m[j] })
	}

	if len(m) == 0 {
		return false, m
	}

	lastr := uint16(0)
	score := 0
	for i, r := range s1 {
		if r >= 0x41 && r <= 0x5a { // A-Z
			r += 0x20 // a-z
		}

		if i > 0 {
			x := uint32(lastr)<<16 + uint32(uint16(r))
			xi := sort.Search(len(m), func(i int) bool { return m[i] >= x })
			if xi < len(m) && m[xi] == x {
				score++
				if score >= len(m)/2+1 {
					return true, m
				}
			}
		}
		lastr = uint16(r)
	}

	return false, m
}

func (f *Forum) UUID() (v [16]byte, s string) {
	f.Rand.Read(v[:])
	v[8] = (v[8] | 0x80) & 0xBF
	v[6] = (v[6] | 0x40) & 0x4F
	s = hex.EncodeToString(v[:])
	return
}

func DecodeUUID(s string) (v [16]byte) {
	if len(s) != 32 {
		return
	}
	hex.Decode(v[:], *(*[]byte)(unsafe.Pointer(&s)))
	return
}

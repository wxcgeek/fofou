package server

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	POST_ISFIRST = 1 << iota
	POST_ISREF
)

type Image struct {
	Path string
	Name string
	Size uint32
	X    uint16
	Y    uint16
}

type Post struct {
	Message   string
	Image     *Image
	user      [8]byte
	ip        [8]byte
	CreatedAt uint32
	ID        uint16
	IsDeleted bool
	T_Status  byte
	Topic     *Topic
}

func (p *Post) T_SetStatus(v byte) { p.T_Status |= v }

func (p *Post) T_IsFirst() bool { return p.T_Status&POST_ISFIRST > 0 }

func (p *Post) T_IsRef() bool { return p.T_Status&POST_ISREF > 0 }

func (p *Post) Date() string { return time.Unix(int64(p.CreatedAt), 0).Format(stdTimeFormat) }

func (p *Post) MessageHTML() string {
	return urlRx.ReplaceAllStringFunc(p.Message, func(in string) string {
		switch in {
		case " ":
			return "&nbsp;"
		case "\n":
			return "<br>"
		case "<":
			return "&lt;"
		default:
			if strings.HasPrefix(in, "```") {
				return "<code>" + strings.Replace(in[3:len(in)-3], "<", "&lt;", -1) + "</code>"
			} else if strings.HasPrefix(in, ">>") {
				old := in
				if strings.HasPrefix(in, ">>#") {
					in = in[3:]
				} else if strings.HasPrefix(in, ">>No.") {
					in = in[5:]
				} else {
					in = in[2:]
				}
				longid, _ := strconv.Atoi(in)
				return fmt.Sprintf("<a href='javascript:void(0)' onclick='_ref(this, %d)'>%s</a>", longid, old)
			} else {
				return "<a href='" + in + "' target=_blank>" + in + "</a>"
			}
		}
	})
}

func (p *Post) IPXor() [8]byte {
	x := p.ip
	iv := [16]byte{}
	copy(iv[:], p.user[:])
	binary.BigEndian.PutUint64(iv[8:], p.LongID())
	p.Topic.store.block.Encrypt(iv[:], iv[:])
	for i := 0; i < len(x); i++ {
		x[i] ^= iv[i]
	}
	return x
}

func (p *Post) IP() string {
	i, _ := Format8Bytes(p.IPXor())
	return i
}

func (p *Post) LongID() uint64 {
	if p.ID >= 1<<12 || p.ID == 0 {
		panic("invalid post ID")
	}

	ti, pi := uint64(p.Topic.ID), uint64(p.ID-1)
	if pi < 4 {
		// ti(26) | 0 | ti(4) | 0 | ti(2) | pi(2)
		return ti>>6<<10 + (ti>>2&0xf)<<5 + (ti&0x3)<<2 + pi
	}
	if pi < 16 {
		// ti(28) | 0 | ti(4) | 1 | pi(4)
		return ti>>4<<10 + (ti&0xf)<<5 + 1<<4 + pi
	}
	if pi < 256 {
		// ti(32) | 1 | pi(4) | 0 | pi(4)
		return ti<<10 + 1<<9 + (pi>>4)<<5 + (pi & 0xf)
	}
	// ti(32) | pi(4) | 1 | pi(4) | 1 | pi(4)
	return ti<<14 + pi>>8<<10 + 1<<9 + (pi>>4&0xf)<<5 + 1<<4 + (pi & 0xf)
}

func SplitID(longid uint64) (uint32, uint16) {
	x, y := longid>>9&1, longid>>4&1
	if x == 0 && y == 0 {
		return uint32(longid>>10)<<6 + uint32(longid>>5&0xf)<<2 + uint32(longid>>2&0x3), uint16(longid&0x3) + 1
	}
	if x == 0 && y == 1 {
		return uint32(longid>>10)<<4 + uint32(longid>>5&0xf), uint16(longid&0xf) + 1
	}
	if x == 1 && y == 0 {
		return uint32(longid >> 10), uint16(longid>>5&0xf)<<4 + uint16(longid&0xf) + 1
	}
	return uint32(longid >> 14), uint16(longid>>10&0xf)<<8 + uint16(longid>>5&0xf)<<4 + uint16(longid&0xf) + 1
}

func (p *Post) User() string { _, i := Format8Bytes(p.user); return i }

// Topic describes topic
type Topic struct {
	ID         uint32
	Sticky     bool
	Locked     bool
	Archived   bool
	FreeReply  bool
	Saged      bool
	CreatedAt  uint32
	ModifiedAt uint32
	Subject    string
	Next       *Topic
	Prev       *Topic
	Posts      []Post

	T_TotalPosts uint16
	T_IsAdmin    bool
	T_IsExpand   bool

	store *Store
}

func (p *Topic) Date() string { return time.Unix(int64(p.CreatedAt), 0).Format(stdTimeFormat) }

func (p *Topic) LastDate() string { return time.Unix(int64(p.ModifiedAt), 0).Format(stdTimeFormat) }

func (t *Topic) FirstPostID() uint16 { return t.Posts[0].ID }

// ForumConfig is a static configuration of a single forum
type ForumConfig struct {
	Title          string
	Salt           [16]byte
	NoMoreNewUsers bool
	NoImageUpload  bool
	NoRecaptcha    bool
	InProduction   bool
	MaxLiveTopics  int
	MaxImageSize   int
	MaxSubjectLen  int
	MaxMessageLen  int
	MinMessageLen  int
	SearchTimeout  int
	Cooldown       int
	PostsPerPage   int
	TopicsPerPage  int
	Recaptcha      string
	RecaptchaToken string
	URL            string
}

// Forum describes forum
type Forum struct {
	*ForumConfig
	*Store
	*Logger
}

const userStructSize = 8 + 4 + 4 + 8 + 8

const (
	PERM_ADMIN = 1 << iota
	PERM_NO_ROLL
	PERM_LOCK_SAGE_DELETE
	PERM_STICKY_PURGE
	PERM_BLOCK
)

type User struct {
	ID      [8]byte
	N       uint32
	Posts   uint32
	T       int64
	M       byte
	padding [7]byte
	Hash    string
}

func (u User) IsValid() bool { return u.ID != default8Bytes }

func (u User) Can(perm byte) bool { return u.M&perm > 0 }

func (u User) CanModerate() bool {
	return u.Can(PERM_ADMIN) || u.Can(PERM_LOCK_SAGE_DELETE) || u.Can(PERM_STICKY_PURGE) || u.Can(PERM_BLOCK)
}

type SafeJSON struct {
	*bytes.Buffer
}

func (s *SafeJSON) Write(p []byte) (int, error) {
	for _, v := range p {
		if v == ',' {
			s.Buffer.WriteByte('^')
			continue
		}
		if v == '"' {
			s.Buffer.WriteByte('\'')
			continue
		}
		if bytes.IndexByte([]byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ[]{}:"), v) > -1 {
			s.Buffer.WriteByte(v)
			continue
		}
		if v == 0xa {
			continue
		}
		panic(v)
	}
	return len(p), nil
}

func (s *SafeJSON) Read(p []byte) (int, error) {
	n, err := s.Buffer.Read(p)
	for i, v := range p[:n] {
		if v == '^' {
			p[i] = ','
			continue
		}
		if v == '\'' {
			p[i] = '"'
			continue
		}
		if bytes.IndexByte([]byte("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ[]{}:"), v) > -1 {
			continue
		}
	}
	return n, err
}

type ResponseWriterWrapper struct {
	http.ResponseWriter
	Code int
}

func (r *ResponseWriterWrapper) WriteHeader(code int) {
	r.Code = code
	r.ResponseWriter.WriteHeader(code)
}

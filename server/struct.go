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

type Post struct {
	Message   string
	Image     string
	user      [8]byte
	ip        [8]byte
	CreatedAt uint32
	ID        uint16
	IsDeleted bool
	T_IsFirst bool
	Topic     *Topic
}

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
	if p.ID < 128 {
		return uint64(p.Topic.ID)<<8 + uint64(p.ID)
	}
	return uint64(p.Topic.ID)<<13 + uint64(p.ID>>7)<<8 + 1<<7 + uint64(p.ID&0x7f)
}

func SplitID(longid uint64) (uint32, uint16) {
	if longid&80 == 0 {
		return uint32(longid >> 8), uint16(longid) & 0x7f
	}
	return uint32(longid >> 13), uint16((longid>>8)&0x1f)<<7 + uint16(longid&0x7f)
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

func (t *Topic) IsDeleted() bool {
	for _, p := range t.Posts {
		if !p.IsDeleted {
			return false
		}
	}
	return true
}

func (t *Topic) FirstPostID() uint16 {
	return t.Posts[0].ID
}

// ForumConfig is a static configuration of a single forum
type ForumConfig struct {
	Title          string
	Salt           [16]byte
	NoMoreNewUsers bool
	NoImageUpload  bool
	NoRecaptcha    bool
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

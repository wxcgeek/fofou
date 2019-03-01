package server

import (
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
			} else if strings.HasSuffix(in, ".png") || strings.HasSuffix(in, ".jpg") || strings.HasSuffix(in, ".gif") {
				return "<img class=image alt='" + in + "' src='" + in + "'/>"
			} else {
				return "<a href='" + in + "' target=_blank>" + in + "</a>"
			}
		}
	})
}

func (p *Post) IP() string { i, _ := Format8Bytes(p.ip); return i }

func (p *Post) LongID() uint64 { return uint64(p.Topic.ID)<<16 + uint64(p.ID) }

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

// ForumConfig is a static configuration of a single forum
type ForumConfig struct {
	Title          string
	Salt           string
	NoMoreNewUsers bool
	NoImageUpload  bool
	MaxLiveTopics  int
	MaxSubjectLen  int
	MaxMessageLen  int
	MinMessageLen  int
	Recaptcha      string
}

// Forum describes forum
type Forum struct {
	*ForumConfig
	*Store
	*Logger
	TopbarHTML string
}

type User struct {
	ID     [8]byte
	N      int
	Posts  int
	noTest bool
}

func (u User) IsValid() bool { return u.ID != default8Bytes }

func (u User) IsAdmin() bool { return u.ID[0] == 'a' && u.ID[1] == ':' }

func (u User) NoTest() bool { return u.noTest }

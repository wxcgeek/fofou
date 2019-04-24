package server

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coyove/common/rand"
)

var ErrInvalidTopic = fmt.Errorf("can't find the topic")

const (
	OP_NOP       = 'x'
	OP_TOPIC     = 'T'
	OP_TOPICNUM  = 't'
	OP_POST      = 'P'
	OP_APPEND    = 'a'
	OP_IMAGE     = 'I'
	OP_DELETE    = 'D'
	OP_BLOCK     = 'B'
	OP_STICKY    = 'S'
	OP_SAGE      = 'G'
	OP_LOCK      = 'L'
	OP_ARCHIVE   = 'A'
	OP_PURGE     = 'X'
	OP_FREEREPLY = 'F'
	OP_CONFIG    = 'C'
	OP_MAXTOPICS = 'M'
	OP_NSFW      = 'W'
)

// Store describes store
type Store struct {
	sync.RWMutex

	LiveTopicsNum int
	Rand          *rand.Rand

	block         cipher.Block
	ready         uintptr
	ptr           int64
	maxLiveTopics int
	dataFilePath  string
	configStr     string
	configLock    sync.RWMutex
	rootTopic     *Topic
	endTopic      *Topic
	topicsCount   uint32
	blocked       map[[8]byte]bool
	dataFile      *os.File
}

func (store *Store) LoadingProgress() float64 { return float64(atomic.LoadUintptr(&store.ready)) / 1000 }

func (store *Store) IsReady() bool { return atomic.LoadUintptr(&store.ready) == 1000 }

func (store *Store) MaxLiveTopics() int { return store.maxLiveTopics }

func (store *Store) markBlockedOrUnblocked(term [8]byte) {
	if store.blocked[term] {
		delete(store.blocked, term)
	} else {
		store.blocked[term] = true
	}
}

func (store *Store) OperateTopic(topicID uint32, action byte) error {
	store.Lock()
	defer store.Unlock()
	t := store.topicByIDUnlocked(topicID)
	if t == nil {
		return ErrInvalidTopic
	}

	var p buffer
	var err error
	switch action {
	case OP_STICKY:
		if err = store.append(p.WriteByte(OP_STICKY).WriteUInt32(topicID).Bytes()); err == nil {
			t.Sticky = !t.Sticky
			store.moveTopicToFront(t)
		}
	case OP_LOCK:
		if err = store.append(p.WriteByte(OP_LOCK).WriteUInt32(topicID).Bytes()); err == nil {
			t.Locked = !t.Locked
		}
	case OP_FREEREPLY:
		if err = store.append(p.WriteByte(OP_FREEREPLY).WriteUInt32(topicID).Bytes()); err == nil {
			t.FreeReply = !t.FreeReply
		}
	case OP_SAGE:
		if err = store.append(p.WriteByte(OP_SAGE).WriteUInt32(topicID).Bytes()); err == nil {
			t.Saged = !t.Saged
		}
	case OP_PURGE:
		if err = store.append(p.WriteByte(OP_PURGE).WriteUInt32(topicID).Bytes()); err == nil {
			t.Prev.Next = t.Next
			t.Next.Prev = t.Prev
			store.LiveTopicsNum--
		}
	}
	return err
}

func (store *Store) SageTopic(topicID uint32, u User) error {
	store.Lock()
	defer store.Unlock()
	t := store.topicByIDUnlocked(topicID)
	if t == nil {
		return ErrInvalidTopic
	}
	if !u.Can(PERM_LOCK_SAGE_DELETE_FLAG) && u.ID != t.Posts[0].UserXor() {
		return fmt.Errorf("can't sage the topic")
	}

	var p buffer
	if err := store.append(p.WriteByte(OP_SAGE).WriteUInt32(topicID).Bytes()); err != nil {
		return err
	}

	t.Saged = !t.Saged
	return nil
}

// PostsCount returns number of posts
func (store *Store) PostsCount() (int, int) {
	store.RLock()
	defer store.RUnlock()
	a, b := 0, 0
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		a++
		b += len(topic.Posts)
	}
	return a, b
}

// TopicsCount retuns number of topics
func (store *Store) TopicsCount() int {
	return int(store.topicsCount)
}

var DefaultTopicMapper = func(t *Topic) Topic { return *t }
var DefaultTopicFilter = func(t *Topic) bool { return true }

// GetTopics retuns topics
func (store *Store) GetTopics(start, length int, filter func(*Topic) bool, mapper func(*Topic) Topic) []Topic {
	res := make([]Topic, 0, length)
	store.RLock()
	defer store.RUnlock()

	topic, i := store.rootTopic.Next, 0
	for ; topic != store.endTopic; topic, i = topic.Next, i+1 {
		if i >= start && len(res) < length {
			if filter(topic) {
				res = append(res, mapper(topic))
				continue
			}
		}
		if len(res) >= length {
			break
		}
	}
	return res
}

func (store *Store) topicByIDUnlocked(id uint32) *Topic {
	if id == 0 {
		return nil
	}
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		if id == topic.ID {
			return topic
		}
	}
	return nil
}

func (store *Store) GetTopic(id uint32, filter func(*Topic) Topic) Topic {
	store.RLock()
	defer store.RUnlock()
	t := store.topicByIDUnlocked(id)
	if t == nil {
		return Topic{}
	}
	return filter(t)
}

func (store *Store) getPostPtrUnlocked(postLongID uint64) (*Post, error) {
	topicID, postID := SplitID(postLongID)
	topic := store.topicByIDUnlocked(topicID)
	if nil == topic {
		return nil, fmt.Errorf("can't find topic ID: %d", topicID)
	}
	if int(postID) > len(topic.Posts) {
		return nil, fmt.Errorf("can't find post ID: %d", postID)
	}

	post := &topic.Posts[postID-1]
	return post, nil
}

func (store *Store) AppendPost(postLongID uint64, msg string) error {
	store.Lock()
	defer store.Unlock()

	post, err := store.getPostPtrUnlocked(postLongID)
	if err != nil {
		return err
	}

	var p buffer
	if err := store.append(p.WriteByte(OP_APPEND).WriteUInt32(post.Topic.ID).WriteUInt16(post.ID).WriteString(msg).Bytes()); err != nil {
		return err
	}

	post.Message += msg
	return nil
}

func (store *Store) moveTopicToFront(topic *Topic) {
	if topic.Saged {
		return
	}

	root := store.rootTopic.Next
	if !topic.Sticky {
		for ; root != store.endTopic; root = root.Next {
			if !root.Sticky {
				break
			}
		}
	}

	if topic == root {
		return
	}

	if topic.Prev != nil {
		topic.Prev.Next = topic.Next
	}
	if topic.Next != nil {
		topic.Next.Prev = topic.Prev
	}
	topic.Next = root
	topic.Prev = root.Prev
	if root.Prev != nil {
		root.Prev.Next = topic
	}
	root.Prev = topic
}

var errTooManyPosts = fmt.Errorf("too many posts")

func (store *Store) addNewPost(msg string, image *Image, user, ipAddr [8]byte, topic *Topic, sage bool) (uint64, error) {
	newTopic := len(topic.Posts) == 0
	nextID := len(topic.Posts) + 1
	if nextID > 4000 {
		return 0, errTooManyPosts
	}

	p := &Post{
		ID:        uint16(nextID),
		CreatedAt: uint32(time.Now().Unix()),
		user:      user,
		ip:        ipAddr,
		Topic:     topic,
		Message:   msg,
		Image:     image,
	}

	if newTopic {
		p.Topic.CreatedAt = p.CreatedAt
	}

	p.ip = p.IPXor()
	p.user = p.UserXor()

	if sage {
		p.SetStatus(POST_ISSAGE)
	}

	var topicStr buffer
	if newTopic {
		topicStr.WriteByte(OP_TOPIC)
		topicStr.WriteUInt32(uint32(topic.ID))
		topicStr.WriteString(topic.Subject)

		if sage {
			topicStr.WriteByte(OP_SAGE).WriteUInt32(topic.ID)
		}
	}

	topicStr.WriteByte(OP_POST)
	topicStr.WriteUInt32(topic.ID)
	topicStr.WriteUInt16(p.ID)
	topicStr.WriteByte(p.Status)
	topicStr.WriteUInt32(p.CreatedAt)
	topicStr.Write8Bytes(p.ip)
	topicStr.Write8Bytes(p.user)
	topicStr.WriteString(msg)

	if image != nil {
		topicStr.WriteByte(OP_IMAGE).
			WriteUInt32(topic.ID).
			WriteUInt16(p.ID).
			WriteString(image.Path).
			WriteString(image.Name).
			WriteUInt32(image.Size).
			WriteUInt16(image.X).
			WriteUInt16(image.Y)
	}

	if err := store.append(topicStr.Bytes()); err != nil {
		return 0, err
	}

	topic.Posts = append(topic.Posts, *p)

	if !sage || newTopic {
		// as a new topic, even it is saged, it still has the opportunity to stay at the top for once
		store.moveTopicToFront(topic)
	}

	if newTopic {
		topic.CreatedAt = p.CreatedAt
	} else {
		topic.ModifiedAt = p.CreatedAt
	}
	return p.LongID(), nil
}

func (store *Store) buildArchivePath(topicID uint32) string {
	id1, id2 := int(topicID)/100000, int(topicID)/1000
	return filepath.Join(filepath.Dir(store.dataFilePath), "archive", strconv.Itoa(id1), strconv.Itoa(id2), strconv.Itoa(int(topicID)))
}

func (topic *Topic) marshal() buffer {
	buf := buffer{}
	buf.WriteByte(OP_TOPIC).WriteUInt32(topic.ID).WriteString(topic.Subject)

	for _, p := range topic.Posts {
		buf.WriteByte(OP_POST).
			WriteUInt32(topic.ID).
			WriteUInt16(p.ID).
			WriteByte(p.Status).
			WriteUInt32(p.CreatedAt).
			Write8Bytes(p.ip).
			Write8Bytes(p.user).
			WriteString(p.Message) // this will include OP_APPEND

		if p.Image != nil {
			buf.WriteByte(OP_IMAGE).
				WriteUInt32(topic.ID).
				WriteUInt16(p.ID).
				WriteString(p.Image.Path).
				WriteString(p.Image.Name).
				WriteUInt32(p.Image.Size).
				WriteUInt16(p.Image.X).
				WriteUInt16(p.Image.Y)
		}

		if p.T_IsNSFW() {
			buf.WriteByte(OP_NSFW).
				WriteUInt32(topic.ID).
				WriteUInt16(p.ID)
		}
	}

	return buf
}

func archive(topic *Topic, saveToPath string) error {
	if err := os.MkdirAll(filepath.Dir(saveToPath), 0755); err != nil {
		return err
	}
	buf := topic.marshal()
	hdr := make([]byte, 16)
	binary.BigEndian.PutUint64(hdr[2:], uint64(len(buf.Bytes())+16))
	hdr = append(hdr, buf.Bytes()...)
	hdr[0], hdr[1], hdr[2] = 'z', 'z', 'z'
	return ioutil.WriteFile(saveToPath, hdr, 0755)
}

func (store *Store) Dup(path string) error {
	store.RLock()
	defer store.RUnlock()

	os.Remove(path)
	of, err := os.Create(path)
	if err != nil {
		return err
	}

	defer of.Close()
	if _, err := store.dataFile.Seek(0, 0); err != nil {
		return err
	}

	_, err = io.Copy(of, store.dataFile)
	return err
}

func (store *Store) ArchiveJob() error {
	store.Lock()
	defer store.Unlock()
	return store.archiveJob(store.maxLiveTopics)
}

func (store *Store) archiveJob(maxLiveTopics int) error {
	topic, i := store.rootTopic.Next, 0
	for ; topic != store.endTopic; topic = topic.Next {
		if i++; i == maxLiveTopics {
			break
		}
	}

	for topic != store.endTopic.Prev && topic != store.endTopic {
		t := store.endTopic.Prev
		if err := archive(t, store.buildArchivePath(t.ID)); err != nil {
			return err
		}
		var p buffer
		if err := store.append(p.WriteByte(OP_ARCHIVE).WriteUInt32(t.ID).Bytes()); err != nil {
			return err
		}
		t.Prev.Next = t.Next
		t.Next.Prev = t.Prev
		store.LiveTopicsNum--
	}
	return nil
}

func (store *Store) NewTopic(subject, msg string, image *Image, user, ipAddr [8]byte, sage bool) (uint64, error) {
	store.Lock()
	defer store.Unlock()

	if store.topicsCount == math.MaxUint32 {
		return 0, fmt.Errorf("that day finally come")
	}

	topic := &Topic{
		ID:      store.topicsCount + 1,
		Subject: subject,
		Posts:   make([]Post, 0),
		store:   store,
	}

	postLongID, err := store.addNewPost(msg, image, user, ipAddr, topic, sage)
	if err == nil {
		store.topicsCount++
		store.LiveTopicsNum++
	}

	return postLongID, err
}

func (store *Store) NewPost(topicID uint32, msg string, image *Image, user, ipAddr [8]byte, sage bool) (uint64, error) {
	store.Lock()
	defer store.Unlock()

	topic := store.topicByIDUnlocked(topicID)
	if topic == nil {
		return 0, errors.New("invalid topic ID")
	}

	postLongID, err := store.addNewPost(msg, image, user, ipAddr, topic, sage)
	if err == errTooManyPosts {
		var p buffer
		if err = store.append(p.WriteByte(OP_LOCK).WriteUInt32(topicID).Bytes()); err == nil {
			topic.Locked = true
		}
	}
	return postLongID, err
}

func (store *Store) GetPostsBy(q [8]byte, qtext string, max int, timeout int64) ([]Post, int) {
	store.RLock()
	defer store.RUnlock()

	var m []uint32
	res, total := make([]Post, 0), 0

	search := func(topic *Topic) {
		if len(m) > 0 && len(topic.Posts) > 0 {
			if r, _ := stringCompare(topic.Subject, "", m); r {
				if total++; total <= max {
					res = append(res, topic.Posts[0])
				}
				return
			}
		}

		q2 := topic.Posts[0].aes128(q)
		for _, post := range topic.Posts {
			if len(m) > 0 {
				if r, _ := stringCompare(post.Message, "", m); r {
					if total++; total <= max {
						res = append(res, post)
					}
				}
			} else if post.ip == q2 || post.user == q2 {
				if total++; total <= max {
					res = append(res, post)
				}
			}

			if len(m) > 0 && total > max {
				return
			}
		}
	}

	if strings.HasPrefix(qtext, ">>") {
		idx := strings.Index(qtext, " ")
		if idx == -1 {
			return res, 0
		}
		longID, _ := strconv.ParseUint(qtext[2:idx], 10, 64)
		topicID, _ := SplitID(longID)
		topic := store.topicByIDUnlocked(uint32(topicID))
		if topic == nil {
			return res, 0
		}
		_, m = stringCompare("", qtext[idx+1:], nil)
		if len(m) == 0 {
			return res, 0
		}
		search(topic)
		return res, total
	}

	_, m = stringCompare("", qtext, nil)
	start := time.Now().UnixNano()
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		if time.Now().UnixNano()-start > timeout {
			break
		}
		search(topic)
	}
	return res, total
}

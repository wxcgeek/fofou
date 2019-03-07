package server

import (
	"crypto/cipher"
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coyove/common/rand"
)

const (
	OP_TOPIC     = 'T'
	OP_POST      = 'P'
	OP_DELETE    = 'D'
	OP_BLOCK     = 'B'
	OP_STICKY    = 'S'
	OP_SAGE      = 'G'
	OP_LOCK      = 'L'
	OP_ARCHIVE   = 'A'
	OP_PURGE     = 'X'
	OP_FREEREPLY = 'F'
)

// Store describes store
type Store struct {
	sync.RWMutex
	*Logger

	MaxLiveTopics int
	Rand          *rand.Rand

	block        cipher.Block
	ready        uintptr
	ptr          int64
	dataFilePath string
	rootTopic    *Topic
	endTopic     *Topic
	topicsCount  uint32
	blocked      map[[8]byte]bool
	dataFile     *os.File
}

func (store *Store) LoadingProgress() float64 { return float64(atomic.LoadUintptr(&store.ready)) / 1000 }

func (store *Store) IsReady() bool { return atomic.LoadUintptr(&store.ready) == 1000 }

func (store *Store) markBlockedOrUnblocked(term [8]byte) {
	if store.blocked[term] {
		delete(store.blocked, term)
	} else {
		store.blocked[term] = true
	}
}

func (store *Store) OperateTopic(topicID uint32, action byte) {
	store.Lock()
	defer store.Unlock()
	t := store.topicByIDUnlocked(topicID)
	if t == nil {
		return
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
		}
	}
	if err != nil {
		store.Error("OperateTopic(): %v", err)
	}
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

// GetTopics retuns topics
func (store *Store) GetTopics(nMax, from int, withDeleted bool) ([]Topic, int) {
	res := make([]Topic, 0, nMax)
	store.RLock()
	defer store.RUnlock()

	topic, i := store.rootTopic.Next, 0
	for ; topic != store.endTopic; topic, i = topic.Next, i+1 {
		if i >= from && i < from+nMax {
			res = append(res, *topic)
		} else if i >= from+nMax {
			break
		}
	}

	return res, i
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

func (store *Store) TopicByID(id uint32) Topic {
	store.RLock()
	defer store.RUnlock()
	t := store.topicByIDUnlocked(id)
	if t == nil {
		return Topic{}
	}
	return *t
}

// DeletePost deletes/restores a post
func (store *Store) DeletePost(postLongID uint64, onImageDelete func(string)) error {
	store.Lock()
	defer store.Unlock()

	topicID, postID := SplitID(postLongID)
	topic := store.topicByIDUnlocked(topicID)
	if nil == topic {
		return fmt.Errorf("can't find topic ID: %d", topicID)
	}
	if int(postID) > len(topic.Posts) {
		return fmt.Errorf("can't find post ID: %d", postID)
	}

	post := &topic.Posts[postID-1]

	var p buffer
	if err := store.append(p.WriteByte(OP_DELETE).WriteUInt32(topicID).WriteUInt16(postID).Bytes()); err != nil {
		return err
	}

	post.IsDeleted = !post.IsDeleted
	onImageDelete(post.Image)
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

func (store *Store) addNewPost(msg, image string, user [8]byte, ipAddr [8]byte, topic *Topic, newTopic bool) error {
	nextID := len(topic.Posts) + 1
	if nextID > 4001 {
		return errTooManyPosts
	}

	p := &Post{
		ID:        uint16(nextID),
		CreatedAt: uint32(time.Now().Unix()),
		user:      user,
		ip:        ipAddr,
		IsDeleted: false,
		Topic:     topic,
		Message:   msg,
		Image:     image,
	}

	p.ip = p.IPXor()

	var topicStr buffer
	if newTopic {
		topicStr.WriteByte(OP_TOPIC)
		topicStr.WriteUInt32(uint32(topic.ID))
		topicStr.WriteString(topic.Subject)
	}

	topicStr.WriteByte(OP_POST)
	topicStr.WriteUInt32(topic.ID)
	topicStr.WriteUInt16(p.ID)
	topicStr.WriteUInt32(p.CreatedAt)
	topicStr.Write8Bytes(ipAddr)
	topicStr.Write8Bytes(user)
	topicStr.WriteString(image)
	topicStr.WriteString(msg)

	if err := store.append(topicStr.Bytes()); err != nil {
		return err
	}

	topic.Posts = append(topic.Posts, *p)
	store.moveTopicToFront(topic)
	if newTopic {
		topic.CreatedAt = p.CreatedAt
	} else {
		topic.ModifiedAt = p.CreatedAt
	}
	return nil
}

func (store *Store) buildArchivePath(topicID uint32) string {
	id1, id2 := int(topicID)/100000, int(topicID)/1000
	return filepath.Join(filepath.Dir(store.dataFilePath), "archive", strconv.Itoa(id1), strconv.Itoa(id2), strconv.Itoa(int(topicID)))
}

func archive(topic *Topic, saveToPath string) error {

	buf := &buffer{}
	buf.WriteByte(OP_TOPIC).WriteUInt32(topic.ID).WriteString(topic.Subject)

	for _, p := range topic.Posts {
		if p.IsDeleted {
			continue
		}

		buf.WriteByte(OP_POST).
			WriteUInt32(topic.ID).
			WriteUInt16(p.ID).
			WriteUInt32(p.CreatedAt).
			Write8Bytes(p.ip).
			Write8Bytes(p.user).
			WriteString(p.Image).
			WriteString(p.Message)
	}

	// .CreateDirForFileMust(saveToPath)
	if err := os.MkdirAll(filepath.Dir(saveToPath), 0755); err != nil {
		return err
	}
	return ioutil.WriteFile(saveToPath, buf.Bytes(), 0755)
}

func (store *Store) Archive() {
	store.Lock()
	defer store.Unlock()

	topic, i := store.rootTopic.Next, 0
	for ; topic != store.endTopic; topic = topic.Next {
		if i++; i == store.MaxLiveTopics {
			break
		}
	}

	for topic != store.endTopic.Prev && topic != store.endTopic {
		t := store.endTopic.Prev
		if err := archive(t, store.buildArchivePath(t.ID)); err != nil {
			store.Error("failed to archive %d: %v", t.ID, err)
			break
		}
		var p buffer
		if err := store.append(p.WriteByte(OP_ARCHIVE).WriteUInt32(t.ID).Bytes()); err != nil {
			store.Error("failed to archive %d: %v", t.ID, err)
			break
		}
		t.Prev.Next = t.Next
		t.Next.Prev = t.Prev
	}
}

func (store *Store) NewTopic(subject, msg, image string, user [8]byte, ipAddr [8]byte) (uint32, error) {
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

	err := store.addNewPost(msg, image, user, ipAddr, topic, true)
	if err == nil {
		store.topicsCount++
	}

	if store.Rand.Intn(64) == 0 {
		go store.Archive()
	}

	return topic.ID, err
}

func (store *Store) NewPost(topicID uint32, msg, image string, user [8]byte, ipAddr [8]byte) error {
	store.Lock()
	defer store.Unlock()

	topic := store.topicByIDUnlocked(topicID)
	if topic == nil {
		return errors.New("invalid topic ID")
	}
	err := store.addNewPost(msg, image, user, ipAddr, topic, false)
	if err == errTooManyPosts {
		var p buffer
		if err = store.append(p.WriteByte(OP_LOCK).WriteUInt32(topicID).Bytes()); err == nil {
			topic.Locked = true
		}
	}
	return err
}

// BlockIP blocks/unblocks IP address
func (store *Store) Block(term [8]byte) {
	store.Lock()
	defer store.Unlock()
	if term == default8Bytes {
		return
	}
	var p buffer // := fmt.Sprintf("B%s\n", ipAddrInternal)
	if err := store.append(p.WriteByte(OP_BLOCK).Write8Bytes(term).Bytes()); err == nil {
		store.markBlockedOrUnblocked(term)
	}
}

// IsBlocked checks if the term is blocked
func (store *Store) IsBlocked(q [8]byte) bool {
	store.RLock()
	defer store.RUnlock()
	return store.blocked[q]
}

func (store *Store) GetPostsBy(q [8]byte, qtext string, max int, timeout int64) ([]Post, int) {
	store.RLock()
	defer store.RUnlock()

	res, total := make([]Post, 0), 0
	_, m := stringCompare("", qtext, nil)
	start := time.Now().UnixNano()

	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		if len(m) > 0 && len(topic.Posts) > 0 {
			if time.Now().UnixNano()-start > timeout {
				break
			}

			if r, _ := stringCompare(topic.Subject, "", m); r {
				if total++; total <= max {
					res = append(res, topic.Posts[0])
				}
				continue
			}
		}

		for _, post := range topic.Posts {
			if len(m) > 0 {
				if r, _ := stringCompare(post.Message, "", m); r {
					if total++; total <= max {
						res = append(res, post)
					}
				}
			} else if post.ip == q || post.user == q {
				if total++; total <= max {
					res = append(res, post)
				}
			}

			if len(m) > 0 && total > max {
				break
			}
		}
	}
	return res, total
}

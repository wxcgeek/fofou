package server

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
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

	ready        uintptr
	dataFilePath string
	rootTopic    *Topic
	endTopic     *Topic
	topicsCount  uint32
	blocked      map[[8]byte]bool
	dataFile     *os.File
}

func findPostToDelUndel(r *buffer, topicIDToTopic map[uint32]*Topic) (*Post, error) {
	topicID, err1 := r.ReadUInt32()
	postID, err2 := r.ReadUInt16()
	panicif(err1 != nil || err2 != nil, "invalid post ID/topic ID")

	topic, ok := topicIDToTopic[topicID]
	if !ok {
		return nil, fmt.Errorf("no topic with that ID")
	}
	if int(postID) > len(topic.Posts) {
		return nil, fmt.Errorf("invalid post ID")
	}
	return &topic.Posts[postID-1], nil
}

// parse:
// T$id|$subject
func parseTopic(r *buffer) *Topic {
	id, err := r.ReadUInt32()
	panicif(err != nil, "invalid ID")

	subject, err := r.ReadString()
	panicif(err != nil, "invalid subject")

	return &Topic{
		ID:      id,
		Subject: subject,
		Posts:   make([]Post, 0),
	}
}

// parse:
// P1|1|1148874103|4b0af66e|Krzysztof Kowalczyk|message in ascii85 format
func parsePost(r *buffer, topicIDToTopic map[uint32]*Topic) Post {
	topicID, err := r.ReadUInt32()
	panicif(err != nil, "invalid topic ID")

	id, err := r.ReadUInt16()
	panicif(err != nil, "invalid post ID")

	createdOnSeconds, err := r.ReadUInt32()
	panicif(err != nil, "invalid timestamp")

	ipAddrInternal, err := r.Read8Bytes()
	panicif(err != nil, "invalid IP")

	userName, err := r.Read8Bytes()
	panicif(err != nil, "invalid username")

	image, err := r.ReadString()
	panicif(err != nil, "invalid image refer")

	message, err := r.ReadString()
	panicif(err != nil, "invalid message body")

	t, ok := topicIDToTopic[topicID]
	panicif(!ok, "invalid topic ID")

	realPostID := len(t.Posts) + 1
	panicif(int(id) != realPostID, "invalid post ID: %d, topic ID: %d, expected post ID: %d\n", id, topicID, realPostID)
	panicif(realPostID >= 65536, "too many posts (65536)")

	return Post{
		ID:        uint16(realPostID),
		CreatedAt: createdOnSeconds,
		user:      userName,
		ip:        ipAddrInternal,
		IsDeleted: false,
		Topic:     t,
		Message:   message,
		Image:     image,
	}
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

func (store *Store) loadDB(path string, slient bool) (err error) {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}

	topicIDToTopic := make(map[uint32]*Topic)
	r := &buffer{}
	r.SetReader(bufio.NewReaderSize(fh, 1024*1024*10))
	print := func(f string, args ...interface{}) {
		if !slient {
			fmt.Printf(f, args...)
		}
	}

	defer func() {
		if r := recover(); r != nil {
			if slient {
				err = fmt.Errorf("panic error: %v", r)
			} else {
				print("\npanic: %v\n", r)
				panic(0)
			}
		}
	}()

	fhInfo, _ := fh.Stat()
	if fhInfo.Size() == 0 {
		fh.Close()
		print("empty DB")
		return nil
	}

	fsize := int(fhInfo.Size())
	for {
		print("\rloading %02d%% %d/%d", r.pos*100/fsize, r.pos, fsize)
		atomic.StoreUintptr(&store.ready, uintptr(r.pos*1000/fsize))

		op, err := r.ReadByte()
		if err != nil {
			break
		}

		switch op {
		case OP_TOPIC:
			t := parseTopic(r)
			store.moveTopicToFront(t)
			store.topicsCount++
			panicif(topicIDToTopic[t.ID] != nil, "topic %d already existed", t.ID)
			topicIDToTopic[t.ID] = t
		case OP_POST:
			post := parsePost(r, topicIDToTopic)
			t := post.Topic
			t.Posts = append(t.Posts, post)
			if len(t.Posts) == 1 {
				t.CreatedAt = post.CreatedAt
			} else {
				t.ModifiedAt = post.CreatedAt
			}

			store.moveTopicToFront(t)
		case OP_DELETE:
			post, err := findPostToDelUndel(r, topicIDToTopic)
			panicif(err != nil, err)
			post.IsDeleted = !post.IsDeleted
		case OP_BLOCK:
			str, err := r.Read8Bytes()
			panicif(err != nil, "invalid string")
			store.markBlockedOrUnblocked(str)
		case OP_STICKY, OP_ARCHIVE, OP_LOCK, OP_PURGE, OP_FREEREPLY, OP_SAGE:
			topicID, err := r.ReadUInt32()
			panicif(err != nil, err)

			t := topicIDToTopic[topicID]
			panicif(t == nil, "can't find the topic to '%s': %d", string(op), topicID)

			switch op {
			case OP_STICKY:
				if t.Sticky = !t.Sticky; t.Sticky {
					store.moveTopicToFront(t)
				}
			case OP_LOCK:
				t.Locked = !t.Locked
			case OP_FREEREPLY:
				t.FreeReply = !t.FreeReply
			case OP_SAGE:
				t.Saged = !t.Saged
			case OP_ARCHIVE, OP_PURGE:
				t.Prev.Next = t.Next
				t.Next.Prev = t.Prev
				delete(topicIDToTopic, t.ID)
			}
		default:
			panic("unexpected line type")
		}
	}

	fh.Close()
	atomic.StoreUintptr(&store.ready, 1000)
	print("\n")
	return nil
}

func (store *Store) verifyTopics() {
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		if 0 == len(topic.Posts) {
			store.Notice("topics (%v) has no posts!\n", topic)
		}
	}
}

func NewStore(path string, maxLiveTopics int, logger *Logger) *Store {
	store := &Store{
		dataFilePath:  path,
		rootTopic:     &Topic{},
		endTopic:      &Topic{},
		blocked:       make(map[[8]byte]bool),
		Rand:          rand.New(),
		MaxLiveTopics: maxLiveTopics,
		Logger:        logger,
	}

	store.rootTopic.Next = store.endTopic
	store.endTopic.Prev = store.rootTopic

	if _, err := os.Stat(path); err == nil {
		go func() {
			store.loadDB(store.dataFilePath, false)
			store.verifyTopics()
			store.dataFile, err = os.OpenFile(store.dataFilePath, os.O_APPEND|os.O_RDWR, 0666)
			panicif(err != nil, "can't open DB %s: %v", store.dataFilePath, err)
		}()

		return store
	}

	f, err := os.Create(store.dataFilePath)
	panicif(err != nil, "can't create initial DB %s: %v", store.dataFilePath, err)
	f.Close()

	store.dataFile, err = os.OpenFile(store.dataFilePath, os.O_APPEND|os.O_RDWR, 0666)
	panicif(err != nil, "can't open DB %s: %v", store.dataFilePath, err)

	store.ready = 1000

	if true {
		r := store.Rand
		curTopicId := uint32(0)
		for i := 0; i < 200; i++ {
			wg := &sync.WaitGroup{}
			for i := 0; i < 1000; i++ {
				wg.Add(1)
				go func() {
					subject := base64.StdEncoding.EncodeToString(r.Fetch(16))
					msg := base64.StdEncoding.EncodeToString(r.Fetch(r.Intn(64) + 64))
					msg = strings.Repeat(msg, 4)
					userName := [8]byte{'a', 'b', 'c', 'd', 'e', 'f', 0, 0}
					ipAddr := [8]byte{}

					if r.Intn(10) == 1 {
						curTopicId, _ = store.CreateNewTopic(subject, msg, "", userName, ipAddr)
					} else if curTopicId > 0 {
						store.AddPostToTopic(uint32(r.Intn(int(curTopicId))+1), msg, "", userName, ipAddr)
					}
					wg.Done()
				}()
			}
			wg.Wait()
			fmt.Println(i)
		}
	}
	return store
}

func (store *Store) LoadArchivedTopic(topicID uint32) (Topic, error) {
	path := store.buildArchivePath(uint32(topicID))
	if _, err := os.Stat(path); err != nil {
		return Topic{}, err
	}

	store = &Store{
		rootTopic: &Topic{},
		endTopic:  &Topic{},
	}

	store.rootTopic.Next = store.endTopic
	store.endTopic.Prev = store.rootTopic

	var err error
	if err = store.loadDB(path, true); err != nil {
		store.Error("LoadSingleTopicInStore: %s", err)
		return Topic{}, err
	}

	if store.rootTopic.Next == store.endTopic {
		return Topic{}, fmt.Errorf("no topic in %s", path)
	}

	return *store.rootTopic.Next, nil
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

func (store *Store) append(buf []byte) error {
	_, err := store.dataFile.Write(buf)
	return err
}

// DeletePost deletes/restores a post
func (store *Store) DeletePost(postLongID uint64, onImageDelete func(string)) error {
	store.Lock()
	defer store.Unlock()

	topicID := uint32(postLongID >> 16)
	postID := uint16(postLongID)
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

var errTooManyPosts = fmt.Errorf("topic already has 65535 posts")

func (store *Store) addNewPost(msg, image string, user [8]byte, ipAddr [8]byte, topic *Topic, newTopic bool) error {
	nextID := len(topic.Posts) + 1
	if nextID >= 65536 {
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

// CreateNewTopic creates a new topic
func (store *Store) CreateNewTopic(subject, msg, image string, user [8]byte, ipAddr [8]byte) (uint32, error) {
	store.Lock()
	defer store.Unlock()

	if store.topicsCount == math.MaxUint32 {
		return 0, fmt.Errorf("that day finally come")
	}

	topic := &Topic{
		ID:      store.topicsCount + 1,
		Subject: subject,
		Posts:   make([]Post, 0),
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

// AddPostToTopic adds a post to a topic
func (store *Store) AddPostToTopic(topicID uint32, msg, image string, user [8]byte, ipAddr [8]byte) error {
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

func (store *Store) GetPostsBy(q [8]byte, max int) ([]Post, int) {
	store.RLock()
	defer store.RUnlock()
	res, total := make([]Post, 0), 0
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		for _, post := range topic.Posts {
			if post.ip == q || post.user == q {
				total++
				if total <= max {
					res = append(res, post)
				}
			}
		}
	}
	return res, total
}

// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bufio"
	"bytes"
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
	"time"

	"github.com/coyove/common/rand"
	"github.com/kjk/u"
)

const (
	OP_TOPIC     = 'T'
	OP_POST      = 'P'
	OP_DELETE    = 'D'
	OP_BLOCKIP   = 'B'
	OP_BLOCKUSER = 'U'
	OP_STICKY    = 'S'
	OP_LOCK      = 'L'
	OP_ARCHIVE   = 'A'
	OP_PURGE     = 'X'
)

type Post struct {
	Message   string
	User      string
	IP        [8]byte
	CreatedOn uint32
	ID        uint16
	IsDeleted bool
	Topic     *Topic // for convenience, we link to the topic this post belongs to
}

func IPAddress(ip [8]byte) string {
	buf := bytes.Buffer{}
	for i := 0; i < len(ip); i++ {
		buf.WriteString(fmt.Sprintf("%x.", ip[i]))
	}
	buf.Truncate(buf.Len() - 1)
	return buf.String()
}

func (p *Post) IsGithubUser() bool { return strings.HasPrefix(p.User, "g:") }

func (p *Post) UserName() string {
	if p.IsGithubUser() {
		return p.User[2:]
	}
	return p.User
}

// MakeInternalUserName makes internal user name
func MakeInternalUserName(userName string, github bool) string {
	if github {
		return "g:" + userName
	}
	if len(userName) >= 2 && userName[1] == ':' {
		if len(userName) > 2 {
			return userName[2:]
		}
		return userName[:1]
	}
	return userName
}

// Topic describes topic
type Topic struct {
	ID        uint32
	Sticky    bool
	Locked    bool
	Archived  bool
	CreatedOn uint32
	Subject   string
	Next      *Topic
	Prev      *Topic
	Posts     []Post
}

// Store describes store
type Store struct {
	sync.RWMutex

	MaxLiveTopics int

	dataDir      string
	dataFilePath string
	forumName    string
	rootTopic    *Topic
	endTopic     *Topic
	topicsCount  uint32

	blocked  map[string]bool
	dataFile *os.File
}

func stringIndex(arr []string, el string) int {
	for i, s := range arr {
		if s == el {
			return i
		}
	}
	return -1
}

// IsDeleted returns true if topic is deleted
func (t *Topic) IsDeleted() bool {
	for _, p := range t.Posts {
		if !p.IsDeleted {
			return false
		}
	}
	return true
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

	userName, err := r.ReadString()
	panicif(err != nil, "invalid username")

	message, err := r.ReadString()
	panicif(err != nil, "invalid message body")

	t, ok := topicIDToTopic[topicID]
	if !ok {
		fmt.Println("[WARNING] Didn't find topic with the given topic ID")
		return Post{}
	}

	realPostID := len(t.Posts) + 1
	if int(id) != realPostID {
		fmt.Printf("[WARNING] Unexpected post ID:\n")
		fmt.Printf("  topic ID: %d, post ID: %d, expected post ID: %d\n", topicID, id, realPostID)
		fmt.Printf("  %s\n", t.Subject)
	} else if realPostID >= 65536 {
		fmt.Println("having more than 65536 posts in a single topic")
	}

	post := Post{
		ID:        uint16(realPostID),
		CreatedOn: createdOnSeconds,
		User:      userName,
		IP:        ipAddrInternal,
		IsDeleted: false,
		Topic:     t,
		Message:   message,
	}
	return post
}

func (store *Store) markBlockedOrUnblocked(term string) {
	if store.blocked[term] {
		delete(store.blocked, term)
	} else {
		store.blocked[term] = true
	}
}

func (store *Store) readExistingData(fileDataPath string) error {
	fh, err := os.Open(fileDataPath)
	if err != nil {
		return err
	}

	topicIDToTopic := make(map[uint32]*Topic)
	r := &buffer{}
	r.SetReader(bufio.NewReader(fh))

	for {
		fmt.Print("\r%d", r.pos)
		op, err := r.ReadByte()
		if err != nil {
			break
		}

		switch op {
		case OP_TOPIC:
			t := parseTopic(r)
			store.moveTopicToFront(t)
			store.topicsCount++
			panicif(topicIDToTopic[t.ID] != nil, "topic %d already existed, %d", t.ID, r.LastByteCheckpoint())
			topicIDToTopic[t.ID] = t
		case OP_POST:
			post := parsePost(r, topicIDToTopic)
			if post.ID == 0 {
				break
			}
			t := post.Topic
			t.Posts = append(t.Posts, post)
			t.CreatedOn = post.CreatedOn
			store.moveTopicToFront(t)
		case OP_DELETE:
			post, err := findPostToDelUndel(r, topicIDToTopic)
			panicif(err != nil, err)
			post.IsDeleted = !post.IsDeleted
		case OP_BLOCKIP, OP_BLOCKUSER:
			str, err := r.ReadString()
			panicif(err != nil, "invalid string")
			if op == OP_BLOCKIP {
				store.markBlockedOrUnblocked("b" + str)
			} else {
				store.markBlockedOrUnblocked("u" + str)
			}
		case OP_STICKY, OP_ARCHIVE, OP_LOCK, OP_PURGE:
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
	return nil
}

func (store *Store) verifyTopics() {
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		if 0 == len(topic.Posts) {
			fmt.Printf("topics (%v) has no posts!\n", topic)
		}
	}
}

// NewStore creates a new store
func NewStore(dataDir, forumName string) (*Store, error) {
	store := &Store{
		dataDir:       dataDir,
		dataFilePath:  filepath.Join(dataDir, "forum", forumName+".txt"),
		forumName:     forumName,
		rootTopic:     &Topic{},
		endTopic:      &Topic{},
		blocked:       make(map[string]bool),
		MaxLiveTopics: 10000,
	}

	store.rootTopic.Next = store.endTopic
	store.endTopic.Prev = store.rootTopic

	var err error
	if u.PathExists(store.dataFilePath) {
		if err = store.readExistingData(store.dataFilePath); err != nil {
			fmt.Printf("readExistingData: %s\n", err)
			return nil, err
		}
	} else {
		f, err := os.Create(store.dataFilePath)
		if err != nil {
			fmt.Printf("can't create %s: %s\n", store.dataFilePath, err)
			return nil, err
		}
		f.Close()
	}

	store.verifyTopics()

	store.dataFile, err = os.OpenFile(store.dataFilePath, os.O_APPEND|os.O_RDWR, 0666)
	if err != nil {
		fmt.Printf("can't open %s: %s", store.dataFilePath, err)
		return nil, err
	}

	if false {
		r := rand.New()
		curTopicId := uint32(0)
		for i := 0; i < 20; i++ {
			wg := &sync.WaitGroup{}
			for i := 0; i < 1000; i++ {
				wg.Add(1)
				go func() {
					subject := base64.StdEncoding.EncodeToString(r.Fetch(16))
					msg := base64.StdEncoding.EncodeToString(r.Fetch(r.Intn(64) + 64))
					msg = strings.Repeat(msg, 4)
					userName := "abcdefgh"
					ipAddr := "127.0.0.1"

					if r.Intn(10) == 1 {
						curTopicId, _ = store.CreateNewTopic(subject, msg, userName, ipAddr)
					} else if curTopicId > 0 {
						store.AddPostToTopic(uint32(r.Intn(int(curTopicId))+1), msg, userName, ipAddr)
					}
					wg.Done()
				}()
			}
			wg.Wait()
			fmt.Println(i)
		}
	}
	return store, nil
}

func LoadSingleTopicInStore(path string) (*Topic, error) {
	store := &Store{
		rootTopic: &Topic{},
		endTopic:  &Topic{},
	}

	store.rootTopic.Next = store.endTopic
	store.endTopic.Prev = store.rootTopic

	var err error
	if err = store.readExistingData(path); err != nil {
		logger.Errorf("LoadSingleTopicInStore: %s", err)
		return nil, err
	}

	if store.rootTopic.Next == store.endTopic {
		return nil, fmt.Errorf("no topic in %s", path)
	}

	return store.rootTopic.Next, nil
}

// DoAction operates a topic
func (store *Store) DoAction(topicID uint32, action byte) {
	store.Lock()
	defer store.Unlock()
	t := store.topicByIDUnlocked(topicID)
	if t == nil {
		return
	}

	var p buffer
	switch action {
	case OP_STICKY:
		if err := store.append(p.WriteByte(OP_STICKY).WriteUInt32(topicID).Bytes()); err == nil {
			t.Sticky = !t.Sticky
			store.moveTopicToFront(t)
		}
	case OP_LOCK:
		if err := store.append(p.WriteByte(OP_LOCK).WriteUInt32(topicID).Bytes()); err == nil {
			t.Locked = !t.Locked
		}
	case OP_PURGE:
		if err := store.append(p.WriteByte(OP_PURGE).WriteUInt32(topicID).Bytes()); err == nil {
			t.Prev.Next = t.Next
			t.Next.Prev = t.Prev
		}
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
func (store *Store) GetTopics(nMax, from int, withDeleted bool) ([]*Topic, int) {
	res := make([]*Topic, 0, nMax)
	store.RLock()
	defer store.RUnlock()

	topic, i := store.rootTopic.Next, 0
	for ; topic != store.endTopic; topic, i = topic.Next, i+1 {
		if i >= from && i < from+nMax {
			res = append(res, topic)
		} else if i >= from+nMax {
			break
		}
	}

	return res, i
}

func (store *Store) topicByIDUnlocked(id uint32) *Topic {
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		if id == topic.ID {
			return topic
		}
	}
	return nil
}

// TopicByID returns topic given its id
func (store *Store) TopicByID(id uint32) *Topic {
	store.RLock()
	defer store.RUnlock()
	return store.topicByIDUnlocked(id)
}

func (store *Store) findPost(topicID uint32, postID uint16) (*Post, error) {
	topic := store.topicByIDUnlocked(topicID)
	if nil == topic {
		return nil, errors.New("can't find a topic with this ID")
	}
	if int(postID) > len(topic.Posts) {
		return nil, errors.New("can't find post with this ID")
	}

	return &topic.Posts[postID-1], nil
}

func (store *Store) append(buf []byte) error {
	_, err := store.dataFile.Write(buf)
	if err != nil {
		fmt.Printf("appendString() error: %s\n", err)
	}
	return err
}

// DeletePost deletes/restores a post
func (store *Store) DeletePost(topicID uint32, postID uint16) error {
	store.Lock()
	defer store.Unlock()
	post, err := store.findPost(topicID, postID)
	if err != nil {
		return err
	}
	var p buffer
	if err = store.append(p.WriteByte(OP_DELETE).WriteUInt32(topicID).WriteUInt16(postID).Bytes()); err != nil {
		return err
	}
	post.IsDeleted = !post.IsDeleted
	return nil
}

func (store *Store) moveTopicToFront(topic *Topic) {
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

func (store *Store) addNewPost(msg, user, ipAddr string, topic *Topic, newTopic bool) error {
	nextID := len(topic.Posts) + 1
	if nextID >= 65536 {
		return errTooManyPosts
	}

	user = strings.Replace(user, "|", "", -1)
	ipAddrBytes := ipAddrToInternal(ipAddr)
	p := &Post{
		ID:        uint16(nextID),
		CreatedOn: uint32(time.Now().Unix()),
		User:      user,
		IP:        ipAddrBytes,
		IsDeleted: false,
		Topic:     topic,
		Message:   msg,
	}

	var topicStr buffer
	if newTopic {
		topicStr.WriteByte(OP_TOPIC)
		topicStr.WriteUInt32(uint32(topic.ID))
		topicStr.WriteString(topic.Subject)
	}

	topicStr.WriteByte(OP_POST)
	topicStr.WriteUInt32(uint32(topic.ID))
	topicStr.WriteUInt16(uint16(p.ID))
	topicStr.WriteUInt32(p.CreatedOn)
	topicStr.Write8Bytes(ipAddrBytes)
	topicStr.WriteString(user)
	topicStr.WriteString(msg)

	if err := store.append(topicStr.Bytes()); err != nil {
		return err
	}

	topic.Posts = append(topic.Posts, *p)
	store.moveTopicToFront(topic)
	topic.CreatedOn = p.CreatedOn
	return nil
}

func (store *Store) BuildArchivePath(topicID uint32) string {
	id1, id2 := int(topicID)/100000, int(topicID)/1000
	return filepath.Join(store.dataDir, "archive", store.forumName, strconv.Itoa(id1), strconv.Itoa(id2), strconv.Itoa(int(topicID)))
}

func archive(topic *Topic, saveToPath string) error {
	topic.Prev.Next = topic.Next
	topic.Next.Prev = topic.Prev

	buf := &buffer{}
	buf.WriteByte(OP_TOPIC).WriteUInt32(topic.ID).WriteString(topic.Subject)

	for _, p := range topic.Posts {
		if p.IsDeleted {
			continue
		}

		buf.WriteByte(OP_POST).WriteUInt32(topic.ID).WriteUInt16(p.ID).WriteUInt32(p.CreatedOn).Write8Bytes(p.IP).WriteString(p.User).WriteString(p.Message)
	}

	u.CreateDirForFileMust(saveToPath)
	return ioutil.WriteFile(saveToPath, buf.Bytes(), 0777)
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

	info := &bytes.Buffer{}
	info.WriteString("archive:")
	for topic != store.endTopic.Prev && topic != store.endTopic {
		t := store.endTopic.Prev
		var p buffer
		if err := store.append(p.WriteByte(OP_ARCHIVE).WriteUInt32(t.ID).Bytes()); err != nil {
			info.WriteString(fmt.Sprintf(" %d(%v)", t.ID, err))
			continue
		}
		err := archive(t, store.BuildArchivePath(t.ID))
		if err == nil {
			info.WriteString(fmt.Sprintf(" %d(ok)", t.ID))
		} else {
			info.WriteString(fmt.Sprintf(" %d(%v)", t.ID, err))
		}
	}
	logger.Notice(info.String())
}

// CreateNewTopic creates a new topic
func (store *Store) CreateNewTopic(subject, msg, user, ipAddr string) (uint32, error) {
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

	err := store.addNewPost(msg, user, ipAddr, topic, true)
	if err == nil {
		store.topicsCount++
	}

	if randG.Intn(64) == 0 {
		go store.Archive()
	}

	return topic.ID, err
}

// AddPostToTopic adds a post to a topic
func (store *Store) AddPostToTopic(topicID uint32, msg, user, ipAddr string) error {
	store.Lock()
	defer store.Unlock()

	topic := store.topicByIDUnlocked(topicID)
	if topic == nil {
		return errors.New("invalid topic ID")
	}
	err := store.addNewPost(msg, user, ipAddr, topic, false)
	if err == errTooManyPosts {
		var p buffer
		if err = store.append(p.WriteByte(OP_LOCK).WriteUInt32(topicID).Bytes()); err == nil {
			topic.Locked = true
		}
	}
	return err
}

// BlockIP blocks/unblocks IP address
func (store *Store) BlockIP(ipAddrInternal string) {
	store.Lock()
	defer store.Unlock()
	if len(ipAddrInternal) == 0 {
		return
	}
	var p buffer // := fmt.Sprintf("B%s\n", ipAddrInternal)
	if err := store.append(p.WriteByte(OP_BLOCKIP).WriteString(ipAddrInternal).Bytes()); err == nil {
		store.markBlockedOrUnblocked("b" + ipAddrInternal)
	}
}

// BlockUser blocks/unblocks user
func (store *Store) BlockUser(username string) {
	store.Lock()
	defer store.Unlock()
	var p buffer // := fmt.Sprintf("B%s\n", ipAddrInternal)
	if err := store.append(p.WriteByte(OP_BLOCKUSER).WriteString(username).Bytes()); err == nil {
		store.markBlockedOrUnblocked("u" + username)
	}
}

// IsBlocked checks if the term is blocked
func (store *Store) IsBlocked(term string) bool {
	store.RLock()
	defer store.RUnlock()
	if len(term) == 9 && term[0] == 'b' {
		//     bAABBCCDD /32          bAABBCC /24                bAABBC /20                 bAABB /16
		return store.blocked[term] || store.blocked[term[:7]] || store.blocked[term[:6]] || store.blocked[term[:5]]
	}
	return store.blocked[term]
}

// GetPostsByUserInternal returns posts by user
func (store *Store) GetPostsByUserInternal(userNameInternal string, max int) ([]Post, int) {
	return store.getPostsBy(userNameInternal, max, false, true)
}

// GetPostsByIPInternal returns posts from an ip address
func (store *Store) GetPostsByIPInternal(ipAddrInternal string, max int) ([]Post, int) {
	return store.getPostsBy(ipAddrInternal, max, true, false)
}

func (store *Store) getPostsBy(term string, max int, ip, name bool) ([]Post, int) {
	store.RLock()
	defer store.RUnlock()
	res, total := make([]Post, 0), 0
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		for _, post := range topic.Posts {
			if ip {
				if strings.Contains(IPAddress(post.IP), term) {
					total++
					if total <= max {
						res = append(res, post)
					}
				}
			}
			if name {
				if strings.HasPrefix(post.UserName(), term) {
					total++
					if total <= max {
						res = append(res, post)
					}
				}
			}
		}
	}
	return res, total
}

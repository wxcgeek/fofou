// This code is in Public Domain. Take all the code you want, I'll just write more.
package main

import (
	"bufio"
	"bytes"
	"encoding/ascii85"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coyove/common/rand"
	"github.com/kjk/u"
)

type Post struct {
	Message   []byte
	Internal  string // form: username|ip
	CreatedOn uint32
	ID        uint16
	IsDeleted bool
	Topic     *Topic // for convenience, we link to the topic this post belongs to
}

func (p *Post) IPAddress() string { return ipAddrInternalToOriginal(p.IPAddressInternal()) }

func (p *Post) IsGithubUser() bool { return strings.HasPrefix(p.Internal, "g:") }

func (p *Post) UserName() string {
	if p.IsGithubUser() {
		return p.UserNameInternal()[2:]
	}
	return p.UserNameInternal()
}

func (p *Post) UserNameInternal() string { return p.Internal[:strings.Index(p.Internal, "|")] }

func (p *Post) IPAddressInternal() string { return p.Internal[strings.Index(p.Internal, "|")+1:] }

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
	ID        int
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
	topicsCount  int

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

func findPostToDelUndel(d []byte, topicIDToTopic map[int]*Topic) (*Post, error) {
	parts := strings.Split(string(d[1:]), "|")
	if len(parts) != 2 {
		panic("len(parts) != 2")
	}
	topicID, err1 := strconv.Atoi(parts[0])
	postID, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		panic("invalid postID/topicID")
	}
	topic, ok := topicIDToTopic[topicID]
	if !ok {
		return nil, fmt.Errorf("no topic with that id")
	}
	if postID > len(topic.Posts) {
		return nil, fmt.Errorf("invalid postId")
	}
	return &topic.Posts[postID-1], nil
}

// parse:
// T$id|$subject
func parseTopic(line []byte) *Topic {
	idx := bytes.IndexByte(line, '|')
	if idx == -1 {
		panic("corrupted topic")
	}
	subject := string(line[idx+1:])
	idStr := string(line[1:idx])
	id, err := strconv.Atoi(idStr)
	if err != nil {
		panic("idStr is not a number")
	}
	return &Topic{
		ID:      id,
		Subject: subject,
		Posts:   make([]Post, 0),
	}
}

// parse:
// P1|1|1148874103|4b0af66e|Krzysztof Kowalczyk|message in ascii85 format
func parsePost(line []byte, topicIDToTopic map[int]*Topic) Post {
	s := line
	var idx int
	readNextSep := func() {
		s = s[idx+1:]
		idx = bytes.IndexByte(s, '|')
		if idx == -1 {
			panic("invalid format")
		}
	}

	readNextSep()
	topicID, err := strconv.Atoi(string(s[:idx]))
	if err != nil {
		panic("topicIdStr not a number")
	}

	readNextSep()
	id, err := strconv.Atoi(string(s[:idx]))
	if err != nil {
		panic("idStr not a number")
	}

	readNextSep()
	createdOnSeconds, err := strconv.Atoi(string(s[:idx]))
	if err != nil {
		panic("createdOnSeconds not a number")
	}

	readNextSep()
	ipAddrInternal := string(s[:idx])

	readNextSep()
	userName := string(s[:idx])

	s = s[idx+1:]
	messageBuf := make([]byte, len(s)*2)
	ndst, nsrc, err := ascii85.Decode(messageBuf, s, true)
	if nsrc != len(s) || err != nil {
		fmt.Println(s, nsrc, err)
		panic("error reading message")
	}

	t, ok := topicIDToTopic[topicID]
	if !ok {
		fmt.Printf("didn't find topic with the given topicId")
		return Post{}
	}

	realPostID := len(t.Posts) + 1
	if id != realPostID {
		fmt.Printf("!Unexpected post id:\n")
		fmt.Printf("  %s\n", string(line))
		fmt.Printf("  id: %d, expectedId: %d, topicId: %d\n", topicID, id, realPostID)
		fmt.Printf("  %s\n", t.Subject)
	} else if realPostID >= 65536 {
		fmt.Printf("having more than 65536 posts in a single topic")
	}
	post := Post{
		ID:        uint16(realPostID),
		CreatedOn: uint32(createdOnSeconds),
		Internal:  userName + "|" + ipAddrInternal,
		IsDeleted: false,
		Topic:     t,
		Message:   messageBuf[:ndst],
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

	topicIDToTopic := make(map[int]*Topic)
	r := bufio.NewReader(fh)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			break
		}

		line = line[:len(line)-1]
		if len(line) == 0 {
			continue
		}

		c := line[0]
		// T - topic
		// P - post
		// D - delete post
		// B - block IP
		// U - block user
		// S - sticky topic
		// L - lock topic
		// X - purge topic
		switch c {
		case 'T':
			t := parseTopic(line)
			store.moveTopicToFront(t)
			store.topicsCount++
			topicIDToTopic[t.ID] = t
		case 'P':
			post := parsePost(line, topicIDToTopic)
			if post.ID == 0 {
				break
			}
			t := post.Topic
			t.Posts = append(t.Posts, post)
			t.CreatedOn = post.CreatedOn
			store.moveTopicToFront(t)
		case 'D':
			// D|1234|1
			post, err := findPostToDelUndel(line, topicIDToTopic)
			if err != nil {
				logger.Errorf("%v", err)
				break
			}
			post.IsDeleted = !post.IsDeleted
		case 'B':
			store.markBlockedOrUnblocked("b" + string(line[1:]))
		case 'U':
			store.markBlockedOrUnblocked("u" + string(line[1:]))
		case 'S', 'L', 'A', 'X':
			topicID, err := strconv.Atoi(string(line[1:]))
			if err != nil {
				panic(err)
			}

			t := topicIDToTopic[topicID]
			if t == nil {
				logger.Errorf("can't find the topic to slax: %d, %s", topicID, string(c))
				break
			}

			switch c {
			case 'S':
				if t.Sticky = !t.Sticky; t.Sticky {
					store.moveTopicToFront(t)
				}
			case 'L':
				t.Locked = !t.Locked
			case 'A', 'X':
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

	r := rand.New()
	_ = r
	// curTopicId := 0
	// for i := 0; i < 20000; i++ {
	// 	wg := &sync.WaitGroup{}
	// 	for i := 0; i < 1000; i++ {
	// 		wg.Add(1)
	// 		go func() {
	// 			subject := base64.StdEncoding.EncodeToString(r.Fetch(16))
	// 			msg := base64.StdEncoding.EncodeToString(r.Fetch(r.Intn(256) + 256))
	// 			userName := "abcdefgh"
	// 			ipAddr := "7f000001"

	// 			if r.Intn(10) == 1 {
	// 				curTopicId, _ = store.CreateNewTopic(subject, msg, userName, ipAddr)
	// 			} else if curTopicId > 0 {
	// 				store.AddPostToTopic(r.Intn(curTopicId)+1, msg, userName, ipAddr)
	// 			}
	// 			wg.Done()
	// 		}()
	// 	}
	// 	wg.Wait()
	// 	fmt.Println(i)
	// }
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
func (store *Store) DoAction(topicID int, action byte) {
	store.Lock()
	defer store.Unlock()
	t := store.topicByIDUnlocked(topicID)
	if t == nil {
		return
	}

	switch action {
	case 'S':
		if err := store.appendString(fmt.Sprintf("S%d\n", topicID)); err == nil {
			t.Sticky = !t.Sticky
			store.moveTopicToFront(t)
		}
	case 'L':
		if err := store.appendString(fmt.Sprintf("L%d\n", topicID)); err == nil {
			t.Locked = !t.Locked
		}
	case 'X':
		if err := store.appendString(fmt.Sprintf("X%d\n", topicID)); err == nil {
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
	return store.topicsCount
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

func (store *Store) topicByIDUnlocked(id int) *Topic {
	for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
		if id == topic.ID {
			return topic
		}
	}
	return nil
}

// TopicByID returns topic given its id
func (store *Store) TopicByID(id int) *Topic {
	store.RLock()
	defer store.RUnlock()
	return store.topicByIDUnlocked(id)
}

func (store *Store) findPost(topicID, postID int) (*Post, error) {
	topic := store.topicByIDUnlocked(topicID)
	if nil == topic {
		return nil, errors.New("didn't find a topic with this id")
	}
	if postID > len(topic.Posts) {
		return nil, errors.New("didn't find post with this id")
	}

	return &topic.Posts[postID-1], nil
}

func (store *Store) appendString(str string) error {
	_, err := store.dataFile.WriteString(str)
	if err != nil {
		fmt.Printf("appendString() error: %s\n", err)
	}
	return err
}

// DeletePost deletes/restores a post
func (store *Store) DeletePost(topicID, postID int) error {
	store.Lock()
	defer store.Unlock()
	post, err := store.findPost(topicID, postID)
	if err != nil {
		return err
	}
	str := fmt.Sprintf("D%d|%d\n", topicID, postID)
	if err = store.appendString(str); err != nil {
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
	msgBytes := plane0StringToBytes(msg)
	nextID := len(topic.Posts) + 1
	if nextID >= 65536 {
		return errTooManyPosts
	}

	user = strings.Replace(user, "|", "", -1)
	ipAddr = strings.Replace(ipAddrToInternal(ipAddr), "|", "", -1)
	p := &Post{
		ID:        uint16(nextID),
		CreatedOn: uint32(time.Now().Unix()),
		Internal:  user + "|" + ipAddr,
		IsDeleted: false,
		Topic:     topic,
		Message:   msgBytes,
	}

	topicStr := ""
	if newTopic {
		topicStr = fmt.Sprintf("T%d|%s\n", topic.ID, topic.Subject)
	}

	s1 := fmt.Sprintf("%d", p.CreatedOn)
	messageBuf := make([]byte, ascii85.MaxEncodedLen(len(msgBytes)))
	n := ascii85.Encode(messageBuf, msgBytes)

	postStr := fmt.Sprintf("P%d|%d|%s|%s|%s|%s\n", topic.ID, p.ID, s1, ipAddr, user, string(messageBuf[:n]))

	str := topicStr + postStr
	if err := store.appendString(str); err != nil {
		return err
	}

	topic.Posts = append(topic.Posts, *p)
	store.moveTopicToFront(topic)
	topic.CreatedOn = p.CreatedOn
	return nil
}

func (store *Store) BuildArchivePath(topicID int) string {
	id1, id2 := topicID/100000, topicID/1000
	return filepath.Join(store.dataDir, "archive", store.forumName, strconv.Itoa(id1), strconv.Itoa(id2), strconv.Itoa(topicID))
}

func archive(topic *Topic, saveToPath string) error {
	topic.Prev.Next = topic.Next
	topic.Next.Prev = topic.Prev

	buf := &bytes.Buffer{}
	buf.WriteString(fmt.Sprintf("T%d|%s\n", topic.ID, topic.Subject))

	for _, p := range topic.Posts {
		if p.IsDeleted {
			continue
		}

		s1 := fmt.Sprintf("%d", p.CreatedOn)
		messageBuf := make([]byte, ascii85.MaxEncodedLen(len(p.Message)))
		n := ascii85.Encode(messageBuf, p.Message)

		parts := strings.Split(p.Internal, "|")
		buf.WriteString(fmt.Sprintf("P%d|%d|%s|%s|%s|%s\n",
			topic.ID, p.ID, s1, parts[1], parts[0], string(messageBuf[:n])))
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
		if err := store.appendString(fmt.Sprintf("A%d\n", t.ID)); err != nil {
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
func (store *Store) CreateNewTopic(subject, msg, user, ipAddr string) (int, error) {
	store.Lock()
	defer store.Unlock()

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
func (store *Store) AddPostToTopic(topicID int, msg, user, ipAddr string) error {
	store.Lock()
	defer store.Unlock()

	topic := store.topicByIDUnlocked(topicID)
	if topic == nil {
		return errors.New("invalid topicID")
	}
	err := store.addNewPost(msg, user, ipAddr, topic, false)
	if err == errTooManyPosts {
		if err = store.appendString(fmt.Sprintf("L%d\n", topicID)); err == nil {
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
	s := fmt.Sprintf("B%s\n", ipAddrInternal)
	if err := store.appendString(s); err == nil {
		store.markBlockedOrUnblocked("b" + ipAddrInternal)
	}
}

// BlockUser blocks/unblocks user
func (store *Store) BlockUser(username string) {
	store.Lock()
	defer store.Unlock()
	s := fmt.Sprintf("U%s\n", username)
	if err := store.appendString(s); err == nil {
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
				if strings.Contains(post.Internal, "|"+term) {
					total++
					if total <= max {
						res = append(res, post)
					}
				}
			}
			if name {
				if strings.HasPrefix(post.Internal, term) {
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

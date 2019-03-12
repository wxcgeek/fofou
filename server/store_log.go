package server

import (
	"bufio"
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coyove/common/rand"
)

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

func parseTopic(r *buffer, store *Store) *Topic {
	id, err := r.ReadUInt32()
	panicif(err != nil, "invalid ID")

	subject, err := r.ReadString()
	panicif(err != nil, "invalid subject")

	return &Topic{
		ID:      id,
		Subject: subject,
		Posts:   make([]Post, 0),
		store:   store,
	}
}

func parsePost(r *buffer, topicIDToTopic map[uint32]*Topic) Post {
	topicID, err := r.ReadUInt32()
	panicif(err != nil, "invalid topic ID")

	id, err := r.ReadUInt16()
	panicif(err != nil, "invalid post ID")

	deleted, err := r.ReadBool()
	panicif(err != nil, "invalid deletion marker")

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
	panicif(realPostID > 4000, "too many posts")

	return Post{
		ID:        uint16(realPostID),
		CreatedAt: createdOnSeconds,
		user:      userName,
		ip:        ipAddrInternal,
		// if this flag is set to true (only can be done in archive()), then no other OP_DELETE shall be performed on this post
		IsDeleted: deleted,
		Topic:     t,
		Message:   message,
		Image:     image,
	}
}

func (store *Store) loadDB(path string, slient bool) (err error) {
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()

	topicIDToTopic := make(map[uint32]*Topic)
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

	header := [16]byte{}
	if n, err := fh.Read(header[:]); err != nil || n != 16 ||
		header[0] != 'z' || header[1] != 'z' || header[2] != 'z' || header[3] > 1 {
		panic("invalid header")
	}

	fsize := int64(binary.BigEndian.Uint64(header[2+header[3]*6:]) & 0xffffffffffff)
	store.ptr = 16
	r := &buffer{}
	r.SetReader(bufio.NewReaderSize(fh, 1024*1024*10))
	r.pos = store.ptr

	for {
		panicif(r.pos > fsize, "invalid DB size")

		print("\rloading %.1f%% %d/%d", float64(r.pos*100)/float64(fsize), r.pos, fsize)
		atomic.StoreUintptr(&store.ready, uintptr(r.pos*1000/fsize))

		store.ptr = r.pos
		if r.pos == fsize {
			break
		}

		op, err := r.ReadByte()
		if err != nil {
			break
		}

		switch op {
		case OP_TOPIC:
			t := parseTopic(r, store)
			store.moveTopicToFront(t)
			store.topicsCount++
			panicif(topicIDToTopic[t.ID] != nil, "topic %d already existed", t.ID)
			topicIDToTopic[t.ID] = t
		case OP_TOPICNUM:
			num, err := r.ReadUInt32()
			panicif(err != nil, "invalid new topics counter")
			print("\ntopic counter updated, old: %d, new: %d\n", store.topicsCount, num)
			store.topicsCount = num
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
			panicif(err != nil, "invalid object to block")
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
			panicif(true, "unexpected line type: %x", op)
		}
	}

	atomic.StoreUintptr(&store.ready, 1000)
	print("\n")
	return nil
}

func NewStore(path string, password string, maxLiveTopics int, logger *Logger) *Store {
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
	store.block, _ = aes.NewCipher(bytes.Repeat([]byte(password+"0"), 16)[:16])

	_, err := os.Stat(path)
	if err != nil {
		f, err := os.Create(store.dataFilePath)
		panicif(err != nil, "can't create initial DB %s: %v", store.dataFilePath, err)
		_, err = f.Write([]byte{'z', 'z', 'z', 0, 0, 0, 0, 0, 0, 0x10, 0, 0, 0, 0, 0, 0x10})
		panicif(err != nil, "can't write initial DB %s: %v", store.dataFilePath, err)
		f.Close()
	}

	go func() {
		store.loadDB(store.dataFilePath, false)
		for topic := store.rootTopic.Next; topic != store.endTopic; topic = topic.Next {
			panicif(0 == len(topic.Posts), "topic %d has no posts!", topic.ID)
		}
		store.dataFile, err = os.OpenFile(store.dataFilePath, os.O_RDWR, 0666)
		panicif(err != nil, "can't open DB %s: %v", store.dataFilePath, err)
	}()

	if false {
		time.Sleep(time.Second)
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
						curTopicId, _ = store.NewTopic(subject, msg, "", userName, ipAddr)
					} else if curTopicId > 0 {
						store.NewPost(uint32(r.Intn(int(curTopicId))+1), msg, "", userName, ipAddr)
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

	// create a dummy store to load a single topic
	store = &Store{
		rootTopic: &Topic{},
		endTopic:  &Topic{},
	}

	store.rootTopic.Next = store.endTopic
	store.endTopic.Prev = store.rootTopic

	var err error
	if err = store.loadDB(path, true); err != nil {
		return Topic{}, err
	}

	if store.rootTopic.Next == store.endTopic {
		return Topic{}, fmt.Errorf("no topic in %s", path)
	}

	return *store.rootTopic.Next, nil
}

// append writes data onto disk with WAL
func (store *Store) append(buf []byte) error {
	// append data uncommitted
	_, err := store.dataFile.Seek(store.ptr, 0)
	if err != nil {
		return err
	}
	if _, err = store.dataFile.Write(buf); err != nil {
		return err
	}
	newptr, err := store.dataFile.Seek(0, 1)
	if err != nil {
		return err
	}

	// start committing header
	if _, err = store.dataFile.Seek(0, 0); err != nil {
		return err
	}
	tmp := [8]byte{}
	if _, err = store.dataFile.Read(tmp[:]); err != nil {
		return err
	}
	flag := tmp[3]
	binary.BigEndian.PutUint64(tmp[:], uint64(newptr))

	if flag == 0 {
		flag = 1
		_, err = store.dataFile.Seek(10, 0)
	} else {
		flag = 0
		_, err = store.dataFile.Seek(4, 0)
	}
	if err != nil {
		return err
	}

	if _, err = store.dataFile.Write(tmp[2:]); err != nil {
		return err
	}
	if _, err = store.dataFile.Seek(3, 0); err != nil {
		return err
	}
	if _, err = store.dataFile.Write([]byte{flag}); err != nil {
		return err
	}

	// all clear
	store.ptr = newptr
	return nil
}

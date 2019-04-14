package server

import (
	"encoding/json"
	"fmt"
	"os"
)

// BlockIP blocks/unblocks IP address
func (store *Store) Block(term [8]byte) error {
	store.Lock()
	defer store.Unlock()
	if term == default8Bytes {
		return nil
	}
	var p buffer // := fmt.Sprintf("B%s\n", ipAddrInternal)
	if err := store.append(p.WriteByte(OP_BLOCK).Write8Bytes(term).Bytes()); err != nil {
		return err
	}
	store.markBlockedOrUnblocked(term)
	return nil
}

// IsBlocked checks if the term is blocked
func (store *Store) IsBlocked(q [8]byte) bool {
	store.RLock()
	defer store.RUnlock()
	return store.blocked[q]
}

func (store *Store) DeletePost(u User, postLongID uint64, imageOnly bool, onImageDelete func(*Image)) error {
	store.Lock()
	defer store.Unlock()

	post, err := store.getPostPtrUnlocked(postLongID)
	if err != nil {
		return err
	}

	if !u.Can(PERM_LOCK_SAGE_DELETE_FLAG) && u.ID != post.UserXor() {
		return fmt.Errorf("can't delete the post")
	}

	if imageOnly {
		onImageDelete(post.Image)
		return nil
	}

	var p buffer
	if err := store.append(p.WriteByte(OP_DELETE).WriteUInt32(post.Topic.ID).WriteUInt16(post.ID).Bytes()); err != nil {
		return err
	}

	post.InvertStatus(POST_ISDELETE)

	if post.Image != nil {
		onImageDelete(post.Image)
	}
	return nil
}

func (store *Store) FlagPost(u User, postLongID uint64, flag byte, callback func(p *Post)) error {
	store.Lock()
	defer store.Unlock()

	post, err := store.getPostPtrUnlocked(postLongID)
	if err != nil {
		return err
	}

	if !u.Can(PERM_LOCK_SAGE_DELETE_FLAG) && u.ID != post.UserXor() {
		return fmt.Errorf("can't flag the post")
	}

	var p buffer
	if err := store.append(p.WriteByte(flag).WriteUInt32(post.Topic.ID).WriteUInt16(post.ID).Bytes()); err != nil {
		return err
	}

	callback(post)
	return nil
}

func SnapshotStore(output string, store *Store) {
	os.Remove(output)
	dst, err := os.Create(output)
	panicif(err != nil, "%v", err)
	defer dst.Close()

	store.RLock()
	defer store.RUnlock()

	write := func(buf []byte) {
		_, err := dst.Write(buf)
		panicif(err != nil, "%v", err)
	}

	// header
	write([]byte{'z', 'z', 'z', 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	for topic := store.endTopic.Prev; topic != store.rootTopic; topic = topic.Prev {
		p := topic.marshal()
		write(p.Bytes())

		if topic.Locked {
			write(p.Reset().WriteByte(OP_LOCK).WriteUInt32(topic.ID).Bytes())
		}

		if topic.FreeReply {
			write(p.Reset().WriteByte(OP_FREEREPLY).WriteUInt32(topic.ID).Bytes())
		}

		if topic.Saged {
			write(p.Reset().WriteByte(OP_SAGE).WriteUInt32(topic.ID).Bytes())
		}

		if topic.Sticky {
			// TODO: should be written in ID asc order
			write(p.Reset().WriteByte(OP_STICKY).WriteUInt32(topic.ID).Bytes())
		}
	}

	var p buffer
	write(p.WriteByte(OP_TOPICNUM).WriteUInt32(store.topicsCount).Bytes())

	for k := range store.blocked {
		write(p.Reset().WriteByte(OP_BLOCK).Write8Bytes(k).Bytes())
	}

	write(p.Reset().WriteByte(OP_CONFIG).WriteString(store.configStr).Bytes())
	write(p.Reset().WriteByte(OP_MAXTOPICS).WriteUInt32(uint32(store.maxLiveTopics)).Bytes())

	n, err := dst.Seek(0, 1)
	panicif(err != nil, "%v", err)

	_, err = dst.Seek(4, 0)
	panicif(err != nil, "%v", err)

	write(p.Reset().WriteUInt48(uint64(n)).Bytes())
}

func (store *Store) SetMaxLiveTopics(num int) error {
	store.configLock.Lock()
	defer store.configLock.Unlock()

	if num < store.maxLiveTopics {
		// new maxLiveTopics is smaller than before
		// so archive those which should be archived
		if err := store.archiveJob(num); err != nil {
			return err
		}
	}

	var p buffer
	if err := store.append(p.WriteByte(OP_MAXTOPICS).WriteUInt32(uint32(num)).Bytes()); err != nil {
		return err
	}

	store.maxLiveTopics = num
	return nil
}

func (store *Store) GetConfig(v interface{}) error {
	store.configLock.RLock()
	defer store.configLock.RUnlock()
	if store.configStr == "" {
		return nil
	}
	return json.Unmarshal([]byte(store.configStr), v)
}

func (store *Store) UpdateConfig(v interface{}) error {
	store.configLock.Lock()
	defer store.configLock.Unlock()

	buf, _ := json.Marshal(v)

	var p buffer
	if err := store.append(p.WriteByte(OP_CONFIG).WriteString(string(buf)).Bytes()); err != nil {
		json.Unmarshal([]byte(store.configStr), v)
		return err
	}
	store.configStr = string(buf)
	return nil
}

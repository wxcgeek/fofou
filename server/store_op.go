package server

import (
	"fmt"
	"os"
)

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

func (store *Store) DeletePost(u User, postLongID uint64, imageOnly bool, onImageDelete func(*Image)) error {
	store.Lock()
	defer store.Unlock()

	post, err := store.getPostPtrUnlocked(postLongID)
	if err != nil {
		return err
	}

	if !u.Can(PERM_LOCK_SAGE_DELETE) && u.ID != post.user {
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

	post.IsDeleted = !post.IsDeleted

	if post.Image != nil {
		onImageDelete(post.Image)
	}
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

	n, err := dst.Seek(0, 1)
	panicif(err != nil, "%v", err)

	_, err = dst.Seek(4, 0)
	panicif(err != nil, "%v", err)

	write(p.Reset().WriteUInt48(uint64(n)).Bytes())
}

package server

import "fmt"

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

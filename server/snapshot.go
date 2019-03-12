package server

import (
	"os"
)

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

	n, err := dst.Seek(0, 1)
	panicif(err != nil, "%v", err)

	_, err = dst.Seek(4, 0)
	panicif(err != nil, "%v", err)

	write(p.Reset().WriteUInt48(uint64(n)).Bytes())
}

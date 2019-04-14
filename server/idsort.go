package server

import "bytes"

type IDRecord []byte

func NewIDRecord() IDRecord {
	return make(IDRecord, 0)
}

func (idr *IDRecord) Get(id [6]byte) int {
	i := bytes.Index(*idr, id[:])
	if i%6 != 0 {
		return -1
	}
	return i / 6
}

func (idr *IDRecord) Add(id [6]byte) int {
	i := bytes.Index(*idr, id[:])
	if i%6 != 0 {
		n := len(*idr)
		*idr = append(*idr, id[:]...)
		return n / 6
	}
	return i / 6
}

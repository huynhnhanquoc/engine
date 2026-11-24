// Package storage is "kitwork/storage": values by key in three places, memory(), disk() and datastore() (docs/
// KITWORK_STORAGE.md, moved from the first engine on 07/11/2026). A handle names a key; segment() and expire() make
// another handle and never change the one they are called on; the action ends the chain.
//
//	import { memory, disk, datastore } from "kitwork/storage";
//	const rates = memory("rates").expire("5m").get(() => loadRates());   // read, or compute once and keep
//	datastore("flags").segment("checkout").set({ newFlow: true });
//
// Each app has its storage (App): its sites share it, no app sees another's keys. Every value is kept encoded
// (codec.go), in the first engine's format, so a value it kept reads the same here, every read is a value of its own,
// and the three places give back the same value for the same write.
package storage

import (
	"strconv"
	"strings"
)

// Place is where a value is kept: memory(), disk() or datastore(). One name in two places is two keys.
type Place string

const (
	Memory    Place = "memory"
	Disk      Place = "disk"
	Datastore Place = "datastore"
)

// Key is a value's identity inside its app (mục 3): the place, the root name, the segments.
type Key struct {
	Place    Place
	Name     string
	Segments []string
}

// With is the key with segments added after its own; the key itself never changes.
func (k Key) With(segments ...string) Key {
	joined := make([]string, 0, len(k.Segments)+len(segments))
	joined = append(joined, k.Segments...)
	joined = append(joined, segments...)
	return Key{Place: k.Place, Name: k.Name, Segments: joined}
}

// Encoded is the key as the places hold it: the place, the name and each segment written with its length first,
// so no two keys ever meet (segment("a:b") is not segment("a", "b"), mục 4). The first engine's, to the byte: a
// value it kept is found under the same key.
func (k Key) Encoded() string {
	var builder strings.Builder
	write := func(part string) {
		builder.WriteString(strconv.Itoa(len(part)))
		builder.WriteByte(':')
		builder.WriteString(part)
	}
	write(string(k.Place))
	write(k.Name)
	for _, segment := range k.Segments {
		write(segment)
	}
	return builder.String()
}

// Human is the key a person reads, key() of the handle: the name and the segments joined by ":". It is not the key
// a place holds: "a:b" reads the same as "a", "b".
func (k Key) Human() string {
	if len(k.Segments) == 0 {
		return k.Name
	}
	return k.Name + ":" + strings.Join(k.Segments, ":")
}

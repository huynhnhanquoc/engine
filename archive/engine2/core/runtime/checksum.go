package runtime

import (
	"encoding/binary"
	"hash"
)

// checksumWriter is what a program's checksum is written through (programChecksum): the bytes go into
// its buffer, and only the buffer goes to the hash, a block at a time. A field written straight to the
// hash went through the hash.Hash interface, so every byte slice a helper made for it (a number's eight
// bytes, a string made bytes) escaped to the heap: 80% of what building a program allocated (survey,
// 03/11/2026). The bytes hashed are the same, so every checksum is what it was.
type checksumWriter struct {
	hash   hash.Hash
	used   int
	buffer [1024]byte
}

func newChecksumWriter(h hash.Hash) *checksumWriter {
	return &checksumWriter{hash: h}
}

// write copies data in, handing the buffer to the hash whenever it is full.
func (w *checksumWriter) write(data []byte) {
	for len(data) > 0 {
		if w.used == len(w.buffer) {
			w.flush()
		}
		copied := copy(w.buffer[w.used:], data)
		w.used += copied
		data = data[copied:]
	}
}

// writeText is write of a string's bytes, without making them a slice.
func (w *checksumWriter) writeText(text string) {
	for len(text) > 0 {
		if w.used == len(w.buffer) {
			w.flush()
		}
		copied := copy(w.buffer[w.used:], text)
		w.used += copied
		text = text[copied:]
	}
}

func (w *checksumWriter) writeByte(b byte) {
	if w.used == len(w.buffer) {
		w.flush()
	}
	w.buffer[w.used] = b
	w.used++
}

func (w *checksumWriter) flush() {
	w.hash.Write(w.buffer[:w.used])
	w.used = 0
}

// sum is the hash of everything written.
func (w *checksumWriter) sum() []byte {
	w.flush()
	return w.hash.Sum(nil)
}

// writeBytes writes data's length, then data.
func writeBytes(w *checksumWriter, data []byte) {
	writeUint64(w, uint64(len(data)))
	w.write(data)
}

// writeString writes a string as writeBytes writes its bytes.
func writeString(w *checksumWriter, text string) {
	writeUint64(w, uint64(len(text)))
	w.writeText(text)
}

func writeUint16(w *checksumWriter, number uint16) {
	var data [2]byte
	binary.BigEndian.PutUint16(data[:], number)
	w.write(data[:])
}

func writeUint32(w *checksumWriter, number uint32) {
	var data [4]byte
	binary.BigEndian.PutUint32(data[:], number)
	w.write(data[:])
}

func writeUint64(w *checksumWriter, number uint64) {
	var data [8]byte
	binary.BigEndian.PutUint64(data[:], number)
	w.write(data[:])
}

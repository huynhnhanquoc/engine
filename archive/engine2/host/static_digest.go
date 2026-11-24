package host

import "sync"

// staticDigests keeps the SHA-256 of the static files whose names look hashed, by file, size and time of
// change, so a file is read for it once and again only after it changes. A request that asks for one
// while another reads it waits for that read rather than reading it again. The store is the process's,
// every site's, and holds at most maxStaticDigests files: past that one is let go for each new one (the
// names that look hashed are few on a site).
type staticDigests struct {
	mutex   sync.Mutex
	known   map[string]string        // key → digest
	reading map[string]chan struct{} // key → closed when its read ends
}

const maxStaticDigests = 4096

var staticHashes staticDigests

// digest is the digest of key, read once by read; false when read could not.
func (d *staticDigests) digest(key string, read func() (string, bool)) (string, bool) {
	d.mutex.Lock()
	if digest, ok := d.known[key]; ok {
		d.mutex.Unlock()
		return digest, true
	}
	if done, ok := d.reading[key]; ok {
		d.mutex.Unlock()
		<-done
		d.mutex.Lock()
		digest, ok := d.known[key]
		d.mutex.Unlock()
		return digest, ok
	}
	if d.reading == nil {
		d.known = map[string]string{}
		d.reading = map[string]chan struct{}{}
	}
	done := make(chan struct{})
	d.reading[key] = done
	d.mutex.Unlock()

	digest, ok := read()

	d.mutex.Lock()
	delete(d.reading, key)
	if ok {
		if len(d.known) >= maxStaticDigests {
			for other := range d.known { // one let go, whichever: the others stay
				delete(d.known, other)
				break
			}
		}
		d.known[key] = digest
	}
	d.mutex.Unlock()
	close(done)
	return digest, ok
}

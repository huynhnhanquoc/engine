package storage

import (
	"container/list"
	"strings"
	"sync"
	"time"
)

// memoryPlace is the RAM of memory() for one app (mục 2, 9): encoded values by key, each with the time it expires
// (zero for never), bounded by a number of keys and a number of bytes. Full, it pushes out the key used least
// recently; an expired value is gone for every read, and is dropped when it is read or reached at the cold end.
type memoryPlace struct {
	mu       sync.Mutex
	maxKeys  int
	maxBytes int
	bytes    int
	order    *list.List // front: used most recently
	items    map[string]*list.Element
}

type memoryEntry struct {
	key      string
	data     []byte
	expireAt time.Time
}

func newMemory(maxKeys, maxBytes int) *memoryPlace {
	return &memoryPlace{maxKeys: maxKeys, maxBytes: maxBytes, order: list.New(), items: map[string]*list.Element{}}
}

func entrySize(entry *memoryEntry) int { return len(entry.key) + len(entry.data) }

// live is the entry of key if it has not expired, dropping it if it has. Called with mu held.
func (m *memoryPlace) live(key string, now time.Time) *list.Element {
	element := m.items[key]
	if element == nil {
		return nil
	}
	if entry := element.Value.(*memoryEntry); expired(entry.expireAt, now) {
		m.drop(element)
		return nil
	}
	return element
}

func (m *memoryPlace) drop(element *list.Element) {
	entry := element.Value.(*memoryEntry)
	m.order.Remove(element)
	delete(m.items, entry.key)
	m.bytes -= entrySize(entry)
}

func (m *memoryPlace) get(key string) ([]byte, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	element := m.live(key, clock())
	if element == nil {
		return nil, false, nil
	}
	m.order.MoveToFront(element)
	return element.Value.(*memoryEntry).data, true, nil
}

func (m *memoryPlace) expiry(key string) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	element := m.live(key, clock())
	if element == nil {
		return time.Time{}, false, nil
	}
	return element.Value.(*memoryEntry).expireAt, true, nil
}

// set writes the value of key, then pushes out what the bounds no longer hold: what has expired at the cold end
// first, then the keys used least recently. The value written stays, even alone past the bounds of bytes: one value
// has its own ceiling.
func (m *memoryPlace) set(key string, data []byte, expireAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if element := m.items[key]; element != nil {
		m.drop(element)
	}
	entry := &memoryEntry{key: key, data: data, expireAt: expireAt}
	m.items[key] = m.order.PushFront(entry)
	m.bytes += entrySize(entry)
	now := clock()
	for element := m.order.Back(); element != nil && m.order.Len() > 1; {
		previous := element.Prev()
		cold := element.Value.(*memoryEntry)
		over := (m.maxKeys > 0 && m.order.Len() > m.maxKeys) || (m.maxBytes > 0 && m.bytes > m.maxBytes)
		if !expired(cold.expireAt, now) && !over {
			break
		}
		m.drop(element)
		element = previous
	}
	return nil
}

func (m *memoryPlace) remove(key string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	element := m.live(key, clock())
	if element == nil {
		return false, nil
	}
	m.drop(element)
	return true, nil
}

func (m *memoryPlace) clear(branch string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now, cleared := clock(), 0
	for key, element := range m.items {
		if strings.HasPrefix(key, branch) {
			if !expired(element.Value.(*memoryEntry).expireAt, now) {
				cleared++
			}
			m.drop(element)
		}
	}
	return cleared, nil
}

func (m *memoryPlace) close() {}

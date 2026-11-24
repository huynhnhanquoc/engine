package storage

import "time"

// answersPlace is the place kitwork/http keeps the answers of .persist() under, on the app's disk(): a place of its
// own, so no key a program writes ever meets one of them.
const answersPlace Place = "http"

// Answers is the disk of the app for what kitwork/http keeps of its answers (.persist(), engine2/docs/http.md mục
// 2.2), in .database/disk/ beside disk()'s values, counted against its ceilings.
func (a *App) Answers() *Answers { return &Answers{disk: a.places[Disk]} }

// Answers loads and saves kitwork/http's answers on the app's disk.
type Answers struct{ disk place }

// Load is the answer kept under key while it lasts.
func (k *Answers) Load(key string) ([]byte, bool) {
	data, found, err := k.disk.get(Key{Place: answersPlace, Name: key}.Encoded())
	return data, found && err == nil
}

// Save keeps an answer under key for ttl; a disk that refuses it (full, a link out of the app) keeps nothing, and the
// request's result is what it was.
func (k *Answers) Save(key string, data []byte, ttl time.Duration) {
	_ = k.disk.set(Key{Place: answersPlace, Name: key}.Encoded(), data, clock().Add(ttl))
}

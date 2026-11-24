package database

import "github.com/kitwork/engine2/core/value"

// FileOf is the file of the database a value of a program names, for the engine's own tables a module keeps beside the
// app's (kitwork/queue's runs: queue.database(db), docs/KITWORK_QUEUE.md mục 2): its path, and the database as a person
// reads it (connection(), connection("jobs")). ok is false for a value that is no database the app's .shared/schema/
// declares.
func FileOf(v value.Value) (path, name string, ok bool) {
	handle, _ := v.Host().(*Handle)
	if handle == nil {
		return "", "", false
	}
	return handle.entry.Path, label(handle.entry.Name, handle.entry.Schema), true
}

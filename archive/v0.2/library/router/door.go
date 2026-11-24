package router

import "github.com/kitwork/engine2/core/value"

// Door is one method of a folder: router.get(handler) is the folder's door for GET. Handler is the
// program's function the request runs, made when router.work ran at boot.
type Door struct {
	Handler *value.Lambda
	// At is where the door is declared, "router.work:3:1", for the error of a second one.
	At string
	// View is router.view(…): a page of .ui, which the engine answers 501 until it draws .ui pages.
	View bool
	// Rules are the guards and limits written before the door, router.guard(staff).post(…), in that order
	// (rules.go): they run after every rule of the folders.
	Rules []Rule
}

package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/kitwork/engine2/core/runtime"
	"github.com/kitwork/engine2/library/ui/kernel"
)

// Site is one site folder written in .ui, apps/<identity>/<domain>/ (docs/KITWORK_UI.md mục 4): where its
// pages, its frame, its regions and its pieces are found.
type Site struct {
	// Root is the site's folder; App is its app's root (a component's interface beside the file that declares it is
	// found through the page's Components, sitecomponent.go).
	Root string
	App  string
	// Pool is the host's (core/runtime, pool.go), which the page's expressions take from as every run does; nil for
	// none. MaxBytes is what one expression makes at most, the site's runs' own (app.limits); 0 for the default.
	Pool     *runtime.Pool
	MaxBytes uint64
	// Read reads a file; nil reads the disk. A test gives the files it writes.
	Read func(path string) ([]byte, error)
	// expressions are the site's expressions compiled, by their source (expression.go)
	expressions sync.Map
	// images are the sizes of the site's images read so far, by path (image.go)
	images sync.Map
	// told are the warnings said in the terminal, by their line (warning.go)
	told sync.Map
	// files are the site's .ui files read so far, each the tree it parsed into, by path; present is what exists said
	// of each path asked: both for the boot's life (kept.go)
	files   sync.Map
	present sync.Map
	// parts are the parts of its pages drawn so far, each what drawing it wrote, by where it stands; boundParts
	// counts those kept by address (frozen.go)
	parts      sync.Map
	openings   sync.Map
	boundParts atomic.Int64
	// pages are the pages assembled, by route and kind; sheets the page stylesheets written, by look and trace, and
	// masks the icons' stylesheets, by the icons; keptSheets counts both (kept.go)
	pages      sync.Map
	sheets     sync.Map
	masks      sync.Map
	keptSheets atomic.Int64
	// handlers are the handlers of the site's events as compiled, by their source; framedPages the frames of the
	// pages assembled with the parts they do not write, by page; looks the stylesheets of the looks, by look (kept.go)
	handlers sync.Map
	// browser is each source the kernel runs, as JavaScript, by its source and kind; programs each page's program
	// of them, by the key of the values it holds (program.go)
	browser      sync.Map
	programsLock sync.RWMutex
	programs     map[programKey]*kernel.File
	framedPages  sync.Map
	looks        sync.Map
	facts        sync.Map
	// reads are the classes the stylesheet read, by palette and intent, and domains the values an attribute from data
	// takes; keptReads counts them (kept.go)
	reads     sync.Map
	domains   sync.Map
	codes     sync.Map
	tokenized sync.Map
	keptReads atomic.Int64
	// machines are the VMs the site's expressions ran on, for the next page; sizes the sizes of its pages, by page
	machines sync.Pool
	sizes    sync.Map
	// lints are the warnings each node's writing gives, by its source; colours the theme-color of each backdrop, by
	// look and ground; aliased the components with an alias, by page (kept.go)
	lints   sync.Map
	colours sync.Map
	aliased sync.Map
	// pageKeys are the keys of the pages that keep parts, one pointer for each value (frozen.go)
	pageKeys sync.Map
	// editorPlans are the plans of the editors of the pages assembled, by node; richtexts the look of a richtext, by
	// palette (kept.go)
	editorPlans sync.Map
	richtexts   sync.Map
	// spelled says of each value written with {{ }} whether it is data (fromData); prefixes are the prefixes read
	// (readPrefix) (kept.go)
	spelled  sync.Map
	prefixes sync.Map
	// highlights are the code written in its colours, by palette, language and text (kept.go)
	highlights sync.Map
	// tabPlans are the plans of the tabs of each page assembled for the boot (kept.go)
	tabPlans sync.Map
}

func (s *Site) read(path string) ([]byte, error) {
	if s.Read != nil {
		return s.Read(path)
	}
	return os.ReadFile(path)
}

// exists says a file is at path, asked once for the boot (kept.go).
func (s *Site) exists(path string) bool {
	if kept, ok := s.present.Load(path); ok {
		return kept.(bool)
	}
	there, err := s.look(path)
	if keepable(err) {
		s.present.Store(path, there)
	}
	return there
}

// look says a file is at path, and what the look failed on.
func (s *Site) look(path string) (bool, error) {
	if s.Read != nil {
		_, err := s.Read(path)
		return err == nil, err
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir(), err
}

// upward are the folders from folder up to the site's root, the nearest first.
func (s *Site) upward(folder string) []string {
	root := filepath.Clean(s.Root)
	folder = filepath.Clean(folder)
	var out []string
	for {
		out = append(out, folder)
		if folder == root {
			return out
		}
		parent := filepath.Dir(folder)
		if parent == folder || !strings.HasPrefix(parent, root) {
			return out
		}
		folder = parent
	}
}

// find is the nearest <name>.ui from folder up to the site's root, "" when there is none.
func (s *Site) find(folder, name string) string {
	for _, each := range s.upward(folder) {
		if candidate := filepath.Join(each, name+".ui"); s.exists(candidate) {
			return candidate
		}
	}
	return ""
}

// relative is path as an error names it, from the site's folder, '/' between its parts.
func (s *Site) relative(path string) string {
	if rel, err := filepath.Rel(s.Root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(path)
}

// root gives the one root element of the file at path, a tree of the caller's own: a .ui file holds one element at its
// top, its spaces and comments aside. The file is read and parsed once for the boot (kept.go).
func (s *Site) root(path string) (*Node, error) {
	root, err, kept := s.parsedRoot(path)
	if kept {
		return root.clone(), err
	}
	return root, err
}

// shared gives the one root element of the file at path as the boot keeps it, shared by every page: read, never
// changed. A caller that changes it changes a copy.
func (s *Site) shared(path string) (*Node, error) {
	root, err, _ := s.parsedRoot(path)
	return root, err
}

// parsedRoot is the root of the file at path and why it could not be read; kept says the boot keeps it, read once: its
// nodes are then their own source (stand), the file's, which every copy of them names.
func (s *Site) parsedRoot(path string) (root *Node, err error, kept bool) {
	if file, ok := s.files.Load(path); ok {
		return file.(*parsed).root, file.(*parsed).err, true
	}
	root, err = s.parse(path)
	var failed *Error
	if keepable(err) || errors.As(err, &failed) {
		// what the file says, or that it is not there: the same for every page of the boot
		stand(root)
		file, _ := s.files.LoadOrStore(path, &parsed{root: root, err: err})
		return file.(*parsed).root, file.(*parsed).err, true
	}
	return root, err, false
}

// parse reads the file at path and parses its one root.
func (s *Site) parse(path string) (*Node, error) {
	source, err := s.read(path)
	if err != nil {
		return nil, err
	}
	name := s.relative(path)
	nodes, err := Parse(name, source)
	if err != nil {
		return nil, err
	}
	var root *Node
	for _, node := range nodes {
		if node.IsText() {
			if strings.TrimSpace(node.Text) != "" {
				return nil, &Error{File: name, Line: node.Line, Column: node.Column, Message: "chữ nằm ngoài gốc: một file .ui có đúng một phần tử ở ngoài cùng"}
			}
			continue
		}
		if root != nil {
			return nil, &Error{File: name, Line: node.Line, Column: node.Column, Message: "phần tử thứ hai ở ngoài cùng: một file .ui có đúng một gốc, <" + root.Tag + "> ở dòng " + strconv.Itoa(root.Line)}
		}
		root = node
	}
	if root == nil {
		return nil, &Error{File: name, Line: 1, Column: 1, Message: "file trống: một file .ui có đúng một gốc"}
	}
	return root, nil
}

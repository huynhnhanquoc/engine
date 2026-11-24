package host

import (
	"errors"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/router"
	"github.com/kitwork/engine2/library/ui"
)

// page is the folder's page drawn with data (library/ui, slice 1 of the .ui engine): content.ui in its frame,
// a whole HTML document. The error when the drawing failed, which is logged and answered (drawFailed).
func (s *Site) page(r *http.Request, folder *Folder, data value.Value, title, description string, bridge *ui.Bridge, draw *pageDraw) ([]byte, error) {
	route := filepath.Join(s.Folder, filepath.FromSlash(folder.Path))
	assembled, err := s.ui.Assembled(route)
	if err != nil {
		s.pageFailed(r, folder, err)
		return nil, err
	}
	return s.document(r, folder, assembled, data, title, description, bridge, draw)
}

// drawFailed answers a page that could not be drawn: 503, to be asked again in a second, when an expression of it was
// one the host could not give to (ui.ErrBusy); 500 otherwise. pageFailed logged why.
func drawFailed(w http.ResponseWriter, err error) {
	if errors.Is(err, ui.ErrBusy) {
		w.Header().Set("Retry-After", "1")
		answer(w, http.StatusServiceUnavailable, "Service Unavailable")
		return
	}
	answer(w, http.StatusInternalServerError, "Internal Server Error")
}

// pageHead is what a page of folder is drawn with for r: its own title and description over what router.meta of its
// folders says, its language and icon theirs.
func (s *Site) pageHead(r *http.Request, folder *Folder, title, description string, bridge *ui.Bridge) ui.Head {
	origin := "http://" + r.Host
	if r.TLS != nil {
		origin = "https://" + r.Host
	}
	developing := s.developing()
	if title == "" {
		title = folder.head.title()
	}
	if description == "" {
		description = folder.head.meta["description"]
	}
	return ui.Head{Title: title, Description: description, Language: folder.head.language, Favicon: folder.head.favicon,
		Path: r.URL.Path, Drive: s.drive, Origin: origin, Method: r.Method, Development: developing, Minify: !developing, Bridge: bridge}
}

// document draws an assembled page, and says once per page what slice 1 does not draw yet.
func (s *Site) document(r *http.Request, folder *Folder, assembled *ui.Node, data value.Value, title, description string, bridge *ui.Bridge, draw *pageDraw) ([]byte, error) {
	developing := s.developing()
	head := s.pageHead(r, folder, title, description, bridge)
	if draw != nil {
		head.Tab = draw.tab
	}
	if components := s.pageComponents(r); components != nil {
		defer components.release()
		components.draw = draw
		head.Components = components
	}
	document, notes, err := s.ui.DocumentBytes(assembled, data, head, folder.look)
	if err != nil {
		s.pageFailed(r, folder, err)
		return nil, err
	}
	if len(notes.Unread) > 0 {
		if _, said := s.unreadSaid.LoadOrStore(folder.Path, true); !said {
			s.log.Led(s.lead, folder.Path+": the .ui engine does not draw these yet (slice 1): "+strings.Join(notes.Unread, ", "))
		}
	}
	if developing {
		// each warning once while the site runs: a frame every page shares says its own once, not once a page
		for _, w := range notes.Warnings {
			if s.ui.Told(w) {
				s.log.Led(s.lead, "cảnh báo: "+w.String())
			}
		}
	}
	return document, nil
}

// developing says the site runs on a developer's machine (the website allows local): its pages' warnings are
// told, in the terminal and in the browser's console. In production they are not.
func (s *Site) developing() bool {
	return s.static != nil && s.static.local
}

// drawPage answers a GET of a folder that has a content.ui and no door of its own: the page, given nothing.
func (s *Site) drawPage(w http.ResponseWriter, r *http.Request, folder *Folder) {
	draw := s.newDraw(&tabReads{})
	body, err := s.page(r, folder, value.Value{}, "", "", nil, draw)
	if err != nil {
		drawFailed(w, err)
		return
	}
	s.keepDraw(draw)
	writePage(w, http.StatusOK, body, r.Method == http.MethodHead)
}

// drawBound answers context.bind(…) of router.view (docs/view.md mục 2): the folder's page drawn with the names it
// binds and those of the sync before it, sent with the answer's status, headers and cookies, never kept by a shared
// cache unless the handler says otherwise. A page whose names hold a work or a var, held, runs in the browser
// (docs/view.md mục 4, library/ui bridge.go); one whose names hold live ones is drawn for a tab, which the server
// remembers (tab.go), with reads, the keys the request's runs read.
func (s *Site) drawBound(w http.ResponseWriter, r *http.Request, folder *Folder, response *router.Response, held []router.Held, reads *tabReads) {
	names, title, description, _ := response.Bound()
	bridge := bridgeOf(names, held, response.Live())
	draw := s.newDraw(reads)
	if bridge != nil {
		draw.note("page", names, bridge.Live)
	}
	body, err := s.page(r, folder, names, title, description, bridge, draw)
	if err != nil {
		drawFailed(w, err)
		return
	}
	s.keepDraw(draw)
	w.Header().Set("Cache-Control", "no-store")
	response.Drawn(body)
	response.Write(w, r)
}

// drawView answers a handler's context.view(…): the page drawn with what the handler gave it, sent with the
// answer's status and headers.
func (s *Site) drawView(w http.ResponseWriter, r *http.Request, folder *Folder, response *router.Response) {
	data, title, description, _ := response.Page()
	body, err := s.page(r, folder, data, title, description, nil, nil)
	if err != nil {
		drawFailed(w, err)
		return
	}
	response.Drawn(body)
	response.Write(w, r)
}

// notFound answers a path no folder of the site has: the nearest notfound.ui from the site's root in its frame
// when the site has one (KITWORK_UI mục 4), else the engine's own text. Both are 404.
func (s *Site) notFound(w http.ResponseWriter, r *http.Request) {
	assembled, err := s.ui.AssembledNotFound(s.Folder)
	if err == nil && assembled != nil {
		if body, drawn := s.document(r, s.root, assembled, value.Value{}, "", "", nil, nil); drawn == nil {
			writePage(w, http.StatusNotFound, body, r.Method == http.MethodHead)
			return
		}
	}
	if err != nil {
		s.pageFailed(r, s.root, err)
	}
	answer(w, http.StatusNotFound, "Not Found")
}

// pageFailed logs why a page could not be drawn, at the page: the visitor's request, the folder's page, the
// error with its place in the .ui file; 503 for a host that could not give to an expression (ui.ErrBusy), 500 else.
func (s *Site) pageFailed(r *http.Request, folder *Folder, why error) {
	status := "500"
	if errors.Is(why, ui.ErrBusy) {
		status = "503"
	}
	s.log.Led(s.lead, visitorText(r.Method)+" "+visitorText(r.URL.Path)+" ("+path.Join(folder.Path, "content.ui")+"): "+status+": "+why.Error())
}

// writePage writes a page the engine drew: HTML, never stored by a shared cache, never sniffed.
func writePage(w http.ResponseWriter, status int, body []byte, head bool) {
	header := w.Header()
	header.Set("Content-Type", "text/html; charset=utf-8")
	header.Set("Cache-Control", "no-store")
	header.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if !head {
		w.Write(body)
	}
}

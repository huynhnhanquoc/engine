package host

import (
	"path"
	"strings"

	"github.com/kitwork/engine2/library/router"
)

// head is what a folder's pages start their <head> with (library/router head.go): the language and the icon of the
// nearest folder that says one, and router.meta merged key by key from the app down, the nearest winning. A page's
// own title and description are written over it.
type head struct {
	language, favicon string
	meta              map[string]string
}

// under is h with what scope declares over it; folder is the folder's path from the site's root, which a relative
// icon is read from.
func (h head) under(scope *router.Scope, folder string) head {
	if scope == nil {
		return h
	}
	out := h
	if len(scope.Meta) > 0 {
		out.meta = make(map[string]string, len(h.meta)+len(scope.Meta))
		for key, text := range h.meta {
			out.meta[key] = text
		}
		for key, text := range scope.Meta {
			out.meta[key] = text
		}
	}
	switch {
	case scope.Language != "":
		out.language = scope.Language
	case scope.Meta["language"] != "":
		out.language = scope.Meta["language"] // router.meta({ language }), as the first engine read it
	}
	if scope.Favicon != "" {
		out.favicon = iconAddress(scope.Favicon, folder)
	}
	return out
}

// title is the title a page starts with: router.meta's title, else its site.
func (h head) title() string {
	if title := h.meta["title"]; title != "" {
		return title
	}
	return h.meta["site"]
}

// iconAddress is the address a page links for an icon written in folder: a path written with / is the site's,
// another the folder's, an icon on another host as written.
func iconAddress(written, folder string) string {
	if strings.Contains(written, "://") {
		return written
	}
	if strings.HasPrefix(written, "/") {
		return path.Clean(written)
	}
	return path.Join("/", folder, path.Clean(strings.TrimPrefix(written, "./")))
}

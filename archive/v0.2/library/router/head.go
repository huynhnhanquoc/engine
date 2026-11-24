package router

// The words of a page's head (docs/KITWORK_ROUTER.md mục 4, KITWORK_UI mục 5 and 18; moved from the first engine on
// 07/11/2026): router.language, the lang of the folder's pages; router.favicon, the icon they link, and at a site's
// root the file /favicon.ico answers with; router.meta, the title, the site's name and the description every page
// of the folder starts with. language and favicon belong to the app, a site and a branch, the nearest folder winning;
// meta to a site and a branch, merged key by key down the folders.

import (
	"path"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// Language is router.language("vi"): the language of the folder's pages, written on <html lang>, a language code.
func (r *Router) Language(arguments ...value.Value) value.Value {
	if failure, ok := r.unready("language"); ok {
		return failure
	}
	if failure, ok := r.ended("language"); ok {
		return failure
	}
	if len(arguments) != 1 || !arguments[0].IsString() || strings.TrimSpace(arguments[0].Text()) == "" {
		return value.Fail(value.InvalidArgumentCode, `router.language(…) takes a language code: router.language("vi")`)
	}
	if r.Scope.Language != "" {
		return value.Fail(value.InvalidArgumentCode, "router.language(…) is declared twice: the first is at "+r.Scope.languageAt)
	}
	r.Scope.Language = strings.TrimSpace(arguments[0].Text())
	r.Scope.languageAt = r.Scope.where()
	return value.Undefined()
}

// Favicon is router.favicon("/assets/favicon.svg"): the icon the folder's pages link (<link rel="icon">), and at a
// site's root, or the app's, the file /favicon.ico answers with. A path written with / is the site's; another is
// the folder's; an icon on another host is linked as written. A dot file and a source of the site are never served.
func (r *Router) Favicon(arguments ...value.Value) value.Value {
	if failure, ok := r.unready("favicon"); ok {
		return failure
	}
	if failure, ok := r.ended("favicon"); ok {
		return failure
	}
	if len(arguments) != 1 || !arguments[0].IsString() || strings.TrimSpace(arguments[0].Text()) == "" {
		return value.Fail(value.InvalidArgumentCode, `router.favicon(…) takes the path of the icon: router.favicon("/assets/favicon.svg")`)
	}
	written := strings.TrimSpace(arguments[0].Text())
	if !strings.Contains(written, "://") {
		if problem := servedFile(written); problem != "" {
			return value.Fail(value.InvalidArgumentCode, `router.favicon("`+written+`"): `+problem)
		}
	}
	if r.Scope.Favicon != "" {
		return value.Fail(value.InvalidArgumentCode, "router.favicon(…) is declared twice: the first is at "+r.Scope.faviconAt)
	}
	r.Scope.Favicon, r.Scope.faviconAt = written, r.Scope.where()
	return value.Undefined()
}

// FaviconAt is where the folder's router.favicon(…) is written, "" when it writes none.
func (s *Scope) FaviconAt() string { return s.faviconAt }

// servedFile is what is wrong with a path a word publishes on disk, "" when nothing is: a dot folder or file (.env,
// .database/) or a source of the site (.work, .ui, .kitwork.) is never served (KITWORK_UI mục 5), whatever path
// names it.
func servedFile(written string) string {
	clean := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(written, "./")), "/")
	if clean == "" || clean == "." {
		return "a path names a file of the site, /assets/favicon.svg"
	}
	for _, segment := range strings.Split(clean, "/") {
		if strings.HasPrefix(segment, ".") {
			return "a dot file or folder (.env, .database/) is never served: a public file goes in assets/"
		}
	}
	lower := strings.ToLower(clean)
	if strings.HasSuffix(lower, ".work") || strings.HasSuffix(lower, ".ui") || strings.Contains(lower, ".kitwork.") {
		return "a source of the site (.work, .ui, .kitwork.*) is never served"
	}
	return ""
}

// Meta is router.meta({ title, site, description, … }): what every page of the folder starts with, each key a text;
// a page's own title and description (context.title, context.description) are written over it. Written again, in
// the folder or below it, it adds key by key, the nearest winning. The app root has none: a site says its own.
func (r *Router) Meta(arguments ...value.Value) value.Value {
	if failure, ok := r.unready("meta"); ok {
		return failure
	}
	if failure, ok := r.ended("meta"); ok {
		return failure
	}
	if r.Scope.Level == LevelApp {
		return value.Fail(value.InvalidArgumentCode, "router.meta(…) at the app root: the metadata of pages is a site's, written in its router.work or config.work, or a branch's")
	}
	if len(arguments) != 1 || !arguments[0].IsObject() {
		return value.Fail(value.InvalidArgumentCode, `router.meta(…) takes an object: router.meta({ title: "…", description: "…" })`)
	}
	object := arguments[0]
	for _, key := range object.Keys() {
		if !object.Get(key).IsString() {
			return value.Fail(value.InvalidArgumentCode, "router.meta({ "+key+" }): each key of router.meta is a text: router.meta({ "+key+`: "…" })`)
		}
	}
	if r.Scope.Meta == nil {
		r.Scope.Meta = map[string]string{}
	}
	for _, key := range object.Keys() {
		r.Scope.Meta[key] = object.Get(key).Text()
	}
	return value.Undefined()
}

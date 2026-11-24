package router

// The levels a router scope has (docs/KITWORK_ROUTER.md mục 4): the app root's config.work, a site's
// root folder, a branch below it. A word belongs to some levels only.
const (
	LevelApp    = "app"
	LevelSite   = "site"
	LevelBranch = "branch"
)

// Scope is one folder's router as boot fills it: what its config.work and router.work declare, the two
// halves of one scope (config.work runs first).
type Scope struct {
	Level string
	// Doors are the folder's doors by method, "get", "post", "put", "patch", "delete".
	Doors map[string]*Door
	// Branches are the chains of rules .methods(…) ended, in the order written, config.work's first: the
	// folder's and every folder's below it (rules.go).
	Branches []*Branch
	// Reload is router.reload(…) of the site's root: "true" loads each page whole, "false" moves between the
	// site's pages without a reload (Drive); "" when none is declared, which is "false" (KITWORK_UI mục 15).
	Reload string
	// reloadAt is where reload is declared, for the error of a second one.
	reloadAt string
	// Language is router.language(…) of the folder and Favicon its router.favicon(…), as written, "" when it writes
	// none; Meta is its router.meta({ … }), key by key (head.go). languageAt and faviconAt are where each is declared.
	Language, Favicon     string
	languageAt, faviconAt string
	Meta                  map[string]string
	// Where is the place of the call the running file makes now, "config.work:3:1".
	Where func() string
	// Notice prints one boot line of the site.
	Notice func(text string)
	// before is the first word of a chain waiting for its end, with where it is written, and beforeWord the word
	// itself; "" when none waits.
	before, beforeWord string
}

// Unended is the error of a chain no door and no .methods(…) ended, "" when none waits: the host asks it
// as each file ends, since the file's end is the last place its end could stand.
func (s *Scope) Unended() string {
	switch {
	case s.before == "":
		return ""
	case s.beforeWord == "guard" || s.beforeWord == "limit":
		return s.before + " stands alone: a rule stands before a door, router." + s.beforeWord + "(…).get(…) for that door, or before .methods(…), router." + s.beforeWord + "(…).methods(\"*\") for the folder and every folder below it"
	}
	return s.before + " stands before a door and none follows it: write the door on the same line, router.cache(\"5m\").get((context) => …)"
}

// NewScope is the scope of one folder at level, before its files run.
func NewScope(level string, where func() string, notice func(text string)) *Scope {
	return &Scope{Level: level, Doors: map[string]*Door{}, Where: where, Notice: notice}
}

func (s *Scope) where() string {
	if s.Where == nil {
		return ""
	}
	return s.Where()
}

package database

import (
	"context"
	"errors"
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/huynhnhanquoc/sqlitewire/fulltext"
	"github.com/huynhnhanquoc/sqlitewire/search"
	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/core/value"
)

// matches is full text (docs/KITWORK_DATABASE.md mục 2): posts.matches(q) over every .searchable() column of the
// table, posts.title.matches(q) over one, by words, without accents, every word required, the user's text read as it
// is; an empty text matches no row. The index is the table's in the old engine's database/fulltext, in <file>.search
// beside the database's file, the same one the database port's MATCH reads, folded up to date before each read from
// what KitDB's writer recorded in kitwork_history (a row written another way is not seen until the index is built
// again). The index gives the rowids that match, ranked; the rest of the condition runs as SQL on those rows. Without
// ascending or descending the rows come in the order of their score; with them, every row that matches is ordered.
// !posts.matches(q) only filters. A row comes back with score (what its matches give) and snippet (the passage of
// its best column, the words in <b>, HTML escaped).

const (
	// matchesCandidates is how many rows of the index one statement reads at most to rank or order a page.
	matchesCandidates = 30_000
	// snippetTokens and snippetContext are the words of a snippet, and those kept around a match.
	snippetTokens  = 18
	snippetContext = 4
)

// hit is a row the index gave, with what ranks it.
type hit struct {
	rowid int64
	score float64
	rank  int
}

// term is one matches of a statement, bound: the columns it reads and the user's text.
type term struct {
	fields []string
	text   string
}

func (t term) query() search.MatchQuery { return search.MatchQuery{Fields: t.fields, Text: t.text} }

// fullText is the index of table in the entry's file, the file held for the catalog until it closes.
func (c *Catalog) fullText(entry *Entry, table *Table) (*fulltext.Table, value.Value) {
	path, err := filepath.Abs(entry.Path)
	if err != nil {
		return nil, value.Fail(value.UnavailableCode, err.Error())
	}
	c.mutex.Lock()
	if c.searches[path] == nil {
		release, err := fulltext.Shared().Acquire(path)
		if err != nil {
			c.mutex.Unlock()
			return nil, value.Fail(value.UnavailableCode, "matches trên "+table.Name+": "+err.Error())
		}
		c.searches[path] = release
	}
	c.mutex.Unlock()
	var columns []fulltext.Column
	for _, f := range table.Fields {
		if f.Searchable {
			columns = append(columns, fulltext.Column{Name: f.Name, Boost: 1})
		}
	}
	sort.Slice(columns, func(i, j int) bool { return columns[i].Name < columns[j].Name })
	index, err := fulltext.Shared().Open(path, table.Name, filepath.Base(filepath.Dir(path))+"."+table.Name, columns, false)
	if err != nil {
		return nil, searchFailure(table.Name, err)
	}
	return index, value.Value{}
}

// searchFailure says why the index did not answer.
func searchFailure(table string, err error) value.Value {
	var building *fulltext.BuildingError
	if errors.As(err, &building) {
		progress := "chưa bắt đầu"
		if building.Total > 0 {
			progress = fmt.Sprintf("%d/%d dòng", building.Built, building.Total)
		}
		return value.Fail(value.UnavailableCode, fmt.Sprintf("chỉ mục tìm kiếm của bảng %s đang dựng (%s): thử lại sau", table, progress))
	}
	return value.Fail(value.DatabaseCode, fmt.Sprintf("matches trên %s: %v", table, err))
}

// searched runs a statement with full text.
func (q *Query) searched(p *plan, database *kitdb.Database, limit, offset int) value.Value {
	m, main := p.matches, p.tables[0]
	bound := func(each part) (term, value.Value) {
		n := each.node
		item := n.right.literal
		if n.right.op == "hole" {
			var failure value.Value
			if item, failure = holeValue(n.right, q.steps[each.step].lambda); failure.IsInvalid() {
				return term{}, failure
			}
		}
		if !item.IsString() {
			return term{}, value.Fail(value.InvalidArgumentCode, n.at+": matches nhận một chữ")
		}
		return term{fields: n.fields, text: strings.TrimSpace(item.Text())}, value.Value{}
	}
	var ranking, excluding []term
	for _, each := range m.positive {
		t, failure := bound(each)
		if failure.IsInvalid() {
			return failure
		}
		if t.text == "" {
			return nothing(p) // an empty text matches no row
		}
		ranking = append(ranking, t)
	}
	for _, each := range m.negative {
		t, failure := bound(each)
		if failure.IsInvalid() {
			return failure
		}
		if t.text != "" {
			excluding = append(excluding, t)
		}
	}
	index, failure := q.handle.catalog.fullText(q.handle.entry, main.table)
	if failure.IsInvalid() {
		return failure
	}
	ctx, cancel := value.WaitContext(q.handle.waits, time.Time{})
	defer cancel()
	if len(ranking) == 0 {
		// !matches alone filters: the rows none of its texts finds
		var excluded []int64
		for _, t := range excluding {
			found, err := index.Search(ctx, t.query(), search.MaximumSearchLimit)
			if err != nil {
				return searchFailure(main.name, err)
			}
			if len(found) == search.MaximumSearchLimit {
				return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("!matches(%q) trên %s khớp quá %d dòng: thu hẹp chữ tìm", t.text, main.name, search.MaximumSearchLimit))
			}
			for _, h := range found {
				if rowid, err := fulltext.RowID(h); err == nil {
					excluded = append(excluded, rowid)
				}
			}
		}
		return q.read(database, p, &renderer{plan: p, steps: q.steps, searched: true, rowids: excluded, exclude: true}, limit, offset)
	}
	hits := func(limit int) ([]hit, bool, value.Value) {
		return rank(ctx, index, main.name, ranking, excluding, limit)
	}
	switch p.action {
	case "count", "exists", "sum", "avg", "min", "max":
		if p.action == "count" && len(ranking) == 1 && len(excluding) == 0 && len(p.where) == 0 && len(p.tables) == 1 {
			var counted uint64
			err := index.Reading(ctx, func(read *search.Index) error {
				var err error
				counted, err = read.Count(ctx, ranking[0].query(), nil)
				return err
			})
			if err != nil {
				return searchFailure(main.name, err)
			}
			return value.Number(float64(counted))
		}
		found, more, failure := hits(search.MaximumSearchLimit)
		if failure.IsInvalid() {
			return failure
		}
		if more {
			return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("%s với matches đọc tối đa %d dòng khớp, và câu này khớp nhiều hơn: thu hẹp chữ tìm", p.action, search.MaximumSearchLimit))
		}
		return q.read(database, p, &renderer{plan: p, steps: q.steps, searched: true, rowids: rowidsOf(found)}, limit, offset)
	}
	var fields []string
	var words []string
	for _, t := range ranking {
		words = append(words, t.text)
		for _, field := range t.fields {
			if !contains(fields, field) {
				fields = append(fields, field)
			}
		}
	}
	sort.Strings(fields)
	decorate := func(rows []value.Value, rowids []int64, found []hit) ([]value.Value, value.Value) {
		byRowid := make(map[int64]hit, len(found))
		for _, h := range found {
			byRowid[h.rowid] = h
		}
		type ranked struct {
			row  value.Value
			rank int
		}
		list := make([]ranked, 0, len(rows))
		for i, row := range rows {
			h, ok := byRowid[rowids[i]]
			if !ok {
				continue
			}
			snippet, err := bestSnippet(ctx, row, main.name, fields, strings.Join(words, " "))
			if err != nil {
				return nil, value.Fail(value.DatabaseCode, fmt.Sprintf("matches trên %s: snippet: %v", main.name, err))
			}
			list = append(list, ranked{row: row.Set("score", value.Number(h.score)).Set("snippet", value.String(snippet)), rank: h.rank})
		}
		if m.ranked {
			sort.SliceStable(list, func(i, j int) bool { return list[i].rank < list[j].rank })
		}
		out := make([]value.Value, len(list))
		for i, each := range list {
			out[i] = each.row
		}
		return out, value.Value{}
	}
	if !m.ranked {
		// ascending or descending says the order: every row that matches is ordered
		found, more, failure := hits(matchesCandidates)
		if failure.IsInvalid() {
			return failure
		}
		if more {
			return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("matches cùng ascending, descending xếp mọi dòng khớp, và %s khớp quá %d dòng: thu hẹp chữ tìm, hay bỏ thứ tự để xếp theo độ khớp", main.name, matchesCandidates))
		}
		rows, rowids, failure := q.rows(database, p, &renderer{plan: p, steps: q.steps, searched: true, rowids: rowidsOf(found), rowid: true}, limit, offset)
		if failure.IsInvalid() {
			return failure
		}
		decorated, failure := decorate(rows, rowids, found)
		if failure.IsInvalid() {
			return failure
		}
		return shaped(p, decorated)
	}
	// in the order of the score: the best candidates of the index, the rest of the condition run on them, more of them
	// read until the page is full
	needed := offset + limit
	candidates := min(max(needed*4, 64), matchesCandidates)
	for {
		found, more, failure := hits(candidates)
		if failure.IsInvalid() {
			return failure
		}
		rows, rowids, failure := q.rows(database, p, &renderer{plan: p, steps: q.steps, searched: true, rowids: rowidsOf(found), rowid: true}, max(len(found), 1), 0)
		if failure.IsInvalid() {
			return failure
		}
		decorated, failure := decorate(rows, rowids, found)
		if failure.IsInvalid() {
			return failure
		}
		if len(decorated) >= needed || !more || candidates == matchesCandidates {
			if offset >= len(decorated) {
				if more && offset > 0 {
					return value.Fail(value.InvalidArgumentCode, fmt.Sprintf("trang này của matches trên %s nằm sau %d dòng khớp nhất mà một câu đọc: thu hẹp chữ tìm", main.name, matchesCandidates))
				}
				return shaped(p, nil)
			}
			return shaped(p, decorated[offset:min(needed, len(decorated))])
		}
		candidates = min(candidates*4, matchesCandidates)
	}
}

// rank ranks at most limit rows by the first ranking matches, keeps those every other one finds (their scores added)
// and none of the excluding ones does, and reports whether the index had more.
func rank(ctx context.Context, index *fulltext.Table, table string, ranking, excluding []term, limit int) ([]hit, bool, value.Value) {
	found, err := index.Search(ctx, ranking[0].query(), limit)
	if err != nil {
		return nil, false, searchFailure(table, err)
	}
	more := len(found) == limit
	scores := make(map[string]float64, len(found))
	for _, h := range found {
		scores[h.ID] = h.Score
	}
	every := func(t term) (map[string]float64, value.Value) {
		all, err := index.Search(ctx, t.query(), search.MaximumSearchLimit)
		if err != nil {
			return nil, searchFailure(table, err)
		}
		if len(all) == search.MaximumSearchLimit {
			return nil, value.Fail(value.InvalidArgumentCode, fmt.Sprintf("matches(%q) trên %s khớp quá %d dòng: một câu đọc từng ấy dòng của chỉ mục cho mỗi matches sau cái đầu; thu hẹp chữ tìm", t.text, table, search.MaximumSearchLimit))
		}
		set := make(map[string]float64, len(all))
		for _, h := range all {
			set[h.ID] = h.Score
		}
		return set, value.Value{}
	}
	for _, t := range ranking[1:] {
		set, failure := every(t)
		if failure.IsInvalid() {
			return nil, false, failure
		}
		for id := range scores {
			if score, ok := set[id]; ok {
				scores[id] += score
			} else {
				delete(scores, id)
			}
		}
	}
	for _, t := range excluding {
		set, failure := every(t)
		if failure.IsInvalid() {
			return nil, false, failure
		}
		for id := range set {
			delete(scores, id)
		}
	}
	hits := make([]hit, 0, len(scores))
	for position, h := range found {
		score, ok := scores[h.ID]
		if !ok {
			continue
		}
		rowid, err := fulltext.RowID(h)
		if err != nil {
			return nil, false, value.Fail(value.DatabaseCode, fmt.Sprintf("chỉ mục của %s giữ tài liệu %q không phải rowid", table, h.ID))
		}
		hits = append(hits, hit{rowid: rowid, score: score, rank: position})
	}
	if len(ranking) > 1 {
		// several ranking matches rank by the sum of their scores
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
		for i := range hits {
			hits[i].rank = i
		}
	}
	return hits, more, value.Value{}
}

func rowidsOf(found []hit) []int64 {
	out := make([]int64, len(found))
	for i, h := range found {
		out[i] = h.rowid
	}
	return out
}

// nothing is what a statement gives when its full text matches no row.
func nothing(p *plan) value.Value {
	switch p.action {
	case "count", "sum":
		return value.Number(0)
	case "exists":
		return value.Boolean(false)
	case "avg", "min", "max":
		return value.Null()
	}
	return shaped(p, nil)
}

// read runs a statement's SQL in one snapshot: its result as the action gives it.
func (q *Query) read(database *kitdb.Database, p *plan, r *renderer, limit, offset int) value.Value {
	text, failure := r.statement(limit, offset)
	if failure.IsInvalid() {
		return failure
	}
	var result value.Value
	err := q.reading(database, func(read querier) error {
		var err error
		result, err = run(read, p, r, text)
		return err
	})
	if err != nil {
		return value.Fail(value.DatabaseCode, q.handle.entry.File()+": "+err.Error())
	}
	return result
}

// rows reads a statement's rows in one snapshot, with their rowids.
func (q *Query) rows(database *kitdb.Database, p *plan, r *renderer, limit, offset int) ([]value.Value, []int64, value.Value) {
	text, failure := r.statement(limit, offset)
	if failure.IsInvalid() {
		return nil, nil, failure
	}
	var read []value.Value
	var rowids []int64
	err := q.reading(database, func(source querier) error {
		rows, err := source.Query(text, r.args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		read, rowids, failure, err = readRows(source, p, r, rows)
		return err
	})
	if err != nil {
		return nil, nil, value.Fail(value.DatabaseCode, q.handle.entry.File()+": "+err.Error())
	}
	return read, rowids, failure
}

// bestSnippet is the passage of the row's column the words match best, the matches in <b>, the rest HTML escaped. A
// row nesting its tables reads the main table's.
func bestSnippet(ctx context.Context, row value.Value, table string, fields []string, words string) (string, error) {
	source := row
	if nested := row.Get(table); nested.IsObject() {
		source = nested
	}
	var best search.Fragment
	quality := -1
	for _, field := range fields {
		text := source.Get(field)
		if !text.IsString() || text.Text() == "" {
			continue
		}
		fragment, err := search.Highlight(ctx, search.VietnameseAnalyzer(), text.Text(), words, search.FragmentOptions{MaxTokens: snippetTokens, ContextTokens: snippetContext})
		if err != nil {
			return "", err
		}
		if len(fragment.Matches) > quality {
			best, quality = fragment, len(fragment.Matches)
		}
	}
	if best.Text == "" {
		return "", nil
	}
	var out strings.Builder
	if best.PrefixElided {
		out.WriteString("... ")
	}
	cursor := 0
	for _, match := range best.Matches {
		if match.Start < cursor || match.End < match.Start || match.End > len(best.Text) {
			continue
		}
		out.WriteString(html.EscapeString(best.Text[cursor:match.Start]))
		out.WriteString("<b>")
		out.WriteString(html.EscapeString(best.Text[match.Start:match.End]))
		out.WriteString("</b>")
		cursor = match.End
	}
	out.WriteString(html.EscapeString(best.Text[cursor:]))
	if best.SuffixElided {
		out.WriteString(" ...")
	}
	return out.String(), nil
}

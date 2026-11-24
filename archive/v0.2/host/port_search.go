package host

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/huynhnhanquoc/sqlitewire/fulltext"
	"github.com/huynhnhanquoc/sqlitewire/gateway"
	"github.com/huynhnhanquoc/sqlitewire/pgwire"
	"github.com/huynhnhanquoc/sqlitewire/search"
)

// MATCH on the port (docs/KITWORK_KITDB.md câu 13.5): `WHERE posts MATCH 'words'`, `title MATCH $1`, with _score_ and
// _snippet_, read from the full-text index of the table, <file>.search beside it, the old engine's database/fulltext:
// one container per file for the process, so the port's MATCH and an app's matches read the same index. A table's
// index is in the database: the columns kitwork_columns gives a search weight (.searchable(), CREATE INDEX … USING
// search), found when a session opens the database. A table of the host's .database/ may also be declared by search
// of app.database, opened when the port starts, and what it declares wins over its file. An index not built yet is
// built in the background, and a MATCH before then answers how far the build is (55000).

// portSearch is what the port holds of the indexes: the files it acquired, released when it closes.
type portSearch struct {
	mutex    sync.Mutex
	releases map[string]func() // by file
	closed   bool
}

// openDeclared opens the indexes search of app.database declares, of the host's .database/ at folder; one that
// cannot be opened is said, and its table answers MATCH as having no index.
func (p *databasePort) openDeclared() {
	for _, declared := range p.host.Manifest.Database.Search {
		file := filepath.Join(p.folder, declared.Database, "kitwork.db")
		if declared.Schema != "public" {
			file = filepath.Join(p.folder, declared.Database, declared.Schema+".db")
		}
		columns := make([]fulltext.Column, 0, len(declared.Columns))
		for _, column := range declared.Columns {
			columns = append(columns, fulltext.Column{Name: column.Name, Boost: column.Boost})
		}
		label := searchLabel(declared.Database, declared.Schema, declared.Table)
		file, err := p.search.acquire(file)
		if err == nil {
			_, err = fulltext.Shared().Open(file, declared.Table, label, columns, true)
		}
		if err != nil {
			p.host.log.Line("[database] search " + label + ": " + err.Error())
		}
	}
}

func searchLabel(database, schema, table string) string {
	if schema == "public" {
		return database + "." + table
	}
	return database + "." + schema + "." + table
}

// acquire holds the file's container for the port until close, and gives the file's absolute path, the key the
// container is known by.
func (s *portSearch) acquire(file string) (string, error) {
	absolute, err := filepath.Abs(file)
	if err != nil {
		return "", err
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.closed {
		return "", errors.New("the database port is closing")
	}
	if s.releases[absolute] != nil {
		return absolute, nil
	}
	release, err := fulltext.Shared().Acquire(absolute)
	if err != nil {
		return "", err
	}
	s.releases[absolute] = release
	return absolute, nil
}

// discover opens the indexes the files of a database hold: each table whose kitwork_columns gives columns a search
// weight, over those columns.
func (p *databasePort) discover(database string, schemas map[string]string, columns portColumns) {
	type place struct{ schema, table string }
	wanted := map[place][]fulltext.Column{}
	for key, column := range columns {
		if column.weight <= 0 {
			continue
		}
		parts := strings.SplitN(key, ".", 3)
		at := place{parts[0], column.table}
		if schemas[at.schema] == "" {
			continue
		}
		wanted[at] = append(wanted[at], fulltext.Column{Name: column.name, Boost: float64(column.weight)})
	}
	for at, found := range wanted {
		sort.Slice(found, func(left, right int) bool { return found[left].Name < found[right].Name })
		label := searchLabel(database, at.schema, at.table)
		file, err := p.search.acquire(schemas[at.schema])
		if err != nil {
			continue
		}
		if _, err := fulltext.Shared().Open(file, at.table, label, found, false); err != nil {
			p.host.log.Line("[database] search " + label + ": the index cannot be made: " + err.Error())
		}
	}
}

// close releases every file the port acquired.
func (s *portSearch) close() {
	s.mutex.Lock()
	s.closed = true
	releases := s.releases
	s.releases = map[string]func(){}
	s.mutex.Unlock()
	for _, release := range releases {
		release()
	}
}

// portSearchProvider answers MATCH on one database of the port (gateway.SearchProvider).
type portSearchProvider struct {
	database string
	schemas  map[string]string // schema → file
}

// lookup finds the index of a table and the fields a MATCH names (every indexed column when none).
func (provider portSearchProvider) lookup(schema, table string, fields []string) (*fulltext.Table, []string, error) {
	var entry *fulltext.Table
	if file := provider.schemas[schema]; file != "" {
		if absolute, err := filepath.Abs(file); err == nil {
			entry = fulltext.Shared().Lookup(absolute, table)
		}
	}
	if entry == nil {
		if strings.Contains(provider.database, "/") {
			return nil, nil, pgwire.NewError("42704", fmt.Sprintf(
				"table %s.%s has no search index: mark its columns .searchable() in the app's .shared/schema/ (kitwork/schema)", schema, table))
		}
		return nil, nil, pgwire.NewError("42704", fmt.Sprintf(
			"table %s.%s has no search index: CREATE INDEX %s_search ON %s USING search (columns), or declare it in the search of app.database (application.work)", schema, table, table, table))
	}
	if len(fields) == 0 {
		return entry, entry.ColumnNames(), nil
	}
	named := make([]string, len(fields))
	for position, field := range fields {
		for _, column := range entry.Columns() {
			if strings.EqualFold(column.Name, field) {
				named[position] = column.Name
			}
		}
		if named[position] == "" {
			return nil, nil, pgwire.NewError("42703", fmt.Sprintf("column %s of %s.%s is not in its search index (searchable: %s)",
				field, schema, table, strings.Join(entry.ColumnNames(), ", ")))
		}
	}
	return entry, named, nil
}

// searchReading runs read on the index, and says in PostgreSQL's words why there is none yet.
func searchReading(ctx context.Context, entry *fulltext.Table, read func(*search.Index) error) error {
	err := entry.Reading(ctx, read)
	var building *fulltext.BuildingError
	var protocol *pgwire.Error
	switch {
	case err == nil, errors.As(err, &protocol):
		return err
	case errors.As(err, &building):
		return pgwire.NewError("55000", building.Error())
	}
	return pgwire.NewError("58000", err.Error())
}

func (provider portSearchProvider) Search(ctx context.Context, schema, table string, fields []string, query gateway.SearchQuery, limit int) ([]gateway.SearchHit, error) {
	entry, named, err := provider.lookup(schema, table, fields)
	if err != nil {
		return nil, err
	}
	var hits []gateway.SearchHit
	err = searchReading(ctx, entry, func(index *search.Index) error {
		found, err := index.Search(ctx, matchQuery(named, query), search.SearchOptions{Limit: min(max(limit, 1), search.MaximumSearchLimit)})
		if err != nil {
			return pgwire.NewError("42601", "MATCH: "+err.Error())
		}
		hits = make([]gateway.SearchHit, 0, len(found))
		for _, hit := range found {
			rowID, err := fulltext.RowID(hit)
			if err != nil {
				return fmt.Errorf("search index of %s holds a document %q that is not a rowid", entry.Label(), hit.ID)
			}
			hits = append(hits, gateway.SearchHit{RowID: rowID, Score: hit.Score})
		}
		return nil
	})
	return hits, err
}

// Count counts the matches: on the index alone for words (each required); for OR, a phrase or a prefix, by ranking
// them, at most search.MaximumSearchLimit.
func (provider portSearchProvider) Count(ctx context.Context, schema, table string, fields []string, query gateway.SearchQuery) (uint64, error) {
	if query.Kind != gateway.SearchWords {
		hits, err := provider.Search(ctx, schema, table, fields, query, search.MaximumSearchLimit)
		if err == nil && len(hits) == search.MaximumSearchLimit {
			return 0, pgwire.NewError("54000", fmt.Sprintf(
				"MATCH: counting OR, a phrase or a prefix reads at most %d matches, and this query has more", search.MaximumSearchLimit))
		}
		return uint64(len(hits)), err
	}
	entry, named, err := provider.lookup(schema, table, fields)
	if err != nil {
		return 0, err
	}
	var count uint64
	err = searchReading(ctx, entry, func(index *search.Index) error {
		count, err = index.Count(ctx, matchQuery(named, query), nil)
		if err != nil {
			return pgwire.NewError("42601", "MATCH: "+err.Error())
		}
		return nil
	})
	return count, err
}

// Fields names the indexed columns a MATCH reads (every one when fields is empty), with their weights.
func (provider portSearchProvider) Fields(_ context.Context, schema, table string, fields []string) ([]gateway.SearchField, error) {
	entry, named, err := provider.lookup(schema, table, fields)
	if err != nil {
		return nil, err
	}
	result := make([]gateway.SearchField, 0, len(named))
	for _, name := range named {
		for _, column := range entry.Columns() {
			if column.Name == name {
				result = append(result, gateway.SearchField{Name: column.Name, Boost: column.Boost})
			}
		}
	}
	return result, nil
}

// matchQuery is the search engine's query for one kind of MATCH query.
func matchQuery(fields []string, query gateway.SearchQuery) search.MatchQuery {
	switch query.Kind {
	case gateway.SearchAny:
		return search.MatchQuery{Fields: fields, Text: query.Text, Operator: search.QueryAny}
	case gateway.SearchPhrase:
		return search.MatchQuery{Fields: fields, Phrase: query.Text}
	case gateway.SearchPrefix:
		return search.MatchQuery{Fields: fields, Prefix: query.Text}
	}
	return search.MatchQuery{Fields: fields, Text: query.Text}
}

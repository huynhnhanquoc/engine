package database

import (
	"bytes"
	"errors"
	"runtime"
	"strconv"
	"time"

	kitdb "github.com/huynhnhanquoc/sqlitewire/sqlite"
	"github.com/kitwork/engine2/core/value"
)

// A transaction (docs/KITWORK_DATABASE.md mục 3):
//
//	const order = db.transaction((database) => {
//	  const created = database.insert((orders) => ({ userId, item, amount }));
//	  database.insert((lines) => ({ orderId: created.id, qty }));
//	  return created;
//	});
//
// KitDB's writer opens one write request for it and waits; the callback runs on the request's own VM, its statements
// through the handle it is given (database) reading and writing that request's transaction, its own writes seen. The
// callback's value is given back once the writer has committed; a failure out of the callback rolls every write back
// and goes on out. The handle is dead once the transaction ends.
//
// The writer is the file's one: while the callback runs, every other write of the file waits, and a write of the same
// request through the outer handle (db) would wait for the transaction that waits for it. So a write through any
// other handle on the goroutine that holds a transaction is refused, saying to write through the callback's handle,
// and one schema per transaction: a write to another schema's file is refused too. A read through db is allowed,
// in a snapshot of its own.

// errRolledBack ends the writer's request without committing.
var errRolledBack = errors.New("kitwork: the transaction's callback failed")

// transaction is one transaction in progress: the writer's request, the goroutine running its callback.
type transaction struct {
	tx     *kitdb.Transaction
	entry  *Entry
	owner  uint64
	closed bool
	// hooking counts the hooks running (hooks.go): a write of theirs through db joins the transaction
	hooking int
}

// Transaction is db.transaction((database) => …).
func (h *Handle) Transaction(arguments ...value.Value) value.Value {
	if h.tx != nil {
		return value.Fail(value.UnsupportedCode, "database.transaction(…) lồng trong giao dịch chưa có: viết các câu trong giao dịch đang mở")
	}
	if len(arguments) != 1 || !arguments[0].IsFunction() {
		return value.Fail(value.InvalidArgumentCode, "transaction nhận một hàm: db.transaction((database) => { … })")
	}
	callback := arguments[0]
	return h.within(func(state *transaction) value.Value {
		return callback.Call([]value.Value{value.Struct(&Handle{entry: h.entry, catalog: h.catalog, tx: state, hooks: h.hooks})})
	})
}

// within runs do on the calling goroutine inside one request of KitDB's writer, which it opens and holds: the
// request commits when do gives a value, rolls back when do fails.
func (h *Handle) within(do func(state *transaction) value.Value) value.Value {
	if report := h.entry.Report; report.Blocking() {
		return value.Fail(value.DatabaseCode, blockingMessage(report))
	}
	owner := goroutineID()
	if failure := h.catalog.outsideTransaction(owner, h.entry, "transaction"); failure.IsInvalid() {
		return failure
	}
	database, failure := h.catalog.database(h.entry)
	if failure.IsInvalid() {
		return failure
	}
	ready := make(chan *kitdb.Transaction, 1)
	finish := make(chan error, 1)
	written := make(chan error, 1)
	// the transaction is held no longer than the run that holds it; cancelled once the writer gave its answer
	ctx, cancel := value.WaitContext(h.waits, time.Time{})
	defer cancel()
	go func() {
		_, err := database.Write(ctx, "", func(tx *kitdb.Transaction) error {
			ready <- tx
			return <-finish
		})
		written <- err
	}()
	var tx *kitdb.Transaction
	select {
	case tx = <-ready:
	case err := <-written:
		return writeFailure(h.entry.File(), "transaction", err, nil)
	}
	state := &transaction{tx: tx, entry: h.entry, owner: owner}
	h.catalog.begin(state)
	result := h.run(do, state, finish)
	if result.IsInvalid() {
		<-written
		return result
	}
	finish <- nil
	if err := <-written; err != nil {
		return writeFailure(h.entry.File(), "transaction", err, nil)
	}
	return result
}

// run runs do in the transaction; a failure out of it (or a panic through it) rolls back.
func (h *Handle) run(do func(*transaction) value.Value, state *transaction, finish chan<- error) (result value.Value) {
	ended := false
	end := func(err error) {
		state.closed = true
		h.catalog.end(state)
		if !ended {
			ended = true
			if err != nil {
				finish <- err
			}
		}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			end(errRolledBack)
			panic(recovered)
		}
	}()
	result = do(state)
	if result.IsInvalid() {
		end(errRolledBack)
		return result
	}
	end(nil)
	return result
}

// begin and end keep the transactions open in the catalog, by the goroutine running each.
func (c *Catalog) begin(state *transaction) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.transactions[state.owner] = state
}

// transactionOf is the transaction the goroutine runs, nil when none.
func (c *Catalog) transactionOf(owner uint64) *transaction {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return c.transactions[owner]
}

func (c *Catalog) end(state *transaction) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.transactions[state.owner] == state {
		delete(c.transactions, state.owner)
	}
}

// outsideTransaction refuses a write (or a transaction) through a handle that is not a transaction's, on the
// goroutine running a transaction's callback: it would wait for the writer, which waits for the callback.
func (c *Catalog) outsideTransaction(owner uint64, entry *Entry, action string) value.Value {
	c.mutex.Lock()
	open := c.transactions[owner]
	c.mutex.Unlock()
	switch {
	case open == nil:
		return value.Value{}
	case open.entry != entry:
		return value.Fail(value.InvalidArgumentCode, action+" trên "+entry.File()+" trong giao dịch của "+open.entry.File()+": một giao dịch ghi một schema; bảng cần ghi cùng giao dịch thì để chung schema")
	case action == "transaction":
		return value.Fail(value.UnsupportedCode, "db.transaction(…) lồng trong giao dịch chưa có: viết các câu trong giao dịch đang mở")
	}
	return value.Fail(value.InvalidArgumentCode, action+" qua db trong giao dịch: ghi qua handle của callback, db.transaction((database) => database."+action+"(…)); db ghi ngoài giao dịch, và chờ nó thì treo")
}

// writesOutside reports whether a write through a handle that is no transaction's needs the check: some transaction
// of the catalog is open.
func (c *Catalog) writesOutside() bool {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return len(c.transactions) > 0
}

// goroutineID is the goroutine running the call, as runtime.Stack names it ("goroutine 18 [running]"): read only
// while a transaction is open, to tell its callback from every other request.
func goroutineID() uint64 {
	var buffer [64]byte
	n := runtime.Stack(buffer[:], false)
	fields := bytes.Fields(buffer[:n])
	if len(fields) < 2 {
		return 0
	}
	id, _ := strconv.ParseUint(string(fields[1]), 10, 64)
	return id
}

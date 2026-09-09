package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

func init() {
	sql.Register("pgsim", &pgSimDriver{})
}

type pgSimDriver struct{}

func (d *pgSimDriver) Open(name string) (driver.Conn, error) {
	db := getSimDB(name)
	return &pgSimConn{db: db}, nil
}

var (
	simDBsMu sync.Mutex
	simDBs   = map[string]*simDatabase{}
)

func getSimDB(name string) *simDatabase {
	simDBsMu.Lock()
	defer simDBsMu.Unlock()
	if db, ok := simDBs[name]; ok {
		return db
	}
	db := &simDatabase{
		name:             name,
		executions:       map[string]*simExecutionRow{},
		versions:         map[string][]*simVersionRow{},
		inbox:            map[string]map[string]*simInboxRow{},
		history:          map[string][]*simHistoryRow{},
		tasks:            map[string]*simTaskRow{},
		schedules:        map[string]*simScheduleRow{},
		health:           map[string]*simHealthRow{},
		migrations:       map[int]*simMigrationRow{},
		rowLocks:         map[string]*sync.Mutex{},
	}
	simDBs[name] = db
	return db
}

// ResetSimDB resets database state for testing.
func ResetSimDB(name string) {
	simDBsMu.Lock()
	defer simDBsMu.Unlock()
	delete(simDBs, name)
}

type simDatabase struct {
	mu         sync.Mutex
	name       string
	executions map[string]*simExecutionRow
	versions   map[string][]*simVersionRow
	inbox      map[string]map[string]*simInboxRow
	history    map[string][]*simHistoryRow
	tasks      map[string]*simTaskRow
	schedules  map[string]*simScheduleRow
	health     map[string]*simHealthRow
	migrations map[int]*simMigrationRow

	rowLocksMu sync.Mutex
	rowLocks   map[string]*sync.Mutex
}

func (db *simDatabase) getRowLock(key string) *sync.Mutex {
	db.rowLocksMu.Lock()
	defer db.rowLocksMu.Unlock()
	lock, ok := db.rowLocks[key]
	if !ok {
		lock = &sync.Mutex{}
		db.rowLocks[key] = lock
	}
	return lock
}

type simExecutionRow struct {
	ID         string
	Version    uint64
	Status     string
	PlanID     string
	PlanDigest string
	State      []byte
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type simVersionRow struct {
	ID          string
	Version     uint64
	State       []byte
	CommittedAt time.Time
}

type simInboxRow struct {
	ExecutionID string
	EventID     string
	EventType   string
	Payload     []byte
	ReceivedAt  time.Time
}

type simHistoryRow struct {
	ExecutionID string
	Seq         int64
	EntryType   string
	Payload     []byte
	CreatedAt   time.Time
}

type simTaskRow struct {
	TaskID         string
	ExecutionID    string
	NodeID         string
	Activity       string
	Attempt        int
	Status         string
	LeaseOwner     string
	LeaseUntil     *time.Time
	NotBefore      *time.Time
	IdempotencyKey string
	Input          []byte
	Result         []byte
	Error          string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type simScheduleRow struct {
	ID        string
	Version   uint64
	State     []byte
	UpdatedAt time.Time
}

type simHealthRow struct {
	Capability string
	Provider   string
	State      []byte
	UpdatedAt  time.Time
}

type simMigrationRow struct {
	Version   int
	Name      string
	AppliedAt time.Time
}

type pgSimConn struct {
	db       *simDatabase
	inTx     bool
	stagedFn []func()
	heldKeys []string
}

func (c *pgSimConn) Prepare(query string) (driver.Stmt, error) {
	return &pgSimStmt{conn: c, query: query}, nil
}

func (c *pgSimConn) Close() error {
	c.unlockHeld()
	return nil
}

func (c *pgSimConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *pgSimConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if c.inTx {
		return nil, errors.New("pgsim: already in transaction")
	}
	c.inTx = true
	c.stagedFn = nil
	c.heldKeys = nil
	return &pgSimTx{conn: c}, nil
}

func (c *pgSimConn) unlockHeld() {
	for _, key := range c.heldKeys {
		lock := c.db.getRowLock(key)
		lock.Unlock()
	}
	c.heldKeys = nil
}

type pgSimTx struct {
	conn *pgSimConn
}

func (tx *pgSimTx) Commit() error {
	if !tx.conn.inTx {
		return errors.New("pgsim: transaction not active")
	}
	tx.conn.db.mu.Lock()
	for _, fn := range tx.conn.stagedFn {
		fn()
	}
	tx.conn.db.mu.Unlock()
	tx.conn.stagedFn = nil
	tx.conn.inTx = false
	tx.conn.unlockHeld()
	return nil
}

func (tx *pgSimTx) Rollback() error {
	if !tx.conn.inTx {
		return nil
	}
	tx.conn.stagedFn = nil
	tx.conn.inTx = false
	tx.conn.unlockHeld()
	return nil
}

type pgSimStmt struct {
	conn  *pgSimConn
	query string
}

func (s *pgSimStmt) Close() error { return nil }

func (s *pgSimStmt) NumInput() int { return -1 }

func (s *pgSimStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.conn.exec(context.Background(), s.query, args)
}

func (s *pgSimStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.conn.query(context.Background(), s.query, args)
}

func (c *pgSimConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	dArgs := make([]driver.Value, len(args))
	for i, arg := range args {
		dArgs[i] = arg.Value
	}
	return c.exec(ctx, query, dArgs)
}

func (c *pgSimConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	dArgs := make([]driver.Value, len(args))
	for i, arg := range args {
		dArgs[i] = arg.Value
	}
	return c.query(ctx, query, dArgs)
}

func (c *pgSimConn) stageOrApply(fn func()) {
	if c.inTx {
		c.stagedFn = append(c.stagedFn, fn)
	} else {
		c.db.mu.Lock()
		fn()
		c.db.mu.Unlock()
	}
}

func (c *pgSimConn) exec(ctx context.Context, query string, args []driver.Value) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	qUpper := strings.ToUpper(strings.Join(strings.Fields(query), " "))

	if strings.HasPrefix(qUpper, "CREATE TABLE") || strings.HasPrefix(qUpper, "CREATE INDEX") || strings.Contains(qUpper, "PG_ADVISORY_XACT_LOCK") {
		return driver.RowsAffected(0), nil
	}

	// 1. Schema migration insert
	if strings.HasPrefix(qUpper, "INSERT INTO AXIOM_SCHEMA_MIGRATIONS") {
		ver := int(args[0].(int64))
		name := args[1].(string)
		at := args[2].(time.Time)
		c.stageOrApply(func() {
			c.db.migrations[ver] = &simMigrationRow{Version: ver, Name: name, AppliedAt: at}
		})
		return driver.RowsAffected(1), nil
	}

	// 2. Execution Insert (Create)
	if strings.HasPrefix(qUpper, "INSERT INTO AXIOM_EXECUTIONS") {
		id := args[0].(string)
		c.db.mu.Lock()
		if _, exists := c.db.executions[id]; exists {
			c.db.mu.Unlock()
			return nil, fmt.Errorf("duplicate key value violates unique constraint \"axiom_executions_pkey\"")
		}
		c.db.mu.Unlock()

		row := &simExecutionRow{
			ID:         id,
			Version:    uint64(args[1].(int64)),
			Status:     args[2].(string),
			PlanID:     valStr(args[3]),
			PlanDigest: valStr(args[4]),
			State:      valBytes(args[5]),
			CreatedAt:  valTime(args[6]),
			UpdatedAt:  valTime(args[7]),
		}
		c.stageOrApply(func() {
			c.db.executions[id] = row
		})
		return driver.RowsAffected(1), nil
	}

	// 3. Execution Update (Commit / Save)
	if strings.HasPrefix(qUpper, "UPDATE AXIOM_EXECUTIONS") {
		c.db.mu.Lock()
		var affected int64
		if len(args) == 6 {
			newVer := uint64(args[0].(int64))
			status := args[1].(string)
			state := valBytes(args[2])
			updAt := valTime(args[3])
			id := args[4].(string)
			expVer := uint64(args[5].(int64))

			cur, ok := c.db.executions[id]
			if ok && cur.Version == expVer {
				affected = 1
				c.db.mu.Unlock()
				c.stageOrApply(func() {
					cur.Version = newVer
					cur.Status = status
					cur.State = state
					cur.UpdatedAt = updAt
				})
				return driver.RowsAffected(affected), nil
			}
		} else if len(args) == 2 {
			status := args[0].(string)
			id := args[1].(string)

			cur, ok := c.db.executions[id]
			if ok {
				affected = 1
				c.db.mu.Unlock()
				c.stageOrApply(func() {
					cur.Status = status
				})
				return driver.RowsAffected(affected), nil
			}
		}
		c.db.mu.Unlock()
		return driver.RowsAffected(affected), nil
	}


	// 4. Execution Delete
	if strings.HasPrefix(qUpper, "DELETE FROM AXIOM_EXECUTIONS") {
		id := args[0].(string)
		c.stageOrApply(func() {
			delete(c.db.executions, id)
			delete(c.db.versions, id)
			delete(c.db.inbox, id)
			delete(c.db.history, id)
			for tid, t := range c.db.tasks {
				if t.ExecutionID == id {
					delete(c.db.tasks, tid)
				}
			}
		})
		return driver.RowsAffected(1), nil
	}

	// 5. Execution Versions Insert
	if strings.HasPrefix(qUpper, "INSERT INTO AXIOM_EXECUTION_VERSIONS") {
		id := args[0].(string)
		ver := uint64(args[1].(int64))
		state := valBytes(args[2])
		at := valTime(args[3])
		vRow := &simVersionRow{ID: id, Version: ver, State: state, CommittedAt: at}
		c.stageOrApply(func() {
			c.db.versions[id] = append(c.db.versions[id], vRow)
		})
		return driver.RowsAffected(1), nil
	}

	// 6. Execution Versions Delete
	if strings.HasPrefix(qUpper, "DELETE FROM AXIOM_EXECUTION_VERSIONS") {
		id := args[0].(string)
		c.stageOrApply(func() {
			delete(c.db.versions, id)
		})
		return driver.RowsAffected(1), nil
	}

	// 7. Inbox Insert
	if strings.HasPrefix(qUpper, "INSERT INTO AXIOM_INBOX") {
		execID := args[0].(string)
		eventID := args[1].(string)
		eventType := args[2].(string)
		payload := valBytes(args[3])
		at := valTime(args[4])
		c.stageOrApply(func() {
			if c.db.inbox[execID] == nil {
				c.db.inbox[execID] = map[string]*simInboxRow{}
			}
			if _, ok := c.db.inbox[execID][eventID]; !ok {
				c.db.inbox[execID][eventID] = &simInboxRow{
					ExecutionID: execID,
					EventID:     eventID,
					EventType:   eventType,
					Payload:     payload,
					ReceivedAt:  at,
				}
			}
		})
		return driver.RowsAffected(1), nil
	}

	// 8. Inbox Delete
	if strings.HasPrefix(qUpper, "DELETE FROM AXIOM_INBOX") {
		execID := args[0].(string)
		if len(args) == 1 {
			c.stageOrApply(func() {
				delete(c.db.inbox, execID)
			})
			return driver.RowsAffected(1), nil
		}
		eventID := args[1].(string)
		c.stageOrApply(func() {
			if m := c.db.inbox[execID]; m != nil {
				delete(m, eventID)
			}
		})
		return driver.RowsAffected(1), nil
	}

	// 9. History Insert
	if strings.HasPrefix(qUpper, "INSERT INTO AXIOM_HISTORY") {
		execID := args[0].(string)
		seq := args[1].(int64)
		entryType := args[2].(string)
		payload := valBytes(args[3])
		at := valTime(args[4])
		hRow := &simHistoryRow{ExecutionID: execID, Seq: seq, EntryType: entryType, Payload: payload, CreatedAt: at}
		c.stageOrApply(func() {
			c.db.history[execID] = append(c.db.history[execID], hRow)
		})
		return driver.RowsAffected(1), nil
	}

	// 10. Tasks Insert
	if strings.HasPrefix(qUpper, "INSERT INTO AXIOM_TASKS") {
		t := &simTaskRow{
			TaskID:         args[0].(string),
			ExecutionID:    args[1].(string),
			NodeID:         args[2].(string),
			Activity:       args[3].(string),
			Attempt:        int(args[4].(int64)),
			Status:         args[5].(string),
			LeaseOwner:     valStr(args[6]),
			LeaseUntil:     valTimePtr(args[7]),
			NotBefore:      valTimePtr(args[8]),
			IdempotencyKey: valStr(args[9]),
			Input:          valBytes(args[10]),
			Result:         valBytes(args[11]),
			Error:          valStr(args[12]),
			CreatedAt:      valTime(args[13]),
			UpdatedAt:      valTime(args[14]),
		}
		c.stageOrApply(func() {
			c.db.tasks[t.TaskID] = t
		})
		return driver.RowsAffected(1), nil
	}

	// 11. Tasks Update
	if strings.HasPrefix(qUpper, "UPDATE AXIOM_TASKS") {
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		var affected int64
		// Check Heartbeat: SET lease_until = $1, updated_at = $2 WHERE task_id = $3 AND lease_owner = $4 AND status = 'running'
		if strings.Contains(qUpper, "LEASE_OWNER =") && strings.Contains(qUpper, "LEASE_UNTIL =") {
			until := valTimePtr(args[0])
			upd := valTime(args[1])
			id := args[2].(string)
			owner := args[3].(string)
			if t, ok := c.db.tasks[id]; ok && t.LeaseOwner == owner && t.Status == "running" {
				t.LeaseUntil = until
				t.UpdatedAt = upd
				affected = 1
			}
			return driver.RowsAffected(affected), nil
		}
		// Complete: SET status = 'completed', result = $1, error = '', updated_at = $2 WHERE task_id = $3
		if strings.Contains(qUpper, "STATUS = 'COMPLETED'") {
			res := valBytes(args[0])
			upd := valTime(args[1])
			id := args[2].(string)
			if t, ok := c.db.tasks[id]; ok {
				t.Status = "completed"
				t.Result = res
				t.Error = ""
				t.UpdatedAt = upd
				affected = 1
			}
			return driver.RowsAffected(affected), nil
		}
		// Fail: SET status = 'failed', error = $1, updated_at = $2 WHERE task_id = $3
		if strings.Contains(qUpper, "STATUS = 'FAILED'") {
			errMsg := args[0].(string)
			upd := valTime(args[1])
			id := args[2].(string)
			if t, ok := c.db.tasks[id]; ok {
				t.Status = "failed"
				t.Error = errMsg
				t.UpdatedAt = upd
				affected = 1
			}
			return driver.RowsAffected(affected), nil
		}
		// Recover Expired: SET status = 'pending', lease_owner = '', lease_until = NULL, updated_at = $1 WHERE execution_id = $2 AND status = 'running' AND lease_until <= $3
		if strings.Contains(qUpper, "STATUS = 'PENDING'") && strings.Contains(qUpper, "LEASE_UNTIL <=") {
			upd := valTime(args[0])
			execID := args[1].(string)
			cutoff := valTime(args[2])
			for _, t := range c.db.tasks {
				if t.ExecutionID == execID && t.Status == "running" && t.LeaseUntil != nil && !t.LeaseUntil.After(cutoff) {
					t.Status = "pending"
					t.LeaseOwner = ""
					t.LeaseUntil = nil
					t.UpdatedAt = upd
					affected++
				}
			}
			return driver.RowsAffected(affected), nil
		}
		// Update generic task:
		if strings.Contains(qUpper, "SET ATTEMPT =") {
			att := int(args[0].(int64))
			st := args[1].(string)
			owner := valStr(args[2])
			until := valTimePtr(args[3])
			nb := valTimePtr(args[4])
			inp := valBytes(args[5])
			res := valBytes(args[6])
			errMsg := valStr(args[7])
			upd := valTime(args[8])
			tid := args[9].(string)
			if t, ok := c.db.tasks[tid]; ok {
				t.Attempt = att
				t.Status = st
				t.LeaseOwner = owner
				t.LeaseUntil = until
				t.NotBefore = nb
				t.Input = inp
				t.Result = res
				t.Error = errMsg
				t.UpdatedAt = upd
				affected = 1
			}
			return driver.RowsAffected(affected), nil
		}
		return driver.RowsAffected(affected), nil
	}

	// 12. Schedule Insert / Update
	if strings.HasPrefix(qUpper, "INSERT INTO AXIOM_SCHEDULES") {
		id := args[0].(string)
		ver := uint64(args[1].(int64))
		st := valBytes(args[2])
		upd := valTime(args[3])
		c.stageOrApply(func() {
			c.db.schedules[id] = &simScheduleRow{ID: id, Version: ver, State: st, UpdatedAt: upd}
		})
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(qUpper, "UPDATE AXIOM_SCHEDULES") {
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		var affected int64
		newVer := uint64(args[0].(int64))
		st := valBytes(args[1])
		upd := valTime(args[2])
		id := args[3].(string)
		expVer := uint64(args[4].(int64))
		if s, ok := c.db.schedules[id]; ok && s.Version == expVer {
			s.Version = newVer
			s.State = st
			s.UpdatedAt = upd
			affected = 1
		}
		return driver.RowsAffected(affected), nil
	}

	// 13. Provider Health Insert / Update / Delete
	if strings.HasPrefix(qUpper, "INSERT INTO AXIOM_PROVIDER_HEALTH") {
		capa := args[0].(string)
		prov := args[1].(string)
		st := valBytes(args[2])
		upd := valTime(args[3])
		key := capa + "--" + prov
		c.stageOrApply(func() {
			c.db.health[key] = &simHealthRow{Capability: capa, Provider: prov, State: st, UpdatedAt: upd}
		})
		return driver.RowsAffected(1), nil
	}
	if strings.HasPrefix(qUpper, "UPDATE AXIOM_PROVIDER_HEALTH") {
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		st := valBytes(args[0])
		upd := valTime(args[1])
		capa := args[2].(string)
		prov := args[3].(string)
		key := capa + "--" + prov
		var affected int64
		if h, ok := c.db.health[key]; ok {
			h.State = st
			h.UpdatedAt = upd
			affected = 1
		}
		return driver.RowsAffected(affected), nil
	}
	if strings.HasPrefix(qUpper, "DELETE FROM AXIOM_PROVIDER_HEALTH") {
		capa := args[0].(string)
		prov := args[1].(string)
		key := capa + "--" + prov
		c.stageOrApply(func() {
			delete(c.db.health, key)
		})
		return driver.RowsAffected(1), nil
	}

	return driver.RowsAffected(0), nil
}

func (c *pgSimConn) query(ctx context.Context, query string, args []driver.Value) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	qUpper := strings.ToUpper(strings.Join(strings.Fields(query), " "))

	// 1. Migration check
	if strings.Contains(qUpper, "FROM AXIOM_SCHEMA_MIGRATIONS") {
		if strings.Contains(qUpper, "COUNT(*)") {
			c.db.mu.Lock()
			defer c.db.mu.Unlock()
			return newSimRows([]string{"count"}, [][]driver.Value{{int64(len(c.db.migrations))}}), nil
		}
		if strings.Contains(qUpper, "WHERE VERSION =") {
			ver := int(args[0].(int64))
			c.db.mu.Lock()
			defer c.db.mu.Unlock()
			_, exists := c.db.migrations[ver]
			return newSimRows([]string{"exists"}, [][]driver.Value{{exists}}), nil
		}
	}


	// 1b. Check execution exists
	if strings.Contains(qUpper, "EXISTS(SELECT 1 FROM AXIOM_EXECUTIONS WHERE ID =") {
		id := args[0].(string)
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		_, exists := c.db.executions[id]
		return newSimRows([]string{"exists"}, [][]driver.Value{{exists}}), nil
	}

	// 2. Select execution by ID (with or without FOR UPDATE)
	if strings.Contains(qUpper, "FROM AXIOM_EXECUTIONS WHERE ID =") {
		id := args[0].(string)
		if strings.Contains(qUpper, "FOR UPDATE") {
			lock := c.db.getRowLock(id)
			lock.Lock()
			c.heldKeys = append(c.heldKeys, id)
		}
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		row, ok := c.db.executions[id]
		if !ok {
			return newSimRows([]string{"version", "state"}, nil), nil
		}
		if strings.Contains(qUpper, "VERSION, STATE") {
			return newSimRows([]string{"version", "state"}, [][]driver.Value{{int64(row.Version), row.State}}), nil
		}
		return newSimRows([]string{"state"}, [][]driver.Value{{row.State}}), nil
	}

	// 3. List Execution IDs
	if strings.Contains(qUpper, "SELECT ID FROM AXIOM_EXECUTIONS") {
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		ids := make([]string, 0, len(c.db.executions))
		for id := range c.db.executions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		vals := make([][]driver.Value, len(ids))
		for i, id := range ids {
			vals[i] = []driver.Value{id}
		}
		return newSimRows([]string{"id"}, vals), nil
	}

	// 4. Execution Versions List
	if strings.Contains(qUpper, "FROM AXIOM_EXECUTION_VERSIONS WHERE ID =") {
		id := args[0].(string)
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		vers := c.db.versions[id]
		sort.Slice(vers, func(i, j int) bool { return vers[i].Version < vers[j].Version })
		vals := make([][]driver.Value, len(vers))
		for i, v := range vers {
			vals[i] = []driver.Value{v.State}
		}
		return newSimRows([]string{"state"}, vals), nil
	}

	// 5. Inbox List
	if strings.Contains(qUpper, "FROM AXIOM_INBOX WHERE EXECUTION_ID =") {
		execID := args[0].(string)
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		events := make([]*simInboxRow, 0)
		for _, e := range c.db.inbox[execID] {
			events = append(events, e)
		}
		sort.Slice(events, func(i, j int) bool {
			if events[i].ReceivedAt.Equal(events[j].ReceivedAt) {
				return events[i].EventID < events[j].EventID
			}
			return events[i].ReceivedAt.Before(events[j].ReceivedAt)
		})
		vals := make([][]driver.Value, len(events))
		for i, e := range events {
			vals[i] = []driver.Value{e.EventID, e.EventType, e.Payload, e.ReceivedAt}
		}
		return newSimRows([]string{"event_id", "event_type", "payload", "received_at"}, vals), nil
	}

	// 6. History Sequence
	if strings.Contains(qUpper, "COALESCE(MAX(SEQ), 0) FROM AXIOM_HISTORY") {
		execID := args[0].(string)
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		var maxSeq int64 = 0
		for _, h := range c.db.history[execID] {
			if h.Seq > maxSeq {
				maxSeq = h.Seq
			}
		}
		return newSimRows([]string{"seq"}, [][]driver.Value{{maxSeq}}), nil
	}

	// 7. History List
	if strings.Contains(qUpper, "FROM AXIOM_HISTORY WHERE EXECUTION_ID =") {
		execID := args[0].(string)
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		entries := c.db.history[execID]
		sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })
		vals := make([][]driver.Value, len(entries))
		for i, h := range entries {
			vals[i] = []driver.Value{h.EntryType, h.Payload, h.CreatedAt}
		}
		return newSimRows([]string{"entry_type", "payload", "created_at"}, vals), nil
	}

	// 8. Tasks Poll with Lease (SKIP LOCKED simulation)
	if strings.Contains(qUpper, "FROM AXIOM_TASKS") && strings.Contains(qUpper, "SKIP LOCKED") {
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		execID := args[0].(string)
		now := valTime(args[1])
		workerID := args[2].(string)
		leaseUntil := valTime(args[3])

		candidates := make([]*simTaskRow, 0)
		for _, t := range c.db.tasks {
			if t.ExecutionID != execID {
				continue
			}
			isDue := t.NotBefore == nil || !t.NotBefore.After(now)
			isRunnable := t.Status == "pending" || (t.Status == "running" && t.LeaseUntil != nil && !t.LeaseUntil.After(now))
			if isDue && isRunnable {
				candidates = append(candidates, t)
			}
		}
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].Attempt == candidates[j].Attempt {
				return candidates[i].CreatedAt.Before(candidates[j].CreatedAt)
			}
			return candidates[i].Attempt < candidates[j].Attempt
		})
		if len(candidates) == 0 {
			return newSimRows([]string{"task_id", "execution_id", "node_id", "activity", "attempt", "status", "lease_owner", "lease_until", "not_before", "idempotency_key", "input", "result", "error", "created_at", "updated_at"}, nil), nil
		}
		picked := candidates[0]
		picked.Status = "running"
		picked.LeaseOwner = workerID
		picked.LeaseUntil = &leaseUntil
		picked.UpdatedAt = now

		vals := [][]driver.Value{{
			picked.TaskID, picked.ExecutionID, picked.NodeID, picked.Activity, int64(picked.Attempt),
			picked.Status, picked.LeaseOwner, picked.LeaseUntil, picked.NotBefore, picked.IdempotencyKey,
			picked.Input, picked.Result, picked.Error, picked.CreatedAt, picked.UpdatedAt,
		}}
		return newSimRows([]string{"task_id", "execution_id", "node_id", "activity", "attempt", "status", "lease_owner", "lease_until", "not_before", "idempotency_key", "input", "result", "error", "created_at", "updated_at"}, vals), nil
	}

	// 9. Tasks List by execution
	if strings.Contains(qUpper, "FROM AXIOM_TASKS WHERE EXECUTION_ID =") {
		execID := args[0].(string)
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		matched := make([]*simTaskRow, 0)
		for _, t := range c.db.tasks {
			if t.ExecutionID == execID {
				matched = append(matched, t)
			}
		}
		sort.Slice(matched, func(i, j int) bool { return matched[i].CreatedAt.Before(matched[j].CreatedAt) })
		vals := make([][]driver.Value, len(matched))
		for i, t := range matched {
			vals[i] = []driver.Value{
				t.TaskID, t.ExecutionID, t.NodeID, t.Activity, int64(t.Attempt),
				t.Status, t.LeaseOwner, t.LeaseUntil, t.NotBefore, t.IdempotencyKey,
				t.Input, t.Result, t.Error, t.CreatedAt, t.UpdatedAt,
			}
		}
		return newSimRows([]string{"task_id", "execution_id", "node_id", "activity", "attempt", "status", "lease_owner", "lease_until", "not_before", "idempotency_key", "input", "result", "error", "created_at", "updated_at"}, vals), nil
	}

	// 10. Schedules Load / List
	if strings.Contains(qUpper, "FROM AXIOM_SCHEDULES WHERE ID =") {
		id := args[0].(string)
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		s, ok := c.db.schedules[id]
		if !ok {
			return newSimRows([]string{"version", "state"}, nil), nil
		}
		if strings.Contains(qUpper, "VERSION, STATE") {
			return newSimRows([]string{"version", "state"}, [][]driver.Value{{int64(s.Version), s.State}}), nil
		}
		return newSimRows([]string{"state"}, [][]driver.Value{{s.State}}), nil
	}
	if strings.Contains(qUpper, "FROM AXIOM_SCHEDULES") {
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		list := make([]*simScheduleRow, 0, len(c.db.schedules))
		for _, s := range c.db.schedules {
			list = append(list, s)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		vals := make([][]driver.Value, len(list))
		for i, s := range list {
			vals[i] = []driver.Value{s.State}
		}
		return newSimRows([]string{"state"}, vals), nil
	}

	// 11. Provider Health Load / List
	if strings.Contains(qUpper, "FROM AXIOM_PROVIDER_HEALTH WHERE CAPABILITY =") {
		capa := args[0].(string)
		prov := args[1].(string)
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		h, ok := c.db.health[capa+"--"+prov]
		if !ok {
			return newSimRows([]string{"state"}, nil), nil
		}
		return newSimRows([]string{"state"}, [][]driver.Value{{h.State}}), nil
	}
	if strings.Contains(qUpper, "FROM AXIOM_PROVIDER_HEALTH") {
		c.db.mu.Lock()
		defer c.db.mu.Unlock()
		list := make([]*simHealthRow, 0, len(c.db.health))
		for _, h := range c.db.health {
			list = append(list, h)
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Capability == list[j].Capability {
				return list[i].Provider < list[j].Provider
			}
			return list[i].Capability < list[j].Capability
		})
		vals := make([][]driver.Value, len(list))
		for i, h := range list {
			vals[i] = []driver.Value{h.State}
		}
		return newSimRows([]string{"state"}, vals), nil
	}

	return newSimRows([]string{}, nil), nil
}

type simRows struct {
	columns []string
	rows    [][]driver.Value
	idx     int
}

func newSimRows(cols []string, data [][]driver.Value) *simRows {
	return &simRows{columns: cols, rows: data, idx: 0}
}

func (r *simRows) Columns() []string { return r.columns }
func (r *simRows) Close() error      { return nil }
func (r *simRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.rows) {
		return io.EOF
	}
	row := r.rows[r.idx]
	copy(dest, row)
	r.idx++
	return nil
}

func valStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func valBytes(v any) []byte {
	if v == nil {
		return nil
	}
	switch b := v.(type) {
	case []byte:
		return b
	case string:
		return []byte(b)
	default:
		data, _ := json.Marshal(v)
		return data
	}
}

func valTime(v any) time.Time {
	if v == nil {
		return time.Time{}
	}
	if t, ok := v.(time.Time); ok {
		return t.UTC()
	}
	return time.Time{}
}

func valTimePtr(v any) *time.Time {
	if v == nil {
		return nil
	}
	t := valTime(v)
	if t.IsZero() {
		return nil
	}
	return &t
}

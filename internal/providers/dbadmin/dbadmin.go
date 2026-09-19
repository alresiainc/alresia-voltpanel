// Package dbadmin is the database-admin tool's SQL engine (§ "something
// like Adminer to manage databases"): given a saved internal/domain/dbconn
// connection, it opens a real MySQL/PostgreSQL connection, lists databases/
// tables/columns, browses table rows, and runs arbitrary SQL a user types
// in -- the same shape of tool Adminer/phpMyAdmin provide, native to this
// stack instead of a vendored PHP file.
//
// Two things make this a different risk profile from the rest of the API:
//
//  1. It holds real database passwords. Those never touch this package as
//     plaintext on disk -- callers resolve them through
//     internal/security.SecretStore right before opening a connection, the
//     same as internal/providers/remote/ssh does for SSH keys.
//  2. Running arbitrary user-typed SQL is the entire point of RunQuery --
//     there's no injection vector to close there, the user is deliberately
//     executing their own query. What *is* validated is every identifier
//     (database/table name) this package itself splices into a query it
//     builds (ListTables, BrowseTable): each is checked against a live
//     list this package just fetched from the server's own information_
//     schema before being quoted and interpolated, so a crafted "table"
//     parameter can't smuggle in extra SQL.
package dbadmin

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/dbconn"
	"github.com/alresiainc/alresia-voltpanel/internal/security"
)

// rowLimit caps every browse/query result -- this is an admin tool for
// looking at data, not a data-export pipeline, and an unbounded SELECT
// against a real production-sized table would otherwise happily try to
// load millions of rows into memory and the browser tab.
const rowLimit = 200

// QueryResult is the JSON-safe shape every read/run operation returns.
type QueryResult struct {
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	RowsAffected int64    `json:"rowsAffected"`
	DurationMS   int64    `json:"durationMs"`
	Truncated    bool     `json:"truncated"`
}

// Provider drives real MySQL/PostgreSQL connections.
type Provider struct {
	Connections *dbconn.Repository
	Secrets     *security.SecretStore

	mu    sync.Mutex
	pools map[string]*sql.DB // key: connID + "\x00" + database
}

func New(connections *dbconn.Repository, secrets *security.SecretStore) *Provider {
	return &Provider{Connections: connections, Secrets: secrets, pools: map[string]*sql.DB{}}
}

// TestConnection opens (or reuses) a connection and pings it.
func (p *Provider) TestConnection(ctx context.Context, connID string) error {
	db, _, err := p.open(ctx, connID, "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return db.PingContext(ctx)
}

// ListDatabases lists every database on the server this connection points
// at, probing via whatever database the connection can actually reach
// (its configured default, or a sensible per-engine fallback).
func (p *Provider) ListDatabases(ctx context.Context, connID string) ([]string, error) {
	db, conn, err := p.open(ctx, connID, "")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var query string
	switch conn.Kind {
	case dbconn.KindMySQL:
		query = "SHOW DATABASES"
	default:
		query = "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname"
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// ListTables lists every table in database (schema "public" for
// Postgres, which is what information_schema.tables defaults to
// filtering by below).
func (p *Provider) ListTables(ctx context.Context, connID, database string) ([]string, error) {
	db, conn, err := p.open(ctx, connID, database)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var query string
	var args []any
	switch conn.Kind {
	case dbconn.KindMySQL:
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = ? ORDER BY table_name"
		args = []any{database}
	default:
		query = "SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name"
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// ColumnInfo is one row of a table's schema.
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

// ListColumns describes table's columns -- validated against a live table
// list first so a crafted table name can't reach the query it builds.
func (p *Provider) ListColumns(ctx context.Context, connID, database, table string) ([]ColumnInfo, error) {
	tables, err := p.ListTables(ctx, connID, database)
	if err != nil {
		return nil, err
	}
	if !contains(tables, table) {
		return nil, fmt.Errorf("dbadmin: %q is not a table in %q", table, database)
	}

	db, conn, err := p.open(ctx, connID, database)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var query string
	var args []any
	switch conn.Kind {
	case dbconn.KindMySQL:
		query = "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = ? AND table_name = ? ORDER BY ordinal_position"
		args = []any{database, table}
	default:
		query = "SELECT column_name, data_type, is_nullable FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 ORDER BY ordinal_position"
		args = []any{table}
	}
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ColumnInfo
	for rows.Next() {
		var name, typ, nullable string
		if err := rows.Scan(&name, &typ, &nullable); err != nil {
			return nil, err
		}
		out = append(out, ColumnInfo{Name: name, Type: typ, Nullable: strings.EqualFold(nullable, "YES")})
	}
	return out, rows.Err()
}

// BrowseTable runs a plain, capped SELECT * against table -- table is
// validated against a live table list first for the same reason
// ListColumns does.
func (p *Provider) BrowseTable(ctx context.Context, connID, database, table string, offset int) (*QueryResult, error) {
	tables, err := p.ListTables(ctx, connID, database)
	if err != nil {
		return nil, err
	}
	if !contains(tables, table) {
		return nil, fmt.Errorf("dbadmin: %q is not a table in %q", table, database)
	}

	db, conn, err := p.open(ctx, connID, database)
	if err != nil {
		return nil, err
	}
	if offset < 0 {
		offset = 0
	}
	query := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d", quoteIdent(conn.Kind, table), rowLimit, offset)
	return p.runAndScan(ctx, db, query)
}

// RunQuery executes exactly the SQL a user typed -- see the package doc
// for why this one deliberately doesn't validate/sanitize its input.
func (p *Provider) RunQuery(ctx context.Context, connID, database, query string) (*QueryResult, error) {
	db, _, err := p.open(ctx, connID, database)
	if err != nil {
		return nil, err
	}
	return p.runAndScan(ctx, db, query)
}

func (p *Provider) runAndScan(ctx context.Context, db *sql.DB, query string) (*QueryResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	start := time.Now()

	if !looksLikeSelect(query) {
		res, err := db.ExecContext(ctx, query)
		if err != nil {
			return nil, err
		}
		affected, _ := res.RowsAffected()
		return &QueryResult{RowsAffected: affected, DurationMS: time.Since(start).Milliseconds()}, nil
	}

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := &QueryResult{Columns: cols, Rows: [][]any{}}

	scanDest := make([]any, len(cols))
	scanVals := make([]any, len(cols))
	for i := range scanVals {
		scanDest[i] = &scanVals[i]
	}

	for rows.Next() {
		if len(result.Rows) >= rowLimit {
			result.Truncated = true
			break
		}
		if err := rows.Scan(scanDest...); err != nil {
			return nil, err
		}
		row := make([]any, len(cols))
		for i, v := range scanVals {
			row[i] = normalizeValue(v)
		}
		result.Rows = append(result.Rows, row)
	}
	result.DurationMS = time.Since(start).Milliseconds()
	return result, rows.Err()
}

// normalizeValue converts a database/sql scan result into something
// encoding/json can always marshal predictably -- notably []byte (what
// TEXT/VARCHAR columns commonly scan as) becomes a plain string rather
// than base64, which is what json.Marshal does with a raw []byte.
func normalizeValue(v any) any {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// looksLikeSelect decides Query vs Exec for RunQuery/BrowseTable based on
// the statement's leading keyword -- the standard heuristic lightweight
// SQL clients use, since database/sql itself has no engine-agnostic way
// to ask "will this statement return rows" up front.
func looksLikeSelect(query string) bool {
	trimmed := strings.TrimSpace(query)
	upper := strings.ToUpper(trimmed)
	for _, kw := range []string{"SELECT", "SHOW", "EXPLAIN", "WITH", "DESCRIBE", "DESC ", "PRAGMA"} {
		if strings.HasPrefix(upper, kw) {
			return true
		}
	}
	return false
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func quoteIdent(kind dbconn.Kind, ident string) string {
	if kind == dbconn.KindMySQL {
		return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
	}
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// open returns a cached *sql.DB for (connID, database), opening and
// pinging a fresh one if none is cached yet. database == "" means "use
// whatever this connection is configured to default to, or a sensible
// per-engine fallback" (see defaultDatabase).
func (p *Provider) open(ctx context.Context, connID, database string) (*sql.DB, dbconn.DBConnection, error) {
	conn, err := p.Connections.Get(connID)
	if err != nil {
		return nil, dbconn.DBConnection{}, err
	}
	if database == "" {
		database = defaultDatabase(conn)
	}

	key := connID + "\x00" + database
	p.mu.Lock()
	if db, ok := p.pools[key]; ok {
		p.mu.Unlock()
		return db, conn, nil
	}
	p.mu.Unlock()

	var password string
	if conn.SecretRef != "" {
		b, err := p.Secrets.Get(conn.SecretRef)
		if err != nil {
			return nil, conn, fmt.Errorf("dbadmin: resolve stored password: %w", err)
		}
		password = string(b)
	}

	dsn, driverName := buildDSN(conn, database, password)
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, conn, err
	}
	db.SetMaxOpenConns(4)

	pingCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, conn, err
	}

	p.mu.Lock()
	p.pools[key] = db
	p.mu.Unlock()
	return db, conn, nil
}

func defaultDatabase(conn dbconn.DBConnection) string {
	if conn.DatabaseName != "" {
		return conn.DatabaseName
	}
	if conn.Kind == dbconn.KindPostgres {
		return "postgres" // Postgres has no "no database" connection; this maintenance DB always exists.
	}
	return "" // MySQL connects fine with no database selected.
}

func buildDSN(conn dbconn.DBConnection, database, password string) (dsn, driverName string) {
	if conn.Kind == dbconn.KindMySQL {
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true", conn.Username, password, conn.Host, conn.Port, database), "mysql"
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=prefer", conn.Username, password, conn.Host, conn.Port, database), "pgx"
}

// CloseAll closes every pooled connection -- called on daemon shutdown so
// nothing leaks a real network connection past the process's lifetime.
func (p *Provider) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, db := range p.pools {
		_ = db.Close()
		delete(p.pools, k)
	}
}

// Evict closes and drops every cached pool for connID -- called when a
// connection is deleted, so a stale open socket doesn't outlive its own
// saved connection row.
func (p *Provider) Evict(connID string) {
	prefix := connID + "\x00"
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, db := range p.pools {
		if strings.HasPrefix(k, prefix) {
			_ = db.Close()
			delete(p.pools, k)
		}
	}
}

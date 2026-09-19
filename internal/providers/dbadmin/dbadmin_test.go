package dbadmin

import (
	"testing"

	"github.com/alresiainc/alresia-voltpanel/internal/domain/dbconn"
)

func TestLooksLikeSelect(t *testing.T) {
	selectLike := []string{"select * from x", "  SELECT 1", "SHOW TABLES", "explain select 1", "WITH cte AS (SELECT 1) SELECT * FROM cte"}
	for _, q := range selectLike {
		if !looksLikeSelect(q) {
			t.Errorf("expected %q to look like a SELECT", q)
		}
	}

	execLike := []string{"INSERT INTO x VALUES (1)", "update x set y=1", "DELETE FROM x", "DROP TABLE x", "CREATE TABLE x (id int)"}
	for _, q := range execLike {
		if looksLikeSelect(q) {
			t.Errorf("expected %q to NOT look like a SELECT", q)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	if got := quoteIdent(dbconn.KindMySQL, "users"); got != "`users`" {
		t.Errorf("mysql: got %q", got)
	}
	if got := quoteIdent(dbconn.KindMySQL, "weird`name"); got != "`weird``name`" {
		t.Errorf("mysql escaping: got %q", got)
	}
	if got := quoteIdent(dbconn.KindPostgres, "users"); got != `"users"` {
		t.Errorf("postgres: got %q", got)
	}
	if got := quoteIdent(dbconn.KindPostgres, `weird"name`); got != `"weird""name"` {
		t.Errorf("postgres escaping: got %q", got)
	}
}

func TestContains(t *testing.T) {
	list := []string{"users", "orders"}
	if !contains(list, "users") {
		t.Error("expected contains to find an existing element")
	}
	if contains(list, "products; DROP TABLE users") {
		t.Error("expected contains to reject a value not in the list")
	}
}

func TestNormalizeValue(t *testing.T) {
	if got := normalizeValue([]byte("hello")); got != "hello" {
		t.Errorf("expected []byte to become a string, got %#v", got)
	}
	if got := normalizeValue(int64(42)); got != int64(42) {
		t.Errorf("expected non-[]byte values to pass through unchanged, got %#v", got)
	}
	if normalizeValue(nil) != nil {
		t.Error("expected nil to pass through as nil")
	}
}

func TestDefaultDatabase(t *testing.T) {
	if got := defaultDatabase(dbconn.DBConnection{Kind: dbconn.KindPostgres}); got != "postgres" {
		t.Errorf("expected postgres fallback to be the maintenance db, got %q", got)
	}
	if got := defaultDatabase(dbconn.DBConnection{Kind: dbconn.KindMySQL}); got != "" {
		t.Errorf("expected mysql fallback to be empty (no db required), got %q", got)
	}
	if got := defaultDatabase(dbconn.DBConnection{Kind: dbconn.KindPostgres, DatabaseName: "app"}); got != "app" {
		t.Errorf("expected the configured database to win, got %q", got)
	}
}

func TestBuildDSNDoesNotLeakPasswordShape(t *testing.T) {
	// Not a security test of the DSN itself (a DSN necessarily contains
	// the password) -- just a regression guard that both drivers get a
	// well-formed DSN with the right host/port/user substituted in.
	mysqlDSN, driver := buildDSN(dbconn.DBConnection{Kind: dbconn.KindMySQL, Username: "root", Host: "127.0.0.1", Port: 3306}, "app", "secret")
	if driver != "mysql" || mysqlDSN != "root:secret@tcp(127.0.0.1:3306)/app?parseTime=true" {
		t.Errorf("unexpected mysql DSN: driver=%q dsn=%q", driver, mysqlDSN)
	}

	pgDSN, driver := buildDSN(dbconn.DBConnection{Kind: dbconn.KindPostgres, Username: "postgres", Host: "127.0.0.1", Port: 5432}, "app", "secret")
	if driver != "pgx" || pgDSN != "postgres://postgres:secret@127.0.0.1:5432/app?sslmode=prefer" {
		t.Errorf("unexpected postgres DSN: driver=%q dsn=%q", driver, pgDSN)
	}
}

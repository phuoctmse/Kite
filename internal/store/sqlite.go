package store

import (
	"database/sql"
	"time"

	"github.com/kite-io/kite/internal/brain"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS events (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    kind      TEXT     NOT NULL,
    namespace TEXT     NOT NULL DEFAULT '',
    resource  TEXT     NOT NULL DEFAULT '',
    timestamp DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS actions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    action_name TEXT     NOT NULL,
    output      TEXT     NOT NULL DEFAULT '',
    error       TEXT     NOT NULL DEFAULT '',
    timestamp   DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS turns (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    role      TEXT     NOT NULL,
    content   TEXT     NOT NULL,
    timestamp DATETIME NOT NULL
);
`

type sqliteStore struct {
	db *sql.DB
}

func Open(dsn string) (Store, error) {
	db, err := sql.Open("sqlite", dsn+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) WriteEvent(kind, namespace, resource string, ts time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO events (kind, namespace, resource, timestamp) VALUES (?, ?, ?, ?)`,
		kind, namespace, resource, ts,
	)
	return err
}

func (s *sqliteStore) WriteAction(result ActionResult) error {
	_, err := s.db.Exec(
		`INSERT INTO actions (action_name, output, error, timestamp) VALUES (?, ?, ?, ?)`,
		result.ActionName, result.Output, result.Err, result.Timestamp,
	)
	return err
}

func (s *sqliteStore) WriteHistory(turn brain.Turn) error {
	_, err := s.db.Exec(
		`INSERT INTO turns (role, content, timestamp) VALUES (?, ?, ?)`,
		turn.Role, turn.Content, time.Now().UTC(),
	)
	return err
}

func (s *sqliteStore) RecentHistory(n int) ([]brain.Turn, error) {
	rows, err := s.db.Query(
		`SELECT role, content FROM turns ORDER BY id DESC LIMIT ?`, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var turns []brain.Turn
	for rows.Next() {
		var t brain.Turn
		if err := rows.Scan(&t.Role, &t.Content); err != nil {
			return nil, err
		}
		turns = append(turns, t)
	}
	return turns, rows.Err()
}

func (s *sqliteStore) RecentSimilarEvents(kind, namespace string, n int) ([]StoredEvent, error) {
	rows, err := s.db.Query(
		`SELECT kind, namespace, resource, timestamp FROM events
         WHERE kind = ? AND namespace = ?
         ORDER BY id DESC LIMIT ?`,
		kind, namespace, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []StoredEvent
	for rows.Next() {
		var e StoredEvent
		if err := rows.Scan(&e.Kind, &e.Namespace, &e.Resource, &e.Timestamp); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *sqliteStore) Close() error { return s.db.Close() }

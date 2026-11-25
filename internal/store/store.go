package store

import (
 "context"
 "database/sql"
 "fmt"
 "os"
 "path/filepath"

 _ "modernc.org/sqlite"
)

func Open(dir string) (*sql.DB, error) {
 if err := os.MkdirAll(dir, 0700); err != nil { return nil, fmt.Errorf("data directory: %w", err) }
 path := filepath.Join(dir, "static-host.db")
 f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
 if err != nil { return nil, fmt.Errorf("database file: %w", err) }
 if err = f.Close(); err != nil { return nil, err }
 db, err := sql.Open("sqlite", path)
 if err != nil { return nil, err }
 db.SetMaxOpenConns(1)
 if err = initialize(db); err != nil { db.Close(); return nil, fmt.Errorf("initialize database: %w", err) }
 return db, nil
}

func initialize(db *sql.DB) error {
 for _, q := range []string{"PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000", "PRAGMA journal_mode=DELETE"} { if _, err := db.Exec(q); err != nil { return err } }
 tx, err := db.BeginTx(context.Background(), nil); if err != nil { return err }; defer tx.Rollback()
 var version int
 if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil { return err }
 if version > 1 { return fmt.Errorf("unsupported database schema version %d", version) }
 if version == 0 {
  _, err = tx.Exec(`CREATE TABLE users(id INTEGER PRIMARY KEY CHECK(id=1), login TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL);
CREATE TABLE agent_keys(id TEXT PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES users(id), name TEXT NOT NULL, token_hash BLOB NOT NULL UNIQUE, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, revoked_at INTEGER);
CREATE TABLE sessions(token_hash BLOB PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES users(id), csrf_token TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL);
PRAGMA user_version=1;`)
  if err != nil { return err }
 }
 return tx.Commit()
}

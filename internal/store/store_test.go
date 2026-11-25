package store

import (
 "os"
 "path/filepath"
 "testing"
)

func TestOpenAndReopen(t *testing.T) {
 dir := filepath.Join(t.TempDir(), "data")
 db, err := Open(dir); if err != nil { t.Fatal(err) }
 if _, err := db.Exec(`INSERT INTO users VALUES(1,'admin','hash')`); err != nil { t.Fatal(err) }
 db.Close()
 db, err = Open(dir); if err != nil { t.Fatal(err) }; defer db.Close()
 var login string
 if err := db.QueryRow(`SELECT login FROM users`).Scan(&login); err != nil || login != "admin" { t.Fatalf("login %q: %v", login, err) }
 info, err := os.Stat(filepath.Join(dir, "static-host.db")); if err != nil || info.Mode().Perm() != 0600 { t.Fatalf("database permissions: %v %v", info, err) }
 if _, err := db.Exec(`PRAGMA user_version=2`); err != nil { t.Fatal(err) }; db.Close()
 if newer, err := Open(dir); err == nil { newer.Close(); t.Fatal("accepted newer schema") }
}

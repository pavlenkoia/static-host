package store

import (
 "database/sql"
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
 if _, err := db.Exec(`PRAGMA user_version=3`); err != nil { t.Fatal(err) }; db.Close()
 if newer, err := Open(dir); err == nil { newer.Close(); t.Fatal("accepted newer schema") }
}

func TestMigrationV1(t *testing.T) {
 dir:=t.TempDir()
 db,err:=sql.Open("sqlite",filepath.Join(dir,"static-host.db")); if err!=nil {t.Fatal(err)}
 _,err=db.Exec(`CREATE TABLE users(id INTEGER PRIMARY KEY CHECK(id=1), login TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL);
CREATE TABLE agent_keys(id TEXT PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES users(id), name TEXT NOT NULL, token_hash BLOB NOT NULL UNIQUE, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, revoked_at INTEGER);
CREATE TABLE sessions(token_hash BLOB PRIMARY KEY, owner_id INTEGER NOT NULL REFERENCES users(id), csrf_token TEXT NOT NULL, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL);
INSERT INTO users VALUES(1,'admin','hash'); INSERT INTO sessions VALUES(x'01',1,'csrf',1,20);
INSERT INTO agent_keys VALUES('active',1,'old',x'02',1,20,NULL),('revoked',1,'old2',x'03',2,30,5);
PRAGMA user_version=1;`); if err!=nil {t.Fatal(err)}; db.Close()
 db,err=Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 var version,count int; if err=db.QueryRow("PRAGMA user_version").Scan(&version); err!=nil || version!=2 {t.Fatal(version,err)}
 if err=db.QueryRow("SELECT count(*) FROM agent_keys WHERE token_ciphertext IS NULL AND ((id='active' AND token_hash=x'02' AND expires_at=20 AND revoked_at IS NULL) OR (id='revoked' AND token_hash=x'03' AND expires_at=30 AND revoked_at=5))").Scan(&count); err!=nil || count!=2 {t.Fatal(count,err)}
 if err=db.QueryRow("SELECT count(*) FROM sessions WHERE csrf_token='csrf'").Scan(&count); err!=nil || count!=1 {t.Fatal(count,err)}
}

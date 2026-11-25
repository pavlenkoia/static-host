package auth

import (
 "os"
 "path/filepath"
 "testing"

 "static-host/internal/store"
)

func TestLoadKeyCipher(t *testing.T) {
 dir := t.TempDir()
 db, err := store.Open(dir); if err != nil { t.Fatal(err) }; defer db.Close()
 first, err := LoadKeyCipher(dir, db); if err != nil { t.Fatal(err) }
 path := filepath.Join(dir, "agent-key-encryption.key")
 info, err := os.Stat(path); if err != nil || info.Mode().Perm() != 0600 { t.Fatalf("permissions: %v %v", info, err) }
 nonce := make([]byte, first.NonceSize())
 encrypted := first.Seal(nil, nonce, []byte("secret"), []byte("id"))
 second, err := LoadKeyCipher(dir, db); if err != nil { t.Fatal(err) }
 plain, err := second.Open(nil, nonce, encrypted, []byte("id")); if err != nil || string(plain) != "secret" { t.Fatalf("decrypt: %v", err) }
 if err = os.WriteFile(path, []byte("bad"), 0600); err != nil { t.Fatal(err) }
 if _, err = LoadKeyCipher(dir, db); err == nil { t.Fatal("accepted malformed key") }
 if err = os.Remove(path); err != nil { t.Fatal(err) }
 if _, err = db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at,token_ciphertext) VALUES('id',1,'name',x'01',1,x'01')"); err != nil { t.Fatal(err) }
 if _, err = LoadKeyCipher(dir, db); err == nil { t.Fatal("recreated missing key") }
 if _, err = os.Stat(path); !os.IsNotExist(err) { t.Fatal("key file unexpectedly created") }
}

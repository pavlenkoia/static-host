package auth

import (
 "crypto/aes"
 "crypto/cipher"
 "crypto/rand"
 "database/sql"
 "errors"
 "fmt"
 "io"
 "os"
 "path/filepath"
)

// LoadKeyCipher loads the key used to protect recoverable agent tokens.
func LoadKeyCipher(dir string, db *sql.DB) (cipher.AEAD, error) {
 path := filepath.Join(dir, "agent-key-encryption.key")
 f, err := os.Open(path)
 if errors.Is(err, os.ErrNotExist) {
  var count int
  if err = db.QueryRow("SELECT count(*) FROM agent_keys WHERE token_ciphertext IS NOT NULL").Scan(&count); err != nil { return nil, fmt.Errorf("check encrypted keys: %w", err) }
  if count != 0 { return nil, errors.New("encryption key missing for existing encrypted tokens") }
  var key [32]byte
  if _, err = rand.Read(key[:]); err != nil { return nil, err }
  f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
  if errors.Is(err, os.ErrExist) { f, err = os.Open(path) } else if err == nil {
   _, err = f.Write(key[:]); closeErr := f.Close()
   if err != nil { return nil, err }; if closeErr != nil { return nil, closeErr }
   f, err = os.Open(path)
  }
 }
 if err != nil { return nil, fmt.Errorf("open encryption key: %w", err) }
 defer f.Close()
 info, err := f.Stat(); if err != nil { return nil, err }
 if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 { return nil, errors.New("unsafe encryption key permissions") }
 key, err := io.ReadAll(io.LimitReader(f, 33)); if err != nil { return nil, err }
 if len(key) != 32 { return nil, errors.New("invalid encryption key length") }
 block, err := aes.NewCipher(key); if err != nil { return nil, err }
 return cipher.NewGCM(block)
}

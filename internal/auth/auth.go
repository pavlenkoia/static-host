package auth

import (
 "bytes"
 "database/sql"
 "errors"
 "fmt"
 "io"
 "regexp"
 "strings"

 "golang.org/x/crypto/bcrypt"
)

var loginPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

func Login(value string) (string, error) {
 value = strings.TrimSpace(value)
 if !loginPattern.MatchString(value) { return "", errors.New("invalid login") }
 return value, nil
}

func ReadPassword(r io.Reader) (string, error) {
 b, err := io.ReadAll(io.LimitReader(r, 1025)); if err != nil { return "", err }
 if len(b) > 1024 { return "", errors.New("password input exceeds 1 KiB") }
 lines := bytes.Split(b, []byte{'\n'})
 password := bytes.TrimSuffix(lines[0], []byte{'\r'})
 for _, line := range lines[1:] { if len(bytes.TrimSuffix(line, []byte{'\r'})) != 0 { return "", errors.New("extra password input") } }
 if len(password) < 12 || len(password) > 72 { return "", errors.New("password must be 12–72 bytes") }
 return string(password), nil
}

func Hash(password string) (string, error) { b, err := bcrypt.GenerateFromPassword([]byte(password), 12); return string(b), err }
func Compare(hash, password string) bool { return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil }

func Init(db *sql.DB, login, password string) error {
 login, err := Login(login); if err != nil { return err }
 if len(password) < 12 || len(password) > 72 { return errors.New("password must be 12–72 bytes") }
 hash, err := Hash(password); if err != nil { return err }
 tx, err := db.Begin(); if err != nil { return err }; defer tx.Rollback()
 var count int; if err = tx.QueryRow("SELECT count(*) FROM users").Scan(&count); err != nil { return err }; if count != 0 { return errors.New("owner already exists") }
 if _, err = tx.Exec("INSERT INTO users(id,login,password_hash) VALUES(1,?,?)", login, hash); err != nil { return fmt.Errorf("create owner: %w", err) }; return tx.Commit()
}

func Reset(db *sql.DB, password string) error {
 if len(password) < 12 || len(password) > 72 { return errors.New("password must be 12–72 bytes") }
 hash, err := Hash(password); if err != nil { return err }
 tx, err := db.Begin(); if err != nil { return err }; defer tx.Rollback()
 res, err := tx.Exec("UPDATE users SET password_hash=? WHERE id=1", hash); if err != nil { return err }; n, err := res.RowsAffected(); if err != nil { return err }; if n != 1 { return errors.New("owner not initialized") }
 if _, err = tx.Exec("DELETE FROM sessions"); err != nil { return err }; return tx.Commit()
}

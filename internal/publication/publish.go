package publication

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const ownerQuota int64 = 5 << 30

func (s *Service) siteLock(id string) *sync.RWMutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	lock := s.locks[id]
	if lock == nil {
		lock = &sync.RWMutex{}
		s.locks[id] = lock
	}
	return lock
}
func syncTree(root string) error {
	var dirs []string
	err := filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, name)
			return nil
		}
		if !entry.Type().IsRegular() {
			return failure("invalid_archive")
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		err = f.Sync()
		if e := f.Close(); err == nil {
			err = e
		}
		return err
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err = syncDir(dirs[i]); err != nil {
			return err
		}
	}
	return nil
}
func syncDir(name string) error {
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func validIdempotency(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r)) {
			return false
		}
	}
	return true
}
func (s *Service) Publish(ctx context.Context, identity Identity, siteID, uploadID string, expectedGeneration int64, idempotencyKey string) (Site, error) {
	if !validIdempotency(idempotencyKey) || !validStorageID(uploadID) || expectedGeneration < 0 {
		return Site{}, failure("invalid_request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.siteLock(siteID)
	lock.Lock()
	defer lock.Unlock()
	if !s.keyValid(ctx, identity) {
		return Site{}, failure("invalid_request")
	}
	site, err := s.Get(ctx, identity.OwnerID, siteID)
	if err != nil {
		return Site{}, err
	}
	var existingSite, existingUpload, resultJSON string
	var existingGeneration int64
	err = s.db.QueryRowContext(ctx, "SELECT site_id,upload_id,expected_generation,result_json FROM publish_requests WHERE owner_id=? AND idempotency_key=?", identity.OwnerID, idempotencyKey).Scan(&existingSite, &existingUpload, &existingGeneration, &resultJSON)
	if err == nil {
		if existingSite != siteID || existingUpload != uploadID || existingGeneration != expectedGeneration {
			return Site{}, failure("idempotency_conflict")
		}
		var previous Site
		if err = json.Unmarshal([]byte(resultJSON), &previous); err != nil {
			return Site{}, err
		}
		return previous, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Site{}, err
	}
	var state, key, uploadedSite, dir string
	var owner, expires, gen, size, files int64
	err = s.db.QueryRowContext(ctx, "SELECT site_id,owner_id,key_id,state,expires_at,expected_generation,COALESCE(dir,''),size_bytes,file_count FROM uploads WHERE id=?", uploadID).Scan(&uploadedSite, &owner, &key, &state, &expires, &gen, &dir, &size, &files)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, failure("upload_not_ready")
	}
	if err != nil {
		return Site{}, err
	}
	if owner != identity.OwnerID || key != identity.KeyID || uploadedSite != siteID {
		return Site{}, failure("upload_not_ready")
	}
	if state == "expired" || expires <= s.now().Unix() {
		return Site{}, failure("upload_expired")
	}
	if state != "ready" || dir != uploadID {
		return Site{}, failure("upload_not_ready")
	}
	if gen != expectedGeneration || site.Generation != expectedGeneration {
		return Site{}, failure("generation_conflict")
	}
	var total int64
	if err = s.db.QueryRowContext(ctx, "SELECT COALESCE(sum(size_bytes),0) FROM sites WHERE owner_id=? AND deleted_at IS NULL", identity.OwnerID).Scan(&total); err != nil {
		return Site{}, err
	}
	if total-site.SizeBytes+size > ownerQuota {
		return Site{}, failure("quota_exceeded")
	}
	if !s.freeSpace(storageReserve) {
		return Site{}, failure("storage_full")
	}
	staging := filepath.Join(s.dataDir, "sites", "staging", uploadID)
	content := filepath.Join(s.dataDir, "sites", "content", uploadID)
	if err = syncTree(staging); err != nil {
		return Site{}, err
	}
	if s.failpoint != nil {
		s.failpoint("before_rename")
	}
	if err = os.Rename(staging, content); err != nil {
		return Site{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(content)
			_, _ = s.db.Exec("UPDATE uploads SET state='failed',error_code='upload_interrupted' WHERE id=? AND state='ready'", uploadID)
		}
	}()
	if s.failpoint != nil {
		s.failpoint("after_rename")
	}
	if err = syncDir(filepath.Dir(staging)); err != nil {
		return Site{}, err
	}
	if err = syncDir(filepath.Dir(content)); err != nil {
		return Site{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Site{}, err
	}
	defer tx.Rollback()
	var old sql.NullString
	var current int64
	if err = tx.QueryRowContext(ctx, "SELECT current_dir,generation FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL", siteID, identity.OwnerID).Scan(&old, &current); err != nil {
		return Site{}, err
	}
	if current != expectedGeneration {
		return Site{}, failure("generation_conflict")
	}
	site.Generation++
	site.Status = "published"
	site.SizeBytes = size
	site.FileCount = files
	site.UpdatedAt = s.now().Unix()
	site.Upload = nil
	payload, err := json.Marshal(site)
	if err != nil {
		return Site{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE sites SET current_dir=?,generation=?,size_bytes=?,file_count=?,updated_at=? WHERE id=?", uploadID, site.Generation, size, files, site.UpdatedAt, siteID); err != nil {
		return Site{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE uploads SET state='published' WHERE id=?", uploadID); err != nil {
		return Site{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO publish_requests(owner_id,idempotency_key,site_id,upload_id,expected_generation,result_json) VALUES(?,?,?,?,?,?)", identity.OwnerID, idempotencyKey, siteID, uploadID, expectedGeneration, string(payload)); err != nil {
		return Site{}, err
	}
	if err = tx.Commit(); err != nil {
		return Site{}, err
	}
	committed = true
	if s.failpoint != nil {
		s.failpoint("after_commit")
	}
	if old.Valid && validStorageID(old.String) {
		if err = os.RemoveAll(filepath.Join(s.dataDir, "sites", "content", old.String)); err != nil {
			log.Print("publication cleanup failed for old content")
		}
	}
	return site, nil
}
func (s *Service) Delete(ctx context.Context, ownerID int64, siteID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock := s.siteLock(siteID)
	lock.Lock()
	defer lock.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var dir sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT current_dir FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL", siteID, ownerID).Scan(&dir)
	if errors.Is(err, sql.ErrNoRows) {
		return failure("site_not_found")
	}
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM uploads WHERE site_id=? AND state IN ('pending','ready')", siteID)
	if err != nil {
		return err
	}
	var staging []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		if validStorageID(id) {
			staging = append(staging, id)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE sites SET deleted_at=?,current_dir=NULL,size_bytes=0,file_count=0 WHERE id=?", s.now().Unix(), siteID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE uploads SET state='failed',error_code='site_not_found' WHERE site_id=? AND state IN ('pending','ready')", siteID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM publish_requests WHERE site_id=?", siteID); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if dir.Valid && validStorageID(dir.String) {
		if err = os.RemoveAll(filepath.Join(s.dataDir, "sites", "content", dir.String)); err != nil {
			log.Print("publication cleanup failed for deleted content")
		}
	}
	for _, id := range staging {
		if err = os.RemoveAll(filepath.Join(s.dataDir, "sites", "staging", id)); err != nil {
			log.Print("publication cleanup failed for deleted staging")
		}
		if err = os.Remove(filepath.Join(s.dataDir, "sites", "staging", id+".archive")); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Print("publication cleanup failed for deleted archive")
		}
	}
	return nil
}

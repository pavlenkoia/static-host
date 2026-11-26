package publication

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (s *Service) janitor() {
	defer close(s.done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			if err := s.reconcile(false); err != nil {
				log.Printf("publication cleanup: %v", err)
			}
		}
	}
}

func (s *Service) reconcile(startup bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if startup {
		if _, err := s.db.Exec("UPDATE uploads SET state='failed',error_code='upload_interrupted' WHERE state='receiving'"); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec("UPDATE uploads SET state='expired',error_code='upload_expired' WHERE state IN ('pending','ready') AND expires_at<=?", s.now().Unix()); err != nil {
		return err
	}
	content := make(map[string]bool)
	rows, err := s.db.Query("SELECT id,current_dir FROM sites WHERE deleted_at IS NULL AND current_dir IS NOT NULL")
	if err != nil {
		return err
	}
	for rows.Next() {
		var site, dir string
		if err = rows.Scan(&site, &dir); err != nil {
			break
		}
		if !validStorageID(dir) {
			err = fmt.Errorf("site %s: invalid content directory", site)
			break
		}
		var info os.FileInfo
		info, err = os.Lstat(filepath.Join(s.dataDir, "sites", "content", dir))
		if err != nil || !info.IsDir() {
			err = fmt.Errorf("site %s: referenced content missing", site)
			break
		}
		content[dir] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	staging := make(map[string]bool)
	rows, err = s.db.Query("SELECT id,state FROM uploads WHERE state IN ('pending','receiving','ready')")
	if err != nil {
		return err
	}
	var interrupted []string
	for rows.Next() {
		var id, state string
		if err = rows.Scan(&id, &state); err != nil {
			break
		}
		if !validStorageID(id) {
			err = fmt.Errorf("invalid upload id")
			break
		}
		if startup && state == "ready" {
			info, e := os.Lstat(filepath.Join(s.dataDir, "sites", "staging", id))
			if e != nil || !info.IsDir() {
				interrupted = append(interrupted, id)
				continue
			}
		}
		staging[id] = true
		staging[id+".archive"] = true
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range interrupted {
		if _, err = s.db.Exec("UPDATE uploads SET state='failed',error_code='upload_interrupted' WHERE id=? AND state='ready'", id); err != nil {
			return err
		}
	}
	for _, area := range []struct {
		name string
		keep map[string]bool
	}{{"content", content}, {"staging", staging}} {
		root := filepath.Join(s.dataDir, "sites", area.name)
		entries, e := os.ReadDir(root)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if !area.keep[entry.Name()] {
				if e = os.RemoveAll(filepath.Join(root, entry.Name())); e != nil {
					log.Printf("publication cleanup failed for %s", area.name)
				}
			}
		}
	}
	return nil
}

func validStorageID(id string) bool {
	if len(id) != 32 {
		return false
	}
	return strings.IndexFunc(id, func(r rune) bool { return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) }) < 0
}

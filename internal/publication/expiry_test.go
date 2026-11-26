package publication

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"static-host/internal/store"
)

func TestExpiryReleasesUploadSlot(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('key',1,'agent',x'01',1)"); err != nil {
		t.Fatal(err)
	}
	s, err := New(db, dir, Options{UploadOrigin: "http://localhost:8081", SiteSuffix: "localhost:8081", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Unix(2000000000, 0)
	s.now = func() time.Time { return now }
	s.spaceCheck = func(int64) bool { return true }
	id := Identity{OwnerID: 1, KeyID: "key"}
	site, err := s.Create(context.Background(), 1, "site")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateUpload(context.Background(), id, site.ID, "zip", 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExpiresAt != now.Add(uploadTTL).Unix() {
		t.Fatalf("expiry: %d", first.ExpiresAt)
	}
	now = now.Add(uploadTTL)
	if err = s.reconcile(false); err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateUpload(context.Background(), id, site.ID, "zip", 0)
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(dir, "sites", "staging", second.UploadID)
	if err = os.Mkdir(staged, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(staged, "index.html"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("UPDATE uploads SET state='ready',dir=? WHERE id=?", second.UploadID, second.UploadID); err != nil {
		t.Fatal(err)
	}
	now = now.Add(uploadTTL)
	if err = s.reconcile(false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("expired staging remains: %v", err)
	}
	if _, err = s.CreateUpload(context.Background(), id, site.ID, "zip", 0); err != nil {
		t.Fatalf("slot not released: %v", err)
	}
}

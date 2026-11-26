package publication

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"static-host/internal/store"
)

type blockingWriter struct {
	header  http.Header
	started chan struct{}
	release chan struct{}
	body    strings.Builder
}

func (w *blockingWriter) Header() http.Header { return w.header }
func (w *blockingWriter) WriteHeader(int)     {}
func (w *blockingWriter) Write(p []byte) (int, error) {
	close(w.started)
	<-w.release
	return w.body.Write(p)
}
func TestDeleteWaitsForActiveResponse(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	content := strings.Repeat("a", 32)
	siteID := "s-" + strings.Repeat("b", 32)
	root := filepath.Join(dir, "sites", "content", content)
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "index.html"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO sites(id,owner_id,name,generation,current_dir,size_bytes,file_count,created_at,updated_at) VALUES(?,1,'site',1,?,7,1,1,1)", siteID, content); err != nil {
		t.Fatal(err)
	}
	s, err := New(db, dir, Options{SiteSuffix: "localhost:8081", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w := &blockingWriter{header: make(http.Header), started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		s.SiteHandler().ServeHTTP(w, httptest.NewRequest("GET", "http://"+siteID+".localhost:8081/", nil))
		close(done)
	}()
	select {
	case <-w.started:
	case <-time.After(5 * time.Second):
		t.Fatal("response did not start")
	}
	deleted := make(chan error, 1)
	go func() { deleted <- s.Delete(context.Background(), 1, siteID) }()
	select {
	case err := <-deleted:
		t.Fatalf("delete did not wait: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(w.release)
	<-done
	if err = <-deleted; err != nil {
		t.Fatal(err)
	}
	if w.body.String() != "content" {
		t.Fatalf("truncated response: %q", w.body.String())
	}
	after := httptest.NewRecorder()
	s.SiteHandler().ServeHTTP(after, httptest.NewRequest("GET", "http://"+siteID+".localhost:8081/", nil))
	if after.Code != 404 {
		t.Fatalf("deleted site: %d", after.Code)
	}
}

func TestShortAndLegacyHostsServeSameSite(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	content := strings.Repeat("a", 32)
	id := "s-" + strings.Repeat("b", 32)
	root := filepath.Join(dir, "sites", "content", content)
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "index.html"), []byte("glina"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO sites(id,owner_id,name,current_dir,created_at,updated_at) VALUES(?,1,'glina-i-svet',?,1,1)", id, content); err != nil {
		t.Fatal(err)
	}
	s, err := New(db, dir, Options{SiteSuffix: "sites.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, host := range []string{"glina-i-svet", id} {
		w := httptest.NewRecorder()
		s.SiteHandler().ServeHTTP(w, httptest.NewRequest("GET", "https://"+host+".sites.example.test/", nil))
		if w.Code != 200 || w.Body.String() != "glina" {
			t.Fatalf("%s: %d %s", host, w.Code, w.Body.String())
		}
	}
	if err = s.Delete(context.Background(), 1, id); err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"glina-i-svet", id, "unknown"} {
		w := httptest.NewRecorder()
		s.SiteHandler().ServeHTTP(w, httptest.NewRequest("GET", "https://"+host+".sites.example.test/", nil))
		if w.Code != 404 {
			t.Fatalf("%s: %d", host, w.Code)
		}
	}
}

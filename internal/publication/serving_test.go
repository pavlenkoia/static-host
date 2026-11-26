package publication

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"static-host/internal/store"
)

func customServingFixture(t *testing.T) (*Service, *sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("c", 32)
	siteID := "s-" + strings.Repeat("d", 32)
	root := filepath.Join(dir, "sites", "content", content)
	if err = os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "index.html"), []byte("custom content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "healthz"), []byte("site health file"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO sites(id,owner_id,name,generation,current_dir,size_bytes,file_count,created_at,updated_at) VALUES(?,1,'site',1,?,14,1,1,1)", siteID, content); err != nil {
		t.Fatal(err)
	}
	s, err := New(db, dir, Options{SiteSuffix: "sites.example.test", DomainIngressIPs: []netip.Addr{netip.MustParseAddr("1.1.1.1")}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = s.Close()
		_ = db.Close()
	})
	return s, db, siteID
}

func addServingDomain(t *testing.T, db *sql.DB, id, siteID, host, state, token string) {
	t.Helper()
	claimed, checked := any(int64(1)), any(int64(1))
	if state == "pending_dns" {
		claimed, checked = nil, nil
	}
	next := time.Now().Add(time.Hour).Unix()
	if _, err := db.Exec(`INSERT INTO domains(id,site_id,hostname,state,claimed_at,dns_checked_at,next_check_at,probe_token,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,1,1)`, id, siteID, host, state, claimed, checked, next, token); err != nil {
		t.Fatal(err)
	}
}

func TestCustomDomainServingAndProbe(t *testing.T) {
	s, db, siteID := customServingFixture(t)
	addServingDomain(t, db, "active-domain", siteID, "alias.example.org", "active", "active-probe-token")
	addServingDomain(t, db, "tls-domain", siteID, "tls.example.org", "provisioning_tls", "tls-probe-token")
	addServingDomain(t, db, "pending-domain", siteID, "pending.example.org", "pending_dns", "pending-probe-token")
	addServingDomain(t, db, "suspended-domain", siteID, "suspended.example.org", "suspended", "suspended-probe-token")

	for _, tc := range []struct {
		host, path string
		status     int
		body       string
	}{
		{"alias.example.org", "/", http.StatusOK, "custom content"},
		{"alias.example.org", "/healthz", http.StatusOK, "site health file"},
		{"alias.example.org", "/api/session", http.StatusNotFound, ""},
		{"alias.example.org", "/mcp", http.StatusNotFound, ""},
		{"alias.example.org", "/uploads/file", http.StatusNotFound, ""},
		{"tls.example.org", "/", http.StatusNotFound, ""},
		{"tls.example.org", "/.well-known/static-host-domain/tls-probe-token", http.StatusOK, "tls-probe-token"},
		{"alias.example.org", "/.well-known/static-host-domain/active-probe-token", http.StatusOK, "active-probe-token"},
		{"alias.example.org", "/.well-known/static-host-domain/wrong-token", http.StatusNotFound, ""},
		{"pending.example.org", "/", http.StatusNotFound, ""},
		{"pending.example.org", "/.well-known/static-host-domain/pending-probe-token", http.StatusNotFound, ""},
		{"suspended.example.org", "/", http.StatusNotFound, ""},
		{"suspended.example.org", "/.well-known/static-host-domain/suspended-probe-token", http.StatusNotFound, ""},
	} {
		r := httptest.NewRequest(http.MethodGet, "https://"+tc.host+tc.path, nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		s.SiteHandler().ServeHTTP(w, r)
		if w.Code != tc.status || tc.status != http.StatusNotFound && w.Body.String() != tc.body {
			t.Errorf("%s%s: got %d %q, want status %d and body %q", tc.host, tc.path, w.Code, w.Body.String(), tc.status, tc.body)
		}
		if strings.HasPrefix(tc.path, "/.well-known/static-host-domain/") && tc.status == http.StatusOK && w.Header().Get("Content-Type") != "text/plain" {
			t.Errorf("%s: probe content type %q", tc.host, w.Header().Get("Content-Type"))
		}
	}
}

func TestDeleteDomainWaitsForCustomResponse(t *testing.T) {
	s, db, siteID := customServingFixture(t)
	addServingDomain(t, db, "delete-domain", siteID, "delete.example.org", "active", "delete-probe-token")

	w := &blockingWriter{header: make(http.Header), started: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		r := httptest.NewRequest(http.MethodGet, "https://delete.example.org/", nil)
		r.Host = "delete.example.org"
		s.SiteHandler().ServeHTTP(w, r)
		close(done)
	}()
	select {
	case <-w.started:
	case <-time.After(5 * time.Second):
		t.Fatal("custom response did not start")
	}
	deleted := make(chan error, 1)
	go func() {
		deleted <- s.DeleteDomain(context.Background(), 1, siteID, "delete-domain")
	}()
	select {
	case err := <-deleted:
		t.Fatalf("domain delete did not wait for active response: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(w.release)
	<-done
	if err := <-deleted; err != nil {
		t.Fatal(err)
	}
	after := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "https://delete.example.org/", nil)
	r.Host = "delete.example.org"
	s.SiteHandler().ServeHTTP(after, r)
	if after.Code != http.StatusNotFound {
		t.Fatalf("deleted alias: %d %s", after.Code, after.Body.String())
	}
}

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

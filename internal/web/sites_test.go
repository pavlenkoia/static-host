package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"static-host/internal/publication"
	"static-host/internal/store"
)

func TestSitesSessionAndDelete(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pubs := testPublications(t, db)
	s, err := New(db, testCipher(t), "http://127.0.0.1:8080", "long-password-one", pubs)
	if err != nil {
		t.Fatal(err)
	}
	site, err := pubs.Create(context.Background(), 1, "site")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("INSERT INTO sessions VALUES(?,1,'csrf',?,?)", digest("session"), time.Now().Unix(), time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, origin, csrf string, cookie bool) int {
		r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, nil)
		if cookie {
			r.AddCookie(&http.Cookie{Name: "static_host_session", Value: "session"})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	if got := request("GET", "/api/sites", "", "", false); got != 401 {
		t.Fatal(got)
	}
	if got := request("GET", "/api/sites", "", "", true); got != 200 {
		t.Fatal(got)
	}
	if got := request("DELETE", "/api/sites/"+site.ID, "http://other", "csrf", true); got != 403 {
		t.Fatal(got)
	}
	if got := request("DELETE", "/api/sites/"+site.ID, "http://127.0.0.1:8080", "wrong", true); got != 403 {
		t.Fatal(got)
	}
	if got := request("DELETE", "/api/sites/"+site.ID, "http://127.0.0.1:8080", "csrf", true); got != 204 {
		t.Fatal(got)
	}
	if got := request("DELETE", "/api/sites/"+site.ID, "http://127.0.0.1:8080", "csrf", true); got != 404 {
		t.Fatal(got)
	}
}

func TestSiteDomainsAuthenticationAndErrors(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pubs, err := publication.New(db, t.TempDir(), publication.Options{
		UploadOrigin:     "http://127.0.0.1:8080",
		SiteSuffix:       "localhost:8080",
		Local:            true,
		BaseDomain:       "custom.example",
		DomainIngressIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pubs.Close() })
	s, err := New(db, testCipher(t), "http://127.0.0.1:8080", "long-password-one", pubs)
	if err != nil {
		t.Fatal(err)
	}
	site, err := pubs.Create(context.Background(), 1, "site")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(
		"INSERT INTO sessions VALUES(?,1,'csrf',?,?)",
		digest("session"),
		time.Now().Unix(),
		time.Now().Add(time.Hour).Unix(),
	); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, origin, csrf string, cookie bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if cookie {
			r.AddCookie(&http.Cookie{Name: "static_host_session", Value: "session"})
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	base := "/api/sites/" + site.ID + "/domains"
	if w := request("GET", base, "", "", "", false); w.Code != 401 {
		t.Fatalf("unauthenticated domain list: %d", w.Code)
	}
	if w := request("POST", base+"/domain-id/check", "", "http://127.0.0.1:8080", "", false); w.Code != 401 {
		t.Fatalf("unauthenticated domain check: %d", w.Code)
	}
	for _, tc := range []struct {
		method, path, body string
	}{
		{"POST", base, `{"hostname":"www.example.net"}`},
		{"DELETE", base + "/domain-id", ""},
		{"POST", base + "/domain-id/check", ""},
	} {
		if w := request(tc.method, tc.path, tc.body, "http://other", "csrf", true); w.Code != 403 {
			t.Fatalf("bad origin %s %s: %d", tc.method, tc.path, w.Code)
		}
		if w := request(tc.method, tc.path, tc.body, "http://127.0.0.1:8080", "wrong", true); w.Code != 403 {
			t.Fatalf("bad CSRF %s %s: %d", tc.method, tc.path, w.Code)
		}
	}
	for _, tc := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"GET", "/api/sites/missing/domains", "", 404, "site_not_found"},
		{"POST", "/api/sites/missing/domains", `{"hostname":"www.example.net"}`, 404, "site_not_found"},
		{"POST", base, `{"hostname":"bad hostname"}`, 400, "invalid_domain"},
		{"POST", base, `{"hostname":"www.example.net"}`, 201, ""},
	} {
		w := request(tc.method, tc.path, tc.body, "http://127.0.0.1:8080", "csrf", true)
		if w.Code != tc.status {
			t.Fatalf("%s %s: got %d body %s, want %d", tc.method, tc.path, w.Code, w.Body, tc.status)
		}
		if tc.code != "" {
			var result map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result["error"] != tc.code {
				t.Fatalf("%s %s error: got %v, want %s", tc.method, tc.path, result, tc.code)
			}
		}
	}
	if w := request("GET", "/api/sites/"+site.ID+"/domains/missing", "", "", "", true); w.Code != 405 {
		t.Fatalf("unsupported domain GET: %d", w.Code)
	}
	if w := request("POST", "/api/sites/"+site.ID+"/domains/missing/check", "", "http://127.0.0.1:8080", "csrf", true); w.Code != 404 {
		t.Fatalf("unknown domain check: %d", w.Code)
	}
}

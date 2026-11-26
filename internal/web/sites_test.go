package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

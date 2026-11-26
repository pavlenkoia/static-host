package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"static-host/internal/store"
	"testing"
)

func TestHTTPSSession(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(db, testCipher(t), "https://panel.example.test", "long-password-one", testPublications(t, db))
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path, body string, c *http.Cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://internal"+path, bytes.NewBufferString(body))
		r.Header.Set("Origin", "https://panel.example.test")
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		if c != nil {
			r.AddCookie(c)
		}
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := call("POST", "/api/login", `{"login":"admin","password":"long-password-one"}`, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	c := w.Result().Cookies()[0]
	if c.Name != "__Host-static_host_session" || !c.Secure || !c.HttpOnly || c.Domain != "" || c.Path != "/" || c.SameSite != http.SameSiteStrictMode {
		t.Fatal(c)
	}
	var sess session
	if err = json.Unmarshal(w.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	if w = call("GET", "/api/session", "", c, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = call("POST", "/api/logout", "", c, sess.CSRF)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	deleted := w.Result().Cookies()[0]
	if deleted.Name != c.Name || !deleted.Secure || !deleted.HttpOnly || deleted.Domain != "" || deleted.Path != "/" || deleted.MaxAge != -1 || deleted.SameSite != http.SameSiteStrictMode {
		t.Fatal(deleted)
	}
	if w = call("GET", "/api/session", "", c, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
}

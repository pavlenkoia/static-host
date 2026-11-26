package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouting(t *testing.T) {
	h := New("https://panel.example.test", "https://mcp.example.test", "https://upload.example.test", "sites.example.test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(202) }), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(203) }), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, x := range []struct {
		host, path, method string
		want               int
	}{{"PANEL.EXAMPLE.TEST:443", "/", "GET", 201}, {"panel.example.test", "/healthz", "GET", 200}, {"mcp.example.test", "/mcp", "POST", 202}, {"panel.example.test", "/mcp", "POST", 404}, {"mcp.example.test", "/", "GET", 404}, {"upload.example.test", "/uploads/a", "PUT", 203}, {"upload.example.test", "/api/session", "GET", 404}, {"s-0123456789abcdef0123456789abcdef.sites.example.test", "/healthz", "GET", 204}, {"s-0123456789abcdef0123456789abcdef.sites.example.test:8443", "/", "GET", 403}, {"evil.test", "/", "GET", 204}, {"evil.test", "/healthz", "HEAD", 204}, {"evil.test", "/healthz", "POST", 204}, {"evil.test:443", "/", "GET", 204}, {"evil.test:80", "/", "GET", 403}, {"evil.test:8443", "/", "GET", 403}, {"bad..test", "/", "GET", 403}, {"mcp.example.test", "/mcp/", "GET", 404}} {
		r := httptest.NewRequest(x.method, "http://test"+x.path, nil)
		r.Host = x.host
		r.Header.Set("X-Forwarded-Host", "panel.example.test")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != x.want {
			t.Errorf("%+v got %d", x, w.Code)
		}
	}
}

func TestLocalLANOrigin(t *testing.T) {
	origin := "http://192.0.2.10:8081"
	panel := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := New(origin, origin, origin, "localhost:8081", panel, http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler())
	for _, tc := range []struct {
		host string
		want int
	}{{"192.0.2.10:8081", 204}, {"192.0.2.11:8081", 403}, {"192.0.2.10:8082", 403}, {"attacker.invalid:8081", 404}} {
		r := httptest.NewRequest("GET", "http://internal/", nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("Host %s: got %d want %d", tc.host, w.Code, tc.want)
		}
	}
}

func TestShortSiteRouting(t *testing.T) {
	sites := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	h := New("https://panel.example.test", "https://mcp.example.test", "https://upload.example.test", "sites.example.test", http.NotFoundHandler(), http.NotFoundHandler(), http.NotFoundHandler(), sites)
	for _, tc := range []struct {
		host string
		want int
	}{{"glina-i-svet.sites.example.test", 204}, {"glina-i-svet.sites.example.test:8443", 403}, {"nested.glina.sites.example.test", 403}, {"-glina.sites.example.test", 403}, {"glina_.sites.example.test", 403}} {
		r := httptest.NewRequest("GET", "https://internal/", nil)
		r.Host = tc.host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s: got %d want %d", tc.host, w.Code, tc.want)
		}
	}
}

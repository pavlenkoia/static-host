package httpserver

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"static-host/internal/domainname"
)

func matches(host string, origin *url.URL) bool {
	if strings.ContainsAny(host, "/@, \t\r\n") || host == "" {
		return false
	}
	expected := origin.Hostname()
	name := host
	port := ""
	if strings.HasPrefix(host, "[") || strings.Count(host, ":") > 0 {
		var err error
		name, port, err = net.SplitHostPort(host)
		if err != nil {
			return false
		}
	}
	if !strings.EqualFold(name, expected) {
		return false
	}
	if origin.Scheme == "https" {
		return port == "" || port == "443"
	}
	return port != "" && port == origin.Port()
}
func New(panelOrigin, mcpOrigin, uploadOrigin, siteSuffix string, panel, mcp, upload, sites http.Handler) http.Handler {
	p, _ := url.Parse(panelOrigin)
	m, _ := url.Parse(mcpOrigin)
	u, _ := url.Parse(uploadOrigin)
	suffix := siteSuffix
	local := strings.Contains(siteSuffix, ":")
	localPort := ""
	if local {
		suffix, localPort, _ = net.SplitHostPort(siteSuffix)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hostName, port, validHost := requestHost(r.Host)
		if !validHost {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		name := strings.ToLower(hostName)
		tail := "." + strings.ToLower(suffix)
		if strings.HasSuffix(name, tail) {
			label := strings.TrimSuffix(name, tail)
			valid := len(label) > 0 && len(label) <= 63 && label[0] != '-' && label[len(label)-1] != '-'
			if valid {
				for _, c := range label {
					if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c == '-') {
						valid = false
						break
					}
				}
			}
			if valid {
				scheme := "https://"
				if local {
					scheme = "http://"
				}
				expected, _ := url.Parse(scheme + label + "." + siteSuffix)
				if matches(r.Host, expected) {
					sites.ServeHTTP(w, r)
					return
				}
			}
			if valid && portAllowed(port, local, localPort) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "Forbidden", http.StatusForbidden)
			}
			return
		}

		mp := matches(r.Host, m)
		pp := matches(r.Host, p)
		up := matches(r.Host, u)
		if r.URL.Path == "/healthz" && (mp || pp || up) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if r.Method == "GET" {
				_, _ = w.Write([]byte("ok\n"))
			} else if r.Method != "HEAD" {
				w.Header().Set("Allow", "GET, HEAD")
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
			return
		}
		if up && strings.HasPrefix(r.URL.Path, "/uploads/") {
			upload.ServeHTTP(w, r)
			return
		}
		if mp && r.URL.Path == "/mcp" {
			mcp.ServeHTTP(w, r)
			return
		}
		if pp && r.URL.Path != "/mcp" && !strings.HasPrefix(r.URL.Path, "/uploads/") {
			panel.ServeHTTP(w, r)
			return
		}
		if mp || pp || up {
			http.NotFound(w, r)
			return
		}
		if !portAllowed(port, local, localPort) || !validDNSHostname(hostName) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		sites.ServeHTTP(w, r)
	})
}

func requestHost(raw string) (name, port string, ok bool) {
	if raw == "" || strings.ContainsAny(raw, "/@, \t\r\n") {
		return "", "", false
	}
	name = raw
	if strings.HasPrefix(raw, "[") || strings.Contains(raw, ":") {
		var err error
		name, port, err = net.SplitHostPort(raw)
		if err != nil || name == "" || !validPort(port) {
			return "", "", false
		}
	}
	return name, port, true
}

func validPort(port string) bool {
	if len(port) == 0 || len(port) > 5 {
		return false
	}
	n := 0
	for _, c := range port {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	return n > 0 && n <= 65535
}

func portAllowed(port string, local bool, localPort string) bool {
	return port == "" || port == "443" || local && port == localPort
}

func validDNSHostname(host string) bool {
	_, err := domainname.Normalize(host)
	return err == nil
}

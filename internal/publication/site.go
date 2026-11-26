package publication

import (
	"database/sql"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"static-host/internal/domainname"
)

func (s *Service) SiteHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if siteControlPath(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		host, ok := siteRequestHost(r.Host, s.options.Local, siteListenerPort(s.options.SiteSuffix))
		if !ok {
			http.NotFound(w, r)
			return
		}
		suffix := s.options.SiteSuffix
		if h, _, err := net.SplitHostPort(suffix); err == nil {
			suffix = h
		}
		suffix = strings.ToLower(strings.TrimSuffix(suffix, "."))

		escaped := r.URL.EscapedPath()
		decoded, err := url.PathUnescape(escaped)
		if err != nil || strings.ContainsAny(decoded, "\\\x00") || strings.Contains(decoded, "%") || !strings.HasPrefix(decoded, "/") {
			http.NotFound(w, r)
			return
		}
		if siteControlPath(decoded) {
			http.NotFound(w, r)
			return
		}
		const probeRoot = "/.well-known/static-host-domain"
		isProbe := strings.HasPrefix(decoded, probeRoot+"/")
		if decoded == probeRoot || strings.HasPrefix(decoded, probeRoot+"/") && (decoded == probeRoot+"/" || strings.Contains(strings.TrimPrefix(decoded, probeRoot+"/"), "/")) {
			http.NotFound(w, r)
			return
		}
		probeToken := ""
		if isProbe {
			probeToken = strings.TrimPrefix(decoded, probeRoot+"/")
			if probeToken == "" {
				http.NotFound(w, r)
				return
			}
			if r.Method != http.MethodGet {
				w.Header().Set("Allow", http.MethodGet)
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
		}

		name := strings.TrimPrefix(decoded, "/")
		for _, segment := range strings.Split(name, "/") {
			if segment == "." || segment == ".." {
				http.NotFound(w, r)
				return
			}
		}
		if name != "" && strings.HasSuffix(name, "/") {
			name = strings.TrimSuffix(name, "/")
		}
		if name != "" && !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}

		var id string
		isSystemHost := false
		if strings.HasSuffix(host, "."+suffix) {
			hostname := strings.TrimSuffix(host, "."+suffix)
			if hostname == "" || strings.Contains(hostname, ".") || isProbe {
				http.NotFound(w, r)
				return
			}
			isSystemHost = true
			err = s.db.QueryRowContext(r.Context(), "SELECT id FROM sites WHERE (slug=? OR id=?) AND deleted_at IS NULL", hostname, hostname).Scan(&id)
		} else {
			if isProbe && !strings.HasPrefix(decoded, probeRoot+"/") {
				http.NotFound(w, r)
				return
			}
			isProbe = strings.HasPrefix(decoded, probeRoot+"/")
			stateClause := "d.state='active'"
			if isProbe {
				stateClause = "d.state IN ('provisioning_tls','active')"
			}
			err = s.db.QueryRowContext(r.Context(), `SELECT d.site_id
				FROM domains d JOIN sites s ON s.id=d.site_id
				WHERE d.hostname=? AND d.claimed_at IS NOT NULL AND d.dns_checked_at IS NOT NULL
					AND `+stateClause+` AND s.deleted_at IS NULL`, host).Scan(&id)
		}
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Unavailable", http.StatusServiceUnavailable)
			return
		}

		lock := s.siteLock(id)
		lock.RLock()
		defer lock.RUnlock()

		var dir sql.NullString
		if isSystemHost {
			err = s.db.QueryRowContext(r.Context(), "SELECT current_dir FROM sites WHERE id=? AND deleted_at IS NULL", id).Scan(&dir)
		} else {
			stateClause := "d.state='active'"
			if isProbe {
				stateClause = "d.state IN ('provisioning_tls','active')"
			}
			var currentProbeToken string
			err = s.db.QueryRowContext(r.Context(), `SELECT s.current_dir,d.probe_token
				FROM domains d JOIN sites s ON s.id=d.site_id
				WHERE d.hostname=? AND d.site_id=? AND d.claimed_at IS NOT NULL AND d.dns_checked_at IS NOT NULL
					AND `+stateClause+` AND s.deleted_at IS NULL`, host, id).Scan(&dir, &currentProbeToken)
			if err == nil && isProbe {
				if currentProbeToken != probeToken {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write([]byte(currentProbeToken))
				return
			}
		}
		if errors.Is(err, sql.ErrNoRows) || err == nil && !dir.Valid {
			http.NotFound(w, r)
			return
		}
		if err != nil || !validStorageID(dir.String) {
			http.Error(w, "Unavailable", http.StatusServiceUnavailable)
			return
		}
		root, err := os.OpenRoot(filepath.Join(s.dataDir, "sites", "content", dir.String))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer root.Close()
		lookup := name
		if lookup == "" {
			lookup = "."
		}
		f, err := root.Open(lookup)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		if info.IsDir() {
			f.Close()
			lookup = path.Join(lookup, "index.html")
			f, err = root.Open(lookup)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer f.Close()
			info, err = f.Stat()
			if err != nil || !info.Mode().IsRegular() {
				http.NotFound(w, r)
				return
			}
			if !strings.HasSuffix(decoded, "/") {
				u := *r.URL
				u.Scheme = ""
				u.Host = ""
				u.Path = decoded + "/"
				u.RawPath = ""
				http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
				return
			}
		} else if !info.Mode().IsRegular() || strings.HasSuffix(decoded, "/") {
			http.NotFound(w, r)
			return
		}
		contentType := mime.TypeByExtension(path.Ext(lookup))
		if contentType == "" {
			var sample [512]byte
			n, _ := f.Read(sample[:])
			contentType = http.DetectContentType(sample[:n])
			_, _ = f.Seek(0, io.SeekStart)
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeContent(w, r, path.Base(lookup), info.ModTime(), f)
	})
}

func siteRequestHost(raw string, local bool, localPort string) (string, bool) {
	if raw == "" || strings.ContainsAny(raw, "/@, \t\r\n") {
		return "", false
	}
	host := raw
	port := ""
	if strings.HasPrefix(raw, "[") || strings.Contains(raw, ":") {
		var err error
		host, port, err = net.SplitHostPort(raw)
		if err != nil || !validSitePort(port) || port != "443" && !(local && port == localPort) {
			return "", false
		}
	}
	normalized, err := domainname.Normalize(host)
	return normalized, err == nil
}

func validSitePort(port string) bool {
	if port == "" || len(port) > 5 {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n <= 65535
}

func siteListenerPort(siteSuffix string) string {
	_, port, err := net.SplitHostPort(siteSuffix)
	if err != nil {
		return ""
	}
	return port
}

func siteControlPath(decoded string) bool {
	return decoded == "/api" || strings.HasPrefix(decoded, "/api/") ||
		decoded == "/mcp" || decoded == "/uploads" || strings.HasPrefix(decoded, "/uploads/")
}

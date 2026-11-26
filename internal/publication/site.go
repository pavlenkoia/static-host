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
	"strings"
)

func (s *Service) SiteHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", 405)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		suffix := s.options.SiteSuffix
		if h, _, err := net.SplitHostPort(suffix); err == nil {
			suffix = h
		}
		host = strings.ToLower(host)
		suffix = strings.ToLower(suffix)
		hostname := strings.TrimSuffix(host, "."+suffix)
		if hostname == host || hostname == "" || strings.Contains(hostname, ".") {
			http.NotFound(w, r)
			return
		}
		escaped := r.URL.EscapedPath()
		decoded, err := url.PathUnescape(escaped)
		if err != nil || strings.ContainsAny(decoded, "\\\x00") || strings.Contains(decoded, "%") || !strings.HasPrefix(decoded, "/") {
			http.NotFound(w, r)
			return
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
		err = s.db.QueryRowContext(r.Context(), "SELECT id FROM sites WHERE (slug=? OR id=?) AND deleted_at IS NULL", hostname, hostname).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			http.Error(w, "Unavailable", 503)
			return
		}
		lock := s.siteLock(id)
		lock.RLock()
		defer lock.RUnlock()
		var dir sql.NullString
		err = s.db.QueryRowContext(r.Context(), "SELECT current_dir FROM sites WHERE id=? AND deleted_at IS NULL", id).Scan(&dir)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !dir.Valid {
			http.NotFound(w, r)
			return
		}
		if err != nil || !validStorageID(dir.String) {
			http.Error(w, "Unavailable", 503)
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

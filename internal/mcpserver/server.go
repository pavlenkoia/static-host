package mcpserver

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"static-host/internal/publication"
	"static-host/internal/web"
)

type identityKey struct{}
type Identity struct {
	OwnerID int64
	KeyID   string
}

func New(db *sql.DB, origin string, publications *publication.Service) http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "static-host", Version: "0.1.0"}, nil)
	registerPublicationTools(server, publications)
	// The outer HTTP router validates Host against the configured public authority.
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 1 << 20, DisableLocalhostProtection: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origins := r.Header.Values("Origin")
		if len(origins) > 1 || (len(origins) == 1 && origins[0] != origin) {
			http.Error(w, "Forbidden", 403)
			return
		}
		unauthorized := func() {
			w.Header().Set("WWW-Authenticate", `Bearer realm="static-host"`)
			http.Error(w, "Unauthorized", 401)
		}
		headers := r.Header.Values("Authorization")
		if len(headers) != 1 {
			unauthorized()
			return
		}
		fields := strings.Fields(headers[0])
		if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") || strings.IndexFunc(fields[1], unicode.IsSpace) >= 0 {
			unauthorized()
			return
		}
		id, owner, err := web.AuthenticateKey(db, fields[1], time.Now().Unix())
		if errors.Is(err, sql.ErrNoRows) {
			unauthorized()
			return
		}
		if err != nil {
			http.Error(w, "Service unavailable", 503)
			return
		}
		handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), identityKey{}, Identity{OwnerID: owner, KeyID: id})))
	})
}

package web

import (
	"errors"
	"net/http"
	"strings"

	"static-host/internal/publication"
)

func (s *Server) sites(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/sites" {
		if r.Method != "GET" {
			jsonError(w, 405, "method_not_allowed")
			return
		}
		if _, ok := s.require(w, r, false); !ok {
			return
		}
		sites, err := s.Publications.List(r.Context(), 1)
		if err != nil {
			jsonError(w, 503, "service_unavailable")
			return
		}
		jsonReply(w, 200, map[string]any{"sites": sites})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/sites/")
	parts := strings.Split(id, "/")
	if len(parts) > 1 && parts[1] == "domains" {
		s.siteDomains(w, r, parts[0], parts[2:])
		return
	}
	if id == "" || strings.Contains(id, "/") {
		jsonError(w, 404, "site_not_found")
		return
	}
	if r.Method != "DELETE" {
		jsonError(w, 405, "method_not_allowed")
		return
	}
	if _, ok := s.require(w, r, true); !ok {
		return
	}
	if !emptyBody(w, r) {
		return
	}
	if err := s.Publications.Delete(r.Context(), 1, id); err != nil {
		var e *publication.Error
		if errors.As(err, &e) && e.Code == "site_not_found" {
			jsonError(w, 404, e.Code)
		} else {
			jsonError(w, 503, "service_unavailable")
		}
		return
	}
	w.WriteHeader(204)
}

func (s *Server) siteDomains(w http.ResponseWriter, r *http.Request, siteID string, rest []string) {
	if siteID == "" || len(rest) > 2 {
		jsonError(w, 404, "domain_not_found")
		return
	}
	if len(rest) == 0 || (len(rest) == 1 && rest[0] == "") {
		if r.Method == "GET" {
			if _, ok := s.require(w, r, false); !ok {
				return
			}
			domains, err := s.Publications.ListDomains(r.Context(), 1, siteID)
			if err != nil {
				domainAPIError(w, err)
				return
			}
			jsonReply(w, 200, map[string]any{"domains": domains})
			return
		}
		if r.Method == "POST" {
			if _, ok := s.require(w, r, true); !ok {
				return
			}
			var in struct {
				Hostname string `json:"hostname"`
			}
			if !decode(w, r, &in) {
				return
			}
			domain, err := s.Publications.AddDomain(r.Context(), 1, siteID, in.Hostname)
			if err != nil {
				domainAPIError(w, err)
				return
			}
			jsonReply(w, 201, map[string]any{"domain": domain})
			return
		}
		jsonError(w, 405, "method_not_allowed")
		return
	}
	if rest[0] == "" {
		jsonError(w, 404, "domain_not_found")
		return
	}
	domainID := rest[0]
	action := ""
	if len(rest) == 2 {
		action = rest[1]
	}
	switch action {
	case "check":
		if r.Method != "POST" {
			jsonError(w, 405, "method_not_allowed")
			return
		}
		if _, ok := s.require(w, r, true); !ok {
			return
		}
		if !emptyBody(w, r) {
			return
		}
		domain, err := s.Publications.CheckDomain(r.Context(), 1, siteID, domainID)
		if err != nil {
			domainAPIError(w, err)
			return
		}
		jsonReply(w, 202, map[string]any{"domain": domain})
	case "":
		if r.Method != "DELETE" {
			jsonError(w, 405, "method_not_allowed")
			return
		}
		if _, ok := s.require(w, r, true); !ok {
			return
		}
		if !emptyBody(w, r) {
			return
		}
		if err := s.Publications.DeleteDomain(r.Context(), 1, siteID, domainID); err != nil {
			domainAPIError(w, err)
			return
		}
		w.WriteHeader(204)
	default:
		jsonError(w, 404, "domain_not_found")
	}
}

func domainAPIError(w http.ResponseWriter, err error) {
	var e *publication.Error
	if !errors.As(err, &e) {
		jsonError(w, 503, "service_unavailable")
		return
	}
	code := 503
	switch e.Code {
	case "invalid_domain", "invalid_request":
		code = 400
	case "site_not_found", "domain_not_found":
		code = 404
	case "domain_conflict", "reserved_domain", "domains_disabled":
		code = 409
	}
	jsonError(w, code, e.Code)
}

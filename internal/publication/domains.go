package publication

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"

	"static-host/internal/domainname"
)

type DNSRecord struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Domain struct {
	ID             string      `json:"id"`
	SiteID         string      `json:"site_id"`
	Hostname       string      `json:"hostname"`
	URL            string      `json:"url"`
	State          string      `json:"state"`
	ClaimedAt      *int64      `json:"claimed_at"`
	DNSCheckedAt   *int64      `json:"dns_checked_at"`
	HTTPSCheckedAt *int64      `json:"https_checked_at"`
	NextCheckAt    int64       `json:"next_check_at"`
	ErrorCode      *string     `json:"error_code"`
	CreatedAt      int64       `json:"created_at"`
	UpdatedAt      int64       `json:"updated_at"`
	DNSRecords     []DNSRecord `json:"dns_records"`
}

type domainScanner interface {
	Scan(dest ...any) error
}

type domainRecord struct {
	Domain
	probeToken    string
	revision      int64
	failureCount  int
	mismatchCount int
	siteSlug      string
}

const domainSelect = `SELECT d.id,d.site_id,d.hostname,d.state,d.claimed_at,d.dns_checked_at,d.https_checked_at,d.next_check_at,d.error_code,d.created_at,d.updated_at,d.probe_token,d.revision,d.failure_count,d.mismatch_count,s.slug FROM domains d JOIN sites s ON s.id=d.site_id`

func scanDomain(row domainScanner) (domainRecord, error) {
	var record domainRecord
	var claimedAt, dnsCheckedAt, httpsCheckedAt sql.NullInt64
	var errorCode sql.NullString
	err := row.Scan(
		&record.ID, &record.SiteID, &record.Hostname, &record.State,
		&claimedAt, &dnsCheckedAt, &httpsCheckedAt, &record.NextCheckAt,
		&errorCode, &record.CreatedAt, &record.UpdatedAt, &record.probeToken,
		&record.revision, &record.failureCount, &record.mismatchCount, &record.siteSlug,
	)
	if err != nil {
		return domainRecord{}, err
	}
	if claimedAt.Valid {
		v := claimedAt.Int64
		record.ClaimedAt = &v
	}
	if dnsCheckedAt.Valid {
		v := dnsCheckedAt.Int64
		record.DNSCheckedAt = &v
	}
	if httpsCheckedAt.Valid {
		v := httpsCheckedAt.Int64
		record.HTTPSCheckedAt = &v
	}
	if errorCode.Valid {
		v := errorCode.String
		record.ErrorCode = &v
	}
	return record, nil
}

func (s *Service) configureDomainOptions() error {
	if len(s.options.DomainIngressIPs) > 0 {
		s.options.DomainIngressIPs = append([]netip.Addr(nil), s.options.DomainIngressIPs...)
		for i, addr := range s.options.DomainIngressIPs {
			if !addr.IsValid() {
				return errors.New("invalid domain ingress IP")
			}
			s.options.DomainIngressIPs[i] = addr.Unmap()
		}
	}
	base := strings.TrimSpace(s.options.BaseDomain)
	if base == "" {
		base = siteSuffixHost(s.options.SiteSuffix)
	}
	if base != "" {
		normalized, err := domainname.Normalize(base)
		if err != nil {
			if !s.options.Local || !strings.EqualFold(base, "localhost") {
				return errors.New("invalid base domain")
			}
			normalized = "localhost"
		}
		s.options.BaseDomain = normalized
	}
	reserved := make([]string, 0, len(s.options.DomainReservedHosts))
	for _, hostname := range s.options.DomainReservedHosts {
		normalized, err := domainname.Normalize(hostname)
		if err != nil {
			return errors.New("invalid reserved domain")
		}
		reserved = append(reserved, normalized)
	}
	s.options.DomainReservedHosts = reserved
	return nil
}

func siteSuffixHost(suffix string) string {
	if host, _, err := net.SplitHostPort(suffix); err == nil {
		return strings.TrimSuffix(host, ".")
	}
	return strings.TrimSuffix(strings.Trim(suffix, "[]"), ".")
}

func (s *Service) domainsEnabled() bool { return len(s.options.DomainIngressIPs) != 0 }

func (s *Service) reservedDomain(hostname string) bool {
	if base := s.options.BaseDomain; base != "" && isReservedUnder(hostname, base) {
		return true
	}
	for _, base := range s.options.DomainReservedHosts {
		if isReservedUnder(hostname, base) {
			return true
		}
	}
	return false
}
func isReservedUnder(hostname, base string) bool {
	return hostname == base || strings.HasSuffix(hostname, "."+base)
}

func (s *Service) domainRecords(hostname, slug string) []DNSRecord {
	systemHost := strings.ToLower(strings.TrimSuffix(slug+"."+siteSuffixHost(s.options.SiteSuffix), "."))
	records := make([]DNSRecord, 0, len(s.options.DomainIngressIPs)+1)
	records = append(records, DNSRecord{Type: "CNAME", Name: hostname, Value: systemHost})
	ips := make([]netip.Addr, len(s.options.DomainIngressIPs))
	copy(ips, s.options.DomainIngressIPs)
	sort.Slice(ips, func(i, j int) bool { return ips[i].Less(ips[j]) })
	for _, ip := range ips {
		typeName := "AAAA"
		if ip.Is4() {
			typeName = "A"
		}
		records = append(records, DNSRecord{Type: typeName, Name: hostname, Value: ip.String()})
	}
	return records
}

func (s *Service) finishDomain(record domainRecord) Domain {
	domain := record.Domain
	domain.URL = "https://" + domain.Hostname
	domain.DNSRecords = s.domainRecords(domain.Hostname, record.siteSlug)
	return domain
}

func (s *Service) AddDomain(ctx context.Context, ownerID int64, siteID, hostname string) (Domain, error) {
	if !s.domainsEnabled() {
		return Domain{}, failure("domains_disabled")
	}
	// Serialize same-process retries so an exact duplicate returns its original
	// record rather than surfacing the unique (site_id, hostname) constraint.
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Domain{}, err
	}
	defer tx.Rollback()
	var slug string
	err = tx.QueryRowContext(ctx, "SELECT slug FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL", siteID, ownerID).Scan(&slug)
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, failure("site_not_found")
	}
	if err != nil {
		return Domain{}, err
	}
	normalized, err := domainname.Normalize(hostname)
	if err != nil {
		return Domain{}, failure("invalid_domain")
	}
	if s.reservedDomain(normalized) {
		return Domain{}, failure("reserved_domain")
	}

	record, err := scanDomain(tx.QueryRowContext(ctx, domainSelect+" WHERE d.site_id=? AND d.hostname=?", siteID, normalized))
	if err == nil {
		return s.finishDomain(record), nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Domain{}, err
	}
	var claimed int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM domains d JOIN sites s ON s.id=d.site_id WHERE d.hostname=? AND d.claimed_at IS NOT NULL AND s.deleted_at IS NULL`, normalized).Scan(&claimed)
	if err == nil {
		return Domain{}, failure("domain_conflict")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Domain{}, err
	}
	id, err := randomID()
	if err != nil {
		return Domain{}, err
	}
	probeToken, err := randomID()
	if err != nil {
		return Domain{}, err
	}
	now := s.now().Unix()
	_, err = tx.ExecContext(ctx, `INSERT INTO domains(id,site_id,hostname,state,next_check_at,probe_token,created_at,updated_at) VALUES(?,?,?,'pending_dns',?,?,?,?)`, id, siteID, normalized, now, probeToken, now, now)
	if err != nil {
		if isDomainHostnameConflict(err) {
			existing, lookupErr := scanDomain(tx.QueryRowContext(ctx, domainSelect+" WHERE d.site_id=? AND d.hostname=?", siteID, normalized))
			if lookupErr == nil {
				return s.finishDomain(existing), nil
			}
			if !errors.Is(lookupErr, sql.ErrNoRows) {
				return Domain{}, lookupErr
			}
			return Domain{}, failure("domain_conflict")
		}
		return Domain{}, err
	}
	if err := tx.Commit(); err != nil {
		return Domain{}, err
	}
	return s.loadDomainByID(ctx, id)
}

func (s *Service) ListDomains(ctx context.Context, ownerID int64, siteID string) ([]Domain, error) {
	if !s.domainsEnabled() {
		return nil, failure("domains_disabled")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT 1 FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL", siteID, ownerID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return nil, failure("site_not_found")
	} else if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, domainSelect+" WHERE d.site_id=? AND s.deleted_at IS NULL ORDER BY d.created_at,d.id", siteID)
	if err != nil {
		return nil, err
	}
	domains := make([]Domain, 0)
	for rows.Next() {
		record, scanErr := scanDomain(rows)
		if scanErr != nil {
			err = scanErr
			break
		}
		domains = append(domains, s.finishDomain(record))
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	return domains, nil
}

func (s *Service) CheckDomain(ctx context.Context, ownerID int64, siteID, domainID string) (Domain, error) {
	if !s.domainsEnabled() {
		return Domain{}, failure("domains_disabled")
	}
	var exists int
	if err := s.db.QueryRowContext(ctx, "SELECT 1 FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL", siteID, ownerID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return Domain{}, failure("site_not_found")
	} else if err != nil {
		return Domain{}, err
	}
	now := s.now()
	s.manualChecksMu.Lock()
	last, requested := s.manualChecks[domainID]
	if requested && now.Sub(last) < 30*time.Second {
		s.manualChecksMu.Unlock()
		return s.loadOwnedDomain(ctx, ownerID, siteID, domainID)
	}
	s.manualChecks[domainID] = now
	for id, at := range s.manualChecks {
		if now.Sub(at) >= time.Minute {
			delete(s.manualChecks, id)
		}
	}
	s.manualChecksMu.Unlock()
	result, err := s.db.ExecContext(ctx, `UPDATE domains SET next_check_at=?,revision=revision+1,updated_at=? WHERE id=? AND site_id=? AND EXISTS(SELECT 1 FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL)`, now.Unix(), now.Unix(), domainID, siteID, siteID, ownerID)
	if err != nil {
		return Domain{}, err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return Domain{}, err
	} else if changed == 0 {
		return s.loadOwnedDomain(ctx, ownerID, siteID, domainID)
	}
	s.wakeDomainWorker()
	return s.loadOwnedDomain(ctx, ownerID, siteID, domainID)
}

func (s *Service) DeleteDomain(ctx context.Context, ownerID int64, siteID, domainID string) error {
	if !s.domainsEnabled() {
		return failure("domains_disabled")
	}
	lock := s.siteLock(siteID)
	lock.Lock()
	defer lock.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL", siteID, ownerID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return failure("site_not_found")
	} else if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM domains WHERE id=? AND site_id=?", domainID, siteID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil {
		return err
	} else if changed == 0 {
		return failure("domain_not_found")
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.manualChecksMu.Lock()
	delete(s.manualChecks, domainID)
	s.manualChecksMu.Unlock()
	return nil
}

func (s *Service) DomainAskHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !s.domainsEnabled() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		query := r.URL.Query()
		values, ok := query["domain"]
		if r.URL.Path != "/ask" || len(query) != 1 || !ok || len(values) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		hostname, err := domainname.Normalize(values[0])
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var exists int
		err = s.db.QueryRowContext(r.Context(), `SELECT 1 FROM domains d JOIN sites s ON s.id=d.site_id WHERE d.hostname=? AND d.claimed_at IS NOT NULL AND d.state IN ('provisioning_tls','active') AND s.deleted_at IS NULL`, hostname).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if err != nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func (s *Service) loadDomainByID(ctx context.Context, domainID string) (Domain, error) {
	record, err := scanDomain(s.db.QueryRowContext(ctx, domainSelect+" WHERE d.id=? AND s.deleted_at IS NULL", domainID))
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, failure("domain_not_found")
	}
	if err != nil {
		return Domain{}, err
	}
	return s.finishDomain(record), nil
}

func (s *Service) loadOwnedDomain(ctx context.Context, ownerID int64, siteID, domainID string) (Domain, error) {
	record, err := scanDomain(s.db.QueryRowContext(ctx, domainSelect+" WHERE d.id=? AND d.site_id=? AND s.owner_id=? AND s.deleted_at IS NULL", domainID, siteID, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return Domain{}, failure("domain_not_found")
	}
	if err != nil {
		return Domain{}, err
	}
	return s.finishDomain(record), nil
}

func isDomainHostnameConflict(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "domains.hostname") || strings.Contains(message, "domains_hostname")
}

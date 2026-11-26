package publication

import (
	"bufio"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"static-host/internal/domainname"
)

type domainResolver interface {
	LookupCNAME(context.Context, string) (string, error)
	LookupNetIP(context.Context, string) ([]netip.Addr, error)
}

type systemDomainResolver struct{}

var (
	dnsNameserversOnce sync.Once
	dnsNameservers     []string
	dnsQueryID         atomic.Uint32
)

func (systemDomainResolver) LookupCNAME(ctx context.Context, hostname string) (string, error) {
	servers := systemDNSNameservers()
	if len(servers) == 0 {
		return "", &net.DNSError{Err: "no DNS nameserver configured", Name: hostname, IsTemporary: true}
	}
	var lastErr error
	for _, server := range servers {
		response, err := queryDNS(ctx, server, hostname, dnsmessage.TypeCNAME)
		if err != nil {
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				return "", err
			}
			lastErr = err
			continue
		}
		for _, answer := range response.Answers {
			if answer.Header.Type != dnsmessage.TypeCNAME || !strings.EqualFold(strings.TrimSuffix(answer.Header.Name.String(), "."), hostname) {
				continue
			}
			record, ok := answer.Body.(*dnsmessage.CNAMEResource)
			if !ok {
				return "", &net.DNSError{Err: "invalid CNAME response", Name: hostname, IsTemporary: true}
			}
			return record.CNAME.String(), nil
		}
		return "", nil
	}
	return "", lastErr
}

func (systemDomainResolver) LookupNetIP(ctx context.Context, hostname string) ([]netip.Addr, error) {
	servers := systemDNSNameservers()
	if len(servers) == 0 {
		return nil, &net.DNSError{Err: "no DNS nameserver configured", Name: hostname, IsTemporary: true}
	}
	var lastErr error
	for _, server := range servers {
		addresses, err := lookupDNSAddresses(ctx, server, hostname)
		if err == nil {
			return addresses, nil
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return nil, err
		}
		lastErr = err
	}
	return nil, lastErr
}

func lookupDNSAddresses(ctx context.Context, server, hostname string) ([]netip.Addr, error) {
	addresses := make([]netip.Addr, 0, 4)
	for _, rrType := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		response, err := queryDNS(ctx, server, hostname, rrType)
		if err != nil {
			return nil, err
		}
		for _, answer := range response.Answers {
			if !strings.EqualFold(strings.TrimSuffix(answer.Header.Name.String(), "."), hostname) {
				continue
			}
			if answer.Header.Type == dnsmessage.TypeCNAME {
				return nil, &net.DNSError{Err: "CNAME changed during lookup", Name: hostname, IsNotFound: true}
			}
			switch rrType {
			case dnsmessage.TypeA:
				if answer.Header.Type != dnsmessage.TypeA {
					continue
				}
				record, ok := answer.Body.(*dnsmessage.AResource)
				if !ok {
					return nil, &net.DNSError{Err: "invalid A response", Name: hostname, IsTemporary: true}
				}
				addresses = append(addresses, netip.AddrFrom4(record.A))
			case dnsmessage.TypeAAAA:
				if answer.Header.Type != dnsmessage.TypeAAAA {
					continue
				}
				record, ok := answer.Body.(*dnsmessage.AAAAResource)
				if !ok {
					return nil, &net.DNSError{Err: "invalid AAAA response", Name: hostname, IsTemporary: true}
				}
				addresses = append(addresses, netip.AddrFrom16(record.AAAA))
			}
		}
	}
	return addresses, nil
}

func systemDNSNameservers() []string {
	dnsNameserversOnce.Do(func() {
		contents, err := os.ReadFile("/etc/resolv.conf")
		if err != nil {
			return
		}
		seen := make(map[string]struct{})
		for _, line := range strings.Split(string(contents), "\n") {
			fields := strings.Fields(strings.SplitN(line, "#", 2)[0])
			if len(fields) < 2 || fields[0] != "nameserver" {
				continue
			}
			addr, err := netip.ParseAddr(strings.Trim(fields[1], "[]"))
			if err != nil {
				continue
			}
			server := net.JoinHostPort(addr.String(), "53")
			if _, ok := seen[server]; ok {
				continue
			}
			seen[server] = struct{}{}
			dnsNameservers = append(dnsNameservers, server)
		}
	})
	return dnsNameservers
}

func queryDNS(ctx context.Context, server, hostname string, rrType dnsmessage.Type) (dnsmessage.Message, error) {
	name, err := dnsmessage.NewName(strings.TrimSuffix(hostname, ".") + ".")
	if err != nil {
		return dnsmessage.Message{}, &net.DNSError{Err: "invalid DNS name", Name: hostname, IsNotFound: true}
	}
	id := uint16(dnsQueryID.Add(1))
	query := dnsmessage.Message{
		Header: dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{
			Name: name, Type: rrType, Class: dnsmessage.ClassINET,
		}},
	}
	packet, err := query.Pack()
	if err != nil {
		return dnsmessage.Message{}, &net.DNSError{Err: "DNS query construction failed", Name: hostname, IsTemporary: true}
	}
	response, err := exchangeDNS(ctx, server, packet)
	if err != nil {
		return dnsmessage.Message{}, &net.DNSError{Err: "DNS lookup failed", Name: hostname, IsTemporary: true}
	}
	if response.Header.ID != id || !response.Header.Response || response.Header.Truncated || len(response.Questions) != 1 {
		return dnsmessage.Message{}, &net.DNSError{Err: "invalid DNS response", Name: hostname, IsTemporary: true}
	}
	question := response.Questions[0]
	if question.Type != rrType || question.Class != dnsmessage.ClassINET || !strings.EqualFold(strings.TrimSuffix(question.Name.String(), "."), hostname) {
		return dnsmessage.Message{}, &net.DNSError{Err: "mismatched DNS response", Name: hostname, IsTemporary: true}
	}
	if response.Header.RCode == dnsmessage.RCodeNameError {
		return dnsmessage.Message{}, &net.DNSError{Err: "no such host", Name: hostname, IsNotFound: true}
	}
	if response.Header.RCode != dnsmessage.RCodeSuccess {
		return dnsmessage.Message{}, &net.DNSError{Err: "DNS server failure", Name: hostname, IsTemporary: true}
	}
	return response, nil
}

func exchangeDNS(ctx context.Context, server string, packet []byte) (dnsmessage.Message, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp", server)
	if err != nil {
		return dnsmessage.Message{}, err
	}
	defer conn.Close()
	setDNSDeadline(ctx, conn)
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	if _, err := conn.Write(packet); err != nil {
		return dnsmessage.Message{}, err
	}
	var buffer [4096]byte
	n, err := conn.Read(buffer[:])
	if err != nil {
		return dnsmessage.Message{}, err
	}
	if n >= 4 && buffer[2]&0x02 != 0 {
		_ = conn.Close()
		return exchangeDNSTCP(ctx, server, packet)
	}
	var response dnsmessage.Message
	if err := response.Unpack(buffer[:n]); err != nil {
		return dnsmessage.Message{}, err
	}
	return response, nil
}

func exchangeDNSTCP(ctx context.Context, server string, packet []byte) (dnsmessage.Message, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", server)
	if err != nil {
		return dnsmessage.Message{}, err
	}
	defer conn.Close()
	setDNSDeadline(ctx, conn)
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	var prefix [2]byte
	binary.BigEndian.PutUint16(prefix[:], uint16(len(packet)))
	if err := writeDNS(conn, prefix[:]); err != nil {
		return dnsmessage.Message{}, err
	}
	if err := writeDNS(conn, packet); err != nil {
		return dnsmessage.Message{}, err
	}
	if _, err := io.ReadFull(conn, prefix[:]); err != nil {
		return dnsmessage.Message{}, err
	}
	responsePacket := make([]byte, int(binary.BigEndian.Uint16(prefix[:])))
	if _, err := io.ReadFull(conn, responsePacket); err != nil {
		return dnsmessage.Message{}, err
	}
	var response dnsmessage.Message
	if err := response.Unpack(responsePacket); err != nil {
		return dnsmessage.Message{}, err
	}
	return response, nil
}

func setDNSDeadline(ctx context.Context, conn net.Conn) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
}

func writeDNS(w io.Writer, packet []byte) error {
	for len(packet) > 0 {
		n, err := w.Write(packet)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		packet = packet[n:]
	}
	return nil
}

func (s *Service) domainWorker() {
	defer close(s.domainDone)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		s.processDueDomains()
		select {
		case <-s.stop:
			return
		case <-s.domainWake:
		case <-ticker.C:
		}
	}
}

func (s *Service) processDueDomains() {
	if !s.domainsEnabled() {
		return
	}
	rows, err := s.db.Query(`SELECT d.id FROM domains d JOIN sites s ON s.id=d.site_id WHERE s.deleted_at IS NULL AND d.next_check_at<=? ORDER BY d.next_check_at,d.id`, s.now().Unix())
	if err != nil {
		return
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	for _, id := range ids {
		select {
		case <-s.stop:
			return
		default:
		}
		_ = s.checkDomain(context.Background(), id)
	}
}

func (s *Service) wakeDomainWorker() {
	select {
	case s.domainWake <- struct{}{}:
	default:
	}
}

// checkDomain performs one immediate check. It is kept unexported so tests in
// this package can drive DNS/TLS transitions without waiting for the worker.
func (s *Service) checkDomain(ctx context.Context, domainID string) error {
	record, err := scanDomain(s.db.QueryRowContext(ctx, domainSelect+" WHERE d.id=? AND s.deleted_at IS NULL", domainID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.checkDomainRecord(ctx, record)
}

func (s *Service) checkDomainRecord(ctx context.Context, record domainRecord) error {
	dnsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	addresses, dnsErrCode := s.checkDomainDNS(dnsCtx, record.Hostname, s.siteHostname(record.siteSlug))
	cancel()
	now := s.now().Unix()
	if dnsErrCode == "dns_lookup_failed" {
		return s.applyDNSLookupFailure(ctx, record, now)
	}
	if dnsErrCode == "dns_mismatch" {
		return s.applyDNSMismatch(ctx, record, now)
	}
	failureCount := record.failureCount
	if record.ErrorCode != nil && *record.ErrorCode == "dns_lookup_failed" {
		failureCount = 0
	}

	// A pending request becomes exclusive only after DNS is valid. The partial
	// unique index and this conditional update make concurrent claims atomic.
	if record.ClaimedAt == nil {
		result, err := s.db.ExecContext(ctx, `UPDATE domains SET claimed_at=?,dns_checked_at=?,state='provisioning_tls',next_check_at=?,error_code=NULL,failure_count=0,mismatch_count=0,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND claimed_at IS NULL AND EXISTS(SELECT 1 FROM sites WHERE id=domains.site_id AND deleted_at IS NULL)`, now, now, now, now, record.ID, record.revision)
		if err != nil {
			if isDomainHostnameConflict(err) {
				return s.applyClaimConflict(ctx, record, now)
			}
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed == 0 {
			return err
		}
		record.ClaimedAt = &now
		record.revision++
		record.State = "provisioning_tls"
		record.failureCount = 0
		record.mismatchCount = 0
	} else {
		state := record.State
		if state == "suspended" {
			state = "provisioning_tls"
		}
		result, err := s.db.ExecContext(ctx, `UPDATE domains SET dns_checked_at=?,state=?,next_check_at=?,error_code=NULL,failure_count=?,mismatch_count=0,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND claimed_at IS NOT NULL AND EXISTS(SELECT 1 FROM sites WHERE id=domains.site_id AND deleted_at IS NULL)`, now, state, now, failureCount, now, record.ID, record.revision)
		if err != nil {
			return err
		}
		changed, err := result.RowsAffected()
		if err != nil || changed == 0 {
			return err
		}
		record.revision++
		record.State = state
		record.failureCount = failureCount
		record.mismatchCount = 0
	}

	probeErrCode := s.probeDomainHTTPS(ctx, record.Hostname, record.probeToken, addresses)
	if probeErrCode != "" {
		return s.applyHTTPSFailure(ctx, record, probeErrCode, s.now().Unix())
	}
	checkedAt := s.now().Unix()
	_, err := s.db.ExecContext(ctx, `UPDATE domains SET state='active',https_checked_at=?,next_check_at=?,error_code=NULL,failure_count=0,mismatch_count=0,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND claimed_at IS NOT NULL AND EXISTS(SELECT 1 FROM sites WHERE id=domains.site_id AND deleted_at IS NULL)`, checkedAt, checkedAt+int64((5*time.Minute)/time.Second), checkedAt, record.ID, record.revision)
	return err
}

func (s *Service) checkDomainDNS(ctx context.Context, hostname, systemHostname string) ([]netip.Addr, string) {
	current := hostname
	seen := map[string]struct{}{current: {}}
	includesSystem := current == systemHostname
	hasCNAME := false
	for transitions := 0; transitions <= 10; transitions++ {
		target, err := s.domainResolver.LookupCNAME(ctx, current)
		if err != nil {
			return nil, classifyDNSError(err)
		}
		if target == "" || strings.EqualFold(strings.TrimSuffix(target, "."), current) {
			break
		}
		target, err = domainname.Normalize(target)
		if err != nil {
			return nil, "dns_mismatch"
		}
		if _, exists := seen[target]; exists {
			return nil, "dns_mismatch"
		}
		if transitions == 10 {
			return nil, "dns_mismatch"
		}
		seen[target] = struct{}{}
		current = target
		hasCNAME = true
		if current == systemHostname {
			includesSystem = true
		}
	}
	if hasCNAME && !includesSystem {
		return nil, "dns_mismatch"
	}
	addresses, err := s.domainResolver.LookupNetIP(ctx, current)
	if err != nil {
		return nil, classifyDNSError(err)
	}
	if len(addresses) == 0 {
		return nil, "dns_mismatch"
	}
	allowed := make(map[netip.Addr]struct{}, len(s.options.DomainIngressIPs))
	for _, ip := range s.options.DomainIngressIPs {
		allowed[ip.Unmap()] = struct{}{}
	}
	for i, address := range addresses {
		if !address.IsValid() {
			return nil, "dns_mismatch"
		}
		addresses[i] = address.Unmap()
		if _, ok := allowed[addresses[i]]; !ok {
			return nil, "dns_mismatch"
		}
	}
	return addresses, ""
}

func classifyDNSError(err error) string {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound || (!dnsErr.IsTemporary && !dnsErr.IsTimeout) {
			return "dns_mismatch"
		}
		return "dns_lookup_failed"
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "dns_lookup_failed"
	}
	return "dns_lookup_failed"
}

func (s *Service) applyDNSLookupFailure(ctx context.Context, record domainRecord, now int64) error {
	failures := record.failureCount + 1
	_, err := s.db.ExecContext(ctx, `UPDATE domains SET dns_checked_at=?,error_code='dns_lookup_failed',failure_count=?,mismatch_count=0,next_check_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND EXISTS(SELECT 1 FROM sites WHERE id=domains.site_id AND deleted_at IS NULL)`, now, failures, now+int64(domainRetryDelay(failures)/time.Second), now, record.ID, record.revision)
	return err
}

func (s *Service) applyDNSMismatch(ctx context.Context, record domainRecord, now int64) error {
	mismatches := record.mismatchCount
	state := record.State
	delay := time.Minute
	if record.ClaimedAt != nil {
		mismatches++
		if mismatches >= 2 {
			state = "suspended"
			delay = 5 * time.Minute
		}
	}
	_, err := s.db.ExecContext(ctx, `UPDATE domains SET dns_checked_at=?,state=?,error_code='dns_mismatch',mismatch_count=?,failure_count=0,next_check_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND EXISTS(SELECT 1 FROM sites WHERE id=domains.site_id AND deleted_at IS NULL)`, now, state, mismatches, now+int64(delay/time.Second), now, record.ID, record.revision)
	return err
}

func (s *Service) applyClaimConflict(ctx context.Context, record domainRecord, now int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE domains SET error_code='domain_conflict',next_check_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND claimed_at IS NULL AND EXISTS(SELECT 1 FROM sites WHERE id=domains.site_id AND deleted_at IS NULL)`, now+60, now, record.ID, record.revision)
	return err
}

func (s *Service) applyHTTPSFailure(ctx context.Context, record domainRecord, errorCode string, now int64) error {
	failures := record.failureCount + 1
	state := "provisioning_tls"
	if record.State == "active" {
		state = "active"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE domains SET state=?,https_checked_at=?,error_code=?,failure_count=?,next_check_at=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND claimed_at IS NOT NULL AND EXISTS(SELECT 1 FROM sites WHERE id=domains.site_id AND deleted_at IS NULL)`, state, now, errorCode, failures, now+int64(domainRetryDelay(failures)/time.Second), now, record.ID, record.revision)
	return err
}

func domainRetryDelay(failures int) time.Duration {
	switch failures {
	case 1:
		return 30 * time.Second
	case 2:
		return time.Minute
	case 3:
		return 2 * time.Minute
	case 4:
		return 4 * time.Minute
	case 5:
		return 8 * time.Minute
	default:
		return 15 * time.Minute
	}
}

func (s *Service) siteHostname(slug string) string {
	return strings.ToLower(strings.TrimSuffix(slug+"."+siteSuffixHost(s.options.SiteSuffix), "."))
}

func (s *Service) probeDomainHTTPS(parent context.Context, hostname, token string, addresses []netip.Addr) string {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	errorCode := ""
	for _, address := range addresses {
		if err := s.probeDomainAddress(ctx, hostname, token, address); errorCode == "" && err != "" {
			errorCode = err
		}
	}
	return errorCode
}

func (s *Service) probeDomainAddress(ctx context.Context, hostname, token string, address netip.Addr) string {
	port := s.domainProbePort
	if port == 0 {
		port = 443
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(address.String(), strconv.Itoa(port)))
	if err != nil {
		return "tls_unavailable"
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	tlsConn := tls.Client(conn, &tls.Config{ServerName: hostname, RootCAs: s.domainRootCAs, MinVersion: tls.VersionTLS12})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return "tls_unavailable"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+hostname+"/.well-known/static-host-domain/"+token, nil)
	if err != nil {
		return "https_probe_failed"
	}
	request.Host = hostname
	request.Close = true
	if err := request.Write(tlsConn); err != nil {
		return "https_probe_failed"
	}
	response, err := http.ReadResponse(bufio.NewReader(tlsConn), request)
	if err != nil {
		return "https_probe_failed"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "https_probe_failed"
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "text/plain" {
		return "https_probe_failed"
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(len(token)+1)))
	if err != nil || string(body) != token {
		return "https_probe_failed"
	}
	return ""
}

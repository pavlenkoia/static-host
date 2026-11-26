package publication

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"static-host/internal/store"
)

type mapDomainResolver struct {
	cnames      map[string]string
	ips         map[string][]netip.Addr
	cnameErrors map[string]error
	ipErrors    map[string]error
}

func (r *mapDomainResolver) LookupCNAME(_ context.Context, hostname string) (string, error) {
	if err := r.cnameErrors[hostname]; err != nil {
		return "", err
	}
	return r.cnames[hostname], nil
}

func (r *mapDomainResolver) LookupNetIP(_ context.Context, hostname string) ([]netip.Addr, error) {
	if err := r.ipErrors[hostname]; err != nil {
		return nil, err
	}
	return append([]netip.Addr(nil), r.ips[hostname]...), nil
}

func newDomainTestService(t *testing.T) (*Service, Site, *mapDomainResolver, *time.Time) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := New(db, root, Options{SiteSuffix: "system.test:8081", BaseDomain: "system.test", Local: true, DomainReservedHosts: []string{"service.test"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	now := time.Now().Truncate(time.Second)
	s.now = func() time.Time { return now }
	resolver := &mapDomainResolver{
		cnames:      make(map[string]string),
		ips:         make(map[string][]netip.Addr),
		cnameErrors: make(map[string]error),
		ipErrors:    make(map[string]error),
	}
	s.domainResolver = resolver
	s.options.DomainIngressIPs = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	site, err := s.Create(context.Background(), 1, "Domain test")
	if err != nil {
		t.Fatal(err)
	}
	return s, site, resolver, &now
}

func errorCode(err error) string {
	var domainErr *Error
	if errors.As(err, &domainErr) {
		return domainErr.Code
	}
	return ""
}

func domainByID(t *testing.T, s *Service, ownerID int64, siteID, domainID string) Domain {
	t.Helper()
	got, err := s.loadOwnedDomain(context.Background(), ownerID, siteID, domainID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDomainAddNormalizationOwnershipAndReservedNames(t *testing.T) {
	s, firstSite, _, _ := newDomainTestService(t)
	ctx := context.Background()
	first, err := s.AddDomain(ctx, 1, firstSite.ID, "  ПРИМЕР.РФ. ")
	if err != nil {
		t.Fatal(err)
	}
	if first.Hostname != "xn--e1afmkfd.xn--p1ai" || first.URL != "https://xn--e1afmkfd.xn--p1ai" || first.State != "pending_dns" {
		t.Fatalf("unexpected canonical domain: %+v", first)
	}
	systemHost := strings.TrimPrefix(firstSite.URL, "http://")
	systemHost, _, _ = net.SplitHostPort(systemHost)
	if len(first.DNSRecords) != 2 || first.DNSRecords[0] != (DNSRecord{Type: "CNAME", Name: first.Hostname, Value: systemHost}) || first.DNSRecords[1] != (DNSRecord{Type: "A", Name: first.Hostname, Value: "127.0.0.1"}) {
		t.Fatalf("unexpected DNS instructions: %+v", first.DNSRecords)
	}
	duplicate, err := s.AddDomain(ctx, 1, firstSite.ID, "XN--E1AFMKFD.XN--P1AI")
	if err != nil || duplicate.ID != first.ID {
		t.Fatalf("IDNA duplicate: %+v, %v", duplicate, err)
	}

	secondSite, err := s.Create(ctx, 1, "Second")
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.AddDomain(ctx, 1, secondSite.ID, first.Hostname)
	if err != nil || pending.ID == first.ID || pending.State != "pending_dns" {
		t.Fatalf("pending claims should not block each other: %+v, %v", pending, err)
	}
	listed, err := s.ListDomains(ctx, 1, firstSite.ID)
	if err != nil || len(listed) != 1 || listed[0].ID != first.ID {
		t.Fatalf("owner list: %+v, %v", listed, err)
	}
	if _, err := s.ListDomains(ctx, 2, firstSite.ID); errorCode(err) != "site_not_found" {
		t.Fatalf("foreign site list error = %v", err)
	}
	if _, err := s.CheckDomain(ctx, 2, firstSite.ID, first.ID); errorCode(err) != "site_not_found" {
		t.Fatalf("foreign check error = %v", err)
	}
	if _, err := s.CheckDomain(ctx, 1, firstSite.ID, pending.ID); errorCode(err) != "domain_not_found" {
		t.Fatalf("foreign domain check error = %v", err)
	}
	if err := s.DeleteDomain(ctx, 2, firstSite.ID, first.ID); errorCode(err) != "site_not_found" {
		t.Fatalf("foreign delete error = %v", err)
	}
	if _, err := s.AddDomain(ctx, 2, firstSite.ID, "invalid"); errorCode(err) != "site_not_found" {
		t.Fatalf("invalid domain on foreign site error = %v", err)
	}

	for _, invalid := range []string{"https://example.net", "127.0.0.1", "*.example.net", "singlelabel", "a..example.net"} {
		if _, err := s.AddDomain(ctx, 1, firstSite.ID, invalid); errorCode(err) != "invalid_domain" {
			t.Errorf("AddDomain(%q) error = %v, want invalid_domain", invalid, err)
		}
	}
	for _, reserved := range []string{"foo.system.test", "nested.service.test"} {
		if _, err := s.AddDomain(ctx, 1, firstSite.ID, reserved); errorCode(err) != "reserved_domain" {
			t.Errorf("AddDomain(%q) error = %v, want reserved_domain", reserved, err)
		}
	}
}

func TestDomainDisabledAndManualCheckCooldown(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := New(db, root, Options{SiteSuffix: "system.test:8081", BaseDomain: "system.test", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	site, err := s.Create(ctx, 1, "Domain test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddDomain(ctx, 1, site.ID, "example.invalid"); errorCode(err) != "domains_disabled" {
		t.Fatalf("disabled AddDomain error = %v", err)
	}

	s.options.DomainIngressIPs = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	s.now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	domain, err := s.AddDomain(ctx, 1, site.ID, "example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.CheckDomain(ctx, 1, site.ID, domain.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CheckDomain(ctx, 1, site.ID, domain.ID)
	if err != nil || second.NextCheckAt != first.NextCheckAt {
		t.Fatalf("cooldown did not preserve queued check: %+v, %v", second, err)
	}
	s.now = func() time.Time { return time.Unix(1_800_000_030, 0) }
	third, err := s.CheckDomain(ctx, 1, site.ID, domain.ID)
	if err != nil || third.NextCheckAt != 1_800_000_030 {
		t.Fatalf("check after cooldown: %+v, %v", third, err)
	}
}

func TestDomainDNSValidation(t *testing.T) {
	s, site, resolver, _ := newDomainTestService(t)
	s.options.DomainIngressIPs = append(s.options.DomainIngressIPs, netip.MustParseAddr("::1"))
	hostname := "custom.invalid"
	systemHost := strings.TrimPrefix(site.URL, "http://")
	systemHost, _, _ = net.SplitHostPort(systemHost)
	allowed := netip.MustParseAddr("127.0.0.1")
	foreign := netip.MustParseAddr("192.0.2.4")

	tests := []struct {
		name      string
		setup     func()
		wantCode  string
		wantAddrs int
	}{
		{
			name: "CNAME chain includes system site",
			setup: func() {
				resolver.cnames[hostname] = systemHost
				resolver.ips[systemHost] = []netip.Addr{allowed}
			},
			wantAddrs: 1,
		},
		{
			name: "foreign CNAME cannot pass by matching IP",
			setup: func() {
				resolver.cnames[hostname] = "other.invalid"
				resolver.ips["other.invalid"] = []netip.Addr{allowed}
			},
			wantCode: "dns_mismatch",
		},
		{
			name: "CNAME cycle",
			setup: func() {
				resolver.cnames[hostname] = "alias.invalid"
				resolver.cnames["alias.invalid"] = hostname
			},
			wantCode: "dns_mismatch",
		},
		{
			name: "chain over limit",
			setup: func() {
				for i := range 12 {
					from := hostname
					if i > 0 {
						from = "chain" + strconv.Itoa(i) + ".invalid"
					}
					to := "chain" + strconv.Itoa(i+1) + ".invalid"
					resolver.cnames[from] = to
				}
			},
			wantCode: "dns_mismatch",
		},
		{
			name:     "mixed allowed and foreign addresses",
			setup:    func() { resolver.ips[hostname] = []netip.Addr{allowed, foreign} },
			wantCode: "dns_mismatch",
		},
		{
			name: "NXDOMAIN is content mismatch",
			setup: func() {
				resolver.ipErrors[hostname] = &net.DNSError{Err: "no such host", Name: hostname, IsNotFound: true}
			},
			wantCode: "dns_mismatch",
		},
		{
			name:     "lookup timeout is temporary",
			setup:    func() { resolver.cnameErrors[hostname] = context.DeadlineExceeded },
			wantCode: "dns_lookup_failed",
		},
		{
			name:      "direct A and AAAA allowlist",
			setup:     func() { resolver.ips[hostname] = []netip.Addr{allowed, netip.MustParseAddr("::1")} },
			wantAddrs: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver.cnames = make(map[string]string)
			resolver.ips = make(map[string][]netip.Addr)
			resolver.cnameErrors = make(map[string]error)
			resolver.ipErrors = make(map[string]error)
			test.setup()
			addresses, code := s.checkDomainDNS(context.Background(), hostname, systemHost)
			if code != test.wantCode || len(addresses) != test.wantAddrs {
				t.Fatalf("checkDomainDNS = (%v, %q), want %d addresses and %q", addresses, code, test.wantAddrs, test.wantCode)
			}
		})
	}
}

func TestDomainClaimConflictSuspensionAndRecovery(t *testing.T) {
	s, firstSite, resolver, now := newDomainTestService(t)
	ctx := context.Background()
	first, err := s.AddDomain(ctx, 1, firstSite.ID, "race.invalid")
	if err != nil {
		t.Fatal(err)
	}
	secondSite, err := s.Create(ctx, 1, "Second")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddDomain(ctx, 1, secondSite.ID, first.Hostname)
	if err != nil {
		t.Fatal(err)
	}
	resolver.ips[first.Hostname] = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	s.domainProbePort = 1 // Claim DNS without depending on a local TLS listener.
	start := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	for _, id := range []string{first.ID, second.ID} {
		go func(id string) {
			defer workers.Done()
			<-start
			_ = s.checkDomain(ctx, id)
		}(id)
	}
	close(start)
	workers.Wait()
	firstAfter := domainByID(t, s, 1, firstSite.ID, first.ID)
	secondAfter := domainByID(t, s, 1, secondSite.ID, second.ID)
	claimed := 0
	for _, domain := range []Domain{firstAfter, secondAfter} {
		if domain.ClaimedAt != nil {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("concurrent DNS claims produced %d claimants", claimed)
	}
	pending := firstAfter
	if pending.ClaimedAt != nil {
		pending = secondAfter
	}
	if pending.State != "pending_dns" || pending.ErrorCode == nil || *pending.ErrorCode != "domain_conflict" {
		t.Fatalf("losing claim was not left pending: %+v", pending)
	}

	// Use a local verified HTTPS endpoint for the winner and exercise active,
	// first mismatch, suspension, and recovery without real DNS or sleep.
	winner, winnerSite := firstAfter, firstSite
	if secondAfter.ClaimedAt != nil {
		winner, winnerSite = secondAfter, secondSite
	}
	thirdSite, err := s.Create(ctx, 1, "Third")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddDomain(ctx, 1, thirdSite.ID, winner.Hostname); errorCode(err) != "domain_conflict" {
		t.Fatalf("claimed hostname AddDomain error = %v", err)
	}
	server, roots, port := startDomainProbeServer(t, winner.Hostname, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, path.Base(r.URL.Path))
	})
	defer server.Close()
	s.domainProbePort = port
	s.domainRootCAs = roots
	resolver.ips[winner.Hostname] = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	*now = now.Add(61 * time.Second) // Claim loser retry cooldown has elapsed.
	if err := s.checkDomain(ctx, winner.ID); err != nil {
		t.Fatal(err)
	}
	active := domainByID(t, s, 1, winnerSite.ID, winner.ID)
	if active.State != "active" || active.ClaimedAt == nil {
		t.Fatalf("valid readiness did not activate domain: %+v", active)
	}
	claimTime := *active.ClaimedAt
	ask := s.DomainAskHandler()
	askStatus := func() int {
		recorder := httptest.NewRecorder()
		ask.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/ask?domain="+winner.Hostname, nil))
		return recorder.Code
	}
	if got := askStatus(); got != http.StatusOK {
		t.Fatalf("active domain ask status = %d", got)
	}
	resolver.cnameErrors[winner.Hostname] = &net.DNSError{Err: "server failure", Name: winner.Hostname, IsTemporary: true}
	if err := s.checkDomain(ctx, winner.ID); err != nil {
		t.Fatal(err)
	}
	temporaryFailure := domainByID(t, s, 1, winnerSite.ID, winner.ID)
	if temporaryFailure.State != "active" || temporaryFailure.ErrorCode == nil || *temporaryFailure.ErrorCode != "dns_lookup_failed" {
		t.Fatalf("temporary DNS failure disrupted active domain: %+v", temporaryFailure)
	}
	delete(resolver.cnameErrors, winner.Hostname)

	resolver.ips[winner.Hostname] = []netip.Addr{netip.MustParseAddr("192.0.2.4")}
	if err := s.checkDomain(ctx, winner.ID); err != nil {
		t.Fatal(err)
	}
	firstMismatch := domainByID(t, s, 1, winnerSite.ID, winner.ID)
	if firstMismatch.State != "active" || firstMismatch.ErrorCode == nil || *firstMismatch.ErrorCode != "dns_mismatch" {
		t.Fatalf("first DNS mismatch disrupted active domain: %+v", firstMismatch)
	}
	*now = now.Add(60 * time.Second)
	if err := s.checkDomain(ctx, winner.ID); err != nil {
		t.Fatal(err)
	}
	suspended := domainByID(t, s, 1, winnerSite.ID, winner.ID)
	if suspended.State != "suspended" || suspended.ClaimedAt == nil || *suspended.ClaimedAt != claimTime {
		t.Fatalf("second mismatch did not suspend while retaining claim: %+v", suspended)
	}
	if got := askStatus(); got != http.StatusForbidden {
		t.Fatalf("suspended domain ask status = %d", got)
	}
	resolver.ips[winner.Hostname] = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	*now = now.Add(5 * time.Minute)
	if err := s.checkDomain(ctx, winner.ID); err != nil {
		t.Fatal(err)
	}
	recovered := domainByID(t, s, 1, winnerSite.ID, winner.ID)
	if recovered.State != "active" || recovered.ClaimedAt == nil || *recovered.ClaimedAt != claimTime || recovered.ErrorCode != nil {
		t.Fatalf("corrected DNS did not recover domain: %+v", recovered)
	}
}

func TestDomainAskHandlerAndDeleteDuringProbe(t *testing.T) {
	s, site, resolver, _ := newDomainTestService(t)
	ctx := context.Background()
	hostname := "blocked.invalid"
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server, roots, port := startDomainProbeServer(t, hostname, func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() {
			close(entered)
			<-release
		})
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, path.Base(r.URL.Path))
	})
	defer server.Close()
	s.domainProbePort = port
	s.domainRootCAs = roots
	resolver.ips[hostname] = []netip.Addr{netip.MustParseAddr("127.0.0.1")}
	domain, err := s.AddDomain(ctx, 1, site.ID, hostname)
	if err != nil {
		t.Fatal(err)
	}

	ask := s.DomainAskHandler()
	askStatus := func(target string) int {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		response := httptest.NewRecorder()
		ask.ServeHTTP(response, req)
		return response.Code
	}
	if got := askStatus("/ask?domain=" + hostname); got != http.StatusForbidden {
		t.Fatalf("pending ask status = %d", got)
	}
	if got := askStatus("/bad?domain=" + hostname); got != http.StatusForbidden {
		t.Fatalf("invalid ask path status = %d", got)
	}

	checkDone := make(chan error, 1)
	go func() { checkDone <- s.checkDomain(ctx, domain.ID) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTPS probe did not reach blocking endpoint")
	}
	if got := askStatus("/ask?domain=" + hostname); got != http.StatusOK {
		t.Fatalf("provisioning domain ask status = %d", got)
	}
	if err := s.DeleteDomain(ctx, 1, site.ID, domain.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-checkDone; err != nil {
		t.Fatal(err)
	}
	if got := askStatus("/ask?domain=" + hostname); got != http.StatusForbidden {
		t.Fatalf("deleted ask status = %d", got)
	}
	listed, err := s.ListDomains(ctx, 1, site.ID)
	if err != nil || len(listed) != 0 {
		t.Fatalf("deleted domain restored by stale probe: %+v, %v", listed, err)
	}

	// Deleting the confirmed reservation permits another site to claim it.
	secondSite, err := s.Create(ctx, 1, "Second")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.AddDomain(ctx, 1, secondSite.ID, hostname)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.checkDomain(ctx, again.ID); err != nil {
		t.Fatal(err)
	}
	if claimed := domainByID(t, s, 1, secondSite.ID, again.ID); claimed.ClaimedAt == nil {
		t.Fatalf("released hostname was not claimable: %+v", claimed)
	}
}

func TestDomainSiteDeletionRemovesRows(t *testing.T) {
	s, site, _, _ := newDomainTestService(t)
	domain, err := s.AddDomain(context.Background(), 1, site.ID, "removed.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), 1, site.ID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM domains WHERE site_id=?", site.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("site deletion left domain rows: count=%d err=%v (domain %s)", count, err, domain.ID)
	}
}

func startDomainProbeServer(t *testing.T, hostname string, handler http.HandlerFunc) (*httptest.Server, *x509.CertPool, int) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: hostname},
		DNSNames:              []string{hostname},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKey}))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	t.Cleanup(server.Close)
	parsedCert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsedCert)
	_, portText, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	return server, roots, port
}

func TestDomainProbeRejectsBadReadinessAndUntrustedCertificates(t *testing.T) {
	s, _, _, _ := newDomainTestService(t)
	address := netip.MustParseAddr("127.0.0.1")
	for _, test := range []struct {
		name        string
		hostname    string
		status      int
		contentType string
		body        string
		want        string
	}{
		{name: "wrong body", hostname: "bad-body.invalid", status: http.StatusOK, contentType: "text/plain", body: "not-token", want: "https_probe_failed"},
		{name: "redirect", hostname: "redirect.invalid", status: http.StatusFound, contentType: "text/plain", body: "token", want: "https_probe_failed"},
		{name: "wrong content type", hostname: "wrong-type.invalid", status: http.StatusOK, contentType: "application/json", body: "token", want: "https_probe_failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, roots, port := startDomainProbeServer(t, test.hostname, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", test.contentType)
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			})
			defer server.Close()
			s.domainProbePort = port
			s.domainRootCAs = roots
			if got := s.probeDomainHTTPS(context.Background(), test.hostname, "token", []netip.Addr{address}); got != test.want {
				t.Fatalf("probe error = %q, want %q", got, test.want)
			}
		})
	}

	server, _, port := startDomainProbeServer(t, "wrong-name.invalid", func(w http.ResponseWriter, _ *http.Request) {})
	defer server.Close()
	s.domainProbePort = port
	s.domainRootCAs = nil
	if got := s.probeDomainHTTPS(context.Background(), "untrusted.invalid", "token", []netip.Addr{address}); got != "tls_unavailable" {
		t.Fatalf("untrusted/hostname-mismatched TLS error = %q", got)
	}
}

func TestDomainRetrySchedule(t *testing.T) {
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	for i, delay := range want {
		if got := domainRetryDelay(i + 1); got != delay {
			t.Fatalf("retry %d = %s, want %s", i+1, got, delay)
		}
	}
}

func TestDomainAskRejectsDuplicateParameters(t *testing.T) {
	s, _, _, _ := newDomainTestService(t)
	handler := s.DomainAskHandler()
	for _, raw := range []string{"/ask?domain=a.invalid&domain=b.invalid", "/ask?domain=a.invalid&other=x", "/ask?domain=bad%2Fhost"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, raw, nil))
		if recorder.Code != http.StatusForbidden {
			t.Errorf("ask %q status = %d", raw, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/ask?domain=a.invalid", nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("ask POST status = %d", recorder.Code)
	}
}

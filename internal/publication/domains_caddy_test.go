package publication

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"static-host/internal/store"
)

const domainsCaddyImage = "caddy:2.11.7"

// This test deliberately exercises a real Caddy process and local CA. It is
// opt-in because it requires Docker and the pinned image to have been pulled.
func TestDomainsCaddyIntegration(t *testing.T) {
	if os.Getenv("STATIC_HOST_TEST_CADDY") != "1" {
		return
	}
	setupCtx, setupCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer setupCancel()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatalf("STATIC_HOST_TEST_CADDY=1 requires Docker: %v", err)
	}
	runDocker(t, setupCtx, "image", "inspect", domainsCaddyImage)

	appPort := domainsCaddyFreePort(t)
	askPort := domainsCaddyFreePort(t)
	configDir := t.TempDir()
	caddyfile := fmt.Sprintf(`{
    on_demand_tls {
        ask http://host.docker.internal:%d/ask
    }
}
http://uncloud-priority.test {
    @verify path /.uncloud-verify*
    respond @verify "uncloud-priority" 200
}
http://integration.localhost {
    reverse_proxy host.docker.internal:%d
}
https://reserved-service.test {
    tls internal
    respond "reserved-priority" 200
}
:80 {
    redir https://{host}{uri} 308
}
:443 {
    tls {
        issuer internal
        on_demand
    }
    reverse_proxy host.docker.internal:%d
}
`, askPort, appPort, appPort)
	if err := os.WriteFile(filepath.Join(configDir, "Caddyfile"), []byte(caddyfile), 0600); err != nil {
		t.Fatal(err)
	}
	container := "static-host-domain-caddy-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	runDocker(t, setupCtx, "run", "-d", "--name", container,
		"--add-host=host.docker.internal:host-gateway",
		"-p", "127.0.0.1::80", "-p", "127.0.0.1::443",
		"-v", configDir+":/etc/caddy:ro", domainsCaddyImage,
		"caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, "docker", "rm", "-f", container).Run()
	})

	ports := strings.TrimSpace(string(runDocker(t, setupCtx, "port", container)))
	httpPort, tlsPort := domainsCaddyMappedPorts(t, ports)
	waitDomainsCaddy(t, setupCtx, httpPort, container)
	roots := domainsCaddyRootPool(t, setupCtx, container)

	testCtx, testCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer testCancel()
	dataDir := t.TempDir()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.ExecContext(testCtx, "INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('caddy-test-key',1,'integration',x'01',1)"); err != nil {
		t.Fatal(err)
	}
	s, err := New(db, dataDir, Options{
		UploadOrigin: "http://localhost", SiteSuffix: "localhost", BaseDomain: "base.test", Local: true,
		DomainIngressIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	resolver := &domainsCaddyResolver{ready: make(chan struct{})}
	s.domainResolver = resolver
	s.domainProbePort = tlsPort
	s.domainRootCAs = roots

	appServer := domainsCaddyServe(t, appPort, s.SiteHandler())
	defer appServer.Close()
	askServer := domainsCaddyServe(t, askPort, s.DomainAskHandler())
	defer askServer.Close()

	identity := Identity{OwnerID: 1, KeyID: "caddy-test-key"}
	site, err := s.Create(testCtx, identity.OwnerID, "integration")
	if err != nil {
		t.Fatal(err)
	}
	siteHost, err := url.Parse(site.URL)
	if err != nil {
		t.Fatal(err)
	}
	resolver.setCNAME(siteHost.Hostname())
	domainsCaddyPublish(t, testCtx, s, identity, site.ID, "real-caddy-content")
	hostname := "custom-domain-integration.test"

	// The ask endpoint cannot grant certificates before DNS has claimed the name.
	if status := domainsCaddyAsk(t, testCtx, askPort, hostname); status != http.StatusForbidden {
		t.Fatalf("unclaimed domain ask status = %d, want 403", status)
	}
	if err = domainsCaddyTLSRequest(testCtx, tlsPort, hostname, roots); err == nil {
		t.Fatal("Caddy issued/served TLS for an unclaimed hostname")
	}
	if status := domainsCaddyAsk(t, testCtx, askPort, "unknown-domain-integration.test"); status != http.StatusForbidden {
		t.Fatalf("unknown domain ask status = %d, want 403", status)
	}
	if err = domainsCaddyTLSRequest(testCtx, tlsPort, "unknown-domain-integration.test", roots); err == nil {
		t.Fatal("Caddy issued/served TLS for an unknown hostname")
	}
	if _, err = s.AddDomain(testCtx, identity.OwnerID, site.ID, hostname); err != nil {
		t.Fatal(err)
	}
	if status := domainsCaddyAsk(t, testCtx, askPort, hostname); status != http.StatusForbidden {
		t.Fatalf("pending domain ask status = %d, want 403", status)
	}
	if err = domainsCaddyTLSRequest(testCtx, tlsPort, hostname, roots); err == nil {
		t.Fatal("Caddy issued/served TLS for a DNS-pending hostname")
	}

	// Let the service worker perform DNS claim, Caddy on-demand TLS issuance,
	// and the nonce readiness request through the mapped TLS listener.
	close(resolver.ready)
	if err = domainsCaddyWaitState(testCtx, s, identity.OwnerID, site.ID, hostname, "active"); err != nil {
		t.Fatal(err)
	}

	client := domainsCaddyClient(tlsPort, roots)
	resp, err := domainsCaddyGet(testCtx, client, "https://"+hostname+"/")
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil || resp.StatusCode != http.StatusOK || string(body) != "real-caddy-content" {
		t.Fatalf("custom HTTPS route: status=%d body=%q err=%v", resp.StatusCode, body, readErr)
	}

	systemClient := &http.Client{Transport: domainsCaddyTransport(httpPort, nil)}
	resp, err = domainsCaddyGet(testCtx, systemClient, site.URL+"/")
	if err != nil {
		t.Fatal(err)
	}
	systemBody, systemReadErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if systemReadErr != nil || resp.StatusCode != http.StatusOK || string(systemBody) != "real-caddy-content" {
		t.Fatalf("system host route: status=%d body=%q err=%v", resp.StatusCode, systemBody, systemReadErr)
	}
	if status := domainsCaddyAsk(t, testCtx, askPort, hostname); status != http.StatusOK {
		t.Fatalf("active domain ask status = %d, want 200", status)
	}

	request, err := http.NewRequestWithContext(testCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/a?b=1", httpPort), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = hostname
	noRedirect := &http.Client{Transport: domainsCaddyTransport(httpPort, nil), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err = noRedirect.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	wantLocation := "https://" + hostname + "/a?b=1"
	if resp.StatusCode != http.StatusPermanentRedirect || resp.Header.Get("Location") != wantLocation {
		t.Fatalf("HTTP fallback = %d %q, want 308 %q", resp.StatusCode, resp.Header.Get("Location"), wantLocation)
	}

	// Explicit, operator-owned routes retain priority over the fallback.
	verifyRequest, _ := http.NewRequestWithContext(testCtx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/.uncloud-verify", httpPort), nil)
	verifyRequest.Host = "uncloud-priority.test"
	priority, err := noRedirect.Do(verifyRequest)
	if err != nil {
		t.Fatal(err)
	}
	priorityBody, _ := io.ReadAll(priority.Body)
	priority.Body.Close()
	if priority.StatusCode != http.StatusOK || string(priorityBody) != "uncloud-priority" {
		t.Fatalf("explicit Uncloud verification route: status=%d body=%q", priority.StatusCode, priorityBody)
	}
	reserved, err := domainsCaddyGet(testCtx, client, "https://reserved-service.test/")
	if err != nil {
		t.Fatal(err)
	}
	reservedBody, _ := io.ReadAll(reserved.Body)
	reserved.Body.Close()
	if reserved.StatusCode != http.StatusOK || string(reservedBody) != "reserved-priority" {
		t.Fatalf("explicit service route: status=%d body=%q", reserved.StatusCode, reservedBody)
	}

	// DNS mismatch suspension and recovery use the real Caddy/HTTPS path too.
	resolver.setMismatch(true)
	if err = domainsCaddyCheck(testCtx, s, identity.OwnerID, site.ID, hostname); err != nil {
		t.Fatal(err)
	}
	if err = domainsCaddyWaitState(testCtx, s, identity.OwnerID, site.ID, hostname, "active"); err != nil {
		t.Fatal(err)
	}
	if err = domainsCaddyCheck(testCtx, s, identity.OwnerID, site.ID, hostname); err != nil {
		t.Fatal(err)
	}
	if err = domainsCaddyWaitState(testCtx, s, identity.OwnerID, site.ID, hostname, "suspended"); err != nil {
		t.Fatal(err)
	}
	if status := domainsCaddyAsk(t, testCtx, askPort, hostname); status != http.StatusForbidden {
		t.Fatalf("suspended domain ask status = %d, want 403", status)
	}
	resolver.setMismatch(false)
	if err = domainsCaddyCheck(testCtx, s, identity.OwnerID, site.ID, hostname); err != nil {
		t.Fatal(err)
	}
	if err = domainsCaddyWaitState(testCtx, s, identity.OwnerID, site.ID, hostname, "active"); err != nil {
		t.Fatal(err)
	}

	if err = s.DeleteDomain(testCtx, identity.OwnerID, site.ID, mustDomainID(t, s, identity.OwnerID, site.ID, hostname)); err != nil {
		t.Fatal(err)
	}
	if status := domainsCaddyAsk(t, testCtx, askPort, hostname); status != http.StatusForbidden {
		t.Fatalf("deleted domain ask status = %d, want 403", status)
	}
	resp, err = domainsCaddyGet(testCtx, client, "https://"+hostname+"/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("deleted custom route status = %d, want 404", resp.StatusCode)
	}
}

type domainsCaddyResolver struct {
	ready    chan struct{}
	mu       sync.RWMutex
	mismatch bool
	cname    string
}

func (r *domainsCaddyResolver) LookupCNAME(ctx context.Context, hostname string) (string, error) {
	select {
	case <-r.ready:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	r.mu.RLock()
	mismatch := r.mismatch
	cname := r.cname
	r.mu.RUnlock()
	if hostname == "custom-domain-integration.test" && mismatch {
		return "wrong-domain-integration.test", nil
	}
	if hostname == "custom-domain-integration.test" {
		return cname, nil
	}
	return hostname, nil
}

func (r *domainsCaddyResolver) LookupNetIP(ctx context.Context, hostname string) ([]netip.Addr, error) {
	select {
	case <-r.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

func (r *domainsCaddyResolver) setCNAME(cname string) {
	r.mu.Lock()
	r.cname = cname
	r.mu.Unlock()
}

func (r *domainsCaddyResolver) setMismatch(mismatch bool) {
	r.mu.Lock()
	r.mismatch = mismatch
	r.mu.Unlock()
}

func runDocker(t *testing.T, ctx context.Context, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func domainsCaddyFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func domainsCaddyMappedPorts(t *testing.T, output string) (httpPort, tlsPort int) {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		parts := strings.Split(strings.TrimSpace(line), "->")
		if len(parts) != 2 {
			continue
		}
		port, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(parts[1]), "0.0.0.0:"))
		if err != nil {
			port, err = strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(parts[1]), "127.0.0.1:"))
		}
		if err != nil {
			continue
		}
		source := strings.TrimSpace(parts[0])
		if strings.HasPrefix(source, "80/") {
			httpPort = port
		}
		if strings.HasPrefix(source, "443/") {
			tlsPort = port
		}
	}
	if httpPort == 0 || tlsPort == 0 {
		t.Fatalf("could not parse docker port output %q", output)
	}
	return httpPort, tlsPort
}

func waitDomainsCaddy(t *testing.T, ctx context.Context, port int, container string) {
	t.Helper()
	deadline := time.NewTicker(100 * time.Millisecond)
	defer deadline.Stop()
	for {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 100*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		select {
		case <-ctx.Done():
			logs, _ := exec.CommandContext(ctx, "docker", "logs", container).CombinedOutput()
			t.Fatalf("Caddy did not start: %v\n%s", ctx.Err(), logs)
		case <-deadline.C:
		}
	}
}

func domainsCaddyRootPool(t *testing.T, ctx context.Context, container string) *x509.CertPool {
	t.Helper()
	var pem []byte
	for range 20 {
		cmd := exec.CommandContext(ctx, "docker", "exec", container, "cat", "/data/caddy/pki/authorities/local/root.crt")
		output, err := cmd.CombinedOutput()
		if err == nil {
			pem = output
			pool := x509.NewCertPool()
			if pool.AppendCertsFromPEM(pem) {
				return pool
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("Caddy local root certificate is not valid PEM")
	return nil
}

func domainsCaddyServe(t *testing.T, port int, handler http.Handler) *http.Server {
	t.Helper()
	server := &http.Server{Addr: net.JoinHostPort("0.0.0.0", strconv.Itoa(port)), Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	return server
}

func domainsCaddyPublish(t *testing.T, ctx context.Context, s *Service, identity Identity, siteID, content string) {
	t.Helper()
	grant, err := s.CreateUpload(ctx, identity, siteID, "zip", 0)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	file, err := zipWriter.Create("index.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(file, content); err != nil {
		t.Fatal(err)
	}
	if err = zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, grant.UploadURL, bytes.NewReader(archive.Bytes()))
	request.Header.Set("Content-Type", "application/zip")
	response := httptest.NewRecorder()
	s.UploadHandler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated && response.Code != http.StatusOK {
		t.Fatalf("upload archive: status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = s.Publish(ctx, identity, siteID, grant.UploadID, 0, "caddy-integration-publish"); err != nil {
		t.Fatal(err)
	}
}

func domainsCaddyAsk(t *testing.T, ctx context.Context, port int, hostname string) int {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/ask?domain=%s", port, url.QueryEscape(hostname)), nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: domainsCaddyTransport(port, nil)}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading ask response body: %v", err)
	}
	if response.StatusCode == http.StatusOK && len(body) != 0 {
		t.Fatalf("successful ask response has body %q", body)
	}
	return response.StatusCode
}

func domainsCaddyTransport(port int, roots *x509.CertPool) *http.Transport {
	transport := &http.Transport{Proxy: nil}
	if roots != nil {
		transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	}
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	return transport
}

func domainsCaddyClient(port int, roots *x509.CertPool) *http.Client {
	return &http.Client{Transport: domainsCaddyTransport(port, roots), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func domainsCaddyGet(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(request)
}

func domainsCaddyTLSRequest(ctx context.Context, port int, hostname string, roots *x509.CertPool) error {
	client := domainsCaddyClient(port, roots)
	response, err := domainsCaddyGet(ctx, client, "https://"+hostname+"/")
	if err == nil {
		response.Body.Close()
		if response.StatusCode >= 400 {
			return fmt.Errorf("unexpected response status %d", response.StatusCode)
		}
	}
	return err
}

func domainsCaddyWaitState(ctx context.Context, s *Service, ownerID int64, siteID, hostname, state string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		domains, err := s.ListDomains(ctx, ownerID, siteID)
		if err != nil {
			return err
		}
		for _, domain := range domains {
			if domain.Hostname == hostname && domain.State == state {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for domain %q state %q: %w", hostname, state, ctx.Err())
		case <-ticker.C:
		}
	}
}

func domainsCaddyCheck(ctx context.Context, s *Service, ownerID int64, siteID, hostname string) error {
	domains, err := s.ListDomains(ctx, ownerID, siteID)
	if err != nil {
		return err
	}
	for _, domain := range domains {
		if domain.Hostname == hostname {
			return s.checkDomain(ctx, domain.ID)
		}
	}
	return sql.ErrNoRows
}

func mustDomainID(t *testing.T, s *Service, ownerID int64, siteID, hostname string) string {
	t.Helper()
	domains, err := s.ListDomains(context.Background(), ownerID, siteID)
	if err != nil {
		t.Fatal(err)
	}
	for _, domain := range domains {
		if domain.Hostname == hostname {
			return domain.ID
		}
	}
	t.Fatalf("domain %q missing", hostname)
	return ""
}

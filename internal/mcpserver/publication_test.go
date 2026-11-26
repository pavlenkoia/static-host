package mcpserver

import (
	"context"
	"database/sql"
	"net/netip"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"static-host/internal/publication"
	"static-host/internal/store"
)

func testPublications(t *testing.T, db *sql.DB) *publication.Service {
	t.Helper()
	service, err := publication.New(db, t.TempDir(), publication.Options{UploadOrigin: "http://127.0.0.1:8080", SiteSuffix: "localhost:8080", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close() })
	return service
}

func TestPublicationTools(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pubs, err := publication.New(db, dir, publication.Options{
		SiteSuffix:       "localhost:8081",
		Local:            true,
		DomainIngressIPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer pubs.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	registerPublicationTools(server, pubs)
	registerDomainTools(server, pubs)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.WithValue(context.Background(), identityKey{}, Identity{OwnerID: 1, KeyID: "key"})
	ss, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sites_create", Arguments: map[string]any{"name": "demo"}})
	if err != nil || result.IsError || result.StructuredContent == nil {
		t.Fatalf("create: %+v %v", result, err)
	}
	siteID := result.StructuredContent.(map[string]any)["site"].(map[string]any)["id"]
	domainResult, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "domains_list", Arguments: map[string]any{"site_id": siteID}})
	if err != nil || domainResult.IsError || domainResult.StructuredContent == nil {
		t.Fatalf("domains_list: %+v %v", domainResult, err)
	}
	domains := domainResult.StructuredContent.(map[string]any)["domains"].([]any)
	if len(domains) != 0 {
		t.Fatalf("initial domains: %+v", domains)
	}
	domainResult, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "domains_add", Arguments: map[string]any{"site_id": siteID, "hostname": "www.example.com"}})
	if err != nil || domainResult.IsError || domainResult.StructuredContent == nil {
		t.Fatalf("domains_add: %+v %v", domainResult, err)
	}
	domain := domainResult.StructuredContent.(map[string]any)["domain"].(map[string]any)
	if domain["hostname"] != "www.example.com" || domain["site_id"] != siteID {
		t.Fatalf("domain payload: %+v", domain)
	}
	domainID := domain["id"]
	domainResult, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "domains_check", Arguments: map[string]any{"site_id": siteID, "domain_id": domainID}})
	if err != nil || domainResult.IsError || domainResult.StructuredContent == nil || domainResult.StructuredContent.(map[string]any)["domain"].(map[string]any)["id"] != domainID {
		t.Fatalf("domains_check: %+v %v", domainResult, err)
	}
	domainResult, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "domains_delete", Arguments: map[string]any{"site_id": siteID, "domain_id": domainID}})
	if err != nil || domainResult.IsError || domainResult.StructuredContent == nil {
		t.Fatalf("domains_delete: %+v %v", domainResult, err)
	}
	deleted := domainResult.StructuredContent.(map[string]any)
	if deleted["deleted"] != true || deleted["site_id"] != siteID || deleted["domain_id"] != domainID {
		t.Fatalf("domains_delete payload: %+v", deleted)
	}
	domainResult, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "domains_add", Arguments: map[string]any{"site_id": siteID, "hostname": "bad host"}})
	if err != nil || !domainResult.IsError || domainResult.StructuredContent == nil {
		t.Fatalf("invalid domain: %+v %v", domainResult, err)
	}
	errorObject := domainResult.StructuredContent.(map[string]any)["error"].(map[string]any)
	if code := errorObject["code"]; code != "invalid_domain" {
		t.Fatalf("invalid domain error code: %v", code)
	}
	result, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sites_get", Arguments: map[string]any{"site_id": "missing"}})
	if err != nil || !result.IsError || result.StructuredContent == nil {
		t.Fatalf("missing: %+v %v", result, err)
	}
	result, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sites_create", Arguments: map[string]any{"name": "bad", "owner_id": 1}})
	if err == nil && !result.IsError {
		t.Fatalf("unknown field accepted: %+v", result)
	}
}

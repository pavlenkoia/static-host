package mcpserver

import (
	"context"
	"database/sql"
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
	pubs, err := publication.New(db, dir, publication.Options{SiteSuffix: "localhost:8081", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	defer pubs.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	registerPublicationTools(server, pubs)
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
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 6 {
		t.Fatalf("tools: %d", len(tools.Tools))
	}
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sites_create", Arguments: map[string]any{"name": "demo"}})
	if err != nil || result.IsError || result.StructuredContent == nil {
		t.Fatalf("create: %+v %v", result, err)
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

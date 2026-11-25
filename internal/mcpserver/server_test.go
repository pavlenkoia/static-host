package mcpserver

import (
 "context"
 "crypto/sha256"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/modelcontextprotocol/go-sdk/mcp"
 "static-host/internal/store"
)
type bearer struct { token string }
func (b bearer) RoundTrip(r *http.Request) (*http.Response,error) { r=r.Clone(r.Context()); r.Header.Set("Authorization","Bearer "+b.token); return http.DefaultTransport.RoundTrip(r) }
func TestSDKPingAndRevocation(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 _,err=db.Exec("INSERT INTO users VALUES(1,'admin','hash')"); if err!=nil {t.Fatal(err)}
 token:="sh_test"; hash:=sha256.Sum256([]byte(token)); _,err=db.Exec("INSERT INTO agent_keys VALUES('key',1,'test',?,?,?,NULL)",hash[:],time.Now().Unix(),time.Now().Add(time.Hour).Unix()); if err!=nil {t.Fatal(err)}
 server:=httptest.NewServer(New(db)); defer server.Close()
 client:=mcp.NewClient(&mcp.Implementation{Name:"test",Version:"1"},nil)
 session,err:=client.Connect(context.Background(),&mcp.StreamableClientTransport{Endpoint:server.URL,HTTPClient:&http.Client{Transport:bearer{token}}},&mcp.ClientSessionOptions{ProtocolVersion:"2025-11-25"}); if err!=nil {t.Fatal(err)}; defer session.Close()
 if err=session.Ping(context.Background(),&mcp.PingParams{}); err!=nil {t.Fatal(err)}
 if _,err=db.Exec("UPDATE agent_keys SET revoked_at=?",time.Now().Unix()); err!=nil {t.Fatal(err)}
 if err=session.Ping(context.Background(),&mcp.PingParams{}); err==nil {t.Fatal("revoked key accepted")}
}

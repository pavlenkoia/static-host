package mcpserver

import (
 "context"
 "crypto/aes"
 "crypto/cipher"
 "crypto/sha256"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/modelcontextprotocol/go-sdk/mcp"
 "static-host/internal/store"
 "static-host/internal/web"
)
type bearer struct { token string }
func (b bearer) RoundTrip(r *http.Request) (*http.Response,error) { r=r.Clone(r.Context()); r.Header.Set("Authorization","Bearer "+b.token); return http.DefaultTransport.RoundTrip(r) }
func TestSDKPingAndRevocation(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 token:="sh_test"; hash:=sha256.Sum256([]byte(token)); _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at,expires_at,revoked_at) VALUES('key',1,'test',?,?,?,NULL)",hash[:],time.Now().Unix(),time.Now().Add(time.Hour).Unix()); if err!=nil {t.Fatal(err)}
 server:=httptest.NewServer(New(db,"http://127.0.0.1:8080",testPublications(t,db))); defer server.Close()
 client:=mcp.NewClient(&mcp.Implementation{Name:"test",Version:"1"},nil)
 session,err:=client.Connect(context.Background(),&mcp.StreamableClientTransport{Endpoint:server.URL,HTTPClient:&http.Client{Transport:bearer{token}}},&mcp.ClientSessionOptions{ProtocolVersion:"2025-11-25"}); if err!=nil {t.Fatal(err)}; defer session.Close()
 if err=session.Ping(context.Background(),&mcp.PingParams{}); err!=nil {t.Fatal(err)}
 if _,err=db.Exec("UPDATE agent_keys SET revoked_at=?",time.Now().Unix()); err!=nil {t.Fatal(err)}
 if err=session.Ping(context.Background(),&mcp.PingParams{}); err==nil {t.Fatal("revoked key accepted")}
}

func TestExpiredKeyRejectedByMCPRequest(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	token := "sh_expires"
	hash := sha256.Sum256([]byte(token))
	expires := time.Now().Unix() + 60
	if _, err := db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at,expires_at) VALUES('expiring',1,'test',?,?,?)", hash[:], time.Now().Unix(), expires); err != nil {
		t.Fatal(err)
	}
	if _, _, err := web.AuthenticateKey(db, token, expires-1); err != nil {
		t.Fatalf("key rejected before expiration: %v", err)
	}
	if _, _, err := web.AuthenticateKey(db, token, expires); err == nil {
		t.Fatal("key accepted at expiration boundary")
	}

	server := httptest.NewServer(New(db,"http://127.0.0.1:8080",testPublications(t,db)))
	defer server.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: server.URL,
		HTTPClient: &http.Client{Transport: bearer{token}},
	}, &mcp.ClientSessionOptions{ProtocolVersion: "2025-11-25"})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.Ping(context.Background(), &mcp.PingParams{}); err != nil {
		t.Fatalf("key rejected before expiration: %v", err)
	}
	if _, err := db.Exec("UPDATE agent_keys SET expires_at=?", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := session.Ping(context.Background(), &mcp.PingParams{}); err == nil {
		t.Fatal("MCP request accepted expired key")
	}
}

func TestUnlimitedKeyDeletedByOwner(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 token:="sh_unlimited"; hash:=sha256.Sum256([]byte(token)); sessionHash:=sha256.Sum256([]byte("session"))
 if _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('key',1,'test',?,?)",hash[:],time.Now().Unix()); err!=nil {t.Fatal(err)}
 if _,err=db.Exec("INSERT INTO sessions VALUES(?,1,'csrf',?,?)",sessionHash[:],time.Now().Unix(),time.Now().Add(time.Hour).Unix()); err!=nil {t.Fatal(err)}
 server:=httptest.NewServer(New(db,"http://127.0.0.1:8080",testPublications(t,db))); defer server.Close()
 client:=mcp.NewClient(&mcp.Implementation{Name:"test",Version:"1"},nil)
 session,err:=client.Connect(context.Background(),&mcp.StreamableClientTransport{Endpoint:server.URL,HTTPClient:&http.Client{Transport:bearer{token}}},&mcp.ClientSessionOptions{ProtocolVersion:"2025-11-25"}); if err!=nil {t.Fatal(err)}; defer session.Close()
 if err=session.Ping(context.Background(),&mcp.PingParams{}); err!=nil {t.Fatal(err)}
 block,err:=aes.NewCipher(make([]byte,32)); if err!=nil {t.Fatal(err)}; keyCipher,err:=cipher.NewGCM(block); if err!=nil {t.Fatal(err)}
 app,err:=web.New(db,keyCipher,"http://127.0.0.1:8080","long-password-one",testPublications(t,db)); if err!=nil {t.Fatal(err)}
 r:=httptest.NewRequest("DELETE","http://127.0.0.1:8080/api/keys/key",nil); r.AddCookie(&http.Cookie{Name:"static_host_session",Value:"session"}); r.Header.Set("Origin","http://127.0.0.1:8080"); r.Header.Set("X-CSRF-Token","csrf"); w:=httptest.NewRecorder(); app.ServeHTTP(w,r); if w.Code!=204 {t.Fatal(w.Code,w.Body)}
 if err=session.Ping(context.Background(),&mcp.PingParams{}); err==nil {t.Fatal("deleted key accepted")}
}

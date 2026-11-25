package mcpserver

import (
 "crypto/sha256"
 "net/http/httptest"
 "testing"
 "time"
 "static-host/internal/store"
)
func TestConfiguredOrigin(t *testing.T) {
 db,err:=store.Open(t.TempDir());if err!=nil{t.Fatal(err)};defer db.Close(); h:=sha256.Sum256([]byte("token")); if _,err=db.Exec("INSERT INTO users VALUES(1,'admin','hash')");err!=nil{t.Fatal(err)};if _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('id',1,'agent',?,?)",h[:],time.Now().Unix());err!=nil{t.Fatal(err)}
 handler:=New(db,"https://mcp.example.test")
 for _,tc:=range []struct{origins []string; forbidden bool}{{nil,false},{[]string{"https://mcp.example.test"},false},{[]string{"null"},true},{[]string{"https://other.test"},true},{[]string{"https://mcp.example.test","https://mcp.example.test"},true}} {r:=httptest.NewRequest("POST","http://internal/mcp",nil);r.Header.Set("Authorization","Bearer token");for _,v:=range tc.origins{r.Header.Add("Origin",v)};w:=httptest.NewRecorder();handler.ServeHTTP(w,r);if (w.Code==403)!=tc.forbidden||w.Code==401 {t.Fatalf("%v: %d",tc.origins,w.Code)}}
}

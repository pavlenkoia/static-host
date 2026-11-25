package mcpserver

import (
 "archive/zip"
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/json"
 "fmt"
 "io"
 "net"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "github.com/modelcontextprotocol/go-sdk/mcp"
 "static-host/internal/httpserver"
 "static-host/internal/publication"
 "static-host/internal/store"
 "static-host/internal/web"
)

func TestMCPPublicationHTTP(t *testing.T) {
 dir:=t.TempDir(); db,err:=store.Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 token:="sh_test_publication"; hash:=sha256.Sum256([]byte(token)); if _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('key',1,'test',?,?)",hash[:],time.Now().Unix()); err!=nil {t.Fatal(err)}
 listener,err:=net.Listen("tcp","127.0.0.1:0"); if err!=nil {t.Fatal(err)}; origin:="http://"+listener.Addr().String(); _,port,_:=net.SplitHostPort(listener.Addr().String())
 pubs,err:=publication.New(db,dir,publication.Options{UploadOrigin:origin,SiteSuffix:"localhost:"+port,Local:true}); if err!=nil {t.Fatal(err)}; defer pubs.Close()
 upload:=pubs.UploadHandler(func(value string)(publication.Identity,error){id,owner,e:=web.AuthenticateKey(db,value,time.Now().Unix()); return publication.Identity{OwnerID:owner,KeyID:id},e})
 router:=httpserver.New(origin,origin,origin,"localhost:"+port,http.NotFoundHandler(),New(db,origin,pubs),upload,pubs.SiteHandler())
 server:=httptest.NewUnstartedServer(router); server.Listener.Close(); server.Listener=listener; server.Start(); defer server.Close()
 ctx:=context.Background(); cs,err:=mcp.NewClient(&mcp.Implementation{Name:"test",Version:"1"},nil).Connect(ctx,&mcp.StreamableClientTransport{Endpoint:origin+"/mcp",HTTPClient:&http.Client{Transport:bearer{token}}},nil); if err!=nil {t.Fatal(err)}; defer cs.Close()
 call:=func(name string,args map[string]any,out any){t.Helper(); result,e:=cs.CallTool(ctx,&mcp.CallToolParams{Name:name,Arguments:args}); if e!=nil || result.IsError {t.Fatalf("%s: %+v %v",name,result,e)}; b,e:=json.Marshal(result.StructuredContent); if e!=nil {t.Fatal(e)}; if e=json.Unmarshal(b,out); e!=nil {t.Fatal(e)}}
 var created struct {Site publication.Site `json:"site"`}; call("sites_create",map[string]any{"name":"demo"},&created)
 var grant publication.UploadGrant; call("uploads_create",map[string]any{"site_id":created.Site.ID,"format":"zip","expected_generation":0},&grant)
 var archive bytes.Buffer; zw:=zip.NewWriter(&archive); f,err:=zw.Create("index.html"); if err!=nil {t.Fatal(err)}; if _,err=f.Write([]byte("hello")); err!=nil {t.Fatal(err)}; if err=zw.Close(); err!=nil {t.Fatal(err)}
 request,err:=http.NewRequest("PUT",grant.UploadURL,&archive); if err!=nil {t.Fatal(err)}; request.Header.Set("Authorization","Bearer "+token); request.Header.Set("Content-Type","application/zip"); response,err:=http.DefaultClient.Do(request); if err!=nil {t.Fatal(err)}; body,_:=io.ReadAll(response.Body); response.Body.Close(); if response.StatusCode!=200 {t.Fatalf("upload: %d %s",response.StatusCode,body)}
 var published struct {Site publication.Site `json:"site"`}; call("sites_publish",map[string]any{"site_id":created.Site.ID,"upload_id":grant.UploadID,"expected_generation":0,"idempotency_key":"once"},&published); if published.Site.Generation!=1 {t.Fatal(published.Site)}
 request,err=http.NewRequest("GET",origin+"/",nil); if err!=nil {t.Fatal(err)}; request.Host=fmt.Sprintf("%s.localhost:%s",created.Site.ID,port); response,err=http.DefaultClient.Do(request); if err!=nil {t.Fatal(err)}; body,_=io.ReadAll(response.Body); response.Body.Close(); if response.StatusCode!=200 || string(body)!="hello" {t.Fatalf("site: %d %s",response.StatusCode,body)}
}

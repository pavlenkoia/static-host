package publication

import (
 "archive/zip"
 "bytes"
 "context"
 "io"
 "net/http"
 "net/http/httptest"
 "testing"
 "time"

 "static-host/internal/store"
)

type revokeReader struct {reader io.Reader; revoke func()}
func (r *revokeReader) Read(p []byte)(int,error) {if r.revoke!=nil {r.revoke(); r.revoke=nil}; return r.reader.Read(p)}
func TestUploadRejectsRevokedKeyDuringBody(t *testing.T) {
 dir:=t.TempDir(); db,err:=store.Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 if _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('key',1,'agent',x'01',1)"); err!=nil {t.Fatal(err)}
 s,err:=New(db,dir,Options{UploadOrigin:"http://127.0.0.1:8080",SiteSuffix:"localhost:8080",Local:true}); if err!=nil {t.Fatal(err)}; defer s.Close(); id:=Identity{OwnerID:1,KeyID:"key"}
 site,err:=s.Create(context.Background(),1,"site"); if err!=nil {t.Fatal(err)}; grant,err:=s.CreateUpload(context.Background(),id,site.ID,"zip",0); if err!=nil {t.Fatal(err)}
 var archive bytes.Buffer; w:=zip.NewWriter(&archive); f,err:=w.Create("index.html"); if err!=nil {t.Fatal(err)}; if _,err=f.Write([]byte("content")); err!=nil {t.Fatal(err)}; if err=w.Close(); err!=nil {t.Fatal(err)}
 body:=&revokeReader{reader:bytes.NewReader(archive.Bytes()),revoke:func(){if _,e:=db.Exec("UPDATE agent_keys SET revoked_at=? WHERE id='key'",time.Now().Unix()); e!=nil {t.Fatal(e)}}}
 req:=httptest.NewRequest(http.MethodPut,grant.UploadURL,body); req.Header.Set("Content-Type","application/zip"); res:=httptest.NewRecorder(); s.UploadHandler().ServeHTTP(res,req); if res.Code!=401 {t.Fatalf("revoked upload: %d %s",res.Code,res.Body.String())}
 got,err:=s.Get(context.Background(),1,site.ID); if err!=nil || got.Status!="draft" || got.Upload==nil || got.Upload.State!="failed" {t.Fatalf("site after revocation: %+v %v",got,err)}
}

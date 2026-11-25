package publication

import (
 "archive/zip"
 "bytes"
 "context"
 "errors"
 "net/http"
 "net/http/httptest"
 "testing"

 "static-host/internal/store"
)

func TestUploadGrantAndPut(t *testing.T) {
 dir:=t.TempDir(); db,err:=store.Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 if _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('key',1,'test',x'01',1)"); err!=nil {t.Fatal(err)}
 s,err:=New(db,dir,Options{UploadOrigin:"http://localhost:8081",SiteSuffix:"localhost:8081",Local:true}); if err!=nil {t.Fatal(err)}; defer s.Close()
 identity:=Identity{OwnerID:1,KeyID:"key"}; site,err:=s.Create(context.Background(),1,"site"); if err!=nil {t.Fatal(err)}
 grant,err:=s.CreateUpload(context.Background(),identity,site.ID,"zip",0); if err!=nil {t.Fatal(err)}
 if _,err=s.CreateUpload(context.Background(),identity,site.ID,"zip",0); err==nil {t.Fatal("second active upload accepted")}
 var data bytes.Buffer; zw:=zip.NewWriter(&data); f,err:=zw.Create("index.html"); if err!=nil {t.Fatal(err)}; if _,err=f.Write([]byte("hello")); err!=nil {t.Fatal(err)}; f,err=zw.Create("old.txt"); if err!=nil {t.Fatal(err)}; if _,err=f.Write([]byte("old")); err!=nil {t.Fatal(err)}; f,err=zw.Create("dir/index.html"); if err!=nil {t.Fatal(err)}; if _,err=f.Write([]byte("nested")); err!=nil {t.Fatal(err)}; if err=zw.Close(); err!=nil {t.Fatal(err)}
 handler:=s.UploadHandler(func(token string)(Identity,error){if token=="other" {return Identity{OwnerID:1,KeyID:"other"},nil}; if token!="test" {return Identity{},errors.New("invalid token")}; return identity,nil})
 request:=func() *httptest.ResponseRecorder {r:=httptest.NewRequest(http.MethodPut,grant.UploadURL,bytes.NewReader(data.Bytes())); r.Header.Set("Authorization","Bearer test"); r.Header.Set("Content-Type","application/zip"); w:=httptest.NewRecorder(); handler.ServeHTTP(w,r); return w}
 bad:=httptest.NewRequest(http.MethodPut,grant.UploadURL+"bad",bytes.NewReader(data.Bytes())); bad.Header.Set("Authorization","Bearer test"); bad.Header.Set("Content-Type","application/zip"); badResponse:=httptest.NewRecorder(); handler.ServeHTTP(badResponse,bad); if badResponse.Code!=404 {t.Fatalf("invalid capability: %d",badResponse.Code)}
 wrongKey:=httptest.NewRequest(http.MethodPut,grant.UploadURL,bytes.NewReader(data.Bytes())); wrongKey.Header.Set("Authorization","Bearer other"); wrongKey.Header.Set("Content-Type","application/zip"); badResponse=httptest.NewRecorder(); handler.ServeHTTP(badResponse,wrongKey); if badResponse.Code!=401 {t.Fatalf("wrong key: %d",badResponse.Code)}
 // Oversized declarations are rejected without reading a body and release the slot.
 tooLarge:=httptest.NewRequest(http.MethodPut,grant.UploadURL,bytes.NewReader(nil)); tooLarge.ContentLength=maxArchiveBytes+1; tooLarge.Header.Set("Authorization","Bearer test"); tooLarge.Header.Set("Content-Type","application/zip"); largeResponse:=httptest.NewRecorder(); handler.ServeHTTP(largeResponse,tooLarge); if largeResponse.Code!=413 {t.Fatalf("oversized declaration: %d",largeResponse.Code)}
 grant,err=s.CreateUpload(context.Background(),identity,site.ID,"zip",0); if err!=nil {t.Fatal(err)}
 if w:=request(); w.Code!=200 {t.Fatalf("PUT: %d %s",w.Code,w.Body.String())}; if w:=request(); w.Code!=409 {t.Fatalf("replay: %d",w.Code)}
 published,err:=s.Publish(context.Background(),identity,site.ID,grant.UploadID,0,"request-1"); if err!=nil {t.Fatal(err)}; if published.Generation!=1 || published.Status!="published" {t.Fatalf("published: %+v",published)}
 current,err:=s.Get(context.Background(),1,site.ID); if err!=nil || current.Upload!=nil {t.Fatalf("completed upload still shown: %+v %v",current,err)}
 _,err=s.Publish(context.Background(),identity,site.ID,grant.UploadID,1,"request-1"); var conflict *Error; if !errors.As(err,&conflict) || conflict.Code!="idempotency_conflict" {t.Fatalf("idempotency conflict: %v",err)}
 _,err=s.CreateUpload(context.Background(),identity,site.ID,"zip",0); if !errors.As(err,&conflict) || conflict.Code!="generation_conflict" {t.Fatalf("stale generation: %v",err)}
 r:=httptest.NewRequest(http.MethodGet,published.URL+"/",nil); w:=httptest.NewRecorder(); s.SiteHandler().ServeHTTP(w,r); if w.Code!=200 || w.Body.String()!="hello" {t.Fatalf("site response: %d %q",w.Code,w.Body.String())}
 r=httptest.NewRequest(http.MethodHead,published.URL+"/",nil); w=httptest.NewRecorder(); s.SiteHandler().ServeHTTP(w,r); if w.Code!=200 || w.Body.Len()!=0 {t.Fatalf("HEAD: %d %q",w.Code,w.Body.String())}
 r=httptest.NewRequest(http.MethodGet,published.URL+"/",nil); r.Header.Set("Range","bytes=1-3"); w=httptest.NewRecorder(); s.SiteHandler().ServeHTTP(w,r); if w.Code!=206 || w.Body.String()!="ell" {t.Fatalf("Range: %d %q",w.Code,w.Body.String())}
 r=httptest.NewRequest(http.MethodGet,published.URL+"/dir",nil); w=httptest.NewRecorder(); s.SiteHandler().ServeHTTP(w,r); if w.Code!=301 || w.Header().Get("Location")!="/dir/" {t.Fatalf("directory: %d %q",w.Code,w.Header().Get("Location"))}
 r=httptest.NewRequest(http.MethodGet,published.URL+"/dir/",nil); w=httptest.NewRecorder(); s.SiteHandler().ServeHTTP(w,r); if w.Code!=200 || w.Body.String()!="nested" {t.Fatalf("directory index: %d %q",w.Code,w.Body.String())}
 replay,err:=s.Publish(context.Background(),identity,site.ID,grant.UploadID,0,"request-1"); if err!=nil || replay!=published {t.Fatalf("replay: %+v %v",replay,err)}
 grant,err=s.CreateUpload(context.Background(),identity,site.ID,"zip",1); if err!=nil {t.Fatal(err)}
 data.Reset(); zw=zip.NewWriter(&data); f,err=zw.Create("index.html"); if err!=nil {t.Fatal(err)}; if _,err=f.Write([]byte("replacement")); err!=nil {t.Fatal(err)}; if err=zw.Close(); err!=nil {t.Fatal(err)}
 if w=request(); w.Code!=200 {t.Fatalf("replacement PUT: %d %s",w.Code,w.Body.String())}
 updated,err:=s.Publish(context.Background(),identity,site.ID,grant.UploadID,1,"request-2"); if err!=nil || updated.Generation!=2 {t.Fatalf("replacement: %+v %v",updated,err)}
 r=httptest.NewRequest(http.MethodGet,updated.URL+"/",nil); w=httptest.NewRecorder(); s.SiteHandler().ServeHTTP(w,r); if w.Code!=200 || w.Body.String()!="replacement" {t.Fatalf("replacement response: %d %q",w.Code,w.Body.String())}
 r=httptest.NewRequest(http.MethodGet,updated.URL+"/old.txt",nil); w=httptest.NewRecorder(); s.SiteHandler().ServeHTTP(w,r); if w.Code!=404 {t.Fatalf("old content still served: %d",w.Code)}
 grant,err=s.CreateUpload(context.Background(),identity,site.ID,"zip",2); if err!=nil {t.Fatal(err)}; if w=request(); w.Code!=200 {t.Fatalf("third PUT: %d %s",w.Code,w.Body.String())}
 if _,err=db.Exec("UPDATE uploads SET state='expired' WHERE id=?",grant.UploadID); err!=nil {t.Fatal(err)}
 _,err=s.Publish(context.Background(),identity,site.ID,grant.UploadID,2,"request-3"); var pubErr *Error; if !errors.As(err,&pubErr) || pubErr.Code!="upload_expired" {t.Fatalf("expired upload: %v",err)}
 if err=s.Delete(context.Background(),1,site.ID); err!=nil {t.Fatal(err)}; if _,err=s.Get(context.Background(),1,site.ID); err==nil {t.Fatal("deleted site remains visible")}
}
package publication

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"

 "static-host/internal/store"
)

func TestSiteMetadata(t *testing.T) {
 dir:=t.TempDir(); db,err:=store.Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 s,err:=New(db,dir,Options{SiteSuffix:"localhost:8081",Local:true}); if err!=nil {t.Fatal(err)}; defer s.Close()
 ctx:=context.Background(); site,err:=s.Create(ctx,1,"  Example  "); if err!=nil {t.Fatal(err)}
 if site.Name!="Example" || site.Generation!=0 || site.Status!="draft" || site.URL!="http://"+site.ID+".localhost:8081" {t.Fatalf("unexpected site: %+v",site)}
 got,err:=s.Get(ctx,1,site.ID); if err!=nil || got!=site {t.Fatalf("get: %+v %v",got,err)}
 _,err=s.Get(ctx,2,site.ID); var e *Error; if !errors.As(err,&e) || e.Code!="site_not_found" {t.Fatalf("foreign site: %v",err)}
 sites,err:=s.List(ctx,1); if err!=nil || len(sites)!=1 || sites[0]!=site {t.Fatalf("list: %+v %v",sites,err)}
 if _,err=s.Create(ctx,1," "); !errors.As(err,&e) || e.Code!="invalid_request" {t.Fatalf("empty name: %v",err)}
}

func TestRecoveryPreservesCommittedContent(t *testing.T) {
 dir:=t.TempDir(); db,err:=store.Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 committed:=strings.Repeat("a",32); orphan:=strings.Repeat("b",32)
 for _,name:=range []string{committed,orphan} {if err=os.MkdirAll(filepath.Join(dir,"sites","content",name),0700); err!=nil {t.Fatal(err)}}
 if _,err=db.Exec("INSERT INTO sites(id,owner_id,name,current_dir,created_at,updated_at) VALUES('s-test',1,'site',?,1,1)",committed); err!=nil {t.Fatal(err)}
 s,err:=New(db,dir,Options{}); if err!=nil {t.Fatal(err)}; if err=s.Close(); err!=nil {t.Fatal(err)}
 if _,err=os.Stat(filepath.Join(dir,"sites","content",committed)); err!=nil {t.Fatal(err)}
 if _,err=os.Stat(filepath.Join(dir,"sites","content",orphan)); !os.IsNotExist(err) {t.Fatalf("orphan remains: %v",err)}
 if err=os.RemoveAll(filepath.Join(dir,"sites","content",committed)); err!=nil {t.Fatal(err)}
 if s,err=New(db,dir,Options{}); err==nil {s.Close(); t.Fatal("missing content accepted")}
}

func TestRecoveryReleasesMissingReadyUpload(t *testing.T) {
 dir:=t.TempDir(); db,err:=store.Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 id:=strings.Repeat("c",32)
 if _,err=db.Exec("INSERT INTO sites(id,owner_id,name,created_at,updated_at) VALUES('site',1,'site',1,1)"); err!=nil {t.Fatal(err)}
 if _,err=db.Exec("INSERT INTO uploads(id,site_id,owner_id,key_id,format,expected_generation,token_hash,state,expires_at,dir) VALUES(?,'site',1,'key','zip',0,x'01','ready',4102444800,?)",id,id); err!=nil {t.Fatal(err)}
 s,err:=New(db,dir,Options{}); if err!=nil {t.Fatal(err)}; defer s.Close()
 var state,code string; if err=db.QueryRow("SELECT state,error_code FROM uploads WHERE id=?",id).Scan(&state,&code); err!=nil {t.Fatal(err)}; if state!="failed" || code!="upload_interrupted" {t.Fatalf("state=%s code=%s",state,code)}
}

func TestSiteLimitAndNameValidation(t *testing.T) {
 dir:=t.TempDir(); db,err:=store.Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 s,err:=New(db,dir,Options{}); if err!=nil {t.Fatal(err)}; defer s.Close()
 for i:=0;i<100;i++ {if _,err=s.Create(context.Background(),1,"site"); err!=nil {t.Fatalf("create %d: %v",i,err)}}
 _,err=s.Create(context.Background(),1,"overflow"); var e *Error; if !errors.As(err,&e) || e.Code!="site_limit" {t.Fatalf("site quota: %v",err)}
 if _,err=db.Exec("UPDATE sites SET deleted_at=1 WHERE id=(SELECT id FROM sites LIMIT 1)"); err!=nil {t.Fatal(err)}
 if _,err=s.Create(context.Background(),1,strings.Repeat("я",80)); err!=nil {t.Fatalf("80 code points: %v",err)}
 if _,err=s.Create(context.Background(),1,strings.Repeat("я",81)); !errors.As(err,&e) || e.Code!="invalid_request" {t.Fatalf("81 code points: %v",err)}
}

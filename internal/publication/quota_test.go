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

func TestReplacementQuotaAndStorageFull(t *testing.T) {
 dir:=t.TempDir(); db,err:=store.Open(dir); if err!=nil {t.Fatal(err)}; defer db.Close()
 old,next:=strings.Repeat("a",32),strings.Repeat("b",32)
 for _,v:=range []struct{area,id,text string}{{"content",old,"old"},{"staging",next,"new"}} {p:=filepath.Join(dir,"sites",v.area,v.id); if err=os.MkdirAll(p,0700); err!=nil {t.Fatal(err)}; if err=os.WriteFile(filepath.Join(p,"index.html"),[]byte(v.text),0600); err!=nil {t.Fatal(err)}}
 if _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at) VALUES('key',1,'agent',x'01',1)"); err!=nil {t.Fatal(err)}
 if _,err=db.Exec("INSERT INTO sites(id,owner_id,name,generation,current_dir,size_bytes,file_count,created_at,updated_at) VALUES('site',1,'site',1,?,?,1,1,1)",old,4<<30); err!=nil {t.Fatal(err)}
 if _,err=db.Exec("INSERT INTO sites(id,owner_id,name,size_bytes,created_at,updated_at) VALUES('other',1,'other',?,1,1)",1<<30); err!=nil {t.Fatal(err)}
 if _,err=db.Exec("INSERT INTO uploads(id,site_id,owner_id,key_id,format,expected_generation,token_hash,state,expires_at,dir,size_bytes,file_count) VALUES(?,'site',1,'key','zip',1,x'01','ready',4102444800,?,?,1)",next,next,4<<30); err!=nil {t.Fatal(err)}
 s,err:=New(db,dir,Options{}); if err!=nil {t.Fatal(err)}; defer s.Close(); identity:=Identity{OwnerID:1,KeyID:"key"}; s.spaceCheck=func(int64)bool{return false}
 _,err=s.Publish(context.Background(),identity,"site",next,1,"request"); var e *Error; if !errors.As(err,&e)||e.Code!="storage_full" {t.Fatalf("storage guard: %v",err)}
 data,err:=os.ReadFile(filepath.Join(dir,"sites","content",old,"index.html")); if err!=nil || string(data)!="old" {t.Fatalf("old content changed: %q %v",data,err)}
 if _,err=db.Exec("UPDATE uploads SET size_bytes=? WHERE id=?",(4<<30)+1,next); err!=nil {t.Fatal(err)}
 s.spaceCheck=func(int64)bool{return true}; _,err=s.Publish(context.Background(),identity,"site",next,1,"request"); if !errors.As(err,&e)||e.Code!="quota_exceeded" {t.Fatalf("quota guard: %v",err)}
 if _,err=db.Exec("UPDATE uploads SET size_bytes=? WHERE id=?",4<<30,next); err!=nil {t.Fatal(err)}
 s.spaceCheck=func(int64)bool{return true}; site,err:=s.Publish(context.Background(),identity,"site",next,1,"request"); if err!=nil || site.Generation!=2 {t.Fatalf("replacement quota: %+v %v",site,err)}
}

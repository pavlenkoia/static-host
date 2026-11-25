package web

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "time"
 "testing"

 "static-host/internal/auth"
 "static-host/internal/store"
)
func TestSession(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 if err=auth.Init(db,"admin","long-password-one"); err!=nil {t.Fatal(err)}
 s,err:=New(db); if err!=nil {t.Fatal(err)}
 request:=func(method,path,body string,cookie *http.Cookie,csrf,origin string)*httptest.ResponseRecorder { r:=httptest.NewRequest(method,"http://127.0.0.1:8080"+path,bytes.NewBufferString(body)); if body!="" {r.Header.Set("Content-Type","application/json")}; if cookie!=nil {r.AddCookie(cookie)}; r.Header.Set("Origin",origin); r.Header.Set("X-CSRF-Token",csrf); w:=httptest.NewRecorder(); s.ServeHTTP(w,r); return w }
 if w:=request("GET","/api/session","",nil,"",""); w.Code!=401 {t.Fatal(w.Code)}
 w:=request("POST","/api/login",`{"login":"admin","password":"long-password-one"}`,nil,"","http://127.0.0.1:8080"); if w.Code!=200 {t.Fatal(w.Code,w.Body)}
 var v session; if err=json.Unmarshal(w.Body.Bytes(),&v); err!=nil {t.Fatal(err)}; c:=w.Result().Cookies()[0]; if !c.HttpOnly || c.SameSite!=http.SameSiteStrictMode {t.Fatal(c)}
 if w=request("POST","/api/logout","",c,"bad","http://127.0.0.1:8080"); w.Code!=403 {t.Fatal(w.Code)}
 if w=request("POST","/api/logout","",c,v.CSRF,"http://127.0.0.1:5173"); w.Code!=204 {t.Fatal(w.Code)}
 if w=request("GET","/api/session","",c,"",""); w.Code!=401 {t.Fatal(w.Code)}
}

func TestKeys(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 if err=auth.Init(db,"admin","long-password-one"); err!=nil {t.Fatal(err)}
 s,err:=New(db); if err!=nil {t.Fatal(err)}
 _,err=db.Exec("INSERT INTO sessions VALUES(?,1,?,?,?)",digest("session"),"csrf",0,s.Now().Unix()+1000); if err!=nil {t.Fatal(err)}
 req:=func(method,path,body string)*httptest.ResponseRecorder {r:=httptest.NewRequest(method,"http://127.0.0.1:8080"+path,bytes.NewBufferString(body)); r.AddCookie(&http.Cookie{Name:"static_host_session",Value:"session"}); r.Header.Set("Origin","http://127.0.0.1:8080"); r.Header.Set("X-CSRF-Token","csrf"); if body!="" {r.Header.Set("Content-Type","application/json")}; w:=httptest.NewRecorder(); s.ServeHTTP(w,r); return w}
 w:=req("POST","/api/keys",`{"name":"agent","expires_in_days":90}`); if w.Code!=201 {t.Fatal(w.Code,w.Body)}
 var result struct { Key key `json:"key"`; Token string `json:"token"` }; if err=json.Unmarshal(w.Body.Bytes(),&result); err!=nil {t.Fatal(err)}
 if _,_,err=AuthenticateKey(db,result.Token,s.Now().Unix()); err!=nil {t.Fatal(err)}
 w=req("GET","/api/keys",""); if bytes.Contains(w.Body.Bytes(),[]byte(result.Token)) {t.Fatal("token leaked")}
 w=req("POST","/api/keys/"+result.Key.ID+"/revoke",""); if w.Code!=204 {t.Fatal(w.Code,w.Body)}
 if _,_,err=AuthenticateKey(db,result.Token,s.Now().Unix()); err==nil {t.Fatal("revoked token accepted")}
}

func TestLoginRateLimit(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 if err=auth.Init(db,"admin","long-password-one"); err!=nil {t.Fatal(err)}
 s,err:=New(db); if err!=nil {t.Fatal(err)}
 now:=time.Unix(100000,0); s.Now=func() time.Time{return now}
 attempt:=func() *httptest.ResponseRecorder {r:=httptest.NewRequest("POST","http://127.0.0.1:8080/api/login",bytes.NewBufferString(`{"login":"unknown","password":"wrong"}`)); r.Header.Set("Origin","http://127.0.0.1:8080"); r.Header.Set("Content-Type","application/json"); w:=httptest.NewRecorder(); s.ServeHTTP(w,r); return w}
 for range 5 { if w:=attempt(); w.Code!=401 {t.Fatal(w.Code)} }
 if w:=attempt(); w.Code!=429 || w.Header().Get("Retry-After")!="300" {t.Fatal(w.Code,w.Header())}
 now=now.Add(5*time.Minute); if w:=attempt(); w.Code!=401 {t.Fatal(w.Code)}
}

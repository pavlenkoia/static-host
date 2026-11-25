package web

import (
 "bytes"
 "crypto/aes"
 "crypto/cipher"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "time"
 "testing"

 "static-host/internal/auth"
 "static-host/internal/store"
)
func testCipher(t *testing.T) cipher.AEAD { t.Helper(); block,err:=aes.NewCipher(make([]byte,32)); if err!=nil {t.Fatal(err)}; c,err:=cipher.NewGCM(block); if err!=nil {t.Fatal(err)}; return c }
func TestSession(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 if err=auth.Init(db,"admin","long-password-one"); err!=nil {t.Fatal(err)}
 s,err:=New(db,testCipher(t)); if err!=nil {t.Fatal(err)}
 s.Host="127.0.0.1:8080"
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
 s,err:=New(db,testCipher(t)); if err!=nil {t.Fatal(err)}
 s.Host="127.0.0.1:8080"
 _,err=db.Exec("INSERT INTO sessions VALUES(?,1,?,?,?)",digest("session"),"csrf",0,s.Now().Unix()+1000); if err!=nil {t.Fatal(err)}
 req:=func(method,path,body string)*httptest.ResponseRecorder {r:=httptest.NewRequest(method,"http://127.0.0.1:8080"+path,bytes.NewBufferString(body)); r.AddCookie(&http.Cookie{Name:"static_host_session",Value:"session"}); r.Header.Set("Origin","http://127.0.0.1:8080"); r.Header.Set("X-CSRF-Token","csrf"); if body!="" {r.Header.Set("Content-Type","application/json")}; w:=httptest.NewRecorder(); s.ServeHTTP(w,r); return w}
 w:=req("POST","/api/keys",`{"name":"agent","expires_in_days":90}`); if w.Code!=201 {t.Fatal(w.Code,w.Body)}
 var result struct { Key key `json:"key"`; Token string `json:"token"` }; if err=json.Unmarshal(w.Body.Bytes(),&result); err!=nil {t.Fatal(err)}
 if _,_,err=AuthenticateKey(db,result.Token,s.Now().Unix()); err!=nil {t.Fatal(err)}
 w=req("GET","/api/keys",""); if bytes.Contains(w.Body.Bytes(),[]byte(result.Token)) {t.Fatal("token leaked")}
 w=req("POST","/api/keys/"+result.Key.ID+"/revoke",""); if w.Code!=204 {t.Fatal(w.Code,w.Body)}
 if _,_,err=AuthenticateKey(db,result.Token,s.Now().Unix()); err==nil {t.Fatal("revoked token accepted")}
 w=req("POST","/api/keys",`{"name":"unlimited","expires_in_days":null}`); if w.Code!=201 {t.Fatal(w.Code,w.Body)}
 var unlimited struct { Key key `json:"key"`; Token string `json:"token"` }; if err=json.Unmarshal(w.Body.Bytes(),&unlimited); err!=nil {t.Fatal(err)}
 if unlimited.Key.Expires!=nil || !unlimited.Key.CanReveal {t.Fatal(unlimited.Key)}
 if _,_,err=AuthenticateKey(db,unlimited.Token,253402300799); err!=nil {t.Fatal(err)}
 w=req("POST","/api/keys/"+unlimited.Key.ID+"/reveal",""); if w.Code!=200 {t.Fatal(w.Code,w.Body)}; var revealed map[string]string; if err=json.Unmarshal(w.Body.Bytes(),&revealed); err!=nil || revealed["token"]!=unlimited.Token {t.Fatal("reveal mismatch",err)}
 var ciphertext []byte; if err=db.QueryRow("SELECT token_ciphertext FROM agent_keys WHERE id=?",unlimited.Key.ID).Scan(&ciphertext); err!=nil || bytes.Contains(ciphertext,[]byte(unlimited.Token)) {t.Fatal("ciphertext invalid",err)}
 for _,body:=range []string{`{"name":"bad","expires_in_days":0}`,`{"name":"bad","expires_in_days":-1}`,`{"name":"bad","expires_in_days":1.5}`,`{"name":"bad","expires_in_days":999999999999}`} { if w=req("POST","/api/keys",body); w.Code!=400 {t.Fatal(body,w.Code)} }
 w=req("POST","/api/keys",`{"name":"long","expires_in_days":730}`); if w.Code!=201 {t.Fatal(w.Code,w.Body)}
 w=req("DELETE","/api/keys/"+unlimited.Key.ID,""); if w.Code!=204 {t.Fatal(w.Code,w.Body)}; if _,_,err=AuthenticateKey(db,unlimited.Token,s.Now().Unix()); err==nil {t.Fatal("deleted token accepted")}
 if _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at,expires_at) VALUES('legacy',1,'legacy',?,1,?)",digest("sh_legacy"),s.Now().Unix()+100); err!=nil {t.Fatal(err)}
 if _,_,err=AuthenticateKey(db,"sh_legacy",s.Now().Unix()); err!=nil {t.Fatal(err)}
 if w=req("POST","/api/keys/legacy/reveal",""); w.Code!=409 {t.Fatal(w.Code)}
 if w=req("GET","/api/keys",""); w.Code!=200 || !bytes.Contains(w.Body.Bytes(),[]byte(`"can_reveal":false`)) {t.Fatal(w.Code,w.Body)}
 if w=req("DELETE","/api/keys/legacy",""); w.Code!=204 {t.Fatal(w.Code)}
 w=req("POST","/api/keys",`{"name":"restart"}`); if w.Code!=201 {t.Fatal(w.Code,w.Body)}; var persisted struct { Key key `json:"key"`; Token string `json:"token"` }; if err=json.Unmarshal(w.Body.Bytes(),&persisted); err!=nil {t.Fatal(err)}
 for _,tc:=range []struct{cookie bool; csrf,origin string; want int}{{false,"csrf","http://127.0.0.1:8080",401},{true,"bad","http://127.0.0.1:8080",403},{true,"csrf","http://invalid",403}} {
  r:=httptest.NewRequest("POST","http://127.0.0.1:8080/api/keys/"+persisted.Key.ID+"/reveal",nil); if tc.cookie {r.AddCookie(&http.Cookie{Name:"static_host_session",Value:"session"})}; r.Header.Set("Origin",tc.origin); r.Header.Set("X-CSRF-Token",tc.csrf); rec:=httptest.NewRecorder(); s.ServeHTTP(rec,r); if rec.Code!=tc.want || bytes.Contains(rec.Body.Bytes(),[]byte(persisted.Token)) {t.Fatal(rec.Code,tc.want)}
 }
 s,err=New(db,testCipher(t)); if err!=nil {t.Fatal(err)}; s.Host="127.0.0.1:8080"; if w=req("POST","/api/keys/"+persisted.Key.ID+"/reveal",""); w.Code!=200 || !bytes.Contains(w.Body.Bytes(),[]byte(persisted.Token)) {t.Fatal(w.Code)}
 if _,err=db.Exec("UPDATE agent_keys SET token_ciphertext=x'01' WHERE id=?",persisted.Key.ID); err!=nil {t.Fatal(err)}; if w=req("POST","/api/keys/"+persisted.Key.ID+"/reveal",""); w.Code!=503 {t.Fatal(w.Code)}
 if w=req("POST","/api/keys/"+unlimited.Key.ID+"/reveal",""); w.Code!=404 {t.Fatal(w.Code)}
}

func TestLoginRateLimit(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 if err=auth.Init(db,"admin","long-password-one"); err!=nil {t.Fatal(err)}
 s,err:=New(db,testCipher(t)); if err!=nil {t.Fatal(err)}
 s.Host="127.0.0.1:8080"
 now:=time.Unix(100000,0); s.Now=func() time.Time{return now}
 attempt:=func() *httptest.ResponseRecorder {r:=httptest.NewRequest("POST","http://127.0.0.1:8080/api/login",bytes.NewBufferString(`{"login":"unknown","password":"wrong"}`)); r.Header.Set("Origin","http://127.0.0.1:8080"); r.Header.Set("Content-Type","application/json"); w:=httptest.NewRecorder(); s.ServeHTTP(w,r); return w}
 for range 5 { if w:=attempt(); w.Code!=401 {t.Fatal(w.Code)} }
 if w:=attempt(); w.Code!=429 || w.Header().Get("Retry-After")!="300" {t.Fatal(w.Code,w.Header())}
 now=now.Add(5*time.Minute); if w:=attempt(); w.Code!=401 {t.Fatal(w.Code)}
}

func TestConfiguredHostAndOrigin(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 s,err:=New(db,testCipher(t)); if err!=nil {t.Fatal(err)}; s.Host="127.0.0.1:23456"
 for _,tc:=range []struct{host,origin string; want int}{{"127.0.0.1:23456","http://127.0.0.1:23456",401},{"127.0.0.1:23456","http://127.0.0.1:8080",403},{"127.0.0.1:8080","http://127.0.0.1:23456",403}} {
  r:=httptest.NewRequest("POST","http://"+tc.host+"/api/keys/id/reveal",nil);r.Header.Set("Origin",tc.origin);w:=httptest.NewRecorder();s.ServeHTTP(w,r);if w.Code!=tc.want {t.Fatalf("host=%s origin=%s: %d, want %d",tc.host,tc.origin,w.Code,tc.want)}
 }
}

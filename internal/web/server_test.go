package web

import (
 "bytes"
 "crypto/aes"
 "crypto/cipher"
 "database/sql"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "time"
 "testing"

 "static-host/internal/publication"
 "static-host/internal/store"
)
func testCipher(t *testing.T) cipher.AEAD { t.Helper(); block,err:=aes.NewCipher(make([]byte,32)); if err!=nil {t.Fatal(err)}; c,err:=cipher.NewGCM(block); if err!=nil {t.Fatal(err)}; return c }
func testPublications(t *testing.T,db *sql.DB) *publication.Service {t.Helper(); s,err:=publication.New(db,t.TempDir(),publication.Options{UploadOrigin:"http://127.0.0.1:8080",SiteSuffix:"localhost:8080",Local:true}); if err!=nil {t.Fatal(err)}; t.Cleanup(func(){s.Close()}); return s}
func TestSession(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 s,err:=New(db,testCipher(t),"http://127.0.0.1:8080","long-password-one",testPublications(t,db)); if err!=nil {t.Fatal(err)}
 request:=func(method,path,body string,cookie *http.Cookie,csrf,origin string)*httptest.ResponseRecorder { r:=httptest.NewRequest(method,"http://127.0.0.1:8080"+path,bytes.NewBufferString(body)); if body!="" {r.Header.Set("Content-Type","application/json")}; if cookie!=nil {r.AddCookie(cookie)}; r.Header.Set("Origin",origin); r.Header.Set("X-CSRF-Token",csrf); w:=httptest.NewRecorder(); s.ServeHTTP(w,r); return w }
 if w:=request("GET","/api/session","",nil,"",""); w.Code!=401 {t.Fatal(w.Code)}
 w:=request("POST","/api/login",`{"login":"admin","password":"long-password-one"}`,nil,"","http://127.0.0.1:8080"); if w.Code!=200 {t.Fatal(w.Code,w.Body)}
 var v session; if err=json.Unmarshal(w.Body.Bytes(),&v); err!=nil {t.Fatal(err)}; c:=w.Result().Cookies()[0]; if !c.HttpOnly || c.SameSite!=http.SameSiteStrictMode {t.Fatal(c)}
 if w=request("POST","/api/logout","",c,"bad","http://127.0.0.1:8080"); w.Code!=403 {t.Fatal(w.Code)}
 if w=request("POST","/api/logout","",c,v.CSRF,"http://127.0.0.1:8080"); w.Code!=204 {t.Fatal(w.Code)}
 if w=request("GET","/api/session","",c,"",""); w.Code!=401 {t.Fatal(w.Code)}
}

func TestKeys(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 s,err:=New(db,testCipher(t),"http://127.0.0.1:8080","long-password-one",testPublications(t,db)); if err!=nil {t.Fatal(err)}
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
 s,err=New(db,testCipher(t),"http://127.0.0.1:8080","long-password-one",s.Publications); if err!=nil {t.Fatal(err)}; if w=req("POST","/api/keys/"+persisted.Key.ID+"/reveal",""); w.Code!=200 || !bytes.Contains(w.Body.Bytes(),[]byte(persisted.Token)) {t.Fatal(w.Code)}
 if _,err=db.Exec("UPDATE agent_keys SET token_ciphertext=x'01' WHERE id=?",persisted.Key.ID); err!=nil {t.Fatal(err)}; if w=req("POST","/api/keys/"+persisted.Key.ID+"/reveal",""); w.Code!=503 {t.Fatal(w.Code)}
 if w=req("POST","/api/keys/"+unlimited.Key.ID+"/reveal",""); w.Code!=404 {t.Fatal(w.Code)}
}

func TestLoginRateLimit(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 s,err:=New(db,testCipher(t),"http://127.0.0.1:8080","long-password-one",testPublications(t,db)); if err!=nil {t.Fatal(err)}
 now:=time.Unix(100000,0); s.Now=func() time.Time{return now}
 attempt:=func() *httptest.ResponseRecorder {r:=httptest.NewRequest("POST","http://127.0.0.1:8080/api/login",bytes.NewBufferString(`{"login":"unknown","password":"wrong"}`)); r.Header.Set("Origin","http://127.0.0.1:8080"); r.Header.Set("Content-Type","application/json"); w:=httptest.NewRecorder(); s.ServeHTTP(w,r); return w}
 for range 5 { if w:=attempt(); w.Code!=401 {t.Fatal(w.Code)} }
 if w:=attempt(); w.Code!=429 || w.Header().Get("Retry-After")!="300" {t.Fatal(w.Code,w.Header())}
 now=now.Add(5*time.Minute); if w:=attempt(); w.Code!=401 {t.Fatal(w.Code)}
}

func TestConfiguredOrigin(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 s,err:=New(db,testCipher(t),"http://127.0.0.1:23456","long-password-one",testPublications(t,db)); if err!=nil {t.Fatal(err)}
 for _,tc:=range []struct{origin string; want int}{{"http://127.0.0.1:23456",401},{"http://127.0.0.1:8080",403},{"http://127.0.0.1:5173",403},{"",403}} {
  r:=httptest.NewRequest("POST","http://127.0.0.1:23456/api/keys/id/reveal",nil);r.Header.Set("Origin",tc.origin);w:=httptest.NewRecorder();s.ServeHTTP(w,r);if w.Code!=tc.want {t.Fatalf("origin=%s: %d, want %d",tc.origin,w.Code,tc.want)}
 }
 s,err=New(db,testCipher(t),"http://192.0.2.10:8081","long-password-one",s.Publications); if err!=nil {t.Fatal(err)}
 for _,tc:=range []struct{origin string; want int}{{"http://192.0.2.10:8081",401},{"http://192.0.2.11:8081",403},{"http://192.0.2.10:8082",403}} {
  r:=httptest.NewRequest("POST","http://192.0.2.10:8081/api/keys/id/reveal",nil); r.Header.Set("Origin",tc.origin); w:=httptest.NewRecorder(); s.ServeHTTP(w,r); if w.Code!=tc.want {t.Fatalf("origin=%s: %d, want %d",tc.origin,w.Code,tc.want)}
 }
}

func TestSessionExpirationBoundary(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := New(db, testCipher(t), "http://127.0.0.1:8080", "long-password-one", testPublications(t,db))
	if err != nil {
		t.Fatal(err)
	}
	const now = int64(100000)
	s.Now = func() time.Time { return time.Unix(now-1, 0) }
	if _, err := db.Exec("INSERT INTO sessions VALUES(?,1,'csrf',?,?)", digest("expiring"), now-10, now); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, bytes.NewBufferString(body))
		r.AddCookie(&http.Cookie{Name: "static_host_session", Value: "expiring"})
		r.Header.Set("Origin", "http://127.0.0.1:8080")
		r.Header.Set("X-CSRF-Token", "csrf")
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}

	if w := request("GET", "/api/session", ""); w.Code != http.StatusOK {
		t.Fatalf("session rejected before expiration: %d", w.Code)
	}
	if w := request("POST", "/api/keys", `{"name":"before"}`); w.Code != http.StatusCreated {
		t.Fatalf("mutation rejected before expiration: %d %s", w.Code, w.Body)
	}
	s.Now = func() time.Time { return time.Unix(now, 0) }
	if w := request("GET", "/api/session", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("session accepted at expiration boundary: %d", w.Code)
	}
	if w := request("POST", "/api/keys", `{"name":"after"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("mutation accepted at expiration boundary: %d %s", w.Code, w.Body)
	}
	if w := request("POST", "/api/logout", ""); w.Code != http.StatusNoContent {
		t.Fatalf("logout of expired session: %d %s", w.Code, w.Body)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM agent_keys").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expired session mutation persisted, got %d keys", count)
	}
}

func TestPasswordRotationDoesNotPersistCredentials(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 oldServer,err:=New(db,testCipher(t),"http://127.0.0.1:8080","old-admin-password",testPublications(t,db))
 if err!=nil {t.Fatal(err)}
 request:=func(s *Server,password string)*httptest.ResponseRecorder { body:=`{"login":"admin","password":"`+password+`"}`;r:=httptest.NewRequest("POST","http://127.0.0.1:8080/api/login",bytes.NewBufferString(body));r.Header.Set("Content-Type","application/json");r.Header.Set("Origin","http://127.0.0.1:8080");w:=httptest.NewRecorder();s.ServeHTTP(w,r);return w }
 if w:=request(oldServer,"old-admin-password");w.Code!=200 {t.Fatal(w.Code,w.Body)}
 _,err=db.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at,expires_at,token_ciphertext) VALUES('preserved',1,'agent',x'0102',1,NULL,x'0304')")
 if err!=nil {t.Fatal(err)}
 rotated,err:=New(db,testCipher(t),"http://127.0.0.1:8080","new-admin-password",oldServer.Publications)
 if err!=nil {t.Fatal(err)}
 if w:=request(rotated,"old-admin-password");w.Code!=401 {t.Fatalf("old password accepted: %d",w.Code)}
 if w:=request(rotated,"new-admin-password");w.Code!=200 {t.Fatalf("new password rejected: %d",w.Code)}
 var id string;var tokenHash,ciphertext []byte
 if err=db.QueryRow("SELECT id,token_hash,token_ciphertext FROM agent_keys WHERE id='preserved'").Scan(&id,&tokenHash,&ciphertext);err!=nil||id!="preserved"||!bytes.Equal(tokenHash,[]byte{1,2})||!bytes.Equal(ciphertext,[]byte{3,4}) {t.Fatalf("key data changed: %s %x %x %v",id,tokenHash,ciphertext,err)}
 if _,err=db.Exec("SELECT login FROM users");err==nil {t.Fatal("login credential persisted in database")}
 if _,err=db.Exec("SELECT password_hash FROM users");err==nil {t.Fatal("password hash persisted in database")}
}

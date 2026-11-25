package web

import (
 "crypto/cipher"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "database/sql"
 "embed"
 "encoding/base64"
 "encoding/json"
 "errors"
 "io"
 "mime"
 "net/http"
 "strings"
 "strconv"
 "sync"
 "time"

 "static-host/internal/auth"
)

//go:embed dist
var assets embed.FS

type Server struct { DB *sql.DB; KeyCipher cipher.AEAD; Origin string; cookieName string; secureCookie bool; Now func() time.Time; mu sync.Mutex; failures []time.Time; passwordHash string }
func New(db *sql.DB, keyCipher cipher.AEAD, origin, password string) (*Server,error) { if keyCipher==nil { return nil,errors.New("nil key cipher") }; if origin=="" {return nil,errors.New("empty origin")}; if !auth.ValidPassword(password) { return nil, errors.New("STATIC_HOST_ADMIN_PASSWORD must be 12–72 bytes") }; hash,err:=auth.Hash(password); if err!=nil {return nil,err}; secure:=strings.HasPrefix(origin,"https://"); name:="static_host_session"; if secure {name="__Host-static_host_session"}; return &Server{DB:db,KeyCipher:keyCipher,Origin:origin,cookieName:name,secureCookie:secure,Now:time.Now,passwordHash:hash},nil }
func fmtSeconds(d time.Duration) string { return strconv.FormatInt(int64((d+time.Second-1)/time.Second),10) }
func random(n int) (string,error) { b:=make([]byte,n); if _,err:=rand.Read(b); err!=nil{return "",err}; return base64.RawURLEncoding.EncodeToString(b),nil }
func digest(s string) []byte { h:=sha256.Sum256([]byte(s)); return h[:] }
func jsonError(w http.ResponseWriter, code int, text string) { jsonReply(w,code,map[string]string{"error":text}) }
func jsonReply(w http.ResponseWriter, code int, v any) { w.Header().Set("Content-Type","application/json"); w.WriteHeader(code); _=json.NewEncoder(w).Encode(v) }
func decode(w http.ResponseWriter,r *http.Request,v any) bool { ct,_,_:=mime.ParseMediaType(r.Header.Get("Content-Type")); if ct!="application/json" { jsonError(w,415,"unsupported_media_type"); return false }; r.Body=http.MaxBytesReader(w,r.Body,4096); d:=json.NewDecoder(r.Body); d.DisallowUnknownFields(); if d.Decode(v)!=nil { jsonError(w,400,"invalid_request"); return false }; var extra any; if d.Decode(&extra)!=io.EOF { jsonError(w,400,"invalid_request"); return false }; return true }
func (s *Server) origin(r *http.Request) bool { values:=r.Header.Values("Origin"); return len(values)==1 && values[0]==s.Origin }
func emptyBody(w http.ResponseWriter,r *http.Request) bool { var b [1]byte; n,err:=r.Body.Read(b[:]); if n!=0 || (err!=nil && err!=io.EOF) { jsonError(w,400,"invalid_request"); return false }; return true }
func (s *Server) ServeHTTP(w http.ResponseWriter,r *http.Request) {
 w.Header().Set("X-Content-Type-Options","nosniff"); w.Header().Set("Referrer-Policy","no-referrer"); w.Header().Set("Cache-Control","no-store")
 if strings.HasPrefix(r.URL.Path,"/api/") { if r.Method!="GET" && r.Method!="HEAD" && !s.origin(r) { jsonError(w,403,"forbidden"); return }; s.api(w,r); return }
 if r.URL.Path=="/" || r.URL.Path=="/login" { if r.Method!="GET" && r.Method!="HEAD" { http.Error(w,"Method not allowed",405); return }; w.Header().Set("Content-Security-Policy","default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'"); b,_:=assets.ReadFile("dist/index.html"); w.Header().Set("Content-Type","text/html; charset=utf-8"); if r.Method=="GET" { _,_=w.Write(b) }; return }
 if strings.HasPrefix(r.URL.Path,"/assets/") { b,err:=assets.ReadFile("dist"+r.URL.Path); if err!=nil { http.NotFound(w,r); return }; w.Header().Set("Cache-Control","public, max-age=31536000, immutable"); if ct:=mime.TypeByExtension(r.URL.Path[strings.LastIndex(r.URL.Path,"."):]); ct!="" { w.Header().Set("Content-Type",ct) }; if r.Method=="GET" { _,_=w.Write(b) } else if r.Method!="HEAD" { http.Error(w,"Method not allowed",405) }; return }
 http.NotFound(w,r)
}
type session struct { Login string `json:"login"`; CSRF string `json:"csrf_token"` }
func (s *Server) session(r *http.Request) (session,error) { var result session; c,err:=r.Cookie(s.cookieName); if err!=nil { return result,sql.ErrNoRows }; err=s.DB.QueryRow(`SELECT csrf_token FROM sessions WHERE token_hash=? AND expires_at>?`,digest(c.Value),s.Now().Unix()).Scan(&result.CSRF); result.Login=auth.AdminLogin; return result,err }
func (s *Server) require(w http.ResponseWriter,r *http.Request, mutation bool) (session,bool) { v,err:=s.session(r); if errors.Is(err,sql.ErrNoRows) { jsonError(w,401,"unauthorized"); return v,false }; if err!=nil { jsonError(w,503,"service_unavailable"); return v,false }; if mutation && subtle.ConstantTimeCompare([]byte(v.CSRF),[]byte(r.Header.Get("X-CSRF-Token")))!=1 { jsonError(w,403,"forbidden"); return v,false }; return v,true }
func (s *Server) api(w http.ResponseWriter,r *http.Request) {
 switch r.URL.Path {
 case "/api/session": if r.Method!="GET" { jsonError(w,405,"method_not_allowed"); return }; if v,ok:=s.require(w,r,false); ok { jsonReply(w,200,v) }
 case "/api/login": if r.Method!="POST" { jsonError(w,405,"method_not_allowed"); return }; s.login(w,r)
 case "/api/logout": if r.Method!="POST" { jsonError(w,405,"method_not_allowed"); return }; if !emptyBody(w,r) { return }; v,err:=s.session(r); if err!=nil && !errors.Is(err,sql.ErrNoRows) { jsonError(w,503,"service_unavailable"); return }; if err==nil { if subtle.ConstantTimeCompare([]byte(v.CSRF),[]byte(r.Header.Get("X-CSRF-Token")))!=1 { jsonError(w,403,"forbidden"); return }; c,_:=r.Cookie(s.cookieName); if _,err=s.DB.Exec("DELETE FROM sessions WHERE token_hash=?",digest(c.Value)); err!=nil { jsonError(w,503,"service_unavailable"); return } }; http.SetCookie(w,&http.Cookie{Name:s.cookieName,Path:"/",Secure:s.secureCookie,HttpOnly:true,SameSite:http.SameSiteStrictMode,MaxAge:-1}); w.WriteHeader(204)
 default: if r.URL.Path=="/api/keys" || strings.HasPrefix(r.URL.Path,"/api/keys/") { s.keys(w,r) } else { jsonError(w,404,"not_found") }
 }
}
func (s *Server) login(w http.ResponseWriter,r *http.Request) {
 var input struct { Login *string `json:"login"`; Password *string `json:"password"` }; if !decode(w,r,&input) { return }; if input.Login==nil || input.Password==nil {jsonError(w,400,"invalid_request"); return}
 s.mu.Lock(); defer s.mu.Unlock(); now:=s.Now(); cutoff:=now.Add(-5*time.Minute); for len(s.failures)>0 && !s.failures[0].After(cutoff) { s.failures=s.failures[1:] }; if len(s.failures)>=5 { w.Header().Set("Retry-After",fmtSeconds(s.failures[0].Add(5*time.Minute).Sub(now))); jsonError(w,429,"rate_limited"); return }
 comparisonPassword:=*input.Password
 passwordTooLong:=len(comparisonPassword)>72
 if passwordTooLong { comparisonPassword="constant-time-invalid-password" }
 validPassword:=auth.Compare(s.passwordHash,comparisonPassword) && !passwordTooLong
 validLogin:=subtle.ConstantTimeCompare(digest(*input.Login),digest(auth.AdminLogin))==1
 if !validLogin || !validPassword { s.failures=append(s.failures,now); jsonError(w,401,"invalid_credentials"); return }
 token,err:=random(32); if err!=nil { jsonError(w,503,"service_unavailable"); return }; csrf,err:=random(32); if err!=nil { jsonError(w,503,"service_unavailable"); return }; _,err=s.DB.Exec("DELETE FROM sessions WHERE expires_at<=?",now.Unix()); if err==nil { _,err=s.DB.Exec("INSERT INTO sessions VALUES(?,1,?,?,?)",digest(token),csrf,now.Unix(),now.Add(12*time.Hour).Unix()) }; if err!=nil { jsonError(w,503,"service_unavailable"); return }; s.failures=nil; http.SetCookie(w,&http.Cookie{Name:s.cookieName,Value:token,Path:"/",Secure:s.secureCookie,HttpOnly:true,SameSite:http.SameSiteStrictMode,MaxAge:43200}); jsonReply(w,200,session{auth.AdminLogin,csrf})
}

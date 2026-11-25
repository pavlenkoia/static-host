package web

import (
 "crypto/rand"
 "database/sql"
 "errors"
 "net/http"
 "strings"
 "unicode/utf8"
)

type key struct { ID string `json:"id"`; Name string `json:"name"`; Created int64 `json:"created_at"`; Expires *int64 `json:"expires_at"`; Revoked *int64 `json:"revoked_at"`; Status string `json:"status"`; CanReveal bool `json:"can_reveal"` }
func (s *Server) keys(w http.ResponseWriter,r *http.Request) {
 if r.URL.Path=="/api/keys" {
  if r.Method!="GET" && r.Method!="POST" { jsonError(w,405,"method_not_allowed"); return }
  if _,ok:=s.require(w,r,r.Method=="POST"); !ok {return}
  if r.Method=="GET" {
   rows,err:=s.DB.Query("SELECT id,name,created_at,expires_at,revoked_at,token_ciphertext IS NOT NULL FROM agent_keys WHERE owner_id=1 ORDER BY created_at DESC,id ASC"); if err!=nil {jsonError(w,503,"service_unavailable"); return}; defer rows.Close()
   keys:=[]key{}; now:=s.Now().Unix(); for rows.Next() { var k key; if err=rows.Scan(&k.ID,&k.Name,&k.Created,&k.Expires,&k.Revoked,&k.CanReveal); err!=nil {jsonError(w,503,"service_unavailable"); return}; k.Status="active"; if k.Revoked!=nil {k.Status="revoked"} else if k.Expires!=nil && now>=*k.Expires {k.Status="expired"}; keys=append(keys,k) }; if rows.Err()!=nil {jsonError(w,503,"service_unavailable"); return}; jsonReply(w,200,map[string]any{"keys":keys}); return
  }
  var input struct { Name string `json:"name"`; Days *int64 `json:"expires_in_days"` }; if !decode(w,r,&input) {return}; input.Name=strings.TrimSpace(input.Name); now:=s.Now().Unix()
  if !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name)<1 || utf8.RuneCountInString(input.Name)>80 || (input.Days!=nil && (*input.Days<1 || now>253402300799 || *input.Days>(253402300799-now)/86400)) {jsonError(w,400,"invalid_request"); return}
  id,err:=random(16); if err!=nil {jsonError(w,503,"service_unavailable"); return}; secret,err:=random(32); if err!=nil {jsonError(w,503,"service_unavailable"); return}; token:="sh_"+secret
  nonce:=make([]byte,s.KeyCipher.NonceSize()); if _,err=rand.Read(nonce); err!=nil {jsonError(w,503,"service_unavailable"); return}; encrypted:=s.KeyCipher.Seal(nonce,nonce,[]byte(token),[]byte(id))
  k:=key{ID:id,Name:input.Name,Created:now,Status:"active",CanReveal:true}; if input.Days!=nil { expires:=now+*input.Days*86400; k.Expires=&expires }
  _,err=s.DB.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at,expires_at,token_ciphertext) VALUES(?,1,?,?,?,?,?)",k.ID,k.Name,digest(token),k.Created,k.Expires,encrypted); if err!=nil {jsonError(w,503,"service_unavailable"); return}; jsonReply(w,201,map[string]any{"key":k,"token":token}); return
 }
 parts:=strings.Split(strings.TrimPrefix(r.URL.Path,"/api/keys/"),"/"); if len(parts)<1 || parts[0]=="" || len(parts)>2 || (len(parts)==2 && parts[1]!="revoke" && parts[1]!="reveal") {jsonError(w,404,"not_found"); return}
 if (len(parts)==1 && r.Method!="DELETE") || (len(parts)==2 && r.Method!="POST") {jsonError(w,405,"method_not_allowed"); return}
 if _,ok:=s.require(w,r,true); !ok {return}; if !emptyBody(w,r) {return}; id:=parts[0]
 if len(parts)==2 && parts[1]=="reveal" {
  var encrypted []byte; err:=s.DB.QueryRow("SELECT token_ciphertext FROM agent_keys WHERE id=? AND owner_id=1",id).Scan(&encrypted); if errors.Is(err,sql.ErrNoRows) {jsonError(w,404,"not_found"); return}; if err!=nil {jsonError(w,503,"service_unavailable"); return}; if encrypted==nil {jsonError(w,409,"token_unavailable"); return}; n:=s.KeyCipher.NonceSize(); if len(encrypted)<n {jsonError(w,503,"service_unavailable"); return}; plain,err:=s.KeyCipher.Open(nil,encrypted[:n],encrypted[n:],[]byte(id)); if err!=nil {jsonError(w,503,"service_unavailable"); return}; jsonReply(w,200,map[string]string{"token":string(plain)}); return
 }
 var res sql.Result; var err error; if len(parts)==1 {res,err=s.DB.Exec("DELETE FROM agent_keys WHERE id=? AND owner_id=1",id)} else {res,err=s.DB.Exec("UPDATE agent_keys SET revoked_at=COALESCE(revoked_at,?) WHERE id=? AND owner_id=1",s.Now().Unix(),id)}; if err!=nil {jsonError(w,503,"service_unavailable"); return}; n,err:=res.RowsAffected(); if err!=nil {jsonError(w,503,"service_unavailable"); return}; if n==0 {jsonError(w,404,"not_found"); return}; w.WriteHeader(204)
}
func AuthenticateKey(db *sql.DB, token string, now int64) (string,int64,error) { var id string; var owner int64; err:=db.QueryRow("SELECT id,owner_id FROM agent_keys WHERE token_hash=? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>?)",digest(token),now).Scan(&id,&owner); if errors.Is(err,sql.ErrNoRows) {return "",0,sql.ErrNoRows}; return id,owner,err }

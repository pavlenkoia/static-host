package web

import (
 "database/sql"
 "errors"
 "net/http"
 "strings"
 "unicode/utf8"
)

type key struct { ID string `json:"id"`; Name string `json:"name"`; Created int64 `json:"created_at"`; Expires int64 `json:"expires_at"`; Revoked *int64 `json:"revoked_at"`; Status string `json:"status"` }
func (s *Server) keys(w http.ResponseWriter,r *http.Request) {
 if r.URL.Path=="/api/keys" {
  if r.Method!="GET" && r.Method!="POST" { jsonError(w,405,"method_not_allowed"); return }
  if _,ok:=s.require(w,r,r.Method=="POST"); !ok {return}
  if r.Method=="GET" { rows,err:=s.DB.Query("SELECT id,name,created_at,expires_at,revoked_at FROM agent_keys WHERE owner_id=1 ORDER BY created_at DESC,id ASC"); if err!=nil {jsonError(w,503,"service_unavailable"); return}; defer rows.Close(); keys:=[]key{}; now:=s.Now().Unix(); for rows.Next() { var k key; if err=rows.Scan(&k.ID,&k.Name,&k.Created,&k.Expires,&k.Revoked); err!=nil {jsonError(w,503,"service_unavailable"); return}; k.Status="active"; if k.Revoked!=nil {k.Status="revoked"} else if now>=k.Expires {k.Status="expired"}; keys=append(keys,k) }; if rows.Err()!=nil {jsonError(w,503,"service_unavailable"); return}; jsonReply(w,200,map[string]any{"keys":keys}); return }
  var input struct { Name string `json:"name"`; Days int `json:"expires_in_days"` }; if !decode(w,r,&input) {return}; input.Name=strings.TrimSpace(input.Name); if !utf8.ValidString(input.Name) || utf8.RuneCountInString(input.Name)<1 || utf8.RuneCountInString(input.Name)>80 || input.Days<1 || input.Days>365 {jsonError(w,400,"invalid_request"); return}
  id,err:=random(16); if err!=nil {jsonError(w,503,"service_unavailable"); return}; secret,err:=random(32); if err!=nil {jsonError(w,503,"service_unavailable"); return}; token:="sh_"+secret; now:=s.Now().Unix(); k:=key{ID:id,Name:input.Name,Created:now,Expires:now+int64(input.Days)*86400,Status:"active"}; _,err=s.DB.Exec("INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at,expires_at) VALUES(?,1,?,?,?,?)",k.ID,k.Name,digest(token),k.Created,k.Expires); if err!=nil {jsonError(w,503,"service_unavailable"); return}; jsonReply(w,201,map[string]any{"key":k,"token":token}); return
 }
 id:=strings.TrimSuffix(strings.TrimPrefix(r.URL.Path,"/api/keys/"),"/revoke"); if !strings.HasPrefix(r.URL.Path,"/api/keys/") || !strings.HasSuffix(r.URL.Path,"/revoke") || id=="" || strings.Contains(id,"/") {jsonError(w,404,"not_found"); return}; if r.Method!="POST" {jsonError(w,405,"method_not_allowed"); return}; if _,ok:=s.require(w,r,true); !ok {return}; if !emptyBody(w,r) {return}; res,err:=s.DB.Exec("UPDATE agent_keys SET revoked_at=COALESCE(revoked_at,?) WHERE id=? AND owner_id=1",s.Now().Unix(),id); if err!=nil {jsonError(w,503,"service_unavailable"); return}; n,err:=res.RowsAffected(); if err!=nil {jsonError(w,503,"service_unavailable"); return}; if n==0 {jsonError(w,404,"not_found"); return}; w.WriteHeader(204)
}
func AuthenticateKey(db *sql.DB, token string, now int64) (string,int64,error) { var id string; var owner int64; err:=db.QueryRow("SELECT id,owner_id FROM agent_keys WHERE token_hash=? AND revoked_at IS NULL AND expires_at>?",digest(token),now).Scan(&id,&owner); if errors.Is(err,sql.ErrNoRows) {return "",0,sql.ErrNoRows}; return id,owner,err }

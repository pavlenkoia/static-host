package publication

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "crypto/subtle"
 "database/sql"
 "encoding/base64"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "syscall"
 "time"
)

const uploadTTL = 15*time.Minute
const storageReserve = int64(512<<20)
type UploadLimits struct { ArchiveBytes int64 `json:"archive_bytes"`; ExpandedBytes int64 `json:"expanded_bytes"`; Files int `json:"files"`; Sites int `json:"sites"`; OwnerBytes int64 `json:"owner_bytes"` }
type UploadGrant struct { UploadID string `json:"upload_id"`; UploadURL string `json:"upload_url"`; Method string `json:"method"`; ExpiresAt int64 `json:"expires_at"`; Format string `json:"format"`; Limits UploadLimits `json:"limits"`; Headers map[string]string `json:"headers"` }
func (s *Service) keyValid(ctx context.Context,identity Identity) bool {var count int; err:=s.db.QueryRowContext(ctx,"SELECT count(*) FROM agent_keys WHERE id=? AND owner_id=? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at>?)",identity.KeyID,identity.OwnerID,s.now().Unix()).Scan(&count); return err==nil && count==1}
func (s *Service) freeSpace(required int64) bool {if s.spaceCheck!=nil {return s.spaceCheck(required)}; var st syscall.Statfs_t; if syscall.Statfs(filepath.Join(s.dataDir,"sites"),&st)!=nil {return false}; return st.Bavail*uint64(st.Bsize)>=uint64(required)}
func (s *Service) CreateUpload(ctx context.Context,identity Identity,siteID,format string,expectedGeneration int64) (UploadGrant,error) {
 if format!="zip" && format!="tar.gz" || expectedGeneration<0 {return UploadGrant{},failure("invalid_request")}
 s.mu.Lock(); defer s.mu.Unlock()
 if !s.keyValid(ctx,identity) {return UploadGrant{},failure("invalid_request")}
 var generation int64; err:=s.db.QueryRowContext(ctx,"SELECT generation FROM sites WHERE id=? AND owner_id=? AND deleted_at IS NULL",siteID,identity.OwnerID).Scan(&generation); if errors.Is(err,sql.ErrNoRows) {return UploadGrant{},failure("site_not_found")}; if err!=nil {return UploadGrant{},err}; if generation!=expectedGeneration {return UploadGrant{},failure("generation_conflict")}
 now:=s.now().Unix(); if _,err=s.db.ExecContext(ctx,"UPDATE uploads SET state='expired',error_code='upload_expired' WHERE state IN ('pending','ready') AND expires_at<=?",now); err!=nil {return UploadGrant{},err}
 var count int; if err=s.db.QueryRowContext(ctx,"SELECT count(*) FROM uploads WHERE state IN ('pending','receiving','ready')").Scan(&count); err!=nil {return UploadGrant{},err}; if count!=0 {return UploadGrant{},failure("upload_busy")}; if !s.freeSpace(maxArchiveBytes+maxExpandedBytes+storageReserve) {return UploadGrant{},failure("storage_full")}
 id,err:=randomID(); if err!=nil {return UploadGrant{},err}; var secret [32]byte; if _,err=rand.Read(secret[:]); err!=nil {return UploadGrant{},err}; token:=base64.RawURLEncoding.EncodeToString(secret[:]); digest:=sha256.Sum256([]byte(token)); expires:=now+int64(uploadTTL/time.Second)
 if _,err=s.db.ExecContext(ctx,"INSERT INTO uploads(id,site_id,owner_id,key_id,format,expected_generation,token_hash,state,expires_at) VALUES(?,?,?,?,?,?,?,'pending',?)",id,siteID,identity.OwnerID,identity.KeyID,format,expectedGeneration,digest[:],expires); err!=nil {return UploadGrant{},err}
 contentType:="application/zip"; if format=="tar.gz" {contentType="application/gzip"}
 return UploadGrant{UploadID:id,UploadURL:s.options.UploadOrigin+"/uploads/"+id+"?token="+token,Method:"PUT",ExpiresAt:expires,Format:format,Limits:UploadLimits{maxArchiveBytes,maxExpandedBytes,maxFiles,100,5<<30},Headers:map[string]string{"Content-Type":contentType}},nil
}
func uploadError(w http.ResponseWriter,status int,code string) {w.Header().Set("Content-Type","application/json"); w.WriteHeader(status); _=json.NewEncoder(w).Encode(map[string]any{"error":map[string]string{"code":code,"message":code}})}

func (s *Service) UploadHandler() http.Handler {return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
 if r.Method!="PUT" {w.Header().Set("Allow","PUT"); uploadError(w,405,"invalid_request"); return}
 origins:=r.Header.Values("Origin"); if len(origins)>1 || len(origins)==1 && origins[0]!=s.options.UploadOrigin {uploadError(w,403,"invalid_request"); return}
 token:=r.URL.Query().Get("token")
 id:=strings.TrimPrefix(r.URL.Path,"/uploads/"); if !strings.HasPrefix(r.URL.Path,"/uploads/") || !validStorageID(id) {uploadError(w,404,"not_found"); return}
 hash:=sha256.Sum256([]byte(token))
 s.mu.Lock(); var owner int64; var key,format,state string; var expires int64; var stored []byte
 err:=s.db.QueryRowContext(r.Context(),"SELECT owner_id,key_id,format,state,expires_at,token_hash FROM uploads WHERE id=?",id).Scan(&owner,&key,&format,&state,&expires,&stored)
 if err!=nil && !errors.Is(err,sql.ErrNoRows) {s.mu.Unlock(); uploadError(w,503,"service_unavailable"); return}; if err!=nil || len(stored)!=len(hash) || subtle.ConstantTimeCompare(hash[:],stored)!=1 {s.mu.Unlock(); uploadError(w,404,"not_found"); return}
 identity:=Identity{OwnerID:owner,KeyID:key}
 if expires<=s.now().Unix() {s.mu.Unlock(); uploadError(w,410,"upload_expired"); return}; if state!="pending" {s.mu.Unlock(); uploadError(w,409,"upload_not_ready"); return}
 if !s.keyValid(r.Context(),identity) {s.mu.Unlock(); uploadError(w,401,"unauthorized"); return}
 contentType:="application/zip"; if format=="tar.gz" {contentType="application/gzip"}; if r.Header.Get("Content-Type")!=contentType {s.mu.Unlock(); uploadError(w,400,"invalid_request"); return}; if r.ContentLength>maxArchiveBytes {_,err=s.db.ExecContext(r.Context(),"UPDATE uploads SET state='failed',error_code='archive_too_large' WHERE id=? AND state='pending'",id); s.mu.Unlock(); if err!=nil {uploadError(w,503,"service_unavailable")} else {uploadError(w,413,"archive_too_large")}; return}
 if !s.freeSpace(maxArchiveBytes+maxExpandedBytes+storageReserve) {s.mu.Unlock(); uploadError(w,507,"storage_full"); return}
 _,err=s.db.ExecContext(r.Context(),"UPDATE uploads SET state='receiving' WHERE id=? AND state='pending'",id); s.mu.Unlock(); if err!=nil {uploadError(w,503,"service_unavailable"); return}
 archive:=filepath.Join(s.dataDir,"sites","staging",id+".archive"); dir:=filepath.Join(s.dataDir,"sites","staging",id)
 success:=false; failureState,failureCode:="failed","invalid_archive"; defer func(){if !success {os.Remove(archive); os.RemoveAll(dir); s.mu.Lock(); _,_=s.db.Exec("UPDATE uploads SET state=?,error_code=? WHERE id=? AND state='receiving'",failureState,failureCode,id); s.mu.Unlock()}}()
 _=http.NewResponseController(w).SetReadDeadline(time.Unix(expires,0)); f,err:=os.OpenFile(archive,os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600); if err!=nil {failureCode="service_unavailable"; uploadError(w,503,failureCode); return}; r.Body=http.MaxBytesReader(w,r.Body,maxArchiveBytes); _,err=io.Copy(f,r.Body); closeErr:=f.Close(); if err==nil {err=closeErr}; var tooLarge *http.MaxBytesError; if errors.As(err,&tooLarge) {failureCode="archive_too_large"; uploadError(w,413,failureCode); return}; if err!=nil {uploadError(w,400,"invalid_archive"); return}
 if err=os.Mkdir(dir,0700); err!=nil {failureCode="service_unavailable"; uploadError(w,503,failureCode); return}; size,files,err:=extractArchive(archive,format,dir); if err!=nil {uploadError(w,400,"invalid_archive"); return}; if err=os.Remove(archive); err!=nil {failureCode="service_unavailable"; uploadError(w,503,failureCode); return}
 s.mu.Lock(); defer s.mu.Unlock(); if s.now().Unix()>=expires {failureState,failureCode="expired","upload_expired"; uploadError(w,410,failureCode); return}; if !s.keyValid(r.Context(),identity) {failureCode="upload_interrupted"; uploadError(w,401,"unauthorized"); return}; result,err:=s.db.ExecContext(r.Context(),"UPDATE uploads SET state='ready',dir=?,size_bytes=?,file_count=? WHERE id=? AND state='receiving' AND EXISTS (SELECT 1 FROM sites WHERE sites.id=uploads.site_id AND sites.deleted_at IS NULL)",id,size,files,id); if err!=nil {failureCode="service_unavailable"; uploadError(w,503,failureCode); return}; affected,_:=result.RowsAffected(); if affected!=1 {uploadError(w,409,"upload_not_ready"); return}; success=true; w.Header().Set("Content-Type","application/json"); _=json.NewEncoder(w).Encode(map[string]any{"upload_id":id,"state":"ready","size_bytes":size,"file_count":files})
})}


package web

import (
 "errors"
 "net/http"
 "strings"

 "static-host/internal/publication"
)

func (s *Server) sites(w http.ResponseWriter,r *http.Request) {
 if r.URL.Path=="/api/sites" {
  if r.Method!="GET" {jsonError(w,405,"method_not_allowed"); return}; if _,ok:=s.require(w,r,false); !ok {return}
  sites,err:=s.Publications.List(r.Context(),1); if err!=nil {jsonError(w,503,"service_unavailable"); return}; jsonReply(w,200,map[string]any{"sites":sites}); return
 }
 id:=strings.TrimPrefix(r.URL.Path,"/api/sites/"); if id=="" || strings.Contains(id,"/") {jsonError(w,404,"site_not_found"); return}
 if r.Method!="DELETE" {jsonError(w,405,"method_not_allowed"); return}; if _,ok:=s.require(w,r,true); !ok {return}; if !emptyBody(w,r) {return}
 if err:=s.Publications.Delete(r.Context(),1,id); err!=nil {var e *publication.Error; if errors.As(err,&e) && e.Code=="site_not_found" {jsonError(w,404,e.Code)} else {jsonError(w,503,"service_unavailable")}; return}; w.WriteHeader(204)
}

package httpserver

import (
 "net"
 "net/http"
 "net/url"
 "strings"
)

func matches(host string, origin *url.URL) bool {
 if strings.ContainsAny(host,"/@, \t\r\n") || host=="" {return false}
 expected:=origin.Hostname(); name:=host; port:=""
 if strings.HasPrefix(host,"[") || strings.Count(host,":")>0 {var err error; name,port,err=net.SplitHostPort(host); if err!=nil{return false}}
 if !strings.EqualFold(name,expected) {return false}
 if origin.Scheme=="https" {return port==""||port=="443"}
 return port!="" && port==origin.Port()
}
func New(panelOrigin,mcpOrigin string,panel,mcp http.Handler) http.Handler {
 p,_:=url.Parse(panelOrigin); m,_:=url.Parse(mcpOrigin)
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/healthz" {w.Header().Set("Content-Type","text/plain; charset=utf-8"); if r.Method=="GET" {_,_=w.Write([]byte("ok\n"))} else if r.Method!="HEAD" {w.Header().Set("Allow","GET, HEAD"); w.WriteHeader(http.StatusMethodNotAllowed)}; return}
  mp:=matches(r.Host,m); pp:=matches(r.Host,p)
  if mp && r.URL.Path=="/mcp" {mcp.ServeHTTP(w,r);return}; if pp && r.URL.Path!="/mcp" {panel.ServeHTTP(w,r);return}; if mp||pp {http.NotFound(w,r);return}; http.Error(w,"Forbidden",http.StatusForbidden)
 })
}

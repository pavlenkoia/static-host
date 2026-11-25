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
func New(panelOrigin,mcpOrigin,uploadOrigin,siteSuffix string,panel,mcp,upload,sites http.Handler) http.Handler {
 p,_:=url.Parse(panelOrigin); m,_:=url.Parse(mcpOrigin); u,_:=url.Parse(uploadOrigin)
 suffix:=siteSuffix; local:=strings.Contains(siteSuffix,":"); if local {suffix,_,_=net.SplitHostPort(siteSuffix)}
 return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  host:=r.Host; if strings.ContainsAny(host,"/@, \t\r\n") {http.Error(w,"Forbidden",403); return}
  if h,_,err:=net.SplitHostPort(host); err==nil {host=h}
  name:=strings.ToLower(host); tail:="."+strings.ToLower(suffix)
  if strings.HasSuffix(name,tail) {label:=strings.TrimSuffix(name,tail); valid:=len(label)>0 && len(label)<=63 && label[0]!='-' && label[len(label)-1]!='-'; if valid {for _,c:=range label {if !(c>='0'&&c<='9'||c>='a'&&c<='z'||c=='-') {valid=false; break}}}; if valid {scheme:="https://"; if local {scheme="http://"}; expected,_:=url.Parse(scheme+label+"."+siteSuffix); if matches(r.Host,expected) {sites.ServeHTTP(w,r); return}}}
  if r.URL.Path=="/healthz" {w.Header().Set("Content-Type","text/plain; charset=utf-8"); if r.Method=="GET" {_,_=w.Write([]byte("ok\n"))} else if r.Method!="HEAD" {w.Header().Set("Allow","GET, HEAD"); w.WriteHeader(http.StatusMethodNotAllowed)}; return}
  mp:=matches(r.Host,m); pp:=matches(r.Host,p); up:=matches(r.Host,u)
  if up && strings.HasPrefix(r.URL.Path,"/uploads/") {upload.ServeHTTP(w,r); return}
  if mp && r.URL.Path=="/mcp" {mcp.ServeHTTP(w,r);return}; if pp && r.URL.Path!="/mcp" && !strings.HasPrefix(r.URL.Path,"/uploads/") {panel.ServeHTTP(w,r);return}; if mp||pp||up {http.NotFound(w,r);return}; http.Error(w,"Forbidden",http.StatusForbidden)
 })
}

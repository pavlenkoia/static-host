package config

import (
 "errors"
 "flag"
 "fmt"
 "net"
 "strconv"
 "strings"
)

type Config struct { Listen, DataDir, BaseDomain, PanelOrigin, MCPOrigin, AdminPassword string }
func value(env func(string)(string,bool), key, fallback string) string { if v,ok:=env(key); ok { return v }; return fallback }
func Parse(args []string, env func(string)(string,bool)) (Config,error) {
 var c Config
 f:=flag.NewFlagSet("static-host",flag.ContinueOnError)
 f.StringVar(&c.Listen,"listen",value(env,"STATIC_HOST_LISTEN","127.0.0.1:8081"),"HTTP listen address")
 f.StringVar(&c.DataDir,"data-dir",value(env,"STATIC_HOST_DATA_DIR","./data"),"data directory")
 f.StringVar(&c.BaseDomain,"base-domain",value(env,"STATIC_HOST_BASE_DOMAIN",""),"public base domain")
 if err:=f.Parse(args); err!=nil{return c,err}; if f.NArg()!=0{return c,errors.New("unexpected arguments")}; if c.DataDir=="" {return c,errors.New("empty data directory")}
 c.AdminPassword, _ = env("STATIC_HOST_ADMIN_PASSWORD")
 if len(c.AdminPassword) < 12 || len(c.AdminPassword) > 72 { return c, errors.New("STATIC_HOST_ADMIN_PASSWORD must be 12–72 bytes") }
 host,port,err:=net.SplitHostPort(c.Listen); if err!=nil{return c,fmt.Errorf("invalid listen: %w",err)}
 ip:=net.ParseIP(host); if ip==nil {return c,errors.New("listen host must be a numeric IP")}; if port=="" {return c,errors.New("invalid port")}; for _,ch:=range port {if ch<'0'||ch>'9'{return c,errors.New("invalid port")}}; p,err:=strconv.Atoi(port); if err!=nil||p<1||p>65535{return c,errors.New("invalid port")}
 for _, ch := range c.BaseDomain { if ch > 127 { return c, errors.New("invalid base domain") } }
 c.BaseDomain=strings.ToLower(c.BaseDomain)
 if c.BaseDomain=="" {if !ip.IsLoopback(){return c,errors.New("local mode requires loopback IP")}; c.PanelOrigin="http://"+c.Listen; c.MCPOrigin=c.PanelOrigin; return c,nil}
 if len(c.BaseDomain)+len("panel.")>253 || net.ParseIP(c.BaseDomain)!=nil {return c,errors.New("invalid base domain")}
 labels:=strings.Split(c.BaseDomain,"."); if len(labels)<2{return c,errors.New("invalid base domain")}; for _,label:=range labels {if len(label)<1||len(label)>63||label[0]=='-'||label[len(label)-1]=='-' {return c,errors.New("invalid base domain")}; for _,ch:=range label {if !(ch>='a'&&ch<='z'||ch>='0'&&ch<='9'||ch=='-'){return c,errors.New("invalid base domain")}}}
 c.PanelOrigin="https://panel."+c.BaseDomain; c.MCPOrigin="https://mcp."+c.BaseDomain; return c,nil
}

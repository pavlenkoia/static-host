package config
import ("strings";"testing")
func TestParse(t *testing.T) {
 env:=func(k string)(string,bool){v,ok:=map[string]string{"STATIC_HOST_LISTEN":"invalid","STATIC_HOST_BASE_DOMAIN":"EXAMPLE.TEST","STATIC_HOST_DATA_DIR":"env","STATIC_HOST_ADMIN_PASSWORD":"valid-admin-password"}[k];return v,ok}
 c,e:=Parse([]string{"--listen=127.0.0.1:1234"},env); if e!=nil||c.PanelOrigin!="https://panel.example.test"||c.MCPOrigin!="https://mcp.example.test"||c.DataDir!="env"{t.Fatalf("%+v %v",c,e)}
 c,e=Parse([]string{"--listen=[::1]:42","--base-domain="},env); if e!=nil||c.PanelOrigin!="http://[::1]:42"{t.Fatalf("%+v %v",c,e)}
 empty:=func(string)(string,bool){return "",false}; for _,args:=range [][]string{{"--listen=localhost:80"},{"--listen=0.0.0.0:80"},{"--listen=127.0.0.1:0"},{"--data-dir="},{"--base-domain=bad_domain.test"},{"--base-domain=127.0.0.1"},{"--base-domain=example.test."}} {if _,e:=Parse(args,func(k string)(string,bool){if k=="STATIC_HOST_ADMIN_PASSWORD"{return "valid-admin-password",true};return empty(k)});e==nil {t.Errorf("accepted %v",args)}}
 for _,password:=range []string{"", "short", string(make([]byte,73))} {_,err:=Parse(nil,func(k string)(string,bool){if k=="STATIC_HOST_ADMIN_PASSWORD"{return password,true};return "",false});if err==nil||!strings.Contains(err.Error(),"STATIC_HOST_ADMIN_PASSWORD"){t.Errorf("accepted invalid admin password length %d",len(password))}}
 if _,err:=Parse(nil,empty);err==nil||!strings.Contains(err.Error(),"STATIC_HOST_ADMIN_PASSWORD"){t.Fatal("missing admin password accepted")}
}

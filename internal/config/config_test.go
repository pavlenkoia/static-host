package config
import "testing"
func TestParse(t *testing.T) {
 env:=func(k string)(string,bool){v,ok:=map[string]string{"STATIC_HOST_LISTEN":"invalid","STATIC_HOST_BASE_DOMAIN":"EXAMPLE.TEST","STATIC_HOST_DATA_DIR":"env"}[k];return v,ok}
 c,e:=Parse([]string{"--listen=127.0.0.1:1234"},env); if e!=nil||c.PanelOrigin!="https://panel.example.test"||c.MCPOrigin!="https://mcp.example.test"||c.DataDir!="env"{t.Fatalf("%+v %v",c,e)}
 c,e=Parse([]string{"--listen=[::1]:42","--base-domain="},env); if e!=nil||c.PanelOrigin!="http://[::1]:42"{t.Fatalf("%+v %v",c,e)}
 empty:=func(string)(string,bool){return "",false}; for _,args:=range [][]string{{"--listen=localhost:80"},{"--listen=0.0.0.0:80"},{"--listen=127.0.0.1:0"},{"--data-dir="},{"--base-domain=bad_domain.test"},{"--base-domain=127.0.0.1"},{"--base-domain=example.test."}} {if _,e:=Parse(args,empty);e==nil {t.Errorf("accepted %v",args)}}
 _,dir,_,e:=ParseOwner([]string{"reset-password"},env); if e!=nil||dir!="env"{t.Fatalf("%s %v",dir,e)}
}

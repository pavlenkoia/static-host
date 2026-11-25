package auth

import (
 "strings"
 "testing"
 "static-host/internal/store"
)
func TestOwnerReset(t *testing.T) {
 db,err:=store.Open(t.TempDir()); if err!=nil {t.Fatal(err)}; defer db.Close()
 if err=Init(db," admin ","first-password"); err!=nil {t.Fatal(err)}
 if err=Init(db,"admin","other-password"); err==nil {t.Fatal("duplicate owner accepted")}
 _,err=db.Exec("INSERT INTO sessions VALUES(x'01',1,'csrf',1,9999999999); INSERT INTO agent_keys(id,owner_id,name,token_hash,created_at,expires_at,revoked_at) VALUES('id',1,'test',x'02',1,9999999999,NULL)"); if err!=nil {t.Fatal(err)}
 if err=Reset(db,"second-password"); err!=nil {t.Fatal(err)}
 var hash string; if err=db.QueryRow("SELECT password_hash FROM users WHERE login='admin'").Scan(&hash); err!=nil {t.Fatal(err)}
 if Compare(hash,"first-password") || !Compare(hash,"second-password") {t.Fatal("password was not reset")}
 var sessions,keys int; if err=db.QueryRow("SELECT (SELECT count(*) FROM sessions),(SELECT count(*) FROM agent_keys)").Scan(&sessions,&keys); err!=nil || sessions!=0 || keys!=1 {t.Fatalf("sessions=%d keys=%d: %v",sessions,keys,err)}
 for _,s:=range []string{"\n",strings.Repeat("a",73)+"\n","long-password\nextra\n"} { if _,err=ReadPassword(strings.NewReader(s)); err==nil {t.Fatalf("accepted invalid password input") } }
 if _,err=Login("invalid login"); err==nil {t.Fatal("accepted invalid login")}
}

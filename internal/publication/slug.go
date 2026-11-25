package publication

import (
 "context"
 "database/sql"
 "strconv"
 "strings"
)

// Slugs are permanent, including after deletion. Reserve s- for legacy ID hosts.
func slugBase(name string) string {
 var b strings.Builder
 separator:=false
 for _,r:=range strings.ToLower(name) {
  part:=""
  if r>='a' && r<='z' || r>='0' && r<='9' {part=string(r)} else if r>='а' && r<='я' {part=[]string{"a","b","v","g","d","e","zh","z","i","i","k","l","m","n","o","p","r","s","t","u","f","kh","ts","ch","sh","shch","","y","","e","yu","ya"}[r-'а']} else if r=='ё' {part="yo"} else {separator=true; continue}
  if part=="" {continue}
  if separator && b.Len()>0 {b.WriteByte('-')}; separator=false; b.WriteString(part)
 }
 base:=strings.TrimRight(b.String(),"-")
 if base=="" {base="site"}
 if strings.HasPrefix(base,"s-") {base="site-"+base}
 // Leave room for a collision suffix without exceeding a DNS label.
 if len(base)>50 {base=strings.TrimRight(base[:50],"-")}
 return base
}

type slugQuerier interface {QueryRowContext(context.Context,string,...any) *sql.Row}
func availableSlug(ctx context.Context,q slugQuerier,name string) (string,error) {
 base:=slugBase(name)
 for n:=1;;n++ {
  candidate:=base; if n>1 {candidate+="-"+strconv.Itoa(n)}
  var exists bool
  if err:=q.QueryRowContext(ctx,"SELECT EXISTS(SELECT 1 FROM sites WHERE slug=? OR id=?)",candidate,candidate).Scan(&exists); err!=nil {return "",err}
  if !exists {return candidate,nil}
 }
}

func (s *Service) assignSiteSlugs() error {
 ctx:=context.Background(); tx,err:=s.db.BeginTx(ctx,nil); if err!=nil {return err}; defer tx.Rollback()
 rows,err:=tx.QueryContext(ctx,"SELECT id,name FROM sites WHERE slug IS NULL ORDER BY created_at,id"); if err!=nil {return err}
 type pendingSite struct {id,name string}; var pending []pendingSite
 for rows.Next() {var site pendingSite; if err=rows.Scan(&site.id,&site.name); err!=nil {rows.Close(); return err}; pending=append(pending,site)}
 err=rows.Err(); rows.Close(); if err!=nil {return err}
 for _,site:=range pending {
  slug,err:=availableSlug(ctx,tx,site.name); if err!=nil {return err}
  if _,err=tx.ExecContext(ctx,"UPDATE sites SET slug=? WHERE id=?",slug,site.id); err!=nil {return err}
 }
 return tx.Commit()
}
